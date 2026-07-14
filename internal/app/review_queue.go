package app

import (
	"context"
	"database/sql"
	"sort"

	"github.com/lieyan/firescribe/internal/recognizer"
)

// ReviewQueueItem is a page that deserves reviewer attention because its
// effective/latest recognition confidence is low or it still has open
// uncertain-text annotations.
type ReviewQueueItem struct {
	DocumentID         string                         `json:"document_id"`
	DocumentTitle      string                         `json:"document_title"`
	PageID             string                         `json:"page_id"`
	PageNo             int                            `json:"page_no"`
	PageStatus         string                         `json:"page_status"`
	ThumbnailURL       string                         `json:"thumbnail_url"`
	Confidence         *float64                       `json:"confidence"`
	RecognitionCount   int                            `json:"recognition_count"`
	OpenUncertainCount int                            `json:"open_uncertain_count"`
	LastProvider       string                         `json:"last_provider"`
	LastModel          string                         `json:"last_model"`
	UpdatedAt          string                         `json:"updated_at"`
	LowConfidence      []recognizer.ConfidenceSegment `json:"low_confidence_segments"`
}

func (s *Store) ListReviewQueue(ctx context.Context, threshold float64, documentID string) ([]ReviewQueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH queue_pages AS (
		  SELECT
		    pd.document_id,
		    d.title AS document_title,
		    pd.page_id,
		    pd.page_no,
		    pd.page_status,
		    pd.recognition_count,
		    pd.last_provider,
		    pd.last_model,
		    pd.updated_at,
		    COALESCE(ev.id, '') AS effective_version_id,
		    COALESCE(ev.text, '') AS effective_text,
		    ev.kind AS effective_kind,
		    EXISTS(SELECT 1 FROM candidate_merges cm WHERE cm.text_version_id = ev.id) AS is_candidate_merge,
		    COALESCE(source_result.confidence, latest_result.confidence) AS confidence,
		    COALESCE(source_result.text, latest_result.text, '') AS result_text,
		    COALESCE(source_result.raw_json, latest_result.raw_json, '') AS result_raw_json,
		    (SELECT COUNT(*) FROM annotations a
		     WHERE a.page_id = pd.page_id AND a.kind = 'uncertain_text' AND a.status = 'open') AS open_uncertain_count
		  FROM page_details pd
		  JOIN documents d ON d.id = pd.document_id
		  LEFT JOIN effective_text_versions ev ON ev.page_id = pd.page_id
		  LEFT JOIN recognition_results source_result ON source_result.id = ev.source_result_id
		  LEFT JOIN recognition_results latest_result ON latest_result.id = (
		    SELECT r.id FROM recognition_results r
		    WHERE r.page_id = pd.page_id
		    ORDER BY r.created_at DESC, r.rowid DESC LIMIT 1
		  )
		  WHERE (? = '' OR pd.document_id = ?)
		)
		SELECT document_id, document_title, page_id, page_no, page_status, confidence, result_text, result_raw_json,
		       recognition_count, open_uncertain_count, last_provider, last_model, updated_at,
		       effective_version_id, effective_text, COALESCE(effective_kind, ''), is_candidate_merge
		FROM queue_pages
		ORDER BY open_uncertain_count DESC, confidence IS NULL, confidence ASC, updated_at ASC
	`, documentID, documentID)
	if err != nil {
		return nil, err
	}
	type queueCandidate struct {
		item                              ReviewQueueItem
		resultText, resultRawJSON         string
		effectiveVersionID, effectiveKind string
		isCandidateMerge                  bool
	}
	candidates := []queueCandidate{}
	for rows.Next() {
		var candidate queueCandidate
		var confidence sql.NullFloat64
		var provider, model sql.NullString
		var resultText, resultRawJSON, effectiveVersionID, effectiveText, effectiveKind string
		var isCandidateMerge bool
		if err := rows.Scan(
			&candidate.item.DocumentID, &candidate.item.DocumentTitle, &candidate.item.PageID, &candidate.item.PageNo, &candidate.item.PageStatus, &confidence, &resultText, &resultRawJSON,
			&candidate.item.RecognitionCount, &candidate.item.OpenUncertainCount, &provider, &model, &candidate.item.UpdatedAt,
			&effectiveVersionID, &effectiveText, &effectiveKind, &isCandidateMerge,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if confidence.Valid {
			candidate.item.Confidence = &confidence.Float64
		}
		candidate.item.LastProvider = nullString(provider)
		candidate.item.LastModel = nullString(model)
		candidate.resultText = resultText
		candidate.resultRawJSON = resultRawJSON
		candidate.effectiveVersionID = effectiveVersionID
		candidate.effectiveKind = effectiveKind
		candidate.isCandidateMerge = isCandidateMerge
		candidates = append(candidates, candidate)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := []ReviewQueueItem{}
	for _, candidate := range candidates {
		item := candidate.item
		if candidate.isCandidateMerge {
			item.Confidence = nil
			item.LowConfidence, err = s.candidateMergeConfidenceSegments(ctx, candidate.effectiveVersionID, threshold)
			if err != nil {
				return nil, err
			}
		} else {
			item.LowConfidence = recognizer.ExtractConfidenceSegments([]byte(candidate.resultRawJSON), candidate.resultText, threshold)
		}
		pageLow := item.Confidence != nil && *item.Confidence <= threshold
		if item.OpenUncertainCount == 0 && (candidate.effectiveKind == "manual" || candidate.effectiveKind == "final" || (!pageLow && len(item.LowConfidence) == 0)) {
			continue
		}
		items = append(items, item)
		if len(items) >= 1000 {
			sort.SliceStable(items, func(i, j int) bool {
				if items[i].OpenUncertainCount != items[j].OpenUncertainCount {
					return items[i].OpenUncertainCount > items[j].OpenUncertainCount
				}
				return reviewQueueConfidence(items[i]) < reviewQueueConfidence(items[j])
			})
			items = items[:500]
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OpenUncertainCount != items[j].OpenUncertainCount {
			return items[i].OpenUncertainCount > items[j].OpenUncertainCount
		}
		return reviewQueueConfidence(items[i]) < reviewQueueConfidence(items[j])
	})
	if len(items) > 500 {
		items = items[:500]
	}
	return items, nil
}

func (s *Store) candidateMergeConfidenceSegments(ctx context.Context, textVersionID string, threshold float64) ([]recognizer.ConfidenceSegment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT segment.source_start, segment.source_end, segment.output_start, segment.output_end, segment.text,
		       result.text, result.raw_json
		FROM candidate_merges merge
		JOIN candidate_merge_segments segment ON segment.candidate_merge_id = merge.id
		JOIN recognition_results result ON result.id = segment.source_result_id
		WHERE merge.text_version_id = ? ORDER BY segment.ordinal
	`, textVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []recognizer.ConfidenceSegment{}
	for rows.Next() {
		var sourceStart, sourceEnd, outputStart, outputEnd int
		var text, sourceText, raw string
		if err := rows.Scan(&sourceStart, &sourceEnd, &outputStart, &outputEnd, &text, &sourceText, &raw); err != nil {
			return nil, err
		}
		confidence := 2.0
		for _, segment := range recognizer.ExtractConfidenceSegments([]byte(raw), sourceText, threshold) {
			if segment.Start < sourceEnd && segment.End > sourceStart && segment.Confidence < confidence {
				confidence = segment.Confidence
			}
		}
		if confidence <= threshold && outputEnd > outputStart {
			items = append(items, recognizer.ConfidenceSegment{
				Text: text, Start: outputStart, End: outputEnd, Confidence: confidence,
				Level: "paragraph", Source: "candidate_merge_segment",
			})
		}
	}
	return items, rows.Err()
}

func reviewQueueConfidence(item ReviewQueueItem) float64 {
	value := 2.0
	if item.Confidence != nil {
		value = *item.Confidence
	}
	for _, segment := range item.LowConfidence {
		if segment.Confidence < value {
			value = segment.Confidence
		}
	}
	return value
}
