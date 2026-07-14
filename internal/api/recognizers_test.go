package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
	"github.com/lieyan/firescribe/internal/updater"
)

func TestAlignedCandidateMergeAPIExposesSourcesAndSegments(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, page_count, created_at, updated_at) VALUES ('doc-align', '对齐', 'review_pending', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at) VALUES ('page-align', 'doc-align', 1, 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, config_json, status, total_pages, done_pages, failed_pages, created_at) VALUES ('run-align', 'doc-align', 'mock', 'mock', 'v1', '{}', 'succeeded', 1, 1, 0, '2026-01-01T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, raw_json, metadata_json, created_at) VALUES ('result-align', 'run-align', 'page-align', '甲乙', '{}', '{}', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	router := New(application, "", nil, UpdateRuntime{}).Routes()
	request := httptest.NewRequest(http.MethodPost, "/api/pages/page-align/candidate-merges", strings.NewReader(`{"segments":[{"source_result_id":"result-align","source_start":0,"source_end":2,"text":"甲乙"}]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var created app.CandidateMerge
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/text-versions/"+created.TextVersionID+"/candidate-merge", nil))
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), `"source_result_id":"result-align"`) || !strings.Contains(getRecorder.Body.String(), `"sources":[`) {
		t.Fatalf("get status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
}

func TestRecognizerProfileMutationsRequireAdminTokenAndNeverEchoKey(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	router := New(app.New(app.NewStore(conn), files, recognizer.MockRecognizer{}), "", nil,
		UpdateRuntime{Config: updater.Config{AdminToken: "admin-secret"}}).Routes()
	body := []byte(`{"name":"safe OpenAI","driver":"openai-compatible","base_url":"https://api.example.com/v1","model":"vision-model","api_key":"provider-secret","params_json":"{}","is_default":true}`)

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/recognizer-profiles", bytes.NewReader(body))
	unauthorized.RemoteAddr = "192.0.2.10:12345"
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body=%s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodPost, "/api/recognizer-profiles", bytes.NewReader(body))
	authorized.RemoteAddr = "192.0.2.10:12345"
	authorized.Header.Set("Content-Type", "application/json")
	authorized.Header.Set("X-Admin-Token", "admin-secret")
	authorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusCreated {
		t.Fatalf("authorized status = %d, body=%s", authorizedRecorder.Code, authorizedRecorder.Body.String())
	}
	if strings.Contains(authorizedRecorder.Body.String(), "provider-secret") || strings.Contains(authorizedRecorder.Body.String(), `"api_key"`) {
		t.Fatalf("create response leaked key: %s", authorizedRecorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/recognizer-profiles", nil)
	listRequest.RemoteAddr = "192.0.2.10:12345"
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK || strings.Contains(listRecorder.Body.String(), "provider-secret") || strings.Contains(listRecorder.Body.String(), `"api_key"`) {
		t.Fatalf("unsafe list response status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	if !strings.Contains(listRecorder.Body.String(), `"api_key_set":true`) {
		t.Fatalf("list response does not expose safe key-set flag: %s", listRecorder.Body.String())
	}
}

func TestMalformedRecognitionRequestDoesNotStartJob(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, page_count, created_at, updated_at)
		VALUES ('doc_bad_request', '坏请求', 'ready', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at)
		VALUES ('page_bad_request', 'doc_bad_request', 1, 'extracted', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	router := New(application, "", nil).Routes()
	request := httptest.NewRequest(http.MethodPost, "/api/documents/doc_bad_request/recognition-runs", strings.NewReader(`{"image_source":`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	jobs, err := application.Store.ListJobs(request.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("malformed request started jobs: %+v", jobs)
	}
}
