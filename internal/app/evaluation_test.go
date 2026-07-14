package app_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
)

func TestEvaluationMetricsUseBenchmarkFinalTextAndLowConfidenceHits(t *testing.T) {
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
		INSERT INTO documents(id, title, status, page_count, created_at, updated_at) VALUES
		  ('benchmark-doc', '基准手稿', 'ready', 1, '2026-07-14T00:00:00Z', '2026-07-14T00:00:00Z'),
		  ('normal-doc', '普通手稿', 'ready', 1, '2026-07-14T00:00:00Z', '2026-07-14T00:00:00Z');
		INSERT INTO tags(id, name, color) VALUES ('benchmark-tag', 'benchmark', '');
		INSERT INTO document_tags(document_id, tag_id) VALUES ('benchmark-doc', 'benchmark-tag');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at) VALUES
		  ('benchmark-page', 'benchmark-doc', 1, 'verified', '2026-07-14T00:00:00Z', '2026-07-14T00:02:00Z'),
		  ('normal-page', 'normal-doc', 1, 'verified', '2026-07-14T00:00:00Z', '2026-07-14T00:02:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, status, created_at) VALUES
		  ('benchmark-run', 'benchmark-doc', 'provider-a', 'model-a', 'prompt-a', 'succeeded', '2026-07-14T00:00:05Z'),
		  ('normal-run', 'normal-doc', 'provider-b', 'model-b', 'prompt-b', 'succeeded', '2026-07-14T00:00:05Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, raw_json, created_at) VALUES
		  ('benchmark-result', 'benchmark-run', 'benchmark-page', '甲乙', '{"choices":[{"logprobs":{"content":[{"token":"甲","logprob":-0.1},{"token":"乙","logprob":-2}]}}]}', '2026-07-14T00:00:10Z'),
		  ('normal-result', 'normal-run', 'normal-page', '原文', '{}', '2026-07-14T00:00:10Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, source_result_id, text, status, created_at) VALUES
		  ('benchmark-final', 'benchmark-doc', 'benchmark-page', 'final', 'benchmark-result', '甲丙', 'verified', '2026-07-14T00:02:00Z'),
		  ('normal-final', 'normal-doc', 'normal-page', 'final', 'normal-result', '正文', 'verified', '2026-07-14T00:02:00Z');
		INSERT INTO review_activity_sessions(id, document_id, page_id, active_seconds, started_at, updated_at, finished_at)
		VALUES ('review-session', 'benchmark-doc', 'benchmark-page', 30, '2026-07-14T00:01:00Z', '2026-07-14T00:01:30Z', '2026-07-14T00:01:30Z');
	`); err != nil {
		t.Fatal(err)
	}

	store := app.NewStore(conn)
	benchmark, err := store.GetEvaluationMetrics(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if benchmark.SampleCount != 1 || benchmark.EditDistance != 1 || benchmark.CER != 0.5 {
		t.Fatalf("benchmark metrics = %+v", benchmark)
	}
	if benchmark.LowConfidenceItemCount != 1 || benchmark.LowConfidenceHitCount != 1 || benchmark.LowConfidenceHitRate != 1 {
		t.Fatalf("low-confidence metrics = %+v", benchmark)
	}
	if math.Abs(benchmark.AverageCandidateSeconds-10) > 0.01 || math.Abs(benchmark.AverageReviewSeconds-30) > 0.01 || math.Abs(benchmark.AverageTurnaroundSeconds-110) > 0.01 {
		t.Fatalf("timings = candidate %v review %v turnaround %v", benchmark.AverageCandidateSeconds, benchmark.AverageReviewSeconds, benchmark.AverageTurnaroundSeconds)
	}

	all, err := store.GetEvaluationMetrics(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if all.SampleCount != 2 {
		t.Fatalf("all sample count = %d, want 2", all.SampleCount)
	}
}

func TestEvaluationMetricsFollowActualCandidateAncestry(t *testing.T) {
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
		VALUES ('doc', '合并评测', 'ready', 1, '2026-07-14T00:00:00Z', '2026-07-14T00:03:00Z');
		INSERT INTO pages(id, document_id, page_no, status, created_at, updated_at)
		VALUES ('page', 'doc', 1, 'verified', '2026-07-14T00:00:00Z', '2026-07-14T00:03:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, status, created_at)
		VALUES ('run', 'doc', 'raw-provider', 'raw-model', 'raw-prompt', 'succeeded', '2026-07-14T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, created_at)
		VALUES ('result', 'run', 'page', '完全不同的原始结果', '2026-07-14T00:00:10Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, text, status, created_at) VALUES
		  ('old-candidate', 'doc', 'page', 'candidate', '旧稿', 'draft', '2026-07-14T00:00:30Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, base_version_id, text, status, created_at) VALUES
		  ('candidate', 'doc', 'page', 'candidate', 'old-candidate', '合并稿', 'draft', '2026-07-14T00:01:00Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, base_version_id, text, status, created_at) VALUES
		  ('manual', 'doc', 'page', 'manual', 'candidate', '合并高', 'draft', '2026-07-14T00:02:00Z'),
		  ('final', 'doc', 'page', 'final', 'manual', '合并高', 'verified', '2026-07-14T00:03:00Z');
		INSERT INTO candidate_merges(id, page_id, text_version_id, source_result_ids_json, driver, prompt_version, prompt_hash, created_at)
		VALUES ('merge', 'page', 'candidate', '["result"]', 'manual-aligned', 'merge-prompt', 'hash', '2026-07-14T00:01:00Z');
		INSERT INTO review_activity_sessions(id, document_id, page_id, active_seconds, started_at, updated_at, finished_at)
		VALUES ('merge-session', 'doc', 'page', 45, '2026-07-14T00:01:30Z', '2026-07-14T00:02:15Z', '2026-07-14T00:02:15Z');
	`); err != nil {
		t.Fatal(err)
	}

	metrics, err := app.NewStore(conn).GetEvaluationMetrics(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.SampleCount != 1 || metrics.EditDistance != 1 || metrics.CER != 1.0/3.0 {
		t.Fatalf("metrics should compare candidate to final: %+v", metrics)
	}
	if len(metrics.Groups) != 1 || metrics.Groups[0].Provider != "manual-aligned" || metrics.Groups[0].Model != "candidate-merge" || metrics.Groups[0].PromptVersion != "merge-prompt" {
		t.Fatalf("merge attribution = %+v", metrics.Groups)
	}
	if math.Abs(metrics.AverageCandidateSeconds-60) > 0.01 || math.Abs(metrics.AverageReviewSeconds-45) > 0.01 || math.Abs(metrics.AverageTurnaroundSeconds-120) > 0.01 {
		t.Fatalf("candidate ancestry timings = %+v", metrics)
	}
}
