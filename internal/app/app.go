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

	mu       sync.RWMutex
	rec      recognizer.Recognizer
	registry *recognizer.Registry
	options  Options

	runMu      sync.Mutex
	activeRuns map[string]*runHandle // document ID → active run
	runsByID   map[string]*runHandle
	jobMu      sync.Mutex
	activeJobs map[string]context.CancelCauseFunc

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

type ImportStart struct {
	Document
	Job Job `json:"job"`
}

type ExportStart struct {
	ExportFile
	Job Job `json:"job"`
}

type JobRetryResult struct {
	Job Job             `json:"job"`
	Run *RecognitionRun `json:"run,omitempty"`
}

type importJobPayload struct {
	AssetIDs []string `json:"asset_ids"`
}

type exportJobPayload struct {
	ExportID           string `json:"export_id"`
	Format             string `json:"format"`
	IncludePageNumbers bool   `json:"include_page_numbers"`
	TextScope          string `json:"text_scope"`
	IncludeAnnotations bool   `json:"include_annotations"`
	IncludeUncertain   bool   `json:"include_uncertain"`
}

func New(store *Store, files *storage.Storage, rec recognizer.Recognizer) *App {
	if files != nil {
		if err := store.ConfigureSecretFile(context.Background(), filepath.Join(files.Root, "secrets.json")); err != nil {
			log.Printf("configure credential store: %v", err)
		}
	}
	baseCtx, stop := context.WithCancelCause(context.Background())
	return &App{
		Store:      store,
		Storage:    files,
		rec:        rec,
		registry:   recognizer.NewRegistry(),
		options:    Options{PDFRenderDPI: 200},
		activeRuns: map[string]*runHandle{},
		runsByID:   map[string]*runHandle{},
		activeJobs: map[string]context.CancelCauseFunc{},
		baseCtx:    baseCtx,
		stop:       stop,
	}
}

type RecognitionOptions struct {
	PageIDs              []string
	ProfileID            string
	ProviderAdapterID    string
	PromptVersionID      string
	InputSource          string
	preparedRecognizer   recognizer.Recognizer
	preparedSnapshotJSON string
	preparedRun          RecognitionRun
	experimentVariantID  string
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

// StartImport persists the upload, creates the importing document immediately,
// and lets a background job perform page extraction and thumbnail generation.
func (a *App) StartImport(ctx context.Context, opts ImportOptions, files ...ImportFile) (ImportStart, error) {
	if len(files) == 0 {
		return ImportStart{}, errors.New("upload field \"files\" is required")
	}
	storedFiles := make([]storage.StoredFile, 0, len(files))
	for _, file := range files {
		stored, err := a.Storage.StoreOriginal(file.Name, file.Reader)
		if err != nil {
			return ImportStart{}, fmt.Errorf("store %s: %w", file.Name, err)
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
		ID: newID("doc"), Title: title, Description: opts.Description, Author: opts.Author, Source: opts.Source,
		Status: "importing", CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	if err := a.Store.CreateDocument(ctx, doc); err != nil {
		return ImportStart{}, err
	}

	assetIDs := make([]string, 0, len(storedFiles))
	for _, stored := range storedFiles {
		asset, err := a.Store.UpsertAsset(ctx, "original", stored)
		if err != nil {
			_ = a.Store.UpdateDocumentStatus(context.Background(), doc.ID, "failed")
			return ImportStart{}, err
		}
		if err := a.Store.LinkDocumentAsset(ctx, doc.ID, asset.ID, "original"); err != nil {
			_ = a.Store.UpdateDocumentStatus(context.Background(), doc.ID, "failed")
			return ImportStart{}, err
		}
		assetIDs = append(assetIDs, asset.ID)
	}
	job := Job{
		ID: newID("job"), Type: "import_document", Status: "queued", TargetType: "document", TargetID: doc.ID,
		PayloadJSON: mustJSON(importJobPayload{AssetIDs: assetIDs}), MaxAttempts: 3, ProgressTotal: len(assetIDs),
		ProgressMessage: "等待导入", CreatedAt: timestamp,
	}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		_ = a.Store.UpdateDocumentStatus(context.Background(), doc.ID, "failed")
		return ImportStart{}, err
	}
	a.launchJob(job.ID)
	return ImportStart{Document: doc, Job: job}, nil
}

func (a *App) createPagesFromFile(ctx context.Context, documentID string, original storage.StoredFile, pageNo *int) error {
	return a.createPagesFromFileWithProgress(ctx, documentID, original, pageNo, nil)
}

func (a *App) createPagesFromFileWithProgress(ctx context.Context, documentID string, original storage.StoredFile, pageNo *int, pageDone func(int, string)) error {
	ext := strings.ToLower(filepath.Ext(original.OriginalName))
	mimeType := strings.ToLower(original.MimeType)

	if ext == ".pdf" || strings.Contains(mimeType, "pdf") {
		startingPage := *pageNo
		reportedProgress := startingPage
		return pageproc.ProcessPDFPages(ctx, original.AbsPath, a.Options().PDFRenderDPI, func(current, _ int, message string) {
			candidate := startingPage + current
			if candidate > reportedProgress {
				reportedProgress = candidate
				if pageDone != nil {
					pageDone(reportedProgress, message)
				}
			}
		}, func(page pageproc.ExtractedPage) error {
			*pageNo++
			if err := a.addPageFromPath(ctx, documentID, *pageNo, page.Path, page.Ext); err != nil {
				return err
			}
			if pageDone != nil {
				current := *pageNo
				if reportedProgress > current {
					current = reportedProgress
				}
				pageDone(current, fmt.Sprintf("已登记 PDF 第 %d 页", *pageNo-startingPage))
			}
			return nil
		})
	}

	if strings.HasPrefix(mimeType, "image/") || pageproc.SupportedImageExt(ext) {
		path, normalizedExt, cleanup, err := pageproc.NormalizeImage(original.AbsPath, ext)
		if err != nil {
			return err
		}
		defer cleanup()
		*pageNo++
		if err := a.addPageFromPath(ctx, documentID, *pageNo, path, normalizedExt); err != nil {
			return err
		}
		if pageDone != nil {
			pageDone(*pageNo, fmt.Sprintf("已导入第 %d 页", *pageNo))
		}
		return nil
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

func (a *App) launchJob(jobID string) {
	jobCtx, cancel := context.WithCancelCause(a.baseCtx)
	a.jobMu.Lock()
	if _, exists := a.activeJobs[jobID]; exists {
		a.jobMu.Unlock()
		cancel(errors.New("duplicate worker"))
		return
	}
	a.activeJobs[jobID] = cancel
	a.jobMu.Unlock()

	a.wg.Add(1)
	go a.executeJob(jobCtx, jobID)
}

func (a *App) releaseJob(jobID string) {
	a.jobMu.Lock()
	delete(a.activeJobs, jobID)
	a.jobMu.Unlock()
}

func (a *App) finalizeJob(ctx context.Context, jobID, status string, cause error, resultJSON string) {
	var err error
	switch status {
	case "succeeded":
		err = a.Store.MarkJobDoneWithResult(ctx, jobID, resultJSON)
	case "canceled":
		err = a.Store.MarkJobCanceled(ctx, jobID)
	case "failed":
		err = a.Store.MarkJobFailed(ctx, jobID, cause)
	default:
		log.Printf("job %s: invalid terminal status %q", jobID, status)
		return
	}
	if err == nil {
		return
	}
	log.Printf("job %s: persist terminal status %s with event: %v; forcing terminal row", jobID, status, err)
	if forceErr := a.Store.ForceJobTerminal(ctx, jobID, status, errorString(cause), resultJSON); forceErr != nil {
		log.Printf("job %s: force terminal status %s: %v", jobID, status, forceErr)
	}
}

func (a *App) executeJob(ctx context.Context, jobID string) {
	dbCtx := context.Background()
	defer a.wg.Done()
	defer a.releaseJob(jobID)
	defer func() {
		if recovered := recover(); recovered != nil {
			a.finalizeJob(dbCtx, jobID, "failed", fmt.Errorf("worker panicked: %v", recovered), "{}")
		}
	}()
	if err := a.Store.MarkJobRunning(dbCtx, jobID); err != nil {
		return
	}
	job, err := a.Store.GetJob(dbCtx, jobID)
	if err != nil {
		a.finalizeJob(dbCtx, jobID, "failed", err, "{}")
		return
	}
	var result any
	switch job.Type {
	case "import_document":
		result, err = a.runImportJob(ctx, job)
	case "export_document":
		result, err = a.runExportJob(ctx, job)
	case "export_project":
		result, err = a.runProjectExportJob(ctx, job)
	case "rebuild_search_index":
		result, err = a.runRebuildSearchIndexJob(ctx, job)
	case "process_pages":
		result, err = a.runPageProcessingJob(ctx, job)
	case "recognition_experiment":
		result, err = a.runRecognitionExperimentJob(ctx, job)
	default:
		err = fmt.Errorf("unsupported background job type %q", job.Type)
	}
	if ctx.Err() != nil {
		a.finalizeJob(dbCtx, jobID, "canceled", nil, "{}")
		return
	}
	if err != nil {
		a.finalizeJob(dbCtx, jobID, "failed", err, "{}")
		return
	}
	a.finalizeJob(dbCtx, jobID, "succeeded", nil, mustJSON(result))
}

func (a *App) runImportJob(ctx context.Context, job Job) (Document, error) {
	var payload importJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return Document{}, fmt.Errorf("decode import payload: %w", err)
	}
	if len(payload.AssetIDs) == 0 {
		return Document{}, errors.New("import job has no source assets")
	}
	if job.Attempts > 1 {
		if err := a.Store.ResetDocumentImport(context.Background(), job.TargetID); err != nil {
			return Document{}, fmt.Errorf("reset import: %w", err)
		}
	}
	type source struct {
		asset Asset
		pages int
	}
	sources := make([]source, 0, len(payload.AssetIDs))
	totalPages := 0
	for _, assetID := range payload.AssetIDs {
		asset, err := a.Store.GetAsset(ctx, assetID)
		if err != nil {
			return Document{}, fmt.Errorf("load source asset: %w", err)
		}
		count := 1
		if strings.EqualFold(filepath.Ext(asset.OriginalName), ".pdf") || strings.Contains(strings.ToLower(asset.MimeType), "pdf") {
			count, err = pageproc.PDFPageCount(ctx, a.Storage.Abs(asset.StoragePath))
			if err != nil {
				// Extraction still has a pdftoppm fallback on systems without
				// pdfinfo. Keep imports compatible and grow the total as pages
				// are discovered in that environment.
				count = 1
			}
		}
		totalPages += count
		sources = append(sources, source{asset: asset, pages: count})
	}
	_ = a.Store.UpdateJobProgress(context.Background(), job.ID, 0, totalPages, fmt.Sprintf("准备导入 %d 页", totalPages))
	pageNo := 0
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			_ = a.Store.UpdateDocumentStatus(context.Background(), job.TargetID, "failed")
			return Document{}, err
		}
		asset := source.asset
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, pageNo, totalPages, fmt.Sprintf("正在处理 %s（%d 页）", asset.OriginalName, source.pages))
		stored := storage.StoredFile{
			OriginalName: asset.OriginalName, MimeType: asset.MimeType, ByteSize: asset.ByteSize, SHA256: asset.SHA256,
			RelativePath: asset.StoragePath, AbsPath: a.Storage.Abs(asset.StoragePath),
		}
		if err := a.createPagesFromFileWithProgress(ctx, job.TargetID, stored, &pageNo, func(done int, message string) {
			if done > totalPages {
				totalPages = done
			}
			_ = a.Store.UpdateJobProgress(context.Background(), job.ID, done, totalPages, message)
		}); err != nil {
			_ = a.Store.UpdateDocumentStatus(context.Background(), job.TargetID, "failed")
			return Document{}, fmt.Errorf("import %s: %w", asset.OriginalName, err)
		}
	}
	if pageNo == 0 {
		_ = a.Store.UpdateDocumentStatus(context.Background(), job.TargetID, "failed")
		return Document{}, errors.New("no pages could be created from the uploaded files")
	}
	if err := a.Store.UpdateDocumentReady(context.Background(), job.TargetID, pageNo); err != nil {
		_ = a.Store.UpdateDocumentStatus(context.Background(), job.TargetID, "failed")
		return Document{}, err
	}
	return a.Store.GetDocument(context.Background(), job.TargetID)
}

func (a *App) runExportJob(ctx context.Context, job Job) (ExportFile, error) {
	var payload exportJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return ExportFile{}, fmt.Errorf("decode export payload: %w", err)
	}
	if err := a.Store.MarkExportRunning(context.Background(), payload.ExportID); err != nil {
		return ExportFile{}, err
	}
	exported, err := a.renderExport(ctx, job.TargetID, payload.ExportID, ExportOptions{
		Format: payload.Format, IncludePageNumbers: payload.IncludePageNumbers, TextScope: payload.TextScope,
		IncludeAnnotations: payload.IncludeAnnotations, IncludeUncertain: payload.IncludeUncertain,
	}, func(current, total int, message string) {
		_ = a.Store.UpdateJobProgress(context.Background(), job.ID, current, total, message)
	})
	if err != nil {
		if ctx.Err() != nil {
			_ = a.Store.CancelExport(context.Background(), payload.ExportID)
			return ExportFile{}, err
		}
		_ = a.Store.FinishExport(context.Background(), payload.ExportID, "", err)
		return ExportFile{}, err
	}
	if err := a.Store.FinishExport(context.Background(), payload.ExportID, exported.AssetID, nil); err != nil {
		return ExportFile{}, err
	}
	return a.Store.GetExport(context.Background(), payload.ExportID)
}

func (a *App) runRebuildSearchIndexJob(ctx context.Context, job Job) (map[string]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = a.Store.UpdateJobProgress(context.Background(), job.ID, 0, 1, "正在重建全文索引")
	count, err := a.Store.RebuildSearchIndex(ctx)
	if err != nil {
		return nil, err
	}
	_ = a.Store.UpdateJobProgress(context.Background(), job.ID, 1, 1, fmt.Sprintf("已索引 %d 页", count))
	return map[string]int{"indexed_pages": count}, nil
}

// StartRecognition creates a recognition run over the given pages (all pages
// when pageIDs is empty) and starts a background worker for it. Only one run
// per document may be active at a time.
func (a *App) StartRecognition(ctx context.Context, documentID string, pageIDs []string) (RecognitionStart, error) {
	return a.StartRecognitionWithOptions(ctx, documentID, RecognitionOptions{PageIDs: pageIDs})
}

// StartRecognitionWithOptions selects a run-local recognizer profile and/or
// immutable prompt snapshot. It never changes the globally active settings.
func (a *App) StartRecognitionWithOptions(ctx context.Context, documentID string, options RecognitionOptions) (RecognitionStart, error) {
	inputSource, err := normalizeImageSource(options.InputSource)
	if err != nil {
		return RecognitionStart{}, err
	}
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
	if len(options.PageIDs) > 0 {
		wanted := make(map[string]bool, len(options.PageIDs))
		for _, id := range options.PageIDs {
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
			return RecognitionStart{}, fmt.Errorf("unknown page ids for document: %d of %d not found", len(wanted), len(options.PageIDs))
		}
	}
	if inputSource == "enhanced" {
		for _, page := range targets {
			if _, err := a.Store.LatestEnhancedResult(ctx, page.ID); err != nil {
				return RecognitionStart{}, fmt.Errorf("page %d has no successful enhanced image: %w", page.PageNo, err)
			}
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

	var rec recognizer.Recognizer
	var profile RecognizerProfile
	var adapter ProviderAdapter
	var prompt PromptVersion
	var authorContext AuthorRecognitionContext
	snapshotJSON := ""
	if options.preparedRecognizer != nil {
		rec = options.preparedRecognizer
		profile = RecognizerProfile{ID: options.preparedRun.ProfileID, Driver: options.preparedRun.Driver}
		adapter = ProviderAdapter{ID: options.preparedRun.ProviderAdapterID, Engine: options.preparedRun.Driver}
		snapshotJSON = options.preparedSnapshotJSON
	} else {
		rec, profile, adapter, prompt, authorContext, err = a.recognizerForRun(ctx, documentID, options.ProfileID, options.ProviderAdapterID, options.PromptVersionID)
		if err != nil {
			return RecognitionStart{}, err
		}
		snapshotJSON = recognizerProfileSnapshot(profile, adapter, prompt, authorContext)
	}
	timestamp := now()
	driver := profile.Driver
	if adapter.ID != "" {
		driver = adapter.Engine
	}
	run := RecognitionRun{
		ID:                  newID("run"),
		DocumentID:          documentID,
		Provider:            rec.Provider(),
		Model:               rec.Model(),
		PromptVersion:       rec.PromptVersion(),
		ConfigJSON:          rec.ConfigJSON(),
		ProfileID:           profile.ID,
		Driver:              driver,
		ProfileSnapshotJSON: snapshotJSON,
		ProviderAdapterID:   adapter.ID,
		InputSource:         inputSource,
		Status:              "queued",
		TotalPages:          len(targets),
		CreatedAt:           timestamp,
	}
	if err := a.Store.CreateRecognitionRunForVariant(ctx, run, options.experimentVariantID); err != nil {
		return RecognitionStart{}, err
	}
	if err := a.Store.CreateRunPages(ctx, run.ID, targets); err != nil {
		_ = a.Store.FinishRecognitionRun(ctx, run.ID, "failed", "failed to enqueue pages: "+err.Error())
		return RecognitionStart{}, err
	}
	job := Job{
		ID:              newID("job"),
		Type:            "recognize_document",
		Status:          "queued",
		TargetType:      "recognition_run",
		TargetID:        run.ID,
		PayloadJSON:     mustJSON(map[string]string{"document_id": documentID, "run_id": run.ID, "recognizer_profile_id": profile.ID, "provider_adapter_id": adapter.ID, "prompt_version_id": prompt.ID, "input_source": inputSource}),
		MaxAttempts:     1,
		ProgressTotal:   len(targets),
		ProgressMessage: "等待识别",
		CreatedAt:       timestamp,
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
			a.finalizeJob(dbCtx, jobID, "failed", fmt.Errorf("worker panicked: %v", r), "{}")
			_ = a.Store.FinishRecognitionRun(dbCtx, run.ID, "failed", fmt.Sprintf("worker panicked: %v", r))
		}
	}()

	if err := a.Store.MarkJobRunning(dbCtx, jobID); err != nil {
		log.Printf("run %s: mark job running: %v", run.ID, err)
		_, _ = a.Store.CancelPendingRunPages(dbCtx, run.ID, "recognition job was canceled before start")
		_ = a.Store.RecomputeDocumentStatus(dbCtx, run.DocumentID)
		_ = a.Store.FinishRecognitionRun(dbCtx, run.ID, "canceled", "recognition job was canceled before start")
		return
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
		_ = a.Store.UpdateJobProgress(dbCtx, jobID, succeeded+len(pageErrors), len(pages), fmt.Sprintf("已处理第 %d 页", page.PageNo))
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
		a.finalizeJob(dbCtx, jobID, "succeeded", nil, "{}")
	case "canceled":
		a.finalizeJob(dbCtx, jobID, "canceled", nil, "{}")
	default:
		a.finalizeJob(dbCtx, jobID, "failed", errors.New(runErr), "{}")
	}
	if err := a.Store.FinishRecognitionRun(dbCtx, run.ID, status, runErr); err != nil {
		log.Printf("run %s: finish: %v", run.ID, err)
	}
}

func (a *App) recognizeOnePage(ctx context.Context, rec recognizer.Recognizer, run RecognitionRun, page Page) error {
	input, err := a.resolveRecognitionImage(ctx, run, page)
	if err != nil {
		return err
	}
	result, err := rec.RecognizePage(ctx, recognizer.PageInput{
		DocumentID: run.DocumentID,
		PageID:     page.ID,
		PageNo:     page.PageNo,
		ImagePath:  a.Storage.Abs(input.Asset.StoragePath),
		Width:      input.Width,
		Height:     input.Height,
	})
	if err != nil {
		return err
	}
	raw := string(result.RawJSON)
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	metadata := make(map[string]any, len(result.Metadata)+1)
	for key, value := range result.Metadata {
		metadata[key] = value
	}
	metadata["input"] = map[string]any{
		"document_id":          run.DocumentID,
		"page_id":              page.ID,
		"page_no":              page.PageNo,
		"image_asset_id":       input.Asset.ID,
		"image_source":         input.Source,
		"processing_result_id": input.ProcessingResultID,
		"width":                input.Width,
		"height":               input.Height,
	}
	storedResult := RecognitionResult{
		ID:           newID("res"),
		RunID:        run.ID,
		PageID:       page.ID,
		Text:         result.Text,
		Confidence:   result.Confidence,
		RawJSON:      raw,
		MetadataJSON: mustJSON(metadata),
		CreatedAt:    now(),
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

// setPageStatus updates a page's status unless a human version is currently
// effective; re-running recognition must not make a manual draft or final look
// like an untouched recognized page.
func (a *App) setPageStatus(ctx context.Context, pageID, status string) {
	page, err := a.Store.GetPageDetail(ctx, pageID)
	if err != nil {
		log.Printf("load page %s: %v", pageID, err)
		return
	}
	if page.HasFinal || page.HasManual {
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
	rec, err := a.recognizerFromRunSnapshot(ctx, run)
	if err != nil {
		return RecognitionStart{}, fmt.Errorf("cannot retry recognition run %s from its immutable snapshot: %w", run.ID, err)
	}
	return a.StartRecognitionWithOptions(ctx, run.DocumentID, RecognitionOptions{
		PageIDs: pageIDs, ProfileID: run.ProfileID, ProviderAdapterID: run.ProviderAdapterID, InputSource: run.InputSource,
		preparedRecognizer: rec, preparedSnapshotJSON: run.ProfileSnapshotJSON, preparedRun: run,
	})
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
	if job.Status != "queued" && job.Status != "running" {
		return fmt.Errorf("job is not active (status %s)", job.Status)
	}
	if job.Type == "recognize_document" && job.TargetID != "" {
		if err := a.CancelRun(ctx, job.TargetID); err != nil {
			return err
		}
	} else {
		a.jobMu.Lock()
		cancel := a.activeJobs[jobID]
		a.jobMu.Unlock()
		if cancel != nil {
			cancel(errors.New("canceled by user"))
		}
		if job.Type == "export_document" {
			var payload exportJobPayload
			if json.Unmarshal([]byte(job.PayloadJSON), &payload) == nil && payload.ExportID != "" {
				_ = a.Store.CancelExport(context.Background(), payload.ExportID)
			}
		} else if job.Type == "export_project" {
			var payload projectExportJobPayload
			if json.Unmarshal([]byte(job.PayloadJSON), &payload) == nil && payload.ExportID != "" {
				_ = a.Store.CancelProjectExport(context.Background(), payload.ExportID)
			}
		} else if job.Type == "import_document" && job.TargetID != "" {
			_ = a.Store.UpdateDocumentStatus(context.Background(), job.TargetID, "failed")
		} else if job.Type == "process_pages" && job.TargetID != "" {
			_ = a.Store.CancelPendingPageProcessingResults(context.Background(), job.TargetID, "canceled by user")
			run, runErr := a.Store.GetPageProcessingRun(context.Background(), job.TargetID)
			if runErr == nil {
				_ = a.Store.FinishPageProcessingRun(context.Background(), run.ID, "canceled", "canceled by user", run.DonePages, run.FailedPages)
			}
		} else if job.Type == "recognition_experiment" {
			if experiment, expErr := a.Store.ExperimentByJobID(context.Background(), job.ID); expErr == nil {
				for _, variant := range experiment.Variants {
					for _, runID := range variant.RunIDs {
						_ = a.CancelRun(context.Background(), runID)
					}
				}
				_ = a.Store.FinishRecognitionExperiment(context.Background(), experiment.ID, "canceled", "canceled by user")
			}
		}
	}
	return a.Store.MarkJobCanceled(ctx, jobID)
}

// RetryJob retries failed work. Recognition creates a new run; the other
// durable jobs reuse their row so attempts/max_attempts have real semantics.
func (a *App) RetryJob(ctx context.Context, jobID string) (JobRetryResult, error) {
	job, err := a.Store.GetJob(ctx, jobID)
	if err != nil {
		return JobRetryResult{}, err
	}
	if job.Status != "failed" {
		return JobRetryResult{}, fmt.Errorf("only failed jobs can be retried")
	}
	if job.Type == "recognize_document" {
		start, err := a.RetryRun(ctx, job.TargetID)
		if err != nil {
			return JobRetryResult{}, err
		}
		return JobRetryResult{Job: start.Job, Run: &start.Run}, nil
	}
	if job.Type == "process_pages" {
		queued, err := a.Store.RequeuePageProcessingJob(ctx, job.ID, job.TargetID)
		if err != nil {
			return JobRetryResult{}, err
		}
		a.launchJob(jobID)
		return JobRetryResult{Job: queued}, nil
	}
	if job.Type == "recognition_experiment" {
		experiment, err := a.Store.ExperimentByJobID(ctx, job.ID)
		if err != nil {
			return JobRetryResult{}, err
		}
		queued, err := a.Store.RequeueRecognitionExperimentJob(ctx, job.ID, experiment.ID)
		if err != nil {
			return JobRetryResult{}, err
		}
		a.launchJob(jobID)
		return JobRetryResult{Job: queued}, nil
	}
	switch job.Type {
	case "import_document", "export_document", "export_project", "rebuild_search_index":
		queued, err := a.Store.RequeueJob(ctx, jobID)
		if err != nil {
			return JobRetryResult{}, err
		}
		if job.Type == "export_document" {
			var payload exportJobPayload
			if json.Unmarshal([]byte(job.PayloadJSON), &payload) == nil && payload.ExportID != "" {
				// A failed export keeps its stable ID; mark it queued again.
				_ = a.Store.RequeueExport(ctx, payload.ExportID)
			}
		} else if job.Type == "export_project" {
			var payload projectExportJobPayload
			if json.Unmarshal([]byte(job.PayloadJSON), &payload) == nil && payload.ExportID != "" {
				_ = a.Store.RequeueProjectExport(ctx, payload.ExportID)
			}
		}
		a.launchJob(jobID)
		return JobRetryResult{Job: queued}, nil
	default:
		return JobRetryResult{}, fmt.Errorf("job type %q is not retryable", job.Type)
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
	if err := a.Store.RecordAuthorCorrection(ctx, version); err != nil {
		log.Printf("record author correction for text version %s: %v", version.ID, err)
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
	exported, err := a.renderExport(ctx, documentID, newID("exp"), ExportOptions{
		Format: format, IncludePageNumbers: includePageNumbers, TextScope: "current",
	}, nil)
	if err != nil {
		return ExportFile{}, err
	}
	// Keep the direct method compatible with the pre-job API: its identifier
	// is the generated asset and its URL goes through the generic asset route.
	exported.ID = exported.AssetID
	exported.DownloadURL = "/api/assets/" + exported.AssetID + "/download"
	return exported, nil
}

func (a *App) StartExport(ctx context.Context, documentID, format string, includePageNumbers bool) (ExportStart, error) {
	return a.StartExportWithOptions(ctx, documentID, ExportOptions{
		Format: format, IncludePageNumbers: includePageNumbers, TextScope: "current",
	})
}

func (a *App) StartExportWithOptions(ctx context.Context, documentID string, options ExportOptions) (ExportStart, error) {
	if _, err := a.Store.GetDocument(ctx, documentID); err != nil {
		return ExportStart{}, err
	}
	options, err := normalizeExportOptions(options)
	if err != nil {
		return ExportStart{}, err
	}
	timestamp := now()
	exportID := newID("exp")
	job := Job{
		ID: newID("job"), Type: "export_document", Status: "queued", TargetType: "document", TargetID: documentID,
		PayloadJSON: mustJSON(exportJobPayload{
			ExportID: exportID, Format: options.Format, IncludePageNumbers: options.IncludePageNumbers,
			TextScope: options.TextScope, IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
		}),
		MaxAttempts: 3, ProgressMessage: "等待导出", CreatedAt: timestamp,
	}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		return ExportStart{}, err
	}
	export := ExportFile{
		ID: exportID, DocumentID: documentID, JobID: job.ID, Format: options.Format, IncludePageNumbers: options.IncludePageNumbers,
		TextScope: options.TextScope, IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
		Status: "queued", CreatedAt: timestamp,
	}
	if err := a.Store.CreateExport(ctx, export); err != nil {
		_ = a.Store.MarkJobFailed(context.Background(), job.ID, err)
		return ExportStart{}, err
	}
	a.launchJob(job.ID)
	return ExportStart{ExportFile: export, Job: job}, nil
}

func (a *App) StartRebuildSearchIndex(ctx context.Context) (Job, error) {
	active, err := a.Store.HasActiveJobType(ctx, "rebuild_search_index")
	if err != nil {
		return Job{}, err
	}
	if active {
		return Job{}, errors.New("search index rebuild is already queued or running")
	}
	job := Job{
		ID: newID("job"), Type: "rebuild_search_index", Status: "queued", TargetType: "system", TargetID: "text_search",
		PayloadJSON: "{}", MaxAttempts: 3, ProgressTotal: 1, ProgressMessage: "等待重建索引", CreatedAt: now(),
	}
	if err := a.Store.CreateJob(ctx, job); err != nil {
		return Job{}, err
	}
	a.launchJob(job.ID)
	return job, nil
}

func (a *App) renderExport(
	ctx context.Context,
	documentID string,
	exportID string,
	options ExportOptions,
	progress func(current, total int, message string),
) (ExportFile, error) {
	options, err := normalizeExportOptions(options)
	if err != nil {
		return ExportFile{}, err
	}
	doc, err := a.Store.GetDocument(ctx, documentID)
	if err != nil {
		return ExportFile{}, err
	}
	pages, err := a.Store.ListPages(ctx, documentID)
	if err != nil {
		return ExportFile{}, err
	}
	annotations, err := a.Store.ListAnnotations(ctx, documentID, "")
	if err != nil {
		return ExportFile{}, err
	}
	annotationsByPage := make(map[string][]Annotation)
	for _, annotation := range annotations {
		annotationsByPage[annotation.PageID] = append(annotationsByPage[annotation.PageID], annotation)
	}
	pageTexts := make([]exporter.PageText, 0, len(pages))
	snapshots := make([]ExportPageSnapshot, 0, len(pages))
	for index, page := range pages {
		if err := ctx.Err(); err != nil {
			return ExportFile{}, err
		}
		var version TextVersion
		var found bool
		if options.TextScope == "final" {
			version, found, err = a.Store.LatestFinalTextForPage(ctx, page.ID)
		} else {
			version, found, err = a.Store.EffectiveTextForPage(ctx, page.ID)
		}
		if err != nil {
			return ExportFile{}, err
		}
		if !found && options.TextScope == "final" {
			if progress != nil {
				progress(index+1, len(pages), fmt.Sprintf("第 %d 页尚未定稿，已跳过", page.PageNo))
			}
			continue
		}
		exportPage := exporter.PageText{PageNo: page.PageNo, Text: version.Text, VersionID: version.ID, VersionKind: version.Kind}
		for _, annotation := range annotationsByPage[page.ID] {
			anchor := exporter.ParseAnnotationAnchor(annotation.AnchorJSON)
			start, end := exporter.ResolveTextRange(version.Text, anchor.Start, anchor.End, anchor.Text)
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
		pageTexts = append(pageTexts, exportPage)
		snapshots = append(snapshots, ExportPageSnapshot{
			Ordinal: len(snapshots), DocumentID: documentID, DocumentTitle: doc.Title,
			PageID: page.ID, PageNo: page.PageNo, TextVersionID: version.ID, TextVersionKind: version.Kind,
			Annotations: includedExportAnnotations(annotationsByPage[page.ID], version.Text, options), CreatedAt: now(),
		})
		if progress != nil {
			progress(index+1, len(pages), fmt.Sprintf("已整理第 %d 页", page.PageNo))
		}
	}
	if options.TextScope == "final" && len(pageTexts) == 0 {
		return ExportFile{}, errors.New("document has no finalized pages to export")
	}
	content, err := exporter.RenderWithOptions(doc.Title, pageTexts, exporter.Options{
		Format: options.Format, IncludePageNumbers: options.IncludePageNumbers,
		IncludeAnnotations: options.IncludeAnnotations, IncludeUncertain: options.IncludeUncertain,
	})
	if err != nil {
		return ExportFile{}, err
	}

	rel := storage.ExportRel(exportID, options.Format)
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
	if err := a.Store.ReplaceExportPageSnapshots(ctx, exportID, snapshots); err != nil {
		return ExportFile{}, fmt.Errorf("store export provenance: %w", err)
	}
	return ExportFile{
		ID:                 exportID,
		DocumentID:         documentID,
		AssetID:            asset.ID,
		Format:             options.Format,
		IncludePageNumbers: options.IncludePageNumbers,
		TextScope:          options.TextScope,
		IncludeAnnotations: options.IncludeAnnotations,
		IncludeUncertain:   options.IncludeUncertain,
		Status:             "succeeded",
		DownloadURL:        "/api/exports/" + exportID + "/download",
		StoragePath:        asset.StoragePath,
	}, nil
}

func normalizeExportFormat(format string) (string, error) {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if format == "markdown" {
		format = "md"
	}
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "txt" && format != "docx" && format != "pdf" {
		return "", fmt.Errorf("unsupported export format %q", format)
	}
	return format, nil
}

func normalizeExportOptions(options ExportOptions) (ExportOptions, error) {
	format, err := normalizeExportFormat(options.Format)
	if err != nil {
		return ExportOptions{}, err
	}
	options.Format = format
	options.TextScope = strings.ToLower(strings.TrimSpace(options.TextScope))
	if options.TextScope == "" {
		options.TextScope = "current"
	}
	if options.TextScope != "current" && options.TextScope != "final" {
		return ExportOptions{}, fmt.Errorf("unsupported export text scope %q", options.TextScope)
	}
	return options, nil
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
