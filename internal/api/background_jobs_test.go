package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestImportEndpointQueuesJobWithoutBreakingDocumentShape(t *testing.T) {
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
	server := api.New(application, "", nil)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "page.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(apiTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("title", "异步导入"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/documents/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ID     string  `json:"id"`
		Title  string  `json:"title"`
		Status string  `json:"status"`
		Job    app.Job `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID == "" || response.Title != "异步导入" || response.Status != "importing" || response.Job.Type != "import_document" {
		t.Fatalf("unexpected response: %+v", response)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := application.Store.GetJob(context.Background(), response.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			eventsReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+response.Job.ID+"/events", nil)
			eventsRecorder := httptest.NewRecorder()
			server.Routes().ServeHTTP(eventsRecorder, eventsReq)
			if eventsRecorder.Code != http.StatusOK {
				t.Fatalf("events status = %d, body = %s", eventsRecorder.Code, eventsRecorder.Body.String())
			}
			var events []app.JobEvent
			if err := json.Unmarshal(eventsRecorder.Body.Bytes(), &events); err != nil || len(events) == 0 {
				t.Fatalf("events = %+v, err = %v", events, err)
			}
			return
		}
		if job.Status == "failed" {
			t.Fatalf("job failed: %s", job.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("import job did not finish")
}

func TestExportEndpointAcceptsAdvancedOptions(t *testing.T) {
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
	doc, err := application.ImportDocument(ctx, app.ImportOptions{Title: "API DOCX"}, app.ImportFile{Name: "page.png", Reader: bytes.NewReader(apiTestPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := application.Store.ListPages(ctx, doc.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v, err = %v", pages, err)
	}
	if _, err := application.SaveTextVersion(ctx, app.TextVersion{DocumentID: doc.ID, PageID: pages[0].ID, Kind: "final", Status: "verified", Text: "接口导出定稿"}); err != nil {
		t.Fatal(err)
	}
	server := api.New(application, "", nil)
	requestBody := `{"format":"docx","include_page_numbers":true,"text_scope":"final","include_annotations":true,"include_uncertain":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/documents/"+doc.ID+"/exports", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var started app.ExportStart
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.Format != "docx" || started.TextScope != "final" || !started.IncludeAnnotations || !started.IncludeUncertain {
		t.Fatalf("response options = %+v", started)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		exported, err := application.Store.GetExport(ctx, started.ID)
		if err != nil {
			t.Fatal(err)
		}
		if exported.Status == "succeeded" {
			raw, err := os.ReadFile(application.Storage.Abs(exported.StoragePath))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(raw, []byte("PK")) {
				t.Fatalf("DOCX header = %q", raw[:min(len(raw), 8)])
			}
			return
		}
		if exported.Status == "failed" {
			t.Fatalf("export failed: %s", exported.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("export job did not finish")
}

func apiTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 50, G: uint8(80 + x), B: uint8(100 + y), A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
