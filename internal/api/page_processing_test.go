package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestPageProcessingEndpointsStartAndPreviewDerivedImage(t *testing.T) {
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
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "处理 API"},
		app.ImportFile{Name: "page.png", Reader: bytes.NewReader(apiTestPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	server := authedHandler(t, api.New(application, "", nil).Routes())
	req := httptest.NewRequest(http.MethodPost, "/api/documents/"+doc.ID+"/page-processing-runs", bytes.NewBufferString(`{"config":{"auto_crop":true,"normalize_background":true,"deskew":true,"enhance_contrast":true,"detect_segments":true}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var start app.PageProcessingStart
	if err := json.Unmarshal(recorder.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		job, err := application.Store.GetJob(ctx, start.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			break
		}
		if job.Status == "failed" {
			t.Fatalf("processing failed: %s", job.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}

	previewReq := httptest.NewRequest(http.MethodGet, "/api/pages/"+pages[0].ID+"/processing-preview", nil)
	previewRecorder := httptest.NewRecorder()
	server.ServeHTTP(previewRecorder, previewReq)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", previewRecorder.Code, previewRecorder.Body.String())
	}
	var preview app.PageProcessingPreview
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Result == nil || preview.Result.OutputAssetID == "" || preview.Result.EnhancedURL == "" {
		t.Fatalf("preview = %+v", preview)
	}
}
