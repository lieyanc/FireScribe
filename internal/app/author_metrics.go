package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

// AuthorRecognitionMetrics summarizes the edits needed to turn OCR output
// into the author's manually reviewed reference text. CER may exceed 1 when
// the recognizer adds more characters than the reference contains.
type AuthorRecognitionMetrics struct {
	SampleCount        int                 `json:"sample_count"`
	SourceCharCount    int                 `json:"source_char_count"`
	ReferenceCharCount int                 `json:"reference_char_count"`
	EditDistance       int                 `json:"edit_distance"`
	CER                float64             `json:"cer"`
	SubstitutionCount  int                 `json:"substitution_count"`
	OmissionCount      int                 `json:"omission_count"`
	AdditionCount      int                 `json:"addition_count"`
	Groups             []AuthorMetricGroup `json:"groups"`
	Trend              []AuthorMetricTrend `json:"trend"`
	CommonErrors       []AuthorCommonError `json:"common_errors"`
}

type AuthorMetricGroup struct {
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	PromptVersion      string  `json:"prompt_version"`
	SampleCount        int     `json:"sample_count"`
	ReferenceCharCount int     `json:"reference_char_count"`
	EditDistance       int     `json:"edit_distance"`
	CER                float64 `json:"cer"`
	SubstitutionCount  int     `json:"substitution_count"`
	OmissionCount      int     `json:"omission_count"`
	AdditionCount      int     `json:"addition_count"`
}

type AuthorMetricTrend struct {
	Date               string  `json:"date"`
	SampleCount        int     `json:"sample_count"`
	ReferenceCharCount int     `json:"reference_char_count"`
	EditDistance       int     `json:"edit_distance"`
	CER                float64 `json:"cer"`
}

type TextErrorPattern struct {
	Type      string `json:"type"`
	Source    string `json:"source"`
	Corrected string `json:"corrected"`
	Count     int    `json:"count"`
}

type TextEdit struct {
	Type           string `json:"type"`
	SourceStart    int    `json:"source_start"`
	SourceEnd      int    `json:"source_end"`
	ReferenceStart int    `json:"reference_start"`
	ReferenceEnd   int    `json:"reference_end"`
	Source         string `json:"source"`
	Corrected      string `json:"corrected"`
}

// TextCorrectionMetrics is a reusable exact character-level comparison. The
// reference text is the reviewed ground truth; omission means the source
// missed a reference character, while addition means the source guessed an
// extra character.
type TextCorrectionMetrics struct {
	SourceCharCount    int                `json:"source_char_count"`
	ReferenceCharCount int                `json:"reference_char_count"`
	EditDistance       int                `json:"edit_distance"`
	CER                float64            `json:"cer"`
	SubstitutionCount  int                `json:"substitution_count"`
	OmissionCount      int                `json:"omission_count"`
	AdditionCount      int                `json:"addition_count"`
	ErrorPatterns      []TextErrorPattern `json:"error_patterns"`
	Edits              []TextEdit         `json:"edits,omitempty"`
}

type AuthorCommonError = TextErrorPattern

type authorMetricSample struct {
	CorrectionID string
	Provider     string
	Model        string
	Prompt       string
	Source       string
	Corrected    string
	CreatedAt    string
	Metric       TextCorrectionMetrics
	HasMetric    bool
}

type authorEditOperation struct {
	Type      string
	Source    rune
	Corrected rune
}

func (s *Store) GetAuthorRecognitionMetrics(ctx context.Context, profileID string) (AuthorRecognitionMetrics, error) {
	if _, err := s.GetAuthorProfile(ctx, profileID); err != nil {
		return AuthorRecognitionMetrics{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, COALESCE(run.provider, ''), COALESCE(run.model, ''),
		       COALESCE(run.prompt_version, ''), c.source_text, c.corrected_text, c.created_at,
		       m.source_char_count, m.reference_char_count, m.edit_distance,
		       m.substitution_count, m.omission_count, m.addition_count,
		       COALESCE(m.error_patterns_json, '')
		FROM author_corrections c
		LEFT JOIN recognition_results result ON result.id = c.source_result_id
		LEFT JOIN recognition_runs run ON run.id = result.run_id
		LEFT JOIN author_correction_metrics m ON m.correction_id = c.id AND m.algorithm_version = 1
		WHERE c.author_profile_id = ?
		ORDER BY c.created_at, c.id
	`, profileID)
	if err != nil {
		return AuthorRecognitionMetrics{}, err
	}
	defer rows.Close()

	samples := []authorMetricSample{}
	for rows.Next() {
		var sample authorMetricSample
		var sourceChars, referenceChars, distance, substitutions, omissions, additions sql.NullInt64
		var patternsJSON string
		if err := rows.Scan(&sample.CorrectionID, &sample.Provider, &sample.Model, &sample.Prompt,
			&sample.Source, &sample.Corrected, &sample.CreatedAt, &sourceChars, &referenceChars,
			&distance, &substitutions, &omissions, &additions, &patternsJSON); err != nil {
			return AuthorRecognitionMetrics{}, err
		}
		if sourceChars.Valid && referenceChars.Valid && distance.Valid && substitutions.Valid && omissions.Valid && additions.Valid {
			sample.HasMetric = true
			sample.Metric = TextCorrectionMetrics{
				SourceCharCount:    int(sourceChars.Int64),
				ReferenceCharCount: int(referenceChars.Int64),
				EditDistance:       int(distance.Int64),
				SubstitutionCount:  int(substitutions.Int64),
				OmissionCount:      int(omissions.Int64),
				AdditionCount:      int(additions.Int64),
			}
			if err := json.Unmarshal([]byte(patternsJSON), &sample.Metric.ErrorPatterns); err != nil {
				sample.HasMetric = false
			}
		}
		if !sample.HasMetric {
			sample.Metric = EvaluateTextCorrection(sample.Source, sample.Corrected)
		}
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return AuthorRecognitionMetrics{}, err
	}
	return aggregateAuthorMetrics(samples), nil
}

// SyncAuthorCorrectionMetrics persists derived metrics for historical samples.
// New samples are cached when their author correction is first recorded.
func (s *Store) SyncAuthorCorrectionMetrics(ctx context.Context, documentID string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.source_text, c.corrected_text
		FROM author_corrections c
		LEFT JOIN author_correction_metrics m ON m.correction_id = c.id AND m.algorithm_version = 1
		WHERE c.document_id = ? AND m.correction_id IS NULL
		ORDER BY c.created_at, c.id
	`, documentID)
	if err != nil {
		return 0, err
	}
	type pendingMetric struct{ id, source, corrected string }
	pending := []pendingMetric{}
	for rows.Next() {
		var item pendingMetric
		if err := rows.Scan(&item.id, &item.source, &item.corrected); err != nil {
			_ = rows.Close()
			return 0, err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	stored := 0
	for _, item := range pending {
		if err := persistAuthorCorrectionMetric(ctx, s.db, item.id, item.source, item.corrected); err != nil {
			return stored, err
		}
		stored++
	}
	return stored, nil
}

func persistAuthorCorrectionMetric(ctx context.Context, runner authorCorrectionRunner, correctionID, source, corrected string) error {
	metric := EvaluateTextCorrection(source, corrected)
	patterns, err := json.Marshal(metric.ErrorPatterns)
	if err != nil {
		return err
	}
	_, err = runner.ExecContext(ctx, `
		INSERT OR IGNORE INTO author_correction_metrics(
			correction_id, source_char_count, reference_char_count, edit_distance,
			substitution_count, omission_count, addition_count, error_patterns_json, algorithm_version, computed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
	`, correctionID, metric.SourceCharCount, metric.ReferenceCharCount, metric.EditDistance,
		metric.SubstitutionCount, metric.OmissionCount, metric.AdditionCount, string(patterns), now())
	return err
}

func EvaluateTextCorrection(source, reference string) TextCorrectionMetrics {
	sourceRunes := []rune(source)
	correctedRunes := []rune(reference)
	metric := TextCorrectionMetrics{SourceCharCount: len(sourceRunes), ReferenceCharCount: len(correctedRunes), ErrorPatterns: []TextErrorPattern{}, Edits: []TextEdit{}}
	sourceOffset, referenceOffset := 0, 0

	for len(sourceRunes) > 0 && len(correctedRunes) > 0 && sourceRunes[0] == correctedRunes[0] {
		sourceRunes = sourceRunes[1:]
		correctedRunes = correctedRunes[1:]
		sourceOffset++
		referenceOffset++
	}
	for len(sourceRunes) > 0 && len(correctedRunes) > 0 && sourceRunes[len(sourceRunes)-1] == correctedRunes[len(correctedRunes)-1] {
		sourceRunes = sourceRunes[:len(sourceRunes)-1]
		correctedRunes = correctedRunes[:len(correctedRunes)-1]
	}

	counts := map[string]int{}
	for _, operation := range alignAuthorText(sourceRunes, correctedRunes) {
		if operation.Type == "equal" {
			sourceOffset++
			referenceOffset++
			continue
		}
		metric.EditDistance++
		sourceValue, correctedValue := "", ""
		if operation.Source != 0 {
			sourceValue = string(operation.Source)
		}
		if operation.Corrected != 0 {
			correctedValue = string(operation.Corrected)
		}
		switch operation.Type {
		case "substitution":
			metric.SubstitutionCount++
			metric.Edits = append(metric.Edits, TextEdit{Type: operation.Type, SourceStart: sourceOffset, SourceEnd: sourceOffset + 1, ReferenceStart: referenceOffset, ReferenceEnd: referenceOffset + 1, Source: sourceValue, Corrected: correctedValue})
			sourceOffset++
			referenceOffset++
		case "omission":
			metric.OmissionCount++
			metric.Edits = append(metric.Edits, TextEdit{Type: operation.Type, SourceStart: sourceOffset, SourceEnd: sourceOffset, ReferenceStart: referenceOffset, ReferenceEnd: referenceOffset + 1, Source: sourceValue, Corrected: correctedValue})
			referenceOffset++
		case "addition":
			metric.AdditionCount++
			metric.Edits = append(metric.Edits, TextEdit{Type: operation.Type, SourceStart: sourceOffset, SourceEnd: sourceOffset + 1, ReferenceStart: referenceOffset, ReferenceEnd: referenceOffset, Source: sourceValue, Corrected: correctedValue})
			sourceOffset++
		}
		key := strings.Join([]string{operation.Type, sourceValue, correctedValue}, "\x00")
		counts[key]++
	}
	for key, count := range counts {
		parts := strings.Split(key, "\x00")
		metric.ErrorPatterns = append(metric.ErrorPatterns, TextErrorPattern{Type: parts[0], Source: parts[1], Corrected: parts[2], Count: count})
	}
	metric.CER = authorCER(metric.EditDistance, metric.ReferenceCharCount)
	sortCommonErrors(metric.ErrorPatterns)
	return metric
}

// alignAuthorText is a Hirschberg-style Levenshtein alignment. It retains an
// exact edit script with linear working memory, which keeps full-page samples
// bounded even when the manuscript text is long.
func alignAuthorText(source, corrected []rune) []authorEditOperation {
	prefix := 0
	for prefix < len(source) && prefix < len(corrected) && source[prefix] == corrected[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(source)-prefix && suffix < len(corrected)-prefix && source[len(source)-1-suffix] == corrected[len(corrected)-1-suffix] {
		suffix++
	}
	if prefix > 0 || suffix > 0 {
		operations := make([]authorEditOperation, 0, prefix+suffix)
		for i := 0; i < prefix; i++ {
			operations = append(operations, authorEditOperation{Type: "equal", Source: source[i], Corrected: corrected[i]})
		}
		operations = append(operations, alignAuthorText(source[prefix:len(source)-suffix], corrected[prefix:len(corrected)-suffix])...)
		for i := len(source) - suffix; i < len(source); i++ {
			operations = append(operations, authorEditOperation{Type: "equal", Source: source[i], Corrected: source[i]})
		}
		return operations
	}
	if len(source) == 0 {
		operations := make([]authorEditOperation, len(corrected))
		for i, value := range corrected {
			operations[i] = authorEditOperation{Type: "omission", Corrected: value}
		}
		return operations
	}
	if len(corrected) == 0 {
		operations := make([]authorEditOperation, len(source))
		for i, value := range source {
			operations[i] = authorEditOperation{Type: "addition", Source: value}
		}
		return operations
	}
	if len(source) == 1 || len(corrected) == 1 {
		return alignAuthorTextSmall(source, corrected)
	}

	middle := len(source) / 2
	forward := authorDistanceRow(source[:middle], corrected)
	backward := authorDistanceRow(reverseRunes(source[middle:]), reverseRunes(corrected))
	bestSplit, bestCost := 0, forward[0]+backward[len(corrected)]
	for split := 1; split <= len(corrected); split++ {
		cost := forward[split] + backward[len(corrected)-split]
		if cost < bestCost {
			bestSplit, bestCost = split, cost
		}
	}
	left := alignAuthorText(source[:middle], corrected[:bestSplit])
	right := alignAuthorText(source[middle:], corrected[bestSplit:])
	return append(left, right...)
}

func alignAuthorTextSmall(source, corrected []rune) []authorEditOperation {
	width := len(corrected) + 1
	distance := make([]int, (len(source)+1)*width)
	for i := 0; i <= len(source); i++ {
		distance[i*width] = i
	}
	for j := 0; j <= len(corrected); j++ {
		distance[j] = j
	}
	for i := 1; i <= len(source); i++ {
		for j := 1; j <= len(corrected); j++ {
			cost := 0
			if source[i-1] != corrected[j-1] {
				cost = 1
			}
			distance[i*width+j] = min(distance[(i-1)*width+j]+1, distance[i*width+j-1]+1, distance[(i-1)*width+j-1]+cost)
		}
	}

	operations := []authorEditOperation{}
	for i, j := len(source), len(corrected); i > 0 || j > 0; {
		if i > 0 && j > 0 {
			cost := 0
			operationType := "equal"
			if source[i-1] != corrected[j-1] {
				cost = 1
				operationType = "substitution"
			}
			if distance[i*width+j] == distance[(i-1)*width+j-1]+cost {
				operations = append(operations, authorEditOperation{Type: operationType, Source: source[i-1], Corrected: corrected[j-1]})
				i, j = i-1, j-1
				continue
			}
		}
		if j > 0 && distance[i*width+j] == distance[i*width+j-1]+1 {
			operations = append(operations, authorEditOperation{Type: "omission", Corrected: corrected[j-1]})
			j--
			continue
		}
		operations = append(operations, authorEditOperation{Type: "addition", Source: source[i-1]})
		i--
	}
	for left, right := 0, len(operations)-1; left < right; left, right = left+1, right-1 {
		operations[left], operations[right] = operations[right], operations[left]
	}
	return operations
}

func authorDistanceRow(source, corrected []rune) []int {
	previous := make([]int, len(corrected)+1)
	for j := range previous {
		previous[j] = j
	}
	current := make([]int, len(corrected)+1)
	for i, sourceRune := range source {
		current[0] = i + 1
		for j, correctedRune := range corrected {
			cost := 0
			if sourceRune != correctedRune {
				cost = 1
			}
			current[j+1] = min(previous[j+1]+1, current[j]+1, previous[j]+cost)
		}
		previous, current = current, previous
	}
	return previous
}

func reverseRunes(input []rune) []rune {
	output := make([]rune, len(input))
	for i := range input {
		output[len(input)-1-i] = input[i]
	}
	return output
}

func aggregateAuthorMetrics(samples []authorMetricSample) AuthorRecognitionMetrics {
	metrics := AuthorRecognitionMetrics{Groups: []AuthorMetricGroup{}, Trend: []AuthorMetricTrend{}, CommonErrors: []AuthorCommonError{}}
	groups := map[string]*AuthorMetricGroup{}
	trend := map[string]*AuthorMetricTrend{}
	errors := map[string]*AuthorCommonError{}
	for _, sample := range samples {
		metric := sample.Metric
		metrics.SampleCount++
		metrics.SourceCharCount += metric.SourceCharCount
		metrics.ReferenceCharCount += metric.ReferenceCharCount
		metrics.EditDistance += metric.EditDistance
		metrics.SubstitutionCount += metric.SubstitutionCount
		metrics.OmissionCount += metric.OmissionCount
		metrics.AdditionCount += metric.AdditionCount

		groupKey := strings.Join([]string{sample.Provider, sample.Model, sample.Prompt}, "\x00")
		group := groups[groupKey]
		if group == nil {
			group = &AuthorMetricGroup{Provider: sample.Provider, Model: sample.Model, PromptVersion: sample.Prompt}
			groups[groupKey] = group
		}
		group.SampleCount++
		group.ReferenceCharCount += metric.ReferenceCharCount
		group.EditDistance += metric.EditDistance
		group.SubstitutionCount += metric.SubstitutionCount
		group.OmissionCount += metric.OmissionCount
		group.AdditionCount += metric.AdditionCount

		date := sample.CreatedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		point := trend[date]
		if point == nil {
			point = &AuthorMetricTrend{Date: date}
			trend[date] = point
		}
		point.SampleCount++
		point.ReferenceCharCount += metric.ReferenceCharCount
		point.EditDistance += metric.EditDistance

		for _, pattern := range metric.ErrorPatterns {
			key := strings.Join([]string{pattern.Type, pattern.Source, pattern.Corrected}, "\x00")
			if errors[key] == nil {
				copy := pattern
				copy.Count = 0
				errors[key] = &copy
			}
			errors[key].Count += pattern.Count
		}
	}
	metrics.CER = authorCER(metrics.EditDistance, metrics.ReferenceCharCount)
	for _, group := range groups {
		group.CER = authorCER(group.EditDistance, group.ReferenceCharCount)
		metrics.Groups = append(metrics.Groups, *group)
	}
	sort.Slice(metrics.Groups, func(i, j int) bool {
		if metrics.Groups[i].CER != metrics.Groups[j].CER {
			return metrics.Groups[i].CER < metrics.Groups[j].CER
		}
		if metrics.Groups[i].SampleCount != metrics.Groups[j].SampleCount {
			return metrics.Groups[i].SampleCount > metrics.Groups[j].SampleCount
		}
		left := strings.Join([]string{metrics.Groups[i].Provider, metrics.Groups[i].Model, metrics.Groups[i].PromptVersion}, "\x00")
		right := strings.Join([]string{metrics.Groups[j].Provider, metrics.Groups[j].Model, metrics.Groups[j].PromptVersion}, "\x00")
		return left < right
	})
	for _, point := range trend {
		point.CER = authorCER(point.EditDistance, point.ReferenceCharCount)
		metrics.Trend = append(metrics.Trend, *point)
	}
	sort.Slice(metrics.Trend, func(i, j int) bool { return metrics.Trend[i].Date < metrics.Trend[j].Date })
	for _, item := range errors {
		metrics.CommonErrors = append(metrics.CommonErrors, *item)
	}
	sortCommonErrors(metrics.CommonErrors)
	if len(metrics.CommonErrors) > 20 {
		metrics.CommonErrors = metrics.CommonErrors[:20]
	}
	return metrics
}

func authorCER(distance, referenceChars int) float64 {
	if referenceChars == 0 {
		if distance == 0 {
			return 0
		}
		return float64(distance)
	}
	return float64(distance) / float64(referenceChars)
}

func sortCommonErrors(items []AuthorCommonError) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		left := strings.Join([]string{items[i].Type, items[i].Source, items[i].Corrected}, "\x00")
		right := strings.Join([]string{items[j].Type, items[j].Source, items[j].Corrected}, "\x00")
		return left < right
	})
}
