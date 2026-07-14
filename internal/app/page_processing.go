package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lieyan/firescribe/internal/pageproc"
	"github.com/lieyan/firescribe/internal/storage"
)

var ErrPageProcessingActive = errors.New("page processing is already running for this document")

type PageProcessingOptions struct {
	PageIDs []string               `json:"page_ids"`
	Config  pageproc.EnhanceConfig `json:"config"`
}

type PageProcessingStart struct {
	Run PageProcessingRun `json:"run"`
	Job Job               `json:"job"`
}

type pageProcessingJobPayload struct {
	RunID      string `json:"run_id"`
	DocumentID string `json:"document_id"`
}

func (a *App) StartPageProcessing(ctx context.Context, documentID string, options PageProcessingOptions) (PageProcessingStart, error) {
	if _, err := a.Store.GetDocument(ctx, documentID); err != nil {
		return PageProcessingStart{}, err
	}
	if _, active, err := a.Store.ActivePageProcessingRun(ctx, documentID); err != nil {
		return PageProcessingStart{}, err
	} else if active {
		return PageProcessingStart{}, ErrPageProcessingActive
	}
	pages, err := a.Store.ListPages(ctx, documentID)
	if err != nil {
		return PageProcessingStart{}, err
	}
	if len(pages) == 0 {
		return PageProcessingStart{}, errors.New("document has no pages to process")
	}
	if len(options.PageIDs) > 0 {
		wanted := make(map[string]bool, len(options.PageIDs))
		for _, id := range options.PageIDs {
			wanted[id] = true
		}
		selected := make([]Page, 0, len(wanted))
		for _, page := range pages {
			if wanted[page.ID] {
				selected = append(selected, page)
				delete(wanted, page.ID)
			}
		}
		if len(wanted) > 0 {
			return PageProcessingStart{}, fmt.Errorf("unknown page ids for document: %d not found", len(wanted))
		}
		pages = selected
	}
	config := pageproc.NormalizeEnhanceConfig(options.Config)
	configJSON := mustJSON(config)
	timestamp := now()
	runID := newID("proc")
	job := Job{
		ID: newID("job"), Type: "process_pages", Status: "queued",
		TargetType: "page_processing_run", TargetID: runID,
		PayloadJSON: mustJSON(pageProcessingJobPayload{RunID: runID, DocumentID: documentID}),
		MaxAttempts: 3, ProgressTotal: len(pages), ProgressMessage: "等待页图处理", CreatedAt: timestamp,
	}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		return PageProcessingStart{}, err
	}
	run := PageProcessingRun{
		ID: runID, DocumentID: documentID, JobID: job.ID, ConfigJSON: configJSON,
		Status: "queued", TotalPages: len(pages), CreatedAt: timestamp,
	}
	results := make([]PageProcessingResult, 0, len(pages))
	for _, page := range pages {
		if page.ImageAssetID == "" {
			_ = a.Store.MarkJobFailed(context.Background(), job.ID, fmt.Errorf("page %d has no image asset", page.PageNo))
			return PageProcessingStart{}, fmt.Errorf("page %d has no image asset", page.PageNo)
		}
		results = append(results, PageProcessingResult{
			ID: newID("ppr"), RunID: run.ID, PageID: page.ID, PageNo: page.PageNo,
			SourceAssetID: page.ImageAssetID, Status: "queued", ConfigJSON: configJSON,
			MetadataJSON: "{}", CreatedAt: timestamp,
		})
	}
	if err := a.Store.CreatePageProcessingRun(ctx, run, results); err != nil {
		_ = a.Store.MarkJobFailed(context.Background(), job.ID, err)
		return PageProcessingStart{}, err
	}
	a.launchJob(job.ID)
	return PageProcessingStart{Run: run, Job: job}, nil
}

func (a *App) runPageProcessingJob(ctx context.Context, job Job) (PageProcessingRun, error) {
	var payload pageProcessingJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return PageProcessingRun{}, fmt.Errorf("decode page-processing payload: %w", err)
	}
	run, err := a.Store.GetPageProcessingRun(context.Background(), payload.RunID)
	if err != nil {
		return PageProcessingRun{}, err
	}
	if err := a.Store.MarkPageProcessingRunRunning(context.Background(), run.ID); err != nil {
		return PageProcessingRun{}, err
	}
	var config pageproc.EnhanceConfig
	if err := json.Unmarshal([]byte(run.ConfigJSON), &config); err != nil {
		return PageProcessingRun{}, fmt.Errorf("decode processing config: %w", err)
	}
	results, err := a.Store.ListPageProcessingResults(context.Background(), run.ID)
	if err != nil {
		return PageProcessingRun{}, err
	}
	done, failed := 0, 0
	failures := []string{}
	for _, result := range results {
		if result.Status == "succeeded" {
			done++
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = a.Store.CancelPendingPageProcessingResults(context.Background(), run.ID, cancelCause(ctx))
			_ = a.Store.FinishPageProcessingRun(context.Background(), run.ID, "canceled", cancelCause(ctx), done, failed)
			if canceledRun, loadErr := a.Store.GetPageProcessingRun(context.Background(), run.ID); loadErr == nil {
				_ = a.Store.UpdateJobResult(context.Background(), job.ID, mustJSON(canceledRun))
			}
			return PageProcessingRun{}, err
		}
		_ = a.Store.MarkPageProcessingResultRunning(context.Background(), result.ID)
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, done+failed, len(results), fmt.Sprintf("正在处理第 %d 页", result.PageNo))
		if err := a.processOnePage(ctx, run.DocumentID, result, config); err != nil {
			if ctx.Err() != nil {
				reason := cancelCause(ctx)
				_ = a.Store.CancelPendingPageProcessingResults(context.Background(), run.ID, reason)
				_ = a.Store.FinishPageProcessingRun(context.Background(), run.ID, "canceled", reason, done, failed)
				return PageProcessingRun{}, ctx.Err()
			}
			failed++
			failures = append(failures, fmt.Sprintf("第 %d 页: %s", result.PageNo, err))
			_ = a.Store.FinishPageProcessingResult(context.Background(), result.ID, "", "{}", nil, err)
		} else {
			done++
		}
		_ = a.Store.UpdatePageProcessingRunProgress(context.Background(), run.ID, done, failed)
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, done+failed, len(results), fmt.Sprintf("已处理第 %d 页", result.PageNo))
	}
	status, lastError := "succeeded", ""
	if failed > 0 && done > 0 {
		status = "partial"
		lastError = summarizeErrors(failures)
	} else if failed > 0 {
		status = "failed"
		lastError = summarizeErrors(failures)
	}
	if err := a.Store.FinishPageProcessingRun(context.Background(), run.ID, status, lastError, done, failed); err != nil {
		return PageProcessingRun{}, err
	}
	finished, err := a.Store.GetPageProcessingRun(context.Background(), run.ID)
	if err != nil {
		return PageProcessingRun{}, err
	}
	if failed > 0 {
		_ = a.Store.UpdateJobResult(context.Background(), job.ID, mustJSON(finished))
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, done+failed, len(results), fmt.Sprintf("完成 %d 页，失败 %d 页", done, failed))
		return finished, errors.New(lastError)
	}
	return finished, nil
}

func (a *App) processOnePage(ctx context.Context, documentID string, result PageProcessingResult, config pageproc.EnhanceConfig) error {
	source, err := a.Store.GetAsset(ctx, result.SourceAssetID)
	if err != nil {
		return fmt.Errorf("load original page asset: %w", err)
	}
	relative := storage.EnhancedPageRel(documentID, result.PageNo, result.ID)
	abs := a.Storage.Abs(relative)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	metadata, err := pageproc.EnhanceFile(ctx, a.Storage.Abs(source.StoragePath), abs, config)
	if err != nil {
		_ = os.Remove(abs)
		return err
	}
	stored, err := a.Storage.Inspect(relative, fmt.Sprintf("page-%04d-enhanced.png", result.PageNo))
	if err != nil {
		_ = os.Remove(abs)
		return err
	}
	asset, err := a.Store.UpsertAsset(ctx, "enhanced_page", stored)
	if err != nil {
		return err
	}
	if err := a.Store.LinkDocumentAsset(ctx, documentID, asset.ID, "enhanced_page"); err != nil {
		return err
	}
	segments := make([]PageSegment, 0, len(metadata.Segments))
	for _, segment := range metadata.Segments {
		segments = append(segments, PageSegment{
			ID: newID("seg"), PageID: result.PageID, ProcessingResultID: result.ID,
			Kind: segment.Kind, Position: segment.Position, X: segment.X, Y: segment.Y,
			Width: segment.Width, Height: segment.Height, MetadataJSON: "{}", CreatedAt: now(),
		})
	}
	return a.Store.FinishPageProcessingResult(context.Background(), result.ID, asset.ID, metadata.JSON(), segments, nil)
}

func (a *App) PageProcessingPreview(ctx context.Context, pageID string) (PageProcessingPreview, error) {
	page, err := a.Store.GetPage(ctx, pageID)
	if err != nil {
		return PageProcessingPreview{}, err
	}
	preview := PageProcessingPreview{
		PageID: page.ID, OriginalAssetID: page.ImageAssetID,
		OriginalURL: "/api/pages/" + page.ID + "/image", Segments: []PageSegment{},
	}
	result, err := a.Store.LatestEnhancedResult(ctx, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return preview, nil
	}
	if err != nil {
		return PageProcessingPreview{}, err
	}
	result.OriginalURL = preview.OriginalURL
	result.EnhancedURL = "/api/assets/" + result.OutputAssetID + "/download"
	preview.Result = &result
	preview.Segments, err = a.Store.ListPageSegments(ctx, result.ID)
	return preview, err
}

func normalizeImageSource(source string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "original":
		return "original", nil
	case "enhanced":
		return "enhanced", nil
	default:
		return "", fmt.Errorf("unsupported image source %q (want original or enhanced)", source)
	}
}
