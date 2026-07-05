package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lieyan/firescribe/internal/exporter"
	"github.com/lieyan/firescribe/internal/pageproc"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

// ErrRecognitionActive is returned when a document already has a queued or
// running recognition run; the API layer maps it to 409 Conflict.
var ErrRecognitionActive = errors.New("recognition is already running for this document")

// Options are runtime-tunable pipeline settings (see /api/settings).
type Options struct {
	PDFRenderDPI int
}

type App struct {
	Store   *Store
	Storage *storage.Storage

	mu      sync.RWMutex
	rec     recognizer.Recognizer
	options Options

	runMu      sync.Mutex
	activeRuns map[string]*runHandle // document ID → active run
	runsByID   map[string]*runHandle

	baseCtx context.Context
	stop    context.CancelCauseFunc
	wg      sync.WaitGroup
}

type runHandle struct {
	runID      string
	documentID string
	cancel     context.CancelCauseFunc
}

type ImportOptions struct {
	Title       string
	Description string
	Author      string
	Source      string
}

// ImportFile is one uploaded file of a document import.
type ImportFile struct {
	Name   string
	Reader io.Reader
}

type RecognitionStart struct {
	Run RecognitionRun `json:"run"`
	Job Job            `json:"job"`
}

func New(store *Store, files *storage.Storage, rec recognizer.Recognizer) *App {
	baseCtx, stop := context.WithCancelCause(context.Background())
	return &App{
		Store:      store,
		Storage:    files,
		rec:        rec,
		options:    Options{PDFRenderDPI: 200},
		activeRuns: map[string]*runHandle{},
		runsByID:   map[string]*runHandle{},
		baseCtx:    baseCtx,
		stop:       stop,
	}
}

// SetRecognizer swaps the recognizer used by future runs (settings changes).
func (a *App) SetRecognizer(rec recognizer.Recognizer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rec = rec
}

func (a *App) Recognizer() recognizer.Recognizer {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rec
}

func (a *App) SetOptions(options Options) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if options.PDFRenderDPI <= 0 {
		options.PDFRenderDPI = 200
	}
	a.options = options
}

func (a *App) Options() Options {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.options
}

// Shutdown cancels all active recognition runs and waits (bounded by ctx) for
// their workers to persist terminal state.
func (a *App) Shutdown(ctx context.Context) {
	a.stop(errors.New("canceled by server shutdown"))
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("app shutdown: timed out waiting for recognition workers")
	}
}

// ImportDocument creates one document from one or more uploaded files, in
// upload order: each image becomes a page, each PDF is rasterized into pages.
func (a *App) ImportDocument(ctx context.Context, opts ImportOptions, files ...ImportFile) (Document, error) {
	if len(files) == 0 {
		return Document{}, errors.New("upload field \"files\" is required")
	}

	storedFiles := make([]storage.StoredFile, 0, len(files))
	for _, file := range files {
		stored, err := a.Storage.StoreOriginal(file.Name, file.Reader)
		if err != nil {
			return Document{}, fmt.Errorf("store %s: %w", file.Name, err)
		}
		storedFiles = append(storedFiles, stored)
	}

	timestamp := now()
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(storedFiles[0].OriginalName), filepath.Ext(storedFiles[0].OriginalName))
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

	// Terminal document status must be persisted even when the request context
	// is canceled mid-import (client disconnect), or the document would be
	// stuck in "importing" until the next restart.
	dbCtx := context.Background()
	fail := func(err error) (Document, error) {
		_ = a.Store.UpdateDocumentStatus(dbCtx, doc.ID, "failed")
		return Document{}, err
	}

	pageNo := 0
	for _, stored := range storedFiles {
		asset, err := a.Store.UpsertAsset(ctx, "original", stored)
		if err != nil {
			return fail(err)
		}
		if err := a.Store.LinkDocumentAsset(ctx, doc.ID, asset.ID, "original"); err != nil {
			return fail(err)
		}
		if err := a.createPagesFromFile(ctx, doc.ID, stored, &pageNo); err != nil {
			return fail(fmt.Errorf("import %s: %w", stored.OriginalName, err))
		}
	}
	if pageNo == 0 {
		return fail(errors.New("no pages could be created from the uploaded files"))
	}
	if err := a.Store.UpdateDocumentReady(dbCtx, doc.ID, pageNo); err != nil {
		return fail(err)
	}
	doc.PageCount = pageNo
	doc.Status = "ready"
	doc.UpdatedAt = now()
	return doc, nil
}

func (a *App) createPagesFromFile(ctx context.Context, documentID string, original storage.StoredFile, pageNo *int) error {
	ext := strings.ToLower(filepath.Ext(original.OriginalName))
	mimeType := strings.ToLower(original.MimeType)

	if ext == ".pdf" || strings.Contains(mimeType, "pdf") {
		pages, err := pageproc.RasterizePDF(ctx, original.AbsPath, a.Options().PDFRenderDPI)
		if err != nil {
			return err
		}
		defer pageproc.CleanupExtractedPages(pages)
		for _, page := range pages {
			*pageNo++
			if err := a.addPageFromPath(ctx, documentID, *pageNo, page.Path, page.Ext); err != nil {
				return err
			}
		}
		return nil
	}

	if strings.HasPrefix(mimeType, "image/") || pageproc.SupportedImageExt(ext) {
		path, normalizedExt, cleanup, err := pageproc.NormalizeImage(original.AbsPath, ext)
		if err != nil {
			return err
		}
		defer cleanup()
		*pageNo++
		return a.addPageFromPath(ctx, documentID, *pageNo, path, normalizedExt)
	}
	return fmt.Errorf("unsupported file type %q (expected images or PDF)", ext)
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

// StartRecognition creates a recognition run over the given pages (all pages
// when pageIDs is empty) and starts a background worker for it. Only one run
// per document may be active at a time.
func (a *App) StartRecognition(ctx context.Context, documentID string, pageIDs []string) (RecognitionStart, error) {
	if _, err := a.Store.GetDocument(ctx, documentID); err != nil {
		return RecognitionStart{}, err
	}
	allPages, err := a.Store.ListPages(ctx, documentID)
	if err != nil {
		return RecognitionStart{}, err
	}
	if len(allPages) == 0 {
		return RecognitionStart{}, errors.New("document has no pages to recognize")
	}
	targets := allPages
	if len(pageIDs) > 0 {
		wanted := make(map[string]bool, len(pageIDs))
		for _, id := range pageIDs {
			wanted[id] = true
		}
		targets = nil
		for _, page := range allPages {
			if wanted[page.ID] {
				targets = append(targets, page)
				delete(wanted, page.ID)
			}
		}
		if len(wanted) > 0 {
			return RecognitionStart{}, fmt.Errorf("unknown page ids for document: %d of %d not found", len(wanted), len(pageIDs))
		}
	}

	a.runMu.Lock()
	defer a.runMu.Unlock()
	if _, active := a.activeRuns[documentID]; active {
		return RecognitionStart{}, ErrRecognitionActive
	}
	// A queued/running run without an in-process worker is a leftover this
	// process cannot finish; fail it so its pages become retryable.
	if zombie, ok, err := a.Store.ActiveRecognitionRun(ctx, documentID); err != nil {
		return RecognitionStart{}, err
	} else if ok {
		log.Printf("recognition run %s has no worker; marking failed", zombie.ID)
		_, _ = a.Store.CancelPendingRunPages(ctx, zombie.ID, "orphaned run")
		_ = a.Store.FinishRecognitionRun(ctx, zombie.ID, "failed", "orphaned run superseded by a new run")
		_ = a.Store.CancelJobsForTarget(ctx, zombie.ID)
	}

	rec := a.Recognizer()
	timestamp := now()
	run := RecognitionRun{
		ID:            newID("run"),
		DocumentID:    documentID,
		Provider:      rec.Provider(),
		Model:         rec.Model(),
		PromptVersion: rec.PromptVersion(),
		ConfigJSON:    rec.ConfigJSON(),
		Status:        "queued",
		TotalPages:    len(targets),
		CreatedAt:     timestamp,
	}
	if err := a.Store.CreateRecognitionRun(ctx, run); err != nil {
		return RecognitionStart{}, err
	}
	if err := a.Store.CreateRunPages(ctx, run.ID, targets); err != nil {
		_ = a.Store.FinishRecognitionRun(ctx, run.ID, "failed", "failed to enqueue pages: "+err.Error())
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
		_ = a.Store.FinishRecognitionRun(ctx, run.ID, "failed", "failed to create job: "+err.Error())
		_, _ = a.Store.CancelPendingRunPages(ctx, run.ID, "run failed before start")
		return RecognitionStart{}, err
	}

	runCtx, cancel := context.WithCancelCause(a.baseCtx)
	handle := &runHandle{runID: run.ID, documentID: documentID, cancel: cancel}
	a.activeRuns[documentID] = handle
	a.runsByID[run.ID] = handle
	a.wg.Add(1)
	go a.executeRun(runCtx, handle, rec, run, job.ID, targets)

	return RecognitionStart{Run: run, Job: job}, nil
}

func (a *App) releaseRun(handle *runHandle) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.activeRuns[handle.documentID] == handle {
		delete(a.activeRuns, handle.documentID)
	}
	if a.runsByID[handle.runID] == handle {
		delete(a.runsByID, handle.runID)
	}
}

// executeRun drives one recognition run page by page, recording per-page and
// aggregate progress as it goes. It always leaves the run, its job, and the
// document in a terminal/consistent state, including on cancellation, panic,
// and storage errors.
func (a *App) executeRun(ctx context.Context, handle *runHandle, rec recognizer.Recognizer, run RecognitionRun, jobID string, pages []Page) {
	// Persistence below deliberately uses context.Background(): once ctx is
	// canceled we still must record terminal state before returning.
	dbCtx := context.Background()
	defer a.wg.Done()
	defer a.releaseRun(handle)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recognition run %s panicked: %v", run.ID, r)
			_, _ = a.Store.CancelPendingRunPages(dbCtx, run.ID, "worker panicked")
			_ = a.Store.RecomputeDocumentStatus(dbCtx, run.DocumentID)
			_ = a.Store.MarkJobFailed(dbCtx, jobID, fmt.Errorf("worker panicked: %v", r))
			_ = a.Store.FinishRecognitionRun(dbCtx, run.ID, "failed", fmt.Sprintf("worker panicked: %v", r))
		}
	}()

	if err := a.Store.MarkJobRunning(dbCtx, jobID); err != nil {
		log.Printf("run %s: mark job running: %v", run.ID, err)
	}
	if err := a.Store.UpdateRecognitionRunStatus(dbCtx, run.ID, "running", now(), ""); err != nil {
		log.Printf("run %s: mark running: %v", run.ID, err)
	}
	if err := a.Store.UpdateDocumentStatus(dbCtx, run.DocumentID, "recognizing"); err != nil {
		log.Printf("run %s: mark document recognizing: %v", run.ID, err)
	}

	var pageErrors []string
	succeeded := 0
	canceled := false
	for _, page := range pages {
		if ctx.Err() != nil {
			canceled = true
			break
		}
		if err := a.Store.MarkRunPageRunning(dbCtx, run.ID, page.ID); err != nil {
			log.Printf("run %s: mark page %d running: %v", run.ID, page.PageNo, err)
		}
		err := a.recognizeOnePage(ctx, rec, run, page)
		switch {
		case err == nil:
			if err := a.Store.MarkRunPageFinished(dbCtx, run.ID, page.ID, "succeeded", ""); err != nil {
				log.Printf("run %s: mark page %d finished: %v", run.ID, page.PageNo, err)
			}
			a.setPageStatus(dbCtx, page.ID, "recognized")
			succeeded++
		case ctx.Err() != nil:
			canceled = true
			_ = a.Store.MarkRunPageFinished(dbCtx, run.ID, page.ID, "canceled", cancelCause(ctx))
		default:
			message := err.Error()
			pageErrors = append(pageErrors, fmt.Sprintf("第 %d 页: %s", page.PageNo, message))
			if err := a.Store.MarkRunPageFinished(dbCtx, run.ID, page.ID, "failed", message); err != nil {
				log.Printf("run %s: mark page %d failed: %v", run.ID, page.PageNo, err)
			}
			a.setPageStatus(dbCtx, page.ID, "failed")
		}
		if canceled {
			break
		}
	}

	status := "succeeded"
	runErr := ""
	switch {
	case canceled:
		status = "canceled"
		runErr = cancelCause(ctx)
		if _, err := a.Store.CancelPendingRunPages(dbCtx, run.ID, runErr); err != nil {
			log.Printf("run %s: cancel pending pages: %v", run.ID, err)
		}
	case len(pageErrors) > 0 && succeeded > 0:
		status = "partial"
		runErr = summarizeErrors(pageErrors)
	case len(pageErrors) > 0:
		status = "failed"
		runErr = summarizeErrors(pageErrors)
	}
	// The run row is finalized LAST: clients poll run.status, so document and
	// job state must already be consistent when the run turns terminal.
	if err := a.Store.RecomputeDocumentStatus(dbCtx, run.DocumentID); err != nil {
		log.Printf("run %s: recompute document status: %v", run.ID, err)
	}
	switch status {
	case "succeeded":
		_ = a.Store.MarkJobDone(dbCtx, jobID)
	case "canceled":
		_ = a.Store.MarkJobCanceled(dbCtx, jobID)
	default:
		_ = a.Store.MarkJobFailed(dbCtx, jobID, errors.New(runErr))
	}
	if err := a.Store.FinishRecognitionRun(dbCtx, run.ID, status, runErr); err != nil {
		log.Printf("run %s: finish: %v", run.ID, err)
	}
}

func (a *App) recognizeOnePage(ctx context.Context, rec recognizer.Recognizer, run RecognitionRun, page Page) error {
	if page.ImageAssetID == "" {
		return errors.New("page has no image asset")
	}
	asset, err := a.Store.GetAsset(ctx, page.ImageAssetID)
	if err != nil {
		return fmt.Errorf("load page asset: %w", err)
	}
	result, err := rec.RecognizePage(ctx, recognizer.PageInput{
		DocumentID: run.DocumentID,
		PageID:     page.ID,
		PageNo:     page.PageNo,
		ImagePath:  a.Storage.Abs(asset.StoragePath),
		Width:      page.Width,
		Height:     page.Height,
	})
	if err != nil {
		return err
	}
	raw := string(result.RawJSON)
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	storedResult := RecognitionResult{
		ID:         newID("res"),
		RunID:      run.ID,
		PageID:     page.ID,
		Text:       result.Text,
		Confidence: result.Confidence,
		RawJSON:    raw,
		CreatedAt:  now(),
	}
	if err := a.Store.CreateRecognitionResult(ctx, storedResult); err != nil {
		return fmt.Errorf("store result: %w", err)
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
		return fmt.Errorf("store candidate text: %w", err)
	}
	return nil
}

// setPageStatus updates a page's status unless a reviewer already verified it;
// re-running recognition must not downgrade proofread pages.
func (a *App) setPageStatus(ctx context.Context, pageID, status string) {
	page, err := a.Store.GetPage(ctx, pageID)
	if err != nil {
		log.Printf("load page %s: %v", pageID, err)
		return
	}
	if page.Status == "verified" {
		return
	}
	if err := a.Store.UpdatePageStatus(ctx, pageID, status); err != nil {
		log.Printf("update page %s status: %v", pageID, err)
	}
}

// RetryRun starts a new run covering the pages of runID that did not succeed.
func (a *App) RetryRun(ctx context.Context, runID string) (RecognitionStart, error) {
	run, err := a.Store.GetRecognitionRun(ctx, runID)
	if err != nil {
		return RecognitionStart{}, err
	}
	if run.Status == "queued" || run.Status == "running" {
		return RecognitionStart{}, errors.New("run is still active; cancel it before retrying")
	}
	runPages, err := a.Store.ListRunPages(ctx, runID)
	if err != nil {
		return RecognitionStart{}, err
	}
	var pageIDs []string
	for _, page := range runPages {
		if page.Status != "succeeded" {
			pageIDs = append(pageIDs, page.PageID)
		}
	}
	if len(pageIDs) == 0 {
		return RecognitionStart{}, errors.New("nothing to retry: all pages of this run succeeded")
	}
	return a.StartRecognition(ctx, run.DocumentID, pageIDs)
}

// CancelRun stops an active run. Runs with a live worker finalize themselves;
// leftover rows without a worker are finalized directly.
func (a *App) CancelRun(ctx context.Context, runID string) error {
	a.runMu.Lock()
	handle := a.runsByID[runID]
	a.runMu.Unlock()
	if handle != nil {
		handle.cancel(errors.New("canceled by user"))
		return nil
	}

	run, err := a.Store.GetRecognitionRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != "queued" && run.Status != "running" {
		return fmt.Errorf("run is not active (status %s)", run.Status)
	}
	if _, err := a.Store.CancelPendingRunPages(ctx, runID, "canceled by user"); err != nil {
		return err
	}
	if err := a.Store.CancelJobsForTarget(ctx, runID); err != nil {
		return err
	}
	if err := a.Store.RecomputeDocumentStatus(ctx, run.DocumentID); err != nil {
		return err
	}
	return a.Store.FinishRecognitionRun(ctx, runID, "canceled", "canceled by user")
}

// CancelJob cancels the recognition run driven by a job (or just the job row
// itself for other job types).
func (a *App) CancelJob(ctx context.Context, jobID string) error {
	job, err := a.Store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Type == "recognize_document" && job.TargetID != "" {
		if err := a.CancelRun(ctx, job.TargetID); err != nil {
			return err
		}
	}
	return a.Store.MarkJobCanceled(ctx, jobID)
}

// RetryJob re-runs the failed pages of the run a failed job was driving.
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
	return a.RetryRun(ctx, job.TargetID)
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
	} else if version.Kind == "manual" {
		_ = a.Store.UpdatePageStatus(ctx, version.PageID, "reviewing")
	}
	_ = a.Store.RecomputeDocumentStatus(ctx, version.DocumentID)
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

func cancelCause(ctx context.Context) string {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause.Error()
	}
	return "canceled"
}

func summarizeErrors(errorsList []string) string {
	const maxShown = 3
	if len(errorsList) <= maxShown {
		return strings.Join(errorsList, "; ")
	}
	return fmt.Sprintf("%s;(共 %d 页失败)", strings.Join(errorsList[:maxShown], "; "), len(errorsList))
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
