package app_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

// scriptedRecognizer fails the pages whose numbers are in failPages; block, if
// non-nil, is closed to release a run that should hang until canceled.
type scriptedRecognizer struct {
	mu        sync.Mutex
	failPages map[int]bool
	block     chan struct{}
	calls     []int
}

func (r *scriptedRecognizer) Name() string          { return "scripted" }
func (r *scriptedRecognizer) Provider() string      { return "scripted" }
func (r *scriptedRecognizer) Model() string         { return "scripted-vlm" }
func (r *scriptedRecognizer) PromptVersion() string { return "test#0000" }
func (r *scriptedRecognizer) ConfigJSON() string    { return "{}" }

func (r *scriptedRecognizer) RecognizePage(ctx context.Context, input recognizer.PageInput) (recognizer.RecognitionResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, input.PageNo)
	block := r.block
	fail := r.failPages[input.PageNo]
	r.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return recognizer.RecognitionResult{}, ctx.Err()
		}
	}
	if fail {
		return recognizer.RecognitionResult{}, fmt.Errorf("scripted failure for page %d", input.PageNo)
	}
	return recognizer.RecognitionResult{Text: fmt.Sprintf("第 %d 页文本", input.PageNo), RawJSON: []byte(`{}`)}, nil
}

func (r *scriptedRecognizer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newTestApp(t *testing.T, rec recognizer.Recognizer) (*app.App, *sql.DB) {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/firescribe.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return app.New(app.NewStore(conn), files, rec), conn
}

// distinctPNG returns a PNG whose pixels depend on seed, so multi-file tests
// exercise real distinct assets instead of content-addressed dedup.
func distinctPNG(t *testing.T, seed uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: seed, G: uint8(80 + y*3), B: uint8(20 + x*4), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func importThreePages(t *testing.T, application *app.App) app.Document {
	t.Helper()
	doc, err := application.ImportDocument(context.Background(), app.ImportOptions{Title: "多页文档"},
		app.ImportFile{Name: "a.png", Reader: bytes.NewReader(distinctPNG(t, 10))},
		app.ImportFile{Name: "b.png", Reader: bytes.NewReader(distinctPNG(t, 120))},
		app.ImportFile{Name: "c.png", Reader: bytes.NewReader(distinctPNG(t, 230))},
	)
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount != 3 || doc.Status != "ready" {
		t.Fatalf("imported doc = %+v, want 3 ready pages", doc)
	}
	return doc
}

func waitForRun(t *testing.T, application *app.App, runID string) app.RecognitionRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := application.Store.GetRecognitionRun(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		switch run.Status {
		case "succeeded", "partial", "failed", "canceled":
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not reach a terminal status")
	return app.RecognitionRun{}
}

func TestMultiFileImportOrdersPagesByUpload(t *testing.T) {
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)

	pages, err := application.Store.ListPages(context.Background(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("pages = %d", len(pages))
	}
	for i, page := range pages {
		if page.PageNo != i+1 {
			t.Fatalf("page %d has page_no %d", i, page.PageNo)
		}
	}
	assets, err := application.Store.ListDocumentAssets(context.Background(), doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	originals := 0
	for _, asset := range assets {
		if asset.Role == "original" {
			originals++
		}
	}
	if originals != 3 {
		t.Fatalf("original assets = %d, want 3", originals)
	}
}

func TestPartialFailureAndRetryOnlyFailedPages(t *testing.T) {
	ctx := context.Background()
	rec := &scriptedRecognizer{failPages: map[int]bool{2: true}}
	application, _ := newTestApp(t, rec)
	doc := importThreePages(t, application)

	start, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, application, start.Run.ID)
	if run.Status != "partial" {
		t.Fatalf("run status = %s, want partial (error=%s)", run.Status, run.Error)
	}
	if run.TotalPages != 3 || run.DonePages != 3 || run.FailedPages != 1 {
		t.Fatalf("run progress = %d/%d failed=%d", run.DonePages, run.TotalPages, run.FailedPages)
	}
	if run.Error == "" {
		t.Fatal("partial run should carry an error summary")
	}

	runPages, err := application.Store.ListRunPages(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantStatuses := []string{"succeeded", "failed", "succeeded"}
	for i, rp := range runPages {
		if rp.Status != wantStatuses[i] {
			t.Fatalf("run page %d status = %s, want %s", rp.PageNo, rp.Status, wantStatuses[i])
		}
	}
	if runPages[1].Error == "" {
		t.Fatal("failed run page should record its error")
	}

	gotDoc, err := application.Store.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDoc.Status != "reviewing" {
		t.Fatalf("document status = %s, want reviewing", gotDoc.Status)
	}

	// Retry re-runs only the failed page.
	rec.mu.Lock()
	rec.failPages = nil
	rec.mu.Unlock()
	callsBefore := rec.callCount()

	retry, err := application.RetryRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	retryRun := waitForRun(t, application, retry.Run.ID)
	if retryRun.Status != "succeeded" {
		t.Fatalf("retry run status = %s (error=%s)", retryRun.Status, retryRun.Error)
	}
	if retryRun.TotalPages != 1 {
		t.Fatalf("retry run total_pages = %d, want 1", retryRun.TotalPages)
	}
	if rec.callCount()-callsBefore != 1 {
		t.Fatalf("retry made %d recognize calls, want 1", rec.callCount()-callsBefore)
	}

	if _, err := application.RetryRun(ctx, retry.Run.ID); err == nil {
		t.Fatal("retrying a fully succeeded run should fail")
	}
}

func TestConcurrentRecognitionRejectedThenCancel(t *testing.T) {
	ctx := context.Background()
	block := make(chan struct{})
	rec := &scriptedRecognizer{block: block}
	application, _ := newTestApp(t, rec)
	doc := importThreePages(t, application)

	start, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := application.StartRecognition(ctx, doc.ID, nil); !errors.Is(err, app.ErrRecognitionActive) {
		t.Fatalf("second start error = %v, want ErrRecognitionActive", err)
	}

	if err := application.CancelRun(ctx, start.Run.ID); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, application, start.Run.ID)
	if run.Status != "canceled" {
		t.Fatalf("run status = %s, want canceled", run.Status)
	}
	job, err := application.Store.GetJob(ctx, start.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "canceled" {
		t.Fatalf("job status = %s, want canceled", job.Status)
	}
	runPages, err := application.Store.ListRunPages(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rp := range runPages {
		if rp.Status != "canceled" {
			t.Fatalf("run page %d status = %s, want canceled", rp.PageNo, rp.Status)
		}
	}

	// After cancellation a new run can start immediately.
	close(block)
	rec.mu.Lock()
	rec.block = nil
	rec.mu.Unlock()
	second, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatalf("start after cancel: %v", err)
	}
	if got := waitForRun(t, application, second.Run.ID); got.Status != "succeeded" {
		t.Fatalf("second run status = %s (error=%s)", got.Status, got.Error)
	}
}

func TestShutdownCancelsActiveRuns(t *testing.T) {
	ctx := context.Background()
	block := make(chan struct{})
	defer close(block)
	rec := &scriptedRecognizer{block: block}
	application, _ := newTestApp(t, rec)
	doc := importThreePages(t, application)

	start, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	application.Shutdown(shutdownCtx)

	run, err := application.Store.GetRecognitionRun(ctx, start.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "canceled" {
		t.Fatalf("run status after shutdown = %s, want canceled", run.Status)
	}
}

func TestRecoverInterruptedResetsStuckState(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a crash: run + run_pages + job left active, document stuck.
	run := app.RecognitionRun{
		ID: "run_stuck", DocumentID: doc.ID, Provider: "p", Model: "m",
		Status: "running", TotalPages: 3, CreatedAt: "2026-01-01T00:00:00Z",
	}
	if err := application.Store.CreateRecognitionRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := application.Store.CreateRunPages(ctx, run.ID, pages); err != nil {
		t.Fatal(err)
	}
	job := app.Job{
		ID: "job_stuck", Type: "recognize_document", Status: "running",
		TargetType: "recognition_run", TargetID: run.ID, MaxAttempts: 1,
		CreatedAt: "2026-01-01T00:00:00Z",
	}
	if err := application.Store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := application.Store.UpdateDocumentStatus(ctx, doc.ID, "recognizing"); err != nil {
		t.Fatal(err)
	}

	recovered, err := application.Store.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	gotRun, err := application.Store.GetRecognitionRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRun.Status != "failed" || gotRun.FailedPages != 3 || gotRun.DonePages != 3 {
		t.Fatalf("recovered run = status %s done %d failed %d", gotRun.Status, gotRun.DonePages, gotRun.FailedPages)
	}
	gotJob, err := application.Store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotJob.Status != "failed" {
		t.Fatalf("recovered job status = %s", gotJob.Status)
	}
	gotDoc, err := application.Store.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDoc.Status != "ready" {
		t.Fatalf("recovered doc status = %s, want ready", gotDoc.Status)
	}

	// The interrupted run is now retryable page by page.
	retry, err := application.RetryRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForRun(t, application, retry.Run.ID); got.Status != "succeeded" {
		t.Fatalf("retry-after-recover status = %s (error=%s)", got.Status, got.Error)
	}
}

func TestStartRecognitionValidatesPageIDs(t *testing.T) {
	ctx := context.Background()
	application, _ := newTestApp(t, &scriptedRecognizer{})
	doc := importThreePages(t, application)

	if _, err := application.StartRecognition(ctx, doc.ID, []string{"pag_missing"}); err == nil {
		t.Fatal("unknown page id should be rejected")
	}

	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	start, err := application.StartRecognition(ctx, doc.ID, []string{pages[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, application, start.Run.ID)
	if run.Status != "succeeded" || run.TotalPages != 1 {
		t.Fatalf("subset run = %s total %d", run.Status, run.TotalPages)
	}
}

func TestVerifiedPagesAreNotDowngradedByReruns(t *testing.T) {
	ctx := context.Background()
	rec := &scriptedRecognizer{}
	application, _ := newTestApp(t, rec)
	doc := importThreePages(t, application)

	start, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, application, start.Run.ID)

	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pages[0].ID, Kind: "final", Text: "定稿", Status: "verified", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	second, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, application, second.Run.ID)

	page, err := application.Store.GetPage(ctx, pages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Status != "verified" {
		t.Fatalf("verified page downgraded to %s", page.Status)
	}
}
