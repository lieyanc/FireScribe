package app_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
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

func TestMVPWorkflowImportRecognizeReviewSearchExport(t *testing.T) {
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

	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "测试手稿"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount != 1 || doc.Status != "ready" {
		t.Fatalf("unexpected imported doc: %+v", doc)
	}
	if _, err := application.Store.SetDocumentTags(ctx, doc.ID, []string{"手稿", "家庭档案", "Draft"}); err != nil {
		t.Fatal(err)
	}
	if tags, err := application.Store.SetDocumentTags(ctx, doc.ID, []string{"手稿", "家庭档案", "draft"}); err != nil {
		t.Fatal(err)
	} else if len(tags) != 3 {
		t.Fatalf("expected case-insensitive tag reuse, got %+v", tags)
	}
	allTags, err := application.Store.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(allTags) != 3 {
		t.Fatalf("expected 3 distinct tags, got %+v", allTags)
	}
	if _, err := application.Store.SetDocumentTags(ctx, doc.ID, []string{"手稿", "家庭档案"}); err != nil {
		t.Fatal(err)
	}
	taggedDocs, err := application.Store.ListDocuments(ctx, app.DocumentFilter{Tag: "手稿"})
	if err != nil {
		t.Fatal(err)
	}
	if len(taggedDocs) != 1 || taggedDocs[0].ID != doc.ID || len(taggedDocs[0].Tags) != 2 {
		t.Fatalf("unexpected tagged docs: %+v", taggedDocs)
	}
	assets, err := application.Store.ListDocumentAssets(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) < 2 {
		t.Fatalf("expected original/page assets, got %+v", assets)
	}

	start, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, application, start.Job.ID)

	pages, err := application.Store.ListPageDetails(ctx, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || !pages[0].HasCandidate {
		t.Fatalf("unexpected page details: %+v", pages)
	}
	recognitionResults, err := application.Store.ListRecognitionResults(ctx, pages[0].PageID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recognitionResults) != 1 || recognitionResults[0].PromptVersion != "mock" || recognitionResults[0].ConfigJSON == "" {
		t.Fatalf("recognition audit fields missing: %+v", recognitionResults)
	}
	if !strings.Contains(recognitionResults[0].MetadataJSON, `"image_asset_id"`) {
		t.Fatalf("recognition input metadata missing: %s", recognitionResults[0].MetadataJSON)
	}

	_, err = application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID,
		PageID:     pages[0].PageID,
		Kind:       "final",
		Text:       "火光文字已经人工确认。",
		Status:     "verified",
		CreatedBy:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := application.Store.Search(ctx, "火光文")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].PageID != pages[0].PageID {
		t.Fatalf("unexpected search results: %+v", results)
	}

	exported, err := application.ExportDocument(ctx, doc.ID, "md", true)
	if err != nil {
		t.Fatal(err)
	}
	if exported.DownloadURL == "" || exported.StoragePath == "" {
		t.Fatalf("unexpected export: %+v", exported)
	}

	annotation, err := application.CreateAnnotation(ctx, app.Annotation{
		DocumentID: doc.ID,
		PageID:     pages[0].PageID,
		Kind:       "uncertain_text",
		Body:       "这个词需要复核",
		AnchorJSON: `{"type":"text_range","start":0,"end":2}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := application.Store.ListAnnotations(ctx, doc.ID, pages[0].PageID)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 || annotations[0].ID != annotation.ID {
		t.Fatalf("unexpected annotations: %+v", annotations)
	}
	status := "resolved"
	updated, err := application.Store.PatchAnnotation(ctx, annotation.ID, &status, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "resolved" {
		t.Fatalf("annotation status = %q", updated.Status)
	}
}

func TestEffectiveTextVersionReopensFinalAndDrivesSearchExportStatus(t *testing.T) {
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

	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "版本一致性"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	start, err := application.StartRecognition(ctx, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, application, start.Job.ID)
	assertDocumentStatus(t, application, doc.ID, "review_pending")
	pages, err := application.Store.ListPageDetails(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	pageID := pages[0].PageID

	final, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pageID, Kind: "final", Text: "旧定稿关键词", Status: "verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertDocumentStatus(t, application, doc.ID, "finalized")

	// Keep the timestamps in separate SQLite julianday ticks so this test is
	// deterministic even on platforms with coarse clock precision.
	time.Sleep(2 * time.Millisecond)
	manual, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pageID, Kind: "manual", Text: "新草稿关键词", Status: "draft", BaseVersionID: final.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	versionID, text, err := application.Store.LatestTextForPage(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	if versionID != manual.ID || text != manual.Text {
		t.Fatalf("effective text = (%q, %q), want manual (%q, %q)", versionID, text, manual.ID, manual.Text)
	}
	detail, err := application.Store.GetPageDetail(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.HasManual || detail.HasFinal || detail.HasCandidate {
		t.Fatalf("effective flags after reopening = candidate:%v manual:%v final:%v", detail.HasCandidate, detail.HasManual, detail.HasFinal)
	}
	assertDocumentStatus(t, application, doc.ID, "reviewing")
	assertSearchCount(t, application, "旧定稿", 0)
	assertSearchCount(t, application, "新草稿", 1)
	assertSearchCount(t, application, "旧定", 0)
	assertSearchCount(t, application, "新草", 1)

	// A later OCR candidate must not replace the human draft in the effective
	// version or in FTS, because candidate is a lower tier.
	time.Sleep(2 * time.Millisecond)
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pageID, Kind: "candidate", Text: "后来候选关键词", Status: "draft",
	}); err != nil {
		t.Fatal(err)
	}
	assertSearchCount(t, application, "后来候选", 0)
	assertSearchCount(t, application, "新草稿", 1)

	exported, err := application.ExportDocument(ctx, doc.ID, "txt", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(application.Storage.Abs(exported.StoragePath))
	if err != nil {
		t.Fatal(err)
	}
	if output := string(raw); !strings.Contains(output, manual.Text) || strings.Contains(output, final.Text) {
		t.Fatalf("export used stale text: %q", output)
	}

	time.Sleep(2 * time.Millisecond)
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pageID, Kind: "final", Text: "新定稿关键词", Status: "verified", BaseVersionID: manual.ID,
	}); err != nil {
		t.Fatal(err)
	}
	detail, err = application.Store.GetPageDetail(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.HasManual || !detail.HasFinal {
		t.Fatalf("effective flags after refinalizing = manual:%v final:%v", detail.HasManual, detail.HasFinal)
	}
	assertDocumentStatus(t, application, doc.ID, "finalized")
	assertSearchCount(t, application, "新草稿", 0)
	assertSearchCount(t, application, "新定稿", 1)
}

func TestShortSearchUsesSameKindsAsFTS(t *testing.T) {
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
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "短词策略"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(testPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{
		DocumentID: doc.ID, PageID: pages[0].ID, Kind: "raw_selected", Text: "原选短词", Status: "draft",
	}); err != nil {
		t.Fatal(err)
	}
	assertSearchCount(t, application, "原选", 0)
}

func assertDocumentStatus(t *testing.T, application *app.App, documentID, want string) {
	t.Helper()
	doc, err := application.Store.GetDocument(context.Background(), documentID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != want {
		t.Fatalf("document status = %q, want %q", doc.Status, want)
	}
}

func assertSearchCount(t *testing.T, application *app.App, query string, want int) {
	t.Helper()
	results, err := application.Store.Search(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != want {
		t.Fatalf("search %q returned %d results, want %d: %+v", query, len(results), want, results)
	}
}

func waitForJob(t *testing.T, application *app.App, jobID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := application.Store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			return
		}
		if job.Status == "failed" {
			t.Fatalf("job failed: %s", job.LastError)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 + x*4), G: uint8(80 + y*3), B: 150, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
