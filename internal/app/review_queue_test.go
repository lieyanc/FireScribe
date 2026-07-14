package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
)

func TestReviewQueueUsesEffectiveConfidenceAndOpenUncertainAnnotations(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, page_count, created_at, updated_at)
		VALUES ('doc', '低置信测试', 'reviewing', 6, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at) VALUES
		  ('low', 'doc', 1, 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z'),
		  ('manual', 'doc', 2, 'reviewing', '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z'),
		  ('uncertain', 'doc', 3, 'verified', '2026-01-01T00:00:00Z', '2026-01-01T00:00:03Z'),
		  ('high-page-low-token', 'doc', 4, 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:04Z'),
		  ('token-only', 'doc', 5, 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:05Z'),
		  ('merged', 'doc', 6, 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:06Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, status, created_at)
		VALUES ('run', 'doc', 'provider', 'model', 'succeeded', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, confidence, raw_json, created_at) VALUES
		  ('res-low', 'run', 'low', '低置信', 0.35, '{"choices":[{"logprobs":{"content":[{"token":"低","logprob":-0.1},{"token":"置信","logprob":-2}]}}]}', '2026-01-01T00:00:01Z'),
		  ('res-manual', 'run', 'manual', '低置信但已人工处理', 0.20, '', '2026-01-01T00:00:02Z'),
		  ('res-final', 'run', 'uncertain', '已定稿但仍存疑', 0.95, '', '2026-01-01T00:00:03Z'),
		  ('res-high-token', 'run', 'high-page-low-token', '高分低词', 0.95, '{"choices":[{"logprobs":{"content":[{"token":"高分","logprob":-0.1},{"token":"低词","logprob":-3}]}}]}', '2026-01-01T00:00:04Z'),
		  ('res-token-only', 'run', 'token-only', '仅词分数', NULL, '{"choices":[{"logprobs":{"content":[{"token":"仅词","logprob":-0.1},{"token":"分数","logprob":-2}]}}]}', '2026-01-01T00:00:05Z'),
		  ('res-merged-source', 'run', 'merged', '甲乙', 0.95, '{"choices":[{"logprobs":{"content":[{"token":"甲","logprob":-0.1},{"token":"乙","logprob":-3}]}}]}', '2026-01-01T00:00:06Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, source_result_id, text, status, created_at) VALUES
		  ('candidate-low', 'doc', 'low', 'candidate', 'res-low', '低置信', 'draft', '2026-01-01T00:00:01Z'),
		  ('manual-version', 'doc', 'manual', 'manual', 'res-manual', '已人工处理', 'draft', '2026-01-01T00:00:03Z'),
		  ('final-version', 'doc', 'uncertain', 'final', 'res-final', '已定稿', 'verified', '2026-01-01T00:00:04Z'),
		  ('candidate-high-token', 'doc', 'high-page-low-token', 'candidate', 'res-high-token', '高分低词', 'draft', '2026-01-01T00:00:04Z'),
		  ('candidate-token-only', 'doc', 'token-only', 'candidate', 'res-token-only', '仅词分数', 'draft', '2026-01-01T00:00:05Z'),
		  ('candidate-merged', 'doc', 'merged', 'candidate', NULL, '甲乙', 'draft', '2026-01-01T00:00:06Z');
		INSERT INTO candidate_merges(id, page_id, text_version_id, source_result_ids_json, driver, prompt_version, prompt_hash, created_at)
		VALUES ('merge-review', 'merged', 'candidate-merged', '["res-merged-source"]', 'manual-aligned', 'prompt', 'hash', '2026-01-01T00:00:06Z');
		INSERT INTO candidate_merge_segments(id, candidate_merge_id, ordinal, source_result_id, source_start, source_end, output_start, output_end, text)
		VALUES ('merge-review-segment', 'merge-review', 0, 'res-merged-source', 0, 2, 0, 2, '甲乙');
		INSERT INTO annotations(id, document_id, page_id, kind, status, body, created_at, updated_at)
		VALUES ('ann', 'doc', 'uncertain', 'uncertain_text', 'open', '仍需核对', '2026-01-01T00:00:05Z', '2026-01-01T00:00:05Z');
	`); err != nil {
		t.Fatal(err)
	}

	items, err := app.NewStore(conn).ListReviewQueue(ctx, 0.5, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("queue length = %d, want 5: %+v", len(items), items)
	}
	if items[0].PageID != "uncertain" || items[0].OpenUncertainCount != 1 {
		t.Fatalf("first queue item = %+v, want open uncertain page first", items[0])
	}
	byPage := map[string]app.ReviewQueueItem{}
	for _, item := range items {
		byPage[item.PageID] = item
	}
	if byPage["low"].Confidence == nil || *byPage["low"].Confidence != 0.35 || len(byPage["low"].LowConfidence) != 1 {
		t.Fatalf("page-level low item = %+v", byPage["low"])
	}
	if got := byPage["high-page-low-token"].LowConfidence; len(got) != 1 || got[0].Text != "低词" {
		t.Fatalf("high-page low-token segments = %+v", got)
	}
	if got := byPage["token-only"].LowConfidence; len(got) != 1 || got[0].Text != "分数" {
		t.Fatalf("token-only segments = %+v", got)
	}
	if got := byPage["merged"].LowConfidence; len(got) != 1 || got[0].Text != "甲乙" || got[0].Start != 0 || got[0].End != 2 || byPage["merged"].Confidence != nil {
		t.Fatalf("candidate-merge segments = %+v item=%+v", got, byPage["merged"])
	}
}
