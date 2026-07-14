package app_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
)

func TestEvaluateTextCorrectionReportsCERAndRuneRanges(t *testing.T) {
	metric := app.EvaluateTextCorrection("甲滕野先生啊", "甲藤野先生")
	if metric.SourceCharCount != 6 || metric.ReferenceCharCount != 5 || metric.EditDistance != 2 {
		t.Fatalf("counts = %+v", metric)
	}
	if metric.SubstitutionCount != 1 || metric.OmissionCount != 0 || metric.AdditionCount != 1 {
		t.Fatalf("operations = %+v", metric)
	}
	if math.Abs(metric.CER-0.4) > 0.000001 {
		t.Fatalf("CER = %f, want 0.4", metric.CER)
	}
	if len(metric.Edits) != 2 || metric.Edits[0].SourceStart != 1 || metric.Edits[0].SourceEnd != 2 || metric.Edits[0].Corrected != "藤" {
		t.Fatalf("edits = %+v", metric.Edits)
	}
	if metric.Edits[1].Type != "addition" || metric.Edits[1].SourceStart != 5 || metric.Edits[1].SourceEnd != 6 {
		t.Fatalf("addition range = %+v", metric.Edits[1])
	}

	omission := app.EvaluateTextCorrection("鲁先生", "鲁迅先生")
	if omission.EditDistance != 1 || omission.OmissionCount != 1 || len(omission.Edits) != 1 {
		t.Fatalf("omission metric = %+v", omission)
	}
	if omission.Edits[0].SourceStart != 1 || omission.Edits[0].SourceEnd != 1 || omission.Edits[0].ReferenceStart != 1 {
		t.Fatalf("omission range = %+v", omission.Edits[0])
	}
}

func TestEvaluateTextCorrectionMatchesLevenshteinDistance(t *testing.T) {
	values := []string{""}
	for length := 1; length <= 4; length++ {
		var build func(string, int)
		build = func(prefix string, remaining int) {
			if remaining == 0 {
				values = append(values, prefix)
				return
			}
			for _, value := range []string{"甲", "乙", "丙"} {
				build(prefix+value, remaining-1)
			}
		}
		build("", length)
	}
	for _, source := range values {
		for _, reference := range values {
			metric := app.EvaluateTextCorrection(source, reference)
			want := testLevenshteinDistance([]rune(source), []rune(reference))
			if metric.EditDistance != want || metric.SubstitutionCount+metric.OmissionCount+metric.AdditionCount != want {
				t.Fatalf("%q -> %q: metric = %+v, distance = %d", source, reference, metric, want)
			}
		}
	}
}

func testLevenshteinDistance(source, reference []rune) int {
	previous := make([]int, len(reference)+1)
	for index := range previous {
		previous[index] = index
	}
	current := make([]int, len(reference)+1)
	for sourceIndex, sourceRune := range source {
		current[0] = sourceIndex + 1
		for referenceIndex, referenceRune := range reference {
			cost := 0
			if sourceRune != referenceRune {
				cost = 1
			}
			current[referenceIndex+1] = min(previous[referenceIndex+1]+1, current[referenceIndex]+1, previous[referenceIndex]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(reference)]
}

func TestAuthorRecognitionMetricsAggregatesModelsTrendsAndCommonErrors(t *testing.T) {
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
	profile, err := store.CreateAuthorProfile(ctx, "统计作者", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO documents(id, title, status, page_count, author_profile_id, created_at, updated_at)
		VALUES ('metric-doc', '统计文档', 'ready', 3, ?, '2026-01-01T00:00:00Z', '2026-01-03T00:00:00Z');
		INSERT INTO assets(id, kind, sha256, original_name, mime_type, byte_size, storage_path, created_at)
		VALUES ('metric-image', 'page', 'metric-sha', 'page.png', 'image/png', 10, 'assets/page.png', '2026-01-01T00:00:00Z');
		INSERT INTO pages(id, document_id, page_no, image_asset_id, status, created_at, updated_at) VALUES
		  ('metric-page-1', 'metric-doc', 1, 'metric-image', 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		  ('metric-page-2', 'metric-doc', 2, 'metric-image', 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		  ('metric-page-3', 'metric-doc', 3, 'metric-image', 'recognized', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_runs(id, document_id, provider, model, prompt_version, status, created_at) VALUES
		  ('metric-run-1', 'metric-doc', 'provider-a', 'model-a', 'prompt-v1', 'succeeded', '2026-01-01T00:00:00Z'),
		  ('metric-run-2', 'metric-doc', 'provider-a', 'model-b', 'prompt-v2', 'succeeded', '2026-01-01T00:00:00Z');
		INSERT INTO recognition_results(id, run_id, page_id, text, raw_json, created_at) VALUES
		  ('metric-result-1', 'metric-run-1', 'metric-page-1', '滕野', '{}', '2026-01-01T00:00:00Z'),
		  ('metric-result-2', 'metric-run-1', 'metric-page-2', '鲁先', '{}', '2026-01-01T00:00:00Z'),
		  ('metric-result-3', 'metric-run-2', 'metric-page-3', '鲁迅先生啊', '{}', '2026-01-01T00:00:00Z');
		INSERT INTO text_versions(id, document_id, page_id, kind, source_result_id, text, status, created_by, created_at) VALUES
		  ('metric-text-1', 'metric-doc', 'metric-page-1', 'final', 'metric-result-1', '藤野', 'verified', 'user', '2026-01-01T10:00:00Z'),
		  ('metric-text-2', 'metric-doc', 'metric-page-2', 'final', 'metric-result-2', '鲁迅先生', 'verified', 'user', '2026-01-01T11:00:00Z'),
		  ('metric-text-3', 'metric-doc', 'metric-page-3', 'final', 'metric-result-3', '鲁迅先生', 'verified', 'user', '2026-01-02T10:00:00Z');
		INSERT INTO author_corrections(id, author_profile_id, document_id, page_id, text_version_id, source_result_id, source_text, corrected_text, kind, created_at) VALUES
		  ('metric-correction-1', ?, 'metric-doc', 'metric-page-1', 'metric-text-1', 'metric-result-1', '滕野', '藤野', 'final', '2026-01-01T10:00:00Z'),
		  ('metric-correction-2', ?, 'metric-doc', 'metric-page-2', 'metric-text-2', 'metric-result-2', '鲁先', '鲁迅先生', 'final', '2026-01-01T11:00:00Z'),
		  ('metric-correction-3', ?, 'metric-doc', 'metric-page-3', 'metric-text-3', 'metric-result-3', '鲁迅先生啊', '鲁迅先生', 'final', '2026-01-02T10:00:00Z');
	`, profile.ID, profile.ID, profile.ID, profile.ID); err != nil {
		t.Fatal(err)
	}

	metrics, err := store.GetAuthorRecognitionMetrics(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.SampleCount != 3 || metrics.ReferenceCharCount != 10 || metrics.EditDistance != 4 {
		t.Fatalf("totals = %+v", metrics)
	}
	if math.Abs(metrics.CER-0.4) > 0.000001 || metrics.SubstitutionCount != 1 || metrics.OmissionCount != 2 || metrics.AdditionCount != 1 {
		t.Fatalf("edit summary = %+v", metrics)
	}
	if len(metrics.Groups) != 2 || metrics.Groups[0].Model != "model-b" || metrics.Groups[0].SampleCount != 1 || math.Abs(metrics.Groups[0].CER-0.25) > 0.000001 {
		t.Fatalf("groups = %+v", metrics.Groups)
	}
	if len(metrics.Trend) != 2 || metrics.Trend[0].Date != "2026-01-01" || metrics.Trend[0].SampleCount != 2 || metrics.Trend[1].CER != 0.25 {
		t.Fatalf("trend = %+v", metrics.Trend)
	}
	foundSubstitution := false
	for _, item := range metrics.CommonErrors {
		if item.Type == "substitution" && item.Source == "滕" && item.Corrected == "藤" && item.Count == 1 {
			foundSubstitution = true
		}
	}
	if !foundSubstitution {
		t.Fatalf("common errors = %+v", metrics.CommonErrors)
	}

	stored, err := store.SyncAuthorCorrectionMetrics(ctx, "metric-doc")
	if err != nil || stored != 3 {
		t.Fatalf("sync metrics = %d, %v", stored, err)
	}
	var cached int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM author_correction_metrics`).Scan(&cached); err != nil || cached != 3 {
		t.Fatalf("cached metrics = %d, %v", cached, err)
	}
}
