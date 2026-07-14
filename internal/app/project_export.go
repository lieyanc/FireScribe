package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lieyan/firescribe/internal/exporter"
	"github.com/lieyan/firescribe/internal/storage"
)

type projectExportJobPayload struct {
	ExportID           string `json:"export_id"`
	Format             string `json:"format"`
	IncludePageNumbers bool   `json:"include_page_numbers"`
	TextScope          string `json:"text_scope"`
	IncludeAnnotations bool   `json:"include_annotations"`
	IncludeUncertain   bool   `json:"include_uncertain"`
}

func (a *App) CreateProject(ctx context.Context, name, description string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	timestamp := now()
	project := Project{ID: newID("prj"), Name: name, Description: description, CreatedAt: timestamp, UpdatedAt: timestamp}
	if err := a.Store.CreateProject(ctx, project); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (a *App) DeleteProject(ctx context.Context, projectID string) error {
	if _, err := a.Store.GetProject(ctx, projectID); err != nil {
		return err
	}
	jobs, err := a.Store.ListActiveJobsForTarget(ctx, "project", projectID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := a.CancelJob(ctx, job.ID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not active") {
			return err
		}
	}
	return a.Store.DeleteProject(ctx, projectID)
}

func (a *App) StartProjectExport(ctx context.Context, projectID, format string, includePageNumbers bool) (ProjectExportStart, error) {
	return a.StartProjectExportWithOptions(ctx, projectID, ExportOptions{
		Format: format, IncludePageNumbers: includePageNumbers, TextScope: "current",
	})
}

func (a *App) StartProjectExportWithOptions(ctx context.Context, projectID string, options ExportOptions) (ProjectExportStart, error) {
	project, err := a.Store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectExportStart{}, err
	}
	if project.DocumentCount == 0 {
		return ProjectExportStart{}, errors.New("project has no documents to export")
	}
	options, err = normalizeExportOptions(options)
	if err != nil {
		return ProjectExportStart{}, err
	}
	timestamp := now()
	exportID := newID("pex")
	job := Job{
		ID: newID("job"), Type: "export_project", Status: "queued", TargetType: "project", TargetID: projectID,
		PayloadJSON: mustJSON(projectExportJobPayload{
			ExportID: exportID, Format: options.Format, IncludePageNumbers: options.IncludePageNumbers,
			TextScope: options.TextScope, IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
		}),
		MaxAttempts: 3, ProgressTotal: project.PageCount, ProgressMessage: "等待合并导出", CreatedAt: timestamp,
	}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		return ProjectExportStart{}, err
	}
	export := ProjectExport{
		ID: exportID, ProjectID: projectID, JobID: job.ID, Format: options.Format,
		IncludePageNumbers: options.IncludePageNumbers, TextScope: options.TextScope,
		IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
		Status: "queued", CreatedAt: timestamp,
	}
	if err := a.Store.CreateProjectExport(ctx, export); err != nil {
		_ = a.Store.MarkJobFailed(context.Background(), job.ID, err)
		return ProjectExportStart{}, err
	}
	a.launchJob(job.ID)
	return ProjectExportStart{ProjectExport: export, Job: job}, nil
}

func (a *App) runProjectExportJob(ctx context.Context, job Job) (ProjectExport, error) {
	var payload projectExportJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return ProjectExport{}, fmt.Errorf("decode project export payload: %w", err)
	}
	if err := a.Store.MarkProjectExportRunning(context.Background(), payload.ExportID); err != nil {
		return ProjectExport{}, err
	}
	exported, err := a.renderProjectExport(ctx, job.TargetID, payload.ExportID, ExportOptions{
		Format: payload.Format, IncludePageNumbers: payload.IncludePageNumbers, TextScope: payload.TextScope,
		IncludeAnnotations: payload.IncludeAnnotations, IncludeUncertain: payload.IncludeUncertain,
	}, func(current, total int, message string) {
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, current, total, message)
	})
	if err != nil {
		if ctx.Err() != nil {
			_ = a.Store.CancelProjectExport(context.Background(), payload.ExportID)
			return ProjectExport{}, err
		}
		_ = a.Store.FinishProjectExport(context.Background(), payload.ExportID, "", err)
		return ProjectExport{}, err
	}
	if err := a.Store.FinishProjectExport(context.Background(), payload.ExportID, exported.AssetID, nil); err != nil {
		return ProjectExport{}, err
	}
	return a.Store.GetProjectExport(context.Background(), payload.ExportID)
}

func (a *App) renderProjectExport(ctx context.Context, projectID, exportID string, options ExportOptions, progress func(int, int, string)) (ProjectExport, error) {
	detail, err := a.Store.GetProjectDetail(ctx, projectID)
	if err != nil {
		return ProjectExport{}, err
	}
	options, err = normalizeExportOptions(options)
	if err != nil {
		return ProjectExport{}, err
	}
	projectPages := make([]exporter.PageText, 0, detail.PageCount)
	snapshots := make([]ExportPageSnapshot, 0, detail.PageCount)
	processed := 0
	for documentPosition, document := range detail.Documents {
		pages, err := a.Store.ListPages(ctx, document.ID)
		if err != nil {
			return ProjectExport{}, err
		}
		annotations, err := a.Store.ListAnnotations(ctx, document.ID, "")
		if err != nil {
			return ProjectExport{}, err
		}
		annotationsByPage := make(map[string][]Annotation)
		for _, annotation := range annotations {
			annotationsByPage[annotation.PageID] = append(annotationsByPage[annotation.PageID], annotation)
		}
		firstIncludedPage := true
		for _, page := range pages {
			if err := ctx.Err(); err != nil {
				return ProjectExport{}, err
			}
			var version TextVersion
			var found bool
			if options.TextScope == "final" {
				version, found, err = a.Store.LatestFinalTextForPage(ctx, page.ID)
			} else {
				version, found, err = a.Store.EffectiveTextForPage(ctx, page.ID)
			}
			if err != nil {
				return ProjectExport{}, err
			}
			processed++
			if !found && options.TextScope == "final" {
				if progress != nil {
					progress(processed, detail.PageCount, fmt.Sprintf("《%s》第 %d 页尚未定稿，已跳过", document.Title, page.PageNo))
				}
				continue
			}
			text := version.Text
			headingOffset := 0
			if firstIncludedPage {
				heading := projectDocumentHeading(options.Format, document.Title)
				headingOffset = len([]rune(heading))
				text = heading + text
				firstIncludedPage = false
			}
			exportPage := exporter.PageText{PageNo: page.PageNo, Text: text, VersionID: version.ID, VersionKind: version.Kind}
			for _, annotation := range annotationsByPage[page.ID] {
				anchor := exporter.ParseAnnotationAnchor(annotation.AnchorJSON)
				start, end := exporter.ResolveTextRange(version.Text, anchor.Start, anchor.End, anchor.Text)
				if start >= 0 {
					start += headingOffset
					end += headingOffset
				}
				exportPage.Annotations = append(exportPage.Annotations, exporter.Annotation{
					Kind: annotation.Kind, Status: annotation.Status, Body: annotation.Body,
					AnchorType: anchor.Type, Start: start, End: end, AnchorText: anchor.Text,
					X: anchor.X, Y: anchor.Y, Width: anchor.Width, Height: anchor.Height,
				})
			}
			if options.Format == "pdf" && page.ImageAssetID != "" {
				if asset, assetErr := a.Store.GetAsset(ctx, page.ImageAssetID); assetErr == nil {
					exportPage.Image, _ = os.ReadFile(a.Storage.Abs(asset.StoragePath))
				}
			}
			projectPages = append(projectPages, exportPage)
			snapshots = append(snapshots, ExportPageSnapshot{
				Ordinal: len(snapshots), DocumentID: document.ID, DocumentTitle: document.Title,
				DocumentPosition: documentPosition, PageID: page.ID, PageNo: page.PageNo,
				TextVersionID: version.ID, TextVersionKind: version.Kind,
				Annotations: includedExportAnnotations(annotationsByPage[page.ID], version.Text, options), CreatedAt: now(),
			})
			if progress != nil {
				progress(processed, detail.PageCount, fmt.Sprintf("正在整理《%s》第 %d 页", document.Title, page.PageNo))
			}
		}
	}
	if options.TextScope == "final" && len(projectPages) == 0 {
		return ProjectExport{}, errors.New("project has no finalized text to export")
	}
	content, err := exporter.RenderWithOptions(detail.Name, projectPages, exporter.Options{
		Format: options.Format, IncludePageNumbers: options.IncludePageNumbers,
		IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
	})
	if err != nil {
		return ProjectExport{}, err
	}
	rel := storage.ExportRel(exportID, options.Format)
	abs := a.Storage.Abs(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return ProjectExport{}, err
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return ProjectExport{}, err
	}
	stored, err := a.Storage.Inspect(rel, filepath.Base(rel))
	if err != nil {
		return ProjectExport{}, err
	}
	asset, err := a.Store.UpsertAsset(ctx, "export", stored)
	if err != nil {
		return ProjectExport{}, err
	}
	if err := a.Store.ReplaceProjectExportPageSnapshots(ctx, exportID, snapshots); err != nil {
		return ProjectExport{}, fmt.Errorf("store project export provenance: %w", err)
	}
	return ProjectExport{
		ID: exportID, ProjectID: projectID, AssetID: asset.ID, Format: options.Format,
		IncludePageNumbers: options.IncludePageNumbers, TextScope: options.TextScope,
		IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
		Status:      "succeeded",
		DownloadURL: "/api/project-exports/" + exportID + "/download", StoragePath: asset.StoragePath,
	}, nil
}

func projectDocumentHeading(format, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if format == "md" {
		return "**《" + title + "》**\n\n"
	}
	return "《" + title + "》\n\n"
}
