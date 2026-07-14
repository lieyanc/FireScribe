package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestEvaluationAPI(t *testing.T) {
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
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO documents(id, title, status, page_count, created_at, updated_at) VALUES ('doc', '文档', 'ready', 1, '2026-07-14T00:00:00Z', '2026-07-14T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at) VALUES ('page', 'doc', 1, 'verified', '2026-07-14T00:00:00Z', '2026-07-14T00:01:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, status, created_at) VALUES ('run', 'doc', 'mock', 'mock-vlm', 'succeeded', '2026-07-14T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, created_at) VALUES ('result', 'run', 'page', '原稿', '2026-07-14T00:00:10Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, source_result_id, text, status, created_at) VALUES ('final', 'doc', 'page', 'final', 'result', '定稿', 'verified', '2026-07-14T00:01:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	handler := api.New(application, "", nil).Routes()
	response := authorAPIRequest(t, handler, http.MethodGet, "/api/evaluation?benchmark_only=false", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metrics app.EvaluationMetrics
	if err := json.NewDecoder(response.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.SampleCount != 1 || metrics.EditDistance == 0 {
		t.Fatalf("metrics = %+v", metrics)
	}
}
