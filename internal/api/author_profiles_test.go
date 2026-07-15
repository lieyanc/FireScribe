package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestAuthorProfilesAPITrainingDownload(t *testing.T) {
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
	handler := authedHandler(t, api.New(application, "", nil).Routes())

	create := authorAPIRequest(t, handler, http.MethodPost, "/api/author-profiles", map[string]any{"name": "鲁迅", "notes": "测试档案"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var profile app.AuthorProfile
	if err := json.NewDecoder(create.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	term := authorAPIRequest(t, handler, http.MethodPost, "/api/author-profiles/"+profile.ID+"/terms", map[string]any{"term": "藤野先生", "replacement": "滕野先生", "weight": 2})
	if term.Code != http.StatusCreated {
		t.Fatalf("term status = %d, body = %s", term.Code, term.Body.String())
	}

	doc := app.Document{ID: "doc-api-author", Title: "手稿", Status: "ready", PageCount: 1, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := application.Store.CreateDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO assets(id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at)
		VALUES ('api-image', 'page', 'api-sha', 'page.png', 'image/png', 10, 'assets/page.png', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, image_asset_id, status, created_at, updated_at)
		VALUES ('api-page', 'doc-api-author', 1, 'api-image', 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, status, created_at)
		VALUES ('api-run', 'doc-api-author', 'provider-a', 'model-a', 'prompt-a', 'succeeded', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, raw_json, created_at)
		VALUES ('api-result', 'api-run', 'api-page', '滕野先生', '{}', '2026-01-01T00:00:01Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, source_result_id, text, status, created_by, created_at)
		VALUES ('api-final', 'doc-api-author', 'api-page', 'final', 'api-result', '藤野先生', 'verified', 'user', '2026-01-01T00:00:02Z');
	`); err != nil {
		t.Fatal(err)
	}
	link := authorAPIRequest(t, handler, http.MethodPut, "/api/documents/"+doc.ID+"/author-profile", map[string]any{"profile_id": profile.ID})
	if link.Code != http.StatusOK {
		t.Fatalf("link status = %d, body = %s", link.Code, link.Body.String())
	}
	metricsResponse := authorAPIRequest(t, handler, http.MethodGet, "/api/author-profiles/"+profile.ID+"/metrics", nil)
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, body = %s", metricsResponse.Code, metricsResponse.Body.String())
	}
	var metrics app.AuthorRecognitionMetrics
	if err := json.NewDecoder(metricsResponse.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.SampleCount != 1 || metrics.EditDistance != 1 || metrics.SubstitutionCount != 1 || metrics.CER != 0.25 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if len(metrics.Groups) != 1 || metrics.Groups[0].Model != "model-a" || metrics.Groups[0].PromptVersion != "prompt-a" {
		t.Fatalf("metric groups = %+v", metrics.Groups)
	}

	download := authorAPIRequest(t, handler, http.MethodGet, "/api/author-profiles/"+profile.ID+"/training-data", nil)
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", download.Code, download.Body.String())
	}
	if contentType := download.Header().Get("Content-Type"); !strings.Contains(contentType, "application/x-ndjson") {
		t.Fatalf("content type = %q", contentType)
	}
	body := download.Body.String()
	for _, expected := range []string{`"input":"滕野先生"`, `"output":"藤野先生"`, `"image_asset_id":"api-image"`, `"image_url":"/api/pages/api-page/image"`, `"provider":"provider-a"`, `"model":"model-a"`, `"prompt_version":"prompt-a"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("training JSONL missing %s: %s", expected, body)
		}
	}
}

func authorAPIRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&raw).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &raw)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
