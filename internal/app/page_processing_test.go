package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/pageproc"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestPageProcessingCreatesDerivedAssetWithoutChangingOriginal(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "页图处理"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	original, err := application.Store.GetAsset(ctx, pages[0].ImageAssetID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(files.Abs(original.StoragePath))
	if err != nil {
		t.Fatal(err)
	}

	start, err := application.StartPageProcessing(ctx, doc.ID, app.PageProcessingOptions{Config: pageproc.DefaultEnhanceConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if start.Job.Type != "process_pages" || start.Run.TotalPages != 1 {
		t.Fatalf("unexpected processing start: %+v", start)
	}
	waitForJob(t, application, start.Job.ID)
	after, err := os.ReadFile(files.Abs(original.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("original page asset changed during processing")
	}
	preview, err := application.PageProcessingPreview(ctx, pages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Result == nil || preview.Result.OutputAssetID == "" || preview.Result.EnhancedURL == "" {
		t.Fatalf("missing enhanced preview: %+v", preview)
	}
	asset, err := application.Store.GetAsset(ctx, preview.Result.OutputAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Kind != "enhanced_page" || asset.ID == original.ID {
		t.Fatalf("derived asset = %+v, original = %+v", asset, original)
	}
	if _, err := os.Stat(files.Abs(asset.StoragePath)); err != nil {
		t.Fatal(err)
	}
	run, err := application.Store.GetPageProcessingRun(ctx, start.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" || run.DonePages != 1 || run.FailedPages != 0 {
		t.Fatalf("processing run = %+v", run)
	}
	recognitionStart, err := application.StartRecognitionWithOptions(ctx, doc.ID, app.RecognitionOptions{InputSource: "enhanced"})
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, application, recognitionStart.Job.ID)
	recognitionRun, err := application.Store.GetRecognitionRun(ctx, recognitionStart.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recognitionRun.InputSource != "enhanced" {
		t.Fatalf("recognition input source = %q", recognitionRun.InputSource)
	}
	results, err := application.Store.ListRecognitionResults(ctx, pages[0].ID)
	if err != nil || len(results) != 1 {
		t.Fatalf("recognition results = %+v, err = %v", results, err)
	}
	if !strings.Contains(results[0].MetadataJSON, `"image_source":"enhanced"`) ||
		!strings.Contains(results[0].MetadataJSON, preview.Result.OutputAssetID) ||
		!strings.Contains(results[0].MetadataJSON, preview.Result.ID) {
		t.Fatalf("enhanced input audit metadata = %s", results[0].MetadataJSON)
	}
}

func TestPageProcessingFailedJobRetriesOnlyUnfinishedResults(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "处理重试"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := application.Store.ListPages(ctx, doc.ID)
	asset, _ := application.Store.GetAsset(ctx, pages[0].ImageAssetID)
	assetPath := files.Abs(asset.StoragePath)
	backupPath := assetPath + ".bak"
	if err := os.Rename(assetPath, backupPath); err != nil {
		t.Fatal(err)
	}
	start, err := application.StartPageProcessing(ctx, doc.ID, app.PageProcessingOptions{Config: pageproc.DefaultEnhanceConfig()})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := application.Store.GetJob(ctx, start.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	failedJob, err := application.Store.GetJob(ctx, start.Job.ID)
	if err != nil || failedJob.Status != "failed" {
		t.Fatalf("failed job = %+v, err = %v", failedJob, err)
	}
	if err := os.Rename(backupPath, assetPath); err != nil {
		t.Fatal(err)
	}
	retry, err := application.RetryJob(ctx, start.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, application, retry.Job.ID)
	run, err := application.Store.GetPageProcessingRun(ctx, start.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" || run.DonePages != 1 || run.FailedPages != 0 {
		t.Fatalf("retried run = %+v", run)
	}
	job, err := application.Store.GetJob(ctx, start.Job.ID)
	if err != nil || job.Attempts != 2 || job.Status != "succeeded" {
		t.Fatalf("retried job = %+v, err = %v", job, err)
	}
}

func TestCancelPageProcessingJobSynchronizesRunAndResults(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "取消处理"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := application.Store.ListPages(ctx, doc.ID)
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	job := app.Job{ID: "job_cancel_processing", Type: "process_pages", Status: "queued", TargetType: "page_processing_run", TargetID: "proc_cancel", PayloadJSON: `{"run_id":"proc_cancel"}`, MaxAttempts: 3, ProgressTotal: 1, CreatedAt: createdAt}
	if err := application.Store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	run := app.PageProcessingRun{ID: "proc_cancel", DocumentID: doc.ID, JobID: job.ID, ConfigJSON: `{}`, Status: "queued", TotalPages: 1, CreatedAt: createdAt}
	result := app.PageProcessingResult{ID: "ppr_cancel", RunID: run.ID, PageID: pages[0].ID, SourceAssetID: pages[0].ImageAssetID, Status: "queued", ConfigJSON: `{}`, MetadataJSON: `{}`, CreatedAt: createdAt}
	if err := application.Store.CreatePageProcessingRun(ctx, run, []app.PageProcessingResult{result}); err != nil {
		t.Fatal(err)
	}
	if err := application.CancelJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	gotJob, _ := application.Store.GetJob(ctx, job.ID)
	gotRun, _ := application.Store.GetPageProcessingRun(ctx, run.ID)
	results, _ := application.Store.ListPageProcessingResults(ctx, run.ID)
	if gotJob.Status != "canceled" || gotRun.Status != "canceled" || len(results) != 1 || results[0].Status != "canceled" {
		t.Fatalf("cancel state job=%+v run=%+v results=%+v", gotJob, gotRun, results)
	}
}

func TestRecoverInterruptedFinalizesPageProcessingState(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	store := app.NewStore(conn)
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	document := app.Document{ID: "doc_recover_processing", Title: "恢复处理", Status: "ready", PageCount: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := store.CreateDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO assets(id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at)
		VALUES ('asset_recover_processing', 'page_image', 'sha_recover_processing', 'page.png', 'image/png', 1, 'pages/page.png', ?)
	`, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO pages(id, document_id, page_no, image_asset_id, status, created_at, updated_at)
		VALUES ('page_recover_processing', ?, 1, 'asset_recover_processing', 'extracted', ?, ?)
	`, document.ID, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	job := app.Job{ID: "job_recover_processing", Type: "process_pages", Status: "running", TargetType: "page_processing_run", TargetID: "proc_recover", PayloadJSON: `{}`, MaxAttempts: 3, CreatedAt: createdAt}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	run := app.PageProcessingRun{ID: "proc_recover", DocumentID: document.ID, JobID: job.ID, ConfigJSON: `{}`, Status: "running", TotalPages: 1, CreatedAt: createdAt}
	result := app.PageProcessingResult{ID: "ppr_recover", RunID: run.ID, PageID: "page_recover_processing", SourceAssetID: "asset_recover_processing", Status: "running", ConfigJSON: `{}`, MetadataJSON: `{}`, CreatedAt: createdAt}
	if err := store.CreatePageProcessingRun(ctx, run, []app.PageProcessingResult{result}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	recoveredRun, _ := store.GetPageProcessingRun(ctx, run.ID)
	recoveredResults, _ := store.ListPageProcessingResults(ctx, run.ID)
	if recoveredRun.Status != "failed" || recoveredRun.FailedPages != 1 || len(recoveredResults) != 1 || recoveredResults[0].Status != "failed" {
		t.Fatalf("recovered run=%+v results=%+v", recoveredRun, recoveredResults)
	}
	if _, active, err := store.ActivePageProcessingRun(ctx, document.ID); err != nil || active {
		t.Fatalf("active after recovery = %v, err=%v", active, err)
	}
}
