package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/lieyan/firescribe/internal/exporter"
	"github.com/lieyan/firescribe/internal/pageproc"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

type App struct {
	Store      *Store
	Storage    *storage.Storage
	Recognizer recognizer.Recognizer
}

type ImportOptions struct {
	Title       string
	Description string
	Author      string
	Source      string
}

type RecognitionStart struct {
	Run RecognitionRun `json:"run"`
	Job Job            `json:"job"`
}

func New(store *Store, storage *storage.Storage, rec recognizer.Recognizer) *App {
	return &App{Store: store, Storage: storage, Recognizer: rec}
}

func (a *App) ImportDocument(ctx context.Context, originalName string, reader io.Reader, opts ImportOptions) (Document, error) {
	storedOriginal, err := a.Storage.StoreOriginal(originalName, reader)
	if err != nil {
		return Document{}, err
	}

	timestamp := now()
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	}
	if title == "" {
		title = "未命名文档"
	}
	doc := Document{
		ID:          newID("doc"),
		Title:       title,
		Description: opts.Description,
		Author:      opts.Author,
		Source:      opts.Source,
		Status:      "importing",
		CreatedAt:   timestamp,
		UpdatedAt:   timestamp,
	}
	if err := a.Store.CreateDocument(ctx, doc); err != nil {
		return Document{}, err
	}

	originalAsset, err := a.Store.UpsertAsset(ctx, "original", storedOriginal)
	if err != nil {
		_ = a.Store.UpdateDocumentStatus(ctx, doc.ID, "failed")
		return Document{}, err
	}
	if err := a.Store.LinkDocumentAsset(ctx, doc.ID, originalAsset.ID, "original"); err != nil {
		_ = a.Store.UpdateDocumentStatus(ctx, doc.ID, "failed")
		return Document{}, err
	}

	pageCount, err := a.createPages(ctx, doc.ID, storedOriginal)
	if err != nil {
		_ = a.Store.UpdateDocumentStatus(ctx, doc.ID, "failed")
		return Document{}, err
	}
	if err := a.Store.UpdateDocumentReady(ctx, doc.ID, pageCount); err != nil {
		return Document{}, err
	}
	doc.PageCount = pageCount
	doc.Status = "ready"
	doc.UpdatedAt = now()
	return doc, nil
}

func (a *App) StartRecognition(ctx context.Context, documentID string) (RecognitionStart, error) {
	if _, err := a.Store.GetDocument(ctx, documentID); err != nil {
		return RecognitionStart{}, err
	}
	timestamp := now()
	run := RecognitionRun{
		ID:            newID("run"),
		DocumentID:    documentID,
		Provider:      a.Recognizer.Provider(),
		Model:         a.Recognizer.Model(),
		PromptVersion: a.Recognizer.PromptVersion(),
		ConfigJSON:    a.Recognizer.ConfigJSON(),
		Status:        "queued",
		CreatedAt:     timestamp,
	}
	if err := a.Store.CreateRecognitionRun(ctx, run); err != nil {
		return RecognitionStart{}, err
	}
	job := Job{
		ID:          newID("job"),
		Type:        "recognize_document",
		Status:      "queued",
		TargetType:  "recognition_run",
		TargetID:    run.ID,
		PayloadJSON: mustJSON(map[string]string{"document_id": documentID, "run_id": run.ID}),
		MaxAttempts: 1,
		CreatedAt:   timestamp,
	}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		return RecognitionStart{}, err
	}
	go a.RecognizeDocument(context.Background(), run.ID, job.ID)
	return RecognitionStart{Run: run, Job: job}, nil
}

func (a *App) RecognizeDocument(ctx context.Context, runID, jobID string) {
	err := a.recognizeDocument(ctx, runID, jobID)
	if err != nil && jobID != "" {
		_ = a.Store.MarkJobFailed(context.Background(), jobID, err)
	}
}

func (a *App) SaveTextVersion(ctx context.Context, version TextVersion) (TextVersion, error) {
	if version.ID == "" {
		version.ID = newID("txt")
	}
	if version.Status == "" {
		version.Status = "draft"
	}
	if version.Kind == "" {
		version.Kind = "manual"
	}
	if version.CreatedBy == "" {
		version.CreatedBy = "user"
	}
	version.CreatedAt = now()
	if version.DocumentID == "" && version.PageID != "" {
		page, err := a.Store.GetPage(ctx, version.PageID)
		if err != nil {
			return TextVersion{}, err
		}
		version.DocumentID = page.DocumentID
	}
	if err := a.Store.CreateTextVersion(ctx, version); err != nil {
		return TextVersion{}, err
	}
	if version.Kind == "final" || version.Status == "verified" {
		_ = a.Store.UpdatePageStatus(ctx, version.PageID, "verified")
		_ = a.maybeFinalizeDocument(ctx, version.DocumentID)
	} else if version.Kind == "manual" {
		_ = a.Store.UpdatePageStatus(ctx, version.PageID, "reviewing")
	}
	return version, nil
}

func (a *App) CreateAnnotation(ctx context.Context, annotation Annotation) (Annotation, error) {
	if annotation.ID == "" {
		annotation.ID = newID("ann")
	}
	if annotation.Status == "" {
		annotation.Status = "open"
	}
	if annotation.Kind == "" {
		annotation.Kind = "page_note"
	}
	if strings.TrimSpace(annotation.AnchorJSON) == "" {
		annotation.AnchorJSON = "{}"
	}
	if annotation.DocumentID == "" && annotation.PageID != "" {
		page, err := a.Store.GetPage(ctx, annotation.PageID)
		if err != nil {
			return Annotation{}, err
		}
		annotation.DocumentID = page.DocumentID
	}
	annotation.CreatedAt = now()
	annotation.UpdatedAt = annotation.CreatedAt
	if err := a.Store.CreateAnnotation(ctx, annotation); err != nil {
		return Annotation{}, err
	}
	return annotation, nil
}

func (a *App) RetryJob(ctx context.Context, jobID string) (RecognitionStart, error) {
	job, err := a.Store.GetJob(ctx, jobID)
	if err != nil {
		return RecognitionStart{}, err
	}
	if job.Status != "failed" {
		return RecognitionStart{}, fmt.Errorf("only failed jobs can be retried")
	}
	if job.Type != "recognize_document" {
		return RecognitionStart{}, fmt.Errorf("job type %q is not retryable", job.Type)
	}
	payload := decodeJobPayload(job.PayloadJSON)
	documentID := payload["document_id"]
	if documentID == "" {
		run, err := a.Store.GetRecognitionRun(ctx, job.TargetID)
		if err != nil {
			return RecognitionStart{}, err
		}
		documentID = run.DocumentID
	}
	return a.StartRecognition(ctx, documentID)
}

func (a *App) ExportDocument(ctx context.Context, documentID, format string, includePageNumbers bool) (ExportFile, error) {
	doc, err := a.Store.GetDocument(ctx, documentID)
	if err != nil {
		return ExportFile{}, err
	}
	pages, err := a.Store.ListPages(ctx, documentID)
	if err != nil {
		return ExportFile{}, err
	}
	pageTexts := make([]exporter.PageText, 0, len(pages))
	for _, page := range pages {
		_, text, err := a.Store.LatestTextForPage(ctx, page.ID)
		if err != nil {
			return ExportFile{}, err
		}
		pageTexts = append(pageTexts, exporter.PageText{PageNo: page.PageNo, Text: text})
	}
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if format == "markdown" {
		format = "md"
	}
	if format == "" {
		format = "md"
	}
	content, err := exporter.Render(doc.Title, pageTexts, format, includePageNumbers)
	if err != nil {
		return ExportFile{}, err
	}

	exportID := newID("exp")
	rel := storage.ExportRel(exportID, format)
	abs := a.Storage.Abs(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return ExportFile{}, err
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return ExportFile{}, err
	}
	stored, err := a.Storage.Inspect(rel, filepath.Base(rel))
	if err != nil {
		return ExportFile{}, err
	}
	asset, err := a.Store.UpsertAsset(ctx, "export", stored)
	if err != nil {
		return ExportFile{}, err
	}
	if err := a.Store.LinkDocumentAsset(ctx, documentID, asset.ID, "export"); err != nil {
		return ExportFile{}, err
	}
	return ExportFile{
		ID:          asset.ID,
		DocumentID:  documentID,
		Format:      format,
		DownloadURL: "/api/exports/" + asset.ID + "/download",
		StoragePath: asset.StoragePath,
	}, nil
}

func (a *App) AssetPath(ctx context.Context, assetID string) (Asset, string, error) {
	asset, err := a.Store.GetAsset(ctx, assetID)
	if err != nil {
		return Asset{}, "", err
	}
	return asset, a.Storage.Abs(asset.StoragePath), nil
}

func (a *App) createPages(ctx context.Context, documentID string, original storage.StoredFile) (int, error) {
	ext := strings.ToLower(filepath.Ext(original.OriginalName))
	mimeType := strings.ToLower(original.MimeType)
	if ext == ".pdf" || strings.Contains(mimeType, "pdf") {
		pages, err := pageproc.ExtractPDFImages(ctx, original.AbsPath)
		if err != nil {
			return 0, err
		}
		defer pageproc.CleanupExtractedPages(pages)
		for i, page := range pages {
			if err := a.addPageFromPath(ctx, documentID, i+1, page.Path, page.Ext); err != nil {
				return 0, err
			}
		}
		return len(pages), nil
	}

	if strings.HasPrefix(mimeType, "image/") || isImageExt(ext) {
		if err := a.addPageFromPath(ctx, documentID, 1, original.AbsPath, ext); err != nil {
			return 0, err
		}
		return 1, nil
	}
	return 0, fmt.Errorf("unsupported import file type %s", mime.TypeByExtension(ext))
}

func (a *App) addPageFromPath(ctx context.Context, documentID string, pageNo int, sourcePath string, ext string) error {
	if ext == "" {
		ext = filepath.Ext(sourcePath)
	}
	pageRel := storage.PageImageRel(documentID, pageNo, ext)
	pageFile, err := a.Storage.CopyTo(pageRel, sourcePath, filepath.Base(pageRel))
	if err != nil {
		return err
	}
	pageAsset, err := a.Store.UpsertAsset(ctx, "page_image", pageFile)
	if err != nil {
		return err
	}
	if err := a.Store.LinkDocumentAsset(ctx, documentID, pageAsset.ID, "page_image"); err != nil {
		return err
	}

	thumbAssetID := ""
	thumbRel := storage.ThumbnailRel(documentID, pageNo)
	width, height, thumbOK, err := pageproc.GenerateThumbnail(pageFile.AbsPath, a.Storage.Abs(thumbRel), 420)
	if err != nil {
		return err
	}
	if thumbOK {
		thumbFile, err := a.Storage.Inspect(thumbRel, filepath.Base(thumbRel))
		if err != nil {
			return err
		}
		thumbAsset, err := a.Store.UpsertAsset(ctx, "thumbnail", thumbFile)
		if err != nil {
			return err
		}
		if err := a.Store.LinkDocumentAsset(ctx, documentID, thumbAsset.ID, "thumbnail"); err != nil {
			return err
		}
		thumbAssetID = thumbAsset.ID
	}

	page := Page{
		ID:           newID("pag"),
		DocumentID:   documentID,
		PageNo:       pageNo,
		ImageAssetID: pageAsset.ID,
		ThumbAssetID: thumbAssetID,
		Width:        width,
		Height:       height,
		Status:       "extracted",
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}
	return a.Store.CreatePage(ctx, page)
}

func (a *App) recognizeDocument(ctx context.Context, runID, jobID string) error {
	run, err := a.Store.GetRecognitionRun(ctx, runID)
	if err != nil {
		return err
	}
	if jobID != "" {
		_ = a.Store.MarkJobRunning(ctx, jobID)
	}
	startedAt := now()
	if err := a.Store.UpdateRecognitionRunStatus(ctx, runID, "running", startedAt, ""); err != nil {
		return err
	}
	if err := a.Store.UpdateDocumentStatus(ctx, run.DocumentID, "recognizing"); err != nil {
		return err
	}

	pages, err := a.Store.ListPages(ctx, run.DocumentID)
	if err != nil {
		return err
	}
	var failures []string
	successes := 0
	for _, page := range pages {
		if page.ImageAssetID == "" {
			failures = append(failures, fmt.Sprintf("page %d has no image asset", page.PageNo))
			_ = a.Store.UpdatePageStatus(ctx, page.ID, "failed")
			continue
		}
		asset, err := a.Store.GetAsset(ctx, page.ImageAssetID)
		if err != nil {
			failures = append(failures, fmt.Sprintf("page %d: %v", page.PageNo, err))
			_ = a.Store.UpdatePageStatus(ctx, page.ID, "failed")
			continue
		}
		result, err := a.Recognizer.RecognizePage(ctx, recognizer.PageInput{
			DocumentID: run.DocumentID,
			PageID:     page.ID,
			PageNo:     page.PageNo,
			ImagePath:  a.Storage.Abs(asset.StoragePath),
			Width:      page.Width,
			Height:     page.Height,
		})
		if err != nil {
			failures = append(failures, fmt.Sprintf("page %d: %v", page.PageNo, err))
			_ = a.Store.UpdatePageStatus(ctx, page.ID, "failed")
			continue
		}
		raw := string(result.RawJSON)
		if strings.TrimSpace(raw) == "" {
			raw = "{}"
		}
		storedResult := RecognitionResult{
			ID:         newID("res"),
			RunID:      runID,
			PageID:     page.ID,
			Text:       result.Text,
			Confidence: result.Confidence,
			RawJSON:    raw,
			CreatedAt:  now(),
		}
		if err := a.Store.CreateRecognitionResult(ctx, storedResult); err != nil {
			failures = append(failures, fmt.Sprintf("page %d: %v", page.PageNo, err))
			_ = a.Store.UpdatePageStatus(ctx, page.ID, "failed")
			continue
		}
		candidate := TextVersion{
			ID:             newID("txt"),
			DocumentID:     run.DocumentID,
			PageID:         page.ID,
			Kind:           "candidate",
			SourceResultID: storedResult.ID,
			Text:           result.Text,
			Status:         "draft",
			CreatedBy:      "system",
			CreatedAt:      now(),
		}
		if err := a.Store.CreateTextVersion(ctx, candidate); err != nil {
			failures = append(failures, fmt.Sprintf("page %d candidate: %v", page.PageNo, err))
		}
		_ = a.Store.UpdatePageStatus(ctx, page.ID, "recognized")
		successes++
	}

	finishedAt := now()
	if successes == 0 && len(failures) > 0 {
		cause := errors.New(strings.Join(failures, "; "))
		_ = a.Store.UpdateRecognitionRunStatus(ctx, runID, "failed", "", finishedAt)
		_ = a.Store.UpdateDocumentStatus(ctx, run.DocumentID, "failed")
		return cause
	}
	status := "succeeded"
	if len(failures) > 0 {
		status = "failed"
	}
	if err := a.Store.UpdateRecognitionRunStatus(ctx, runID, status, "", finishedAt); err != nil {
		return err
	}
	if err := a.Store.UpdateDocumentStatus(ctx, run.DocumentID, "reviewing"); err != nil {
		return err
	}
	if jobID != "" {
		if len(failures) > 0 {
			_ = a.Store.MarkJobFailed(ctx, jobID, errors.New(strings.Join(failures, "; ")))
		} else {
			_ = a.Store.MarkJobDone(ctx, jobID)
		}
	}
	return nil
}

func (a *App) maybeFinalizeDocument(ctx context.Context, documentID string) error {
	pages, err := a.Store.ListPageDetails(ctx, documentID)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return nil
	}
	for _, page := range pages {
		if !page.HasFinal && page.PageStatus != "verified" {
			return nil
		}
	}
	return a.Store.UpdateDocumentStatus(ctx, documentID, "finalized")
}

func isImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".tif", ".tiff", ".bmp":
		return true
	default:
		return false
	}
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
