package app_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
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

	doc, err := application.ImportDocument(ctx, "page.png", bytes.NewReader(testPNG(t)), app.ImportOptions{Title: "测试手稿"})
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

	start, err := application.StartRecognition(ctx, doc.ID)
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
