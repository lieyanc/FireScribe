package app

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/lieyan/firescribe/internal/recognizer"
)

const maxEvaluationSamples = 200

type EvaluationMetrics struct {
	BenchmarkOnly            bool                    `json:"benchmark_only"`
	SampleCount              int                     `json:"sample_count"`
	Truncated                bool                    `json:"truncated"`
	ReferenceCharCount       int                     `json:"reference_char_count"`
	EditDistance             int                     `json:"edit_distance"`
	CER                      float64                 `json:"cer"`
	SubstitutionCount        int                     `json:"substitution_count"`
	OmissionCount            int                     `json:"omission_count"`
	AdditionCount            int                     `json:"addition_count"`
	MissedLineCount          int                     `json:"missed_line_count"`
	GuessedLineCount         int                     `json:"guessed_line_count"`
	ReorderedLineCount       int                     `json:"reordered_line_count"`
	LowConfidenceItemCount   int                     `json:"low_confidence_item_count"`
	LowConfidenceHitCount    int                     `json:"low_confidence_hit_count"`
	LowConfidenceHitRate     float64                 `json:"low_confidence_hit_rate"`
	AverageCandidateSeconds  float64                 `json:"average_candidate_seconds"`
	AverageReviewSeconds     float64                 `json:"average_review_seconds"`
	AverageTurnaroundSeconds float64                 `json:"average_turnaround_seconds"`
	ReviewSampleCount        int                     `json:"review_sample_count"`
	ConfirmedLastHour        int                     `json:"confirmed_last_hour"`
	PagesPerActiveHour       float64                 `json:"pages_per_active_hour"`
	Groups                   []EvaluationMetricGroup `json:"groups"`
	Trend                    []EvaluationMetricTrend `json:"trend"`
	Samples                  []EvaluationSample      `json:"samples"`
}

type EvaluationMetricGroup struct {
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	PromptVersion      string  `json:"prompt_version"`
	SampleCount        int     `json:"sample_count"`
	ReferenceCharCount int     `json:"reference_char_count"`
	EditDistance       int     `json:"edit_distance"`
	CER                float64 `json:"cer"`
}

type EvaluationMetricTrend struct {
	Date               string  `json:"date"`
	SampleCount        int     `json:"sample_count"`
	ReferenceCharCount int     `json:"reference_char_count"`
	EditDistance       int     `json:"edit_distance"`
	CER                float64 `json:"cer"`
}

type EvaluationSample struct {
	DocumentID             string  `json:"document_id"`
	DocumentTitle          string  `json:"document_title"`
	PageID                 string  `json:"page_id"`
	PageNo                 int     `json:"page_no"`
	Provider               string  `json:"provider"`
	Model                  string  `json:"model"`
	PromptVersion          string  `json:"prompt_version"`
	CER                    float64 `json:"cer"`
	EditDistance           int     `json:"edit_distance"`
	ReferenceCharCount     int     `json:"reference_char_count"`
	CandidateSeconds       float64 `json:"candidate_seconds"`
	ReviewSeconds          float64 `json:"review_seconds"`
	TurnaroundSeconds      float64 `json:"turnaround_seconds"`
	MissedLines            int     `json:"missed_lines"`
	GuessedLines           int     `json:"guessed_lines"`
	ReorderedLines         int     `json:"reordered_lines"`
	LowConfidenceItemCount int     `json:"low_confidence_item_count"`
	LowConfidenceHitCount  int     `json:"low_confidence_hit_count"`
	FinalizedAt            string  `json:"finalized_at"`
}

type evaluationRow struct {
	DocumentID, DocumentTitle, PageID                  string
	PageNo                                             int
	Provider, Model, PromptVersion                     string
	SourceText, SourceRaw, Reference                   string
	CandidateSeconds, ReviewSeconds, TurnaroundSeconds float64
	FinalizedAt                                        string
}

// GetEvaluationMetrics treats the latest final text as ground truth. When
// benchmarkOnly is true, only documents tagged “基准” or “benchmark” are used.
func (s *Store) GetEvaluationMetrics(ctx context.Context, benchmarkOnly bool) (EvaluationMetrics, error) {
	benchmarkFlag := 0
	if benchmarkOnly {
		benchmarkFlag = 1
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE latest_final AS (
		  SELECT v.*, ROW_NUMBER() OVER (PARTITION BY v.page_id ORDER BY v.created_at DESC, v.rowid DESC) AS rank
		  FROM text_versions v WHERE v.kind = 'final'
		), version_chain(final_id, page_id, version_id, base_version_id, kind, source_result_id, text, created_at, depth) AS (
		  SELECT final.id, final.page_id, final.id, final.base_version_id, final.kind, final.source_result_id, final.text, final.created_at, 0
		  FROM latest_final final WHERE final.rank = 1
		  UNION ALL
		  SELECT chain.final_id, chain.page_id, parent.id, parent.base_version_id, parent.kind,
		         parent.source_result_id, parent.text, parent.created_at, chain.depth + 1
		  FROM version_chain chain JOIN text_versions parent ON parent.id = chain.base_version_id
		  WHERE chain.depth < 100
		), candidate_ranked AS (
		  SELECT chain.*, ROW_NUMBER() OVER (PARTITION BY final_id ORDER BY depth ASC) AS candidate_rank
		  FROM version_chain chain WHERE chain.depth > 0 AND chain.kind IN ('candidate', 'raw_selected')
		), baseline AS (
		  SELECT * FROM candidate_ranked WHERE candidate_rank = 1
		)
		SELECT d.id, d.title, p.id, p.page_no,
		       COALESCE(NULLIF(merge.driver, ''), run.provider, ''),
		       CASE WHEN merge.id IS NOT NULL THEN 'candidate-merge' ELSE COALESCE(run.model, '') END,
		       COALESCE(NULLIF(merge.prompt_version, ''), run.prompt_version, ''),
		       COALESCE(baseline.text, source.text),
		       CASE WHEN baseline.version_id IS NULL OR baseline.source_result_id IS NOT NULL THEN source.raw_json ELSE '{}' END,
		       final.text,
		       MAX(0, (julianday(COALESCE(baseline.created_at, source.created_at)) - julianday(p.created_at)) * 86400.0),
		       COALESCE((SELECT SUM(activity.active_seconds) FROM review_activity_sessions activity
		                 WHERE activity.page_id = p.id
		                   AND activity.updated_at >= COALESCE(baseline.created_at, source.created_at)
		                   AND activity.started_at <= final.created_at
		                   AND activity.finished_at IS NOT NULL
		                   AND activity.finished_at <= final.created_at), 0),
		       MAX(0, (julianday(final.created_at) - julianday(COALESCE(baseline.created_at, source.created_at))) * 86400.0),
		       final.created_at
		FROM latest_final final
		JOIN pages p ON p.id = final.page_id
		JOIN documents d ON d.id = p.document_id
		LEFT JOIN baseline ON baseline.final_id = final.id
		LEFT JOIN candidate_merges merge ON merge.text_version_id = baseline.version_id
		JOIN recognition_results source ON source.id = COALESCE(baseline.source_result_id, final.source_result_id, (
		  SELECT fallback.id FROM recognition_results fallback
		  WHERE fallback.page_id = p.id AND fallback.created_at <= final.created_at
		  ORDER BY fallback.created_at DESC, fallback.rowid DESC LIMIT 1
		))
		JOIN recognition_runs run ON run.id = source.run_id
		WHERE final.rank = 1
		  AND (? = 0 OR EXISTS (
		    SELECT 1 FROM document_tags dt JOIN tags t ON t.id = dt.tag_id
		    WHERE dt.document_id = d.id AND lower(t.name) IN ('benchmark', '基准')
		  ))
		ORDER BY final.created_at DESC, final.id DESC
		LIMIT ?
	`, benchmarkFlag, maxEvaluationSamples+1)
	if err != nil {
		return EvaluationMetrics{}, err
	}
	defer rows.Close()

	data := make([]evaluationRow, 0, maxEvaluationSamples+1)
	for rows.Next() {
		var row evaluationRow
		var candidate, review, turnaround sql.NullFloat64
		if err := rows.Scan(&row.DocumentID, &row.DocumentTitle, &row.PageID, &row.PageNo,
			&row.Provider, &row.Model, &row.PromptVersion, &row.SourceText, &row.SourceRaw,
			&row.Reference, &candidate, &review, &turnaround, &row.FinalizedAt); err != nil {
			return EvaluationMetrics{}, err
		}
		if candidate.Valid {
			row.CandidateSeconds = candidate.Float64
		}
		if review.Valid {
			row.ReviewSeconds = review.Float64
		}
		if turnaround.Valid {
			row.TurnaroundSeconds = turnaround.Float64
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return EvaluationMetrics{}, err
	}
	metrics := aggregateEvaluationMetrics(data, benchmarkOnly, time.Now())
	return metrics, nil
}

func aggregateEvaluationMetrics(rows []evaluationRow, benchmarkOnly bool, currentTime time.Time) EvaluationMetrics {
	metrics := EvaluationMetrics{BenchmarkOnly: benchmarkOnly, Groups: []EvaluationMetricGroup{}, Trend: []EvaluationMetricTrend{}, Samples: []EvaluationSample{}}
	if len(rows) > maxEvaluationSamples {
		metrics.Truncated = true
		rows = rows[:maxEvaluationSamples]
	}
	type groupKey struct{ provider, model, prompt string }
	groups := map[groupKey]*EvaluationMetricGroup{}
	trend := map[string]*EvaluationMetricTrend{}
	var candidateTotal, reviewTotal, turnaroundTotal float64
	for _, row := range rows {
		correction := EvaluateTextCorrection(row.SourceText, row.Reference)
		missed, guessed, reordered := evaluateLineErrors(row.SourceText, row.Reference)
		segments := recognizer.ExtractConfidenceSegments([]byte(row.SourceRaw), row.SourceText, 0.8)
		hits := countLowConfidenceHits(row.SourceText, segments, correction.Edits)
		sample := EvaluationSample{
			DocumentID: row.DocumentID, DocumentTitle: row.DocumentTitle, PageID: row.PageID, PageNo: row.PageNo,
			Provider: row.Provider, Model: row.Model, PromptVersion: row.PromptVersion,
			CER: correction.CER, EditDistance: correction.EditDistance, ReferenceCharCount: correction.ReferenceCharCount,
			CandidateSeconds: row.CandidateSeconds, ReviewSeconds: row.ReviewSeconds,
			TurnaroundSeconds: row.TurnaroundSeconds,
			MissedLines:       missed, GuessedLines: guessed, ReorderedLines: reordered,
			LowConfidenceItemCount: len(segments), LowConfidenceHitCount: hits, FinalizedAt: row.FinalizedAt,
		}
		metrics.Samples = append(metrics.Samples, sample)
		metrics.SampleCount++
		metrics.ReferenceCharCount += correction.ReferenceCharCount
		metrics.EditDistance += correction.EditDistance
		metrics.SubstitutionCount += correction.SubstitutionCount
		metrics.OmissionCount += correction.OmissionCount
		metrics.AdditionCount += correction.AdditionCount
		metrics.MissedLineCount += missed
		metrics.GuessedLineCount += guessed
		metrics.ReorderedLineCount += reordered
		metrics.LowConfidenceItemCount += len(segments)
		metrics.LowConfidenceHitCount += hits
		candidateTotal += row.CandidateSeconds
		reviewTotal += row.ReviewSeconds
		turnaroundTotal += row.TurnaroundSeconds
		if row.ReviewSeconds > 0 {
			metrics.ReviewSampleCount++
		}

		key := groupKey{row.Provider, row.Model, row.PromptVersion}
		group := groups[key]
		if group == nil {
			group = &EvaluationMetricGroup{Provider: row.Provider, Model: row.Model, PromptVersion: row.PromptVersion}
			groups[key] = group
		}
		group.SampleCount++
		group.ReferenceCharCount += correction.ReferenceCharCount
		group.EditDistance += correction.EditDistance

		date := timestampPrefix(row.FinalizedAt, 10)
		point := trend[date]
		if point == nil {
			point = &EvaluationMetricTrend{Date: date}
			trend[date] = point
		}
		point.SampleCount++
		point.ReferenceCharCount += correction.ReferenceCharCount
		point.EditDistance += correction.EditDistance
		if finalizedAt, err := time.Parse(time.RFC3339Nano, row.FinalizedAt); err == nil && finalizedAt.After(currentTime.Add(-time.Hour)) {
			metrics.ConfirmedLastHour++
		}
	}
	metrics.CER = authorCER(metrics.EditDistance, metrics.ReferenceCharCount)
	if metrics.LowConfidenceItemCount > 0 {
		metrics.LowConfidenceHitRate = float64(metrics.LowConfidenceHitCount) / float64(metrics.LowConfidenceItemCount)
	}
	if metrics.SampleCount > 0 {
		metrics.AverageCandidateSeconds = candidateTotal / float64(metrics.SampleCount)
		metrics.AverageTurnaroundSeconds = turnaroundTotal / float64(metrics.SampleCount)
	}
	if metrics.ReviewSampleCount > 0 {
		metrics.AverageReviewSeconds = reviewTotal / float64(metrics.ReviewSampleCount)
	}
	if reviewTotal > 0 {
		metrics.PagesPerActiveHour = float64(metrics.ReviewSampleCount) / (reviewTotal / 3600)
	}
	for _, group := range groups {
		group.CER = authorCER(group.EditDistance, group.ReferenceCharCount)
		metrics.Groups = append(metrics.Groups, *group)
	}
	sort.Slice(metrics.Groups, func(i, j int) bool {
		if metrics.Groups[i].CER != metrics.Groups[j].CER {
			return metrics.Groups[i].CER < metrics.Groups[j].CER
		}
		return metrics.Groups[i].SampleCount > metrics.Groups[j].SampleCount
	})
	for _, point := range trend {
		point.CER = authorCER(point.EditDistance, point.ReferenceCharCount)
		metrics.Trend = append(metrics.Trend, *point)
	}
	sort.Slice(metrics.Trend, func(i, j int) bool { return metrics.Trend[i].Date < metrics.Trend[j].Date })
	return metrics
}

func evaluateLineErrors(source, reference string) (missed, guessed, reordered int) {
	left := normalizedEvaluationLines(source)
	right := normalizedEvaluationLines(reference)
	if len(left) > 1000 {
		left = left[:1000]
	}
	if len(right) > 1000 {
		right = right[:1000]
	}
	width := len(right) + 1
	distance := make([]int, (len(left)+1)*width)
	for i := 0; i <= len(left); i++ {
		distance[i*width] = i
	}
	for j := 0; j <= len(right); j++ {
		distance[j] = j
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			cost := 0
			if left[i-1] != right[j-1] {
				cost = 1
			}
			distance[i*width+j] = min(distance[(i-1)*width+j]+1, distance[i*width+j-1]+1, distance[(i-1)*width+j-1]+cost)
		}
	}
	for i, j := len(left), len(right); i > 0 || j > 0; {
		if i > 0 && j > 0 {
			cost := 0
			if left[i-1] != right[j-1] {
				cost = 1
			}
			if distance[i*width+j] == distance[(i-1)*width+j-1]+cost {
				i, j = i-1, j-1
				continue
			}
		}
		if j > 0 && distance[i*width+j] == distance[i*width+j-1]+1 {
			missed++
			j--
			continue
		}
		guessed++
		i--
	}
	common := commonLineCount(left, right)
	reordered = common - lineLCSLength(left, right)
	if reordered < 0 {
		reordered = 0
	}
	return
}

func normalizedEvaluationLines(text string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func commonLineCount(left, right []string) int {
	counts := map[string]int{}
	for _, line := range left {
		counts[line]++
	}
	common := 0
	for _, line := range right {
		if counts[line] > 0 {
			counts[line]--
			common++
		}
	}
	return common
}

func lineLCSLength(left, right []string) int {
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for _, l := range left {
		for j, r := range right {
			if l == r {
				current[j+1] = previous[j] + 1
			} else {
				current[j+1] = max(previous[j+1], current[j])
			}
		}
		previous, current = current, previous
		clear(current)
	}
	return previous[len(right)]
}

func countLowConfidenceHits(source string, segments []recognizer.ConfidenceSegment, edits []TextEdit) int {
	hits := 0
	for _, segment := range segments {
		start := utf16OffsetToRune(source, segment.Start)
		end := utf16OffsetToRune(source, segment.End)
		for _, edit := range edits {
			overlaps := start < edit.SourceEnd && end > edit.SourceStart
			if edit.SourceStart == edit.SourceEnd {
				overlaps = edit.SourceStart >= start && edit.SourceStart <= end
			}
			if overlaps {
				hits++
				break
			}
		}
	}
	return hits
}

func utf16OffsetToRune(value string, offset int) int {
	units, runes := 0, 0
	for _, r := range value {
		if units >= offset {
			break
		}
		units += utf16.RuneLen(r)
		runes++
	}
	return runes
}

func timestampPrefix(value string, length int) string {
	if len(value) >= length {
		return value[:length]
	}
	return value
}
