package app_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
)

func TestAuthorProfileBackfillsCorrectionsAndBuildsBoundedRecognitionContext(t *testing.T) {
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
	document := app.Document{ID: "doc-author", Title: "手稿", Author: "", Status: "ready", PageCount: 1, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := store.CreateDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO assets(id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at)
		VALUES ('image', 'page', 'sha', 'page.png', 'image/png', 10, 'assets/page.png', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, image_asset_id, status, created_at, updated_at)
		VALUES ('page-author', 'doc-author', 1, 'image', 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, status, created_at)
		VALUES ('run-author', 'doc-author', 'openai-compatible', 'vision-model', 'prompt-v1', 'succeeded', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, raw_json, created_at)
		VALUES ('result-author', 'run-author', 'page-author', '張三丰长文本末尾不可进入快照', '{}', '2026-01-01T00:00:01Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, source_result_id, text, status, created_by, created_at)
		VALUES ('final-author', 'doc-author', 'page-author', 'final', 'result-author', '张三丰校对稿', 'verified', 'user', '2026-01-01T00:00:02Z');
	`); err != nil {
		t.Fatal(err)
	}

	profile, err := store.CreateAuthorProfile(ctx, "张先生", "行草，繁简混用")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAuthorTerm(ctx, profile.ID, app.AuthorTerm{Term: "张三丰", Replacement: "張三豐", Note: "人名", Weight: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetDocumentAuthorProfile(ctx, document.ID, profile.ID); err != nil {
		t.Fatal(err)
	}

	corrections, err := store.ListAuthorCorrections(ctx, profile.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(corrections) != 1 {
		t.Fatalf("corrections = %d, want 1", len(corrections))
	}
	if corrections[0].ImageAssetID != "image" || corrections[0].Provider != "openai-compatible" || corrections[0].PromptVersion != "prompt-v1" {
		t.Fatalf("training provenance missing: %+v", corrections[0])
	}

	recognitionContext, err := store.BuildAuthorRecognitionContext(ctx, document.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recognitionContext.PromptContext, "常见误识别“張三豐”应核对为“张三丰”") || !strings.Contains(recognitionContext.PromptContext, "張三丰长文本末尾不可进入快照 → 张三丰校对稿") {
		t.Fatalf("prompt context = %q", recognitionContext.PromptContext)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(recognitionContext.SnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["context_sha256"] == "" || recognitionContext.ProfileID != profile.ID {
		t.Fatalf("snapshot audit fields missing: %s", recognitionContext.SnapshotJSON)
	}

	linked, ok, err := store.GetDocumentAuthorProfile(ctx, document.ID)
	if err != nil || !ok || linked.ID != profile.ID {
		t.Fatalf("linked profile = %+v, %v, %v", linked, ok, err)
	}
	doc, err := store.GetDocument(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Author != profile.Name {
		t.Fatalf("legacy author = %q, want %q", doc.Author, profile.Name)
	}
}
