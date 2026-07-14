package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestBackgroundImportExportAndSearchRebuild(t *testing.T) {
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

	started, err := application.StartImport(ctx, app.ImportOptions{Title: "后台任务文档"}, app.ImportFile{
		Name: "page.png", Reader: bytes.NewReader(testPNG(t)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ID == "" || started.Job.Type != "import_document" || started.Status != "importing" {
		t.Fatalf("unexpected import start: %+v", started)
	}
	importJob := waitForTerminalJob(t, application, started.Job.ID)
	if importJob.Status != "succeeded" || importJob.Attempts != 1 || importJob.ProgressCurrent != 1 || importJob.ProgressTotal != 1 {
		t.Fatalf("unexpected import job: %+v", importJob)
	}
	events, err := application.Store.ListJobEvents(ctx, importJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 4 || events[0].Stage != "queued" || events[len(events)-1].Stage != "succeeded" {
		t.Fatalf("unexpected job events: %+v", events)
	}
	for _, event := range events {
		if event.Stage == "attempt_started" && event.Attempt != 1 {
			t.Fatalf("attempt event = %+v", event)
		}
	}
	doc, err := application.Store.GetDocument(ctx, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != "ready" || doc.PageCount != 1 {
		t.Fatalf("unexpected imported document: %+v", doc)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pages[0].ID, Kind: "manual", Text: "后台索引关键词", Status: "draft",
	}); err != nil {
		t.Fatal(err)
	}

	exportStart, err := application.StartExport(ctx, doc.ID, "txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if exportStart.Job.Type != "export_document" || exportStart.ID == "" {
		t.Fatalf("unexpected export start: %+v", exportStart)
	}
	exportJob := waitForTerminalJob(t, application, exportStart.Job.ID)
	if exportJob.Status != "succeeded" || exportJob.ProgressCurrent != 1 || exportJob.ProgressTotal != 1 {
		t.Fatalf("unexpected export job: %+v", exportJob)
	}
	exported, err := application.Store.GetExport(ctx, exportStart.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exported.Status != "succeeded" || exported.AssetID == "" || exported.DownloadURL == "" {
		t.Fatalf("unexpected export: %+v", exported)
	}
	raw, err := os.ReadFile(application.Storage.Abs(exported.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "后台索引关键词") {
		t.Fatalf("export content = %q", raw)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM text_search`); err != nil {
		t.Fatal(err)
	}
	if results, err := application.Store.Search(ctx, "后台索引"); err != nil || len(results) != 0 {
		t.Fatalf("search before rebuild = %+v, err = %v", results, err)
	}
	rebuild, err := application.StartRebuildSearchIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rebuild = waitForTerminalJob(t, application, rebuild.ID)
	if rebuild.Status != "succeeded" || rebuild.ProgressCurrent != 1 || !strings.Contains(rebuild.ResultJSON, "indexed_pages") {
		t.Fatalf("unexpected rebuild job: %+v", rebuild)
	}
	if results, err := application.Store.Search(ctx, "后台索引"); err != nil || len(results) != 1 {
		t.Fatalf("search after rebuild = %+v, err = %v", results, err)
	}
}

func TestAdvancedExportPersistsOptionsAndUsesRequestedTextScope(t *testing.T) {
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
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "增强导出"}, app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	final, err := application.SaveTextVersion(ctx, app.TextVersion{DocumentID: doc.ID, PageID: pages[0].ID, Kind: "final", Status: "verified", Text: "最终定稿文字"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{DocumentID: doc.ID, PageID: pages[0].ID, Kind: "manual", Status: "draft", Text: "当前人工草稿", BaseVersionID: final.ID}); err != nil {
		t.Fatal(err)
	}
	for _, annotation := range []app.Annotation{
		{DocumentID: doc.ID, PageID: pages[0].ID, Kind: "uncertain_text", Body: "核对首词", AnchorJSON: `{"type":"text_range","start":0,"end":2,"text":"最终"}`},
		{DocumentID: doc.ID, PageID: pages[0].ID, Kind: "page_note", Body: "页级说明"},
		{DocumentID: doc.ID, PageID: pages[0].ID, Kind: "page_region", Body: "区域说明", AnchorJSON: `{"type":"page_region","x":12,"y":34,"width":56,"height":78}`},
	} {
		if _, err := application.CreateAnnotation(ctx, annotation); err != nil {
			t.Fatal(err)
		}
	}

	started, err := application.StartExportWithOptions(ctx, doc.ID, app.ExportOptions{
		Format: "txt", IncludePageNumbers: true, TextScope: "final", IncludeAnnotations: true, IncludeUncertain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForTerminalJob(t, application, started.Job.ID)
	if job.Status != "succeeded" {
		t.Fatalf("export job = %+v", job)
	}
	exported, err := application.Store.GetExport(ctx, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exported.TextScope != "final" || !exported.IncludeAnnotations || !exported.IncludeUncertain || !exported.IncludePageNumbers {
		t.Fatalf("persisted options = %+v", exported)
	}
	raw, err := os.ReadFile(application.Storage.Abs(exported.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	output := string(raw)
	for _, want := range []string{"最终〔存疑：核对首词〕定稿文字", "页级说明", "区域批注", "x=12"} {
		if !strings.Contains(output, want) {
			t.Fatalf("export missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "当前人工草稿") {
		t.Fatalf("final-only export leaked current draft: %s", output)
	}
}

func TestFailedImportJobCanRetryUntilMaxAttempts(t *testing.T) {
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

	started, err := application.StartImport(ctx, app.ImportOptions{Title: "失败导入"}, app.ImportFile{
		Name: "unsupported.bin", Reader: bytes.NewReader([]byte("not an image")),
	})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForTerminalJob(t, application, started.Job.ID)
	if job.Status != "failed" || job.Attempts != 1 || job.LastError == "" {
		t.Fatalf("unexpected first failure: %+v", job)
	}
	for wantAttempts := 2; wantAttempts <= 3; wantAttempts++ {
		if _, err := application.RetryJob(ctx, job.ID); err != nil {
			t.Fatalf("retry %d: %v", wantAttempts, err)
		}
		job = waitForTerminalJob(t, application, job.ID)
		if job.Status != "failed" || job.Attempts != wantAttempts {
			t.Fatalf("attempt %d job = %+v", wantAttempts, job)
		}
	}
	if _, err := application.RetryJob(ctx, job.ID); err == nil {
		t.Fatal("expected retry after max attempts to fail")
	}
}

func TestCancelQueuedImportJobFinalizesDocument(t *testing.T) {
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
	doc := app.Document{
		ID: "doc_cancel", Title: "待取消导入", Status: "importing",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := application.Store.CreateDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}
	job := app.Job{
		ID: "job_cancel", Type: "import_document", Status: "queued", TargetType: "document", TargetID: doc.ID,
		PayloadJSON: `{}`, MaxAttempts: 3, CreatedAt: "2026-01-01T00:00:00Z",
	}
	if err := application.Store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := application.CancelJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job, err = application.Store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "canceled" {
		t.Fatalf("job status = %s", job.Status)
	}
	gotDoc, err := application.Store.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDoc.Status != "failed" {
		t.Fatalf("document status = %s", gotDoc.Status)
	}
}

func TestJobStateAndEventTransitionsAreAtomic(t *testing.T) {
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
	queued := app.Job{ID: "atomic-queued", Type: "test", Status: "queued", TargetType: "test", TargetID: "target", MaxAttempts: 3, CreatedAt: "2026-01-01T00:00:00Z"}
	if err := store.CreateJob(ctx, queued); err != nil {
		t.Fatal(err)
	}
	failed := app.Job{ID: "atomic-failed", Type: "test", Status: "queued", TargetType: "test", TargetID: "target", MaxAttempts: 3, CreatedAt: "2026-01-01T00:00:01Z"}
	if err := store.CreateJob(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkJobRunning(ctx, failed.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkJobFailed(ctx, failed.ID, errors.New("first failure")); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TRIGGER reject_job_events BEFORE INSERT ON job_events BEGIN SELECT RAISE(ABORT, 'reject event'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkJobRunning(ctx, queued.ID); err == nil {
		t.Fatal("expected running transition to fail with event insertion")
	}
	gotQueued, _ := store.GetJob(ctx, queued.ID)
	if gotQueued.Status != "queued" || gotQueued.Attempts != 0 {
		t.Fatalf("queued transition was not rolled back: %+v", gotQueued)
	}
	if _, err := store.RequeueJob(ctx, failed.ID); err == nil {
		t.Fatal("expected retry transition to fail with event insertion")
	}
	gotFailed, _ := store.GetJob(ctx, failed.ID)
	if gotFailed.Status != "failed" || gotFailed.LastError != "first failure" {
		t.Fatalf("retry transition was not rolled back: %+v", gotFailed)
	}
}

func TestBackgroundWorkerForcesTerminalStateWhenTerminalEventFails(t *testing.T) {
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
	if _, err := conn.Exec(`
		CREATE TRIGGER reject_terminal_job_events
		BEFORE INSERT ON job_events
		WHEN NEW.stage IN ('succeeded', 'failed', 'canceled')
		BEGIN SELECT RAISE(ABORT, 'reject terminal event'); END;
	`); err != nil {
		t.Fatal(err)
	}

	t.Run("succeeded", func(t *testing.T) {
		job, err := application.StartRebuildSearchIndex(ctx)
		if err != nil {
			t.Fatal(err)
		}
		job = waitForTerminalJob(t, application, job.ID)
		if job.Status != "succeeded" || !strings.Contains(job.ResultJSON, "indexed_pages") {
			t.Fatalf("job was not forced to succeeded: %+v", job)
		}
	})

	t.Run("failed", func(t *testing.T) {
		started, err := application.StartImport(ctx, app.ImportOptions{Title: "终态事件失败"}, app.ImportFile{
			Name: "unsupported.bin", Reader: bytes.NewReader([]byte("not an image")),
		})
		if err != nil {
			t.Fatal(err)
		}
		job := waitForTerminalJob(t, application, started.Job.ID)
		if job.Status != "failed" || job.LastError == "" {
			t.Fatalf("job was not forced to failed: %+v", job)
		}
	})
}

func waitForTerminalJob(t *testing.T, application *app.App, jobID string) app.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := application.Store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" || job.Status == "failed" || job.Status == "canceled" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", jobID)
	return app.Job{}
}
