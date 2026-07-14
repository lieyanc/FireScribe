package recognizer

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"unicode/utf16"
)

// ConfidenceSegment identifies a provider-reported low-confidence token,
// word, line, or paragraph inside normalized recognition text. Offsets use
// UTF-16 code units so browsers can apply them directly to textarea ranges.
type ConfidenceSegment struct {
	Text       string  `json:"text"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Confidence float64 `json:"confidence"`
	Level      string  `json:"level"`
	Source     string  `json:"source"`
}

// ExtractConfidenceSegments reads common provider response shapes without
// inventing fine-grained confidence when the provider only returns a page
// score. Unknown shapes safely produce an empty list and the caller can keep
// its page-level fallback.
func ExtractConfidenceSegments(raw []byte, resultText string, threshold float64) []ConfidenceSegment {
	if len(raw) == 0 || strings.TrimSpace(resultText) == "" {
		return nil
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}

	segments := extractOpenAITokenSegments(root, resultText, threshold)
	segments = append(segments, extractStructuredSegments(root, resultText, threshold)...)
	return deduplicateConfidenceSegments(segments)
}

func extractOpenAITokenSegments(root any, text string, threshold float64) []ConfidenceSegment {
	value, ok := jsonPath(root, "choices", "0", "logprobs", "content")
	if !ok {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}

	cursor := 0
	segments := make([]ConfidenceSegment, 0)
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		token, _ := entry["token"].(string)
		logprob, ok := numericValue(entry["logprob"])
		if token == "" || !ok || math.IsNaN(logprob) || math.IsInf(logprob, 0) {
			continue
		}
		start := strings.Index(text[cursor:], token)
		if start < 0 {
			continue
		}
		start += cursor
		end := start + len(token)
		cursor = end
		confidence := math.Exp(logprob)
		if confidence > threshold || math.IsNaN(confidence) || math.IsInf(confidence, 0) {
			continue
		}
		segments = append(segments, confidenceSegment(text, start, end, confidence, "token", "choices[0].logprobs.content"))
	}
	return mergeAdjacentTokenSegments(segments, text)
}

func extractStructuredSegments(root any, text string, threshold float64) []ConfidenceSegment {
	type candidate struct {
		text       string
		confidence float64
		level      string
		source     string
		byteStart  int
		byteEnd    int
		explicit   bool
	}
	candidates := make([]candidate, 0)
	var walk func(any, []string, string)
	walk = func(value any, path []string, hintedLevel string) {
		switch current := value.(type) {
		case []any:
			for _, item := range current {
				walk(item, path, hintedLevel)
			}
		case map[string]any:
			level := hintedLevel
			segmentText, _ := current["text"].(string)
			confidence, sourceKey, found := objectConfidence(current)
			if strings.TrimSpace(segmentText) != "" && found && confidence <= threshold && level != "" {
				byteStart, byteEnd, explicit := structuredByteRange(text, current, segmentText)
				candidates = append(candidates, candidate{
					text: segmentText, confidence: confidence, level: level,
					source:    strings.Join(append(path, sourceKey), "."),
					byteStart: byteStart, byteEnd: byteEnd, explicit: explicit,
				})
			}
			keys := make([]string, 0, len(current))
			for key := range current {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := current[key]
				childLevel := level
				if inferred := confidenceLevel(key); inferred != "" {
					childLevel = inferred
				}
				walk(child, append(path, key), childLevel)
			}
		}
	}
	walk(root, nil, "")
	finest := 99
	for _, item := range candidates {
		if rank := confidenceLevelRank(item.level); rank < finest {
			finest = rank
		}
	}

	cursor := 0
	segments := make([]ConfidenceSegment, 0, len(candidates))
	for _, item := range candidates {
		if confidenceLevelRank(item.level) != finest {
			continue
		}
		start := item.byteStart
		if !item.explicit {
			start = indexFrom(text, item.text, cursor)
		}
		if start < 0 {
			continue
		}
		end := start + len(item.text)
		if item.explicit {
			end = item.byteEnd
		}
		cursor = end
		segments = append(segments, confidenceSegment(text, start, end, item.confidence, item.level, item.source))
	}
	return segments
}

func structuredByteRange(text string, object map[string]any, fragment string) (int, int, bool) {
	start, hasStart := integerField(object, "start", "start_offset", "offset")
	end, hasEnd := integerField(object, "end", "end_offset")
	if !hasEnd {
		if length, ok := integerField(object, "length"); ok && hasStart {
			end, hasEnd = start+length, true
		}
	}
	if !hasStart || !hasEnd || start < 0 || end < start {
		return 0, 0, false
	}
	if byteStart, ok := runeOffsetToByte(text, start); ok {
		if byteEnd, ok := runeOffsetToByte(text, end); ok && text[byteStart:byteEnd] == fragment {
			return byteStart, byteEnd, true
		}
	}
	if end <= len(text) && text[start:end] == fragment {
		return start, end, true
	}
	return 0, 0, false
}

func integerField(object map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := numericValue(object[key])
		if ok && value >= 0 && value == math.Trunc(value) {
			return int(value), true
		}
	}
	return 0, false
}

func runeOffsetToByte(value string, target int) (int, bool) {
	if target == 0 {
		return 0, true
	}
	count := 0
	for index := range value {
		if count == target {
			return index, true
		}
		count++
	}
	if count == target {
		return len(value), true
	}
	return 0, false
}

func confidenceLevelRank(level string) int {
	switch level {
	case "token":
		return 0
	case "word":
		return 1
	case "line":
		return 2
	case "paragraph":
		return 3
	default:
		return 99
	}
}

func indexFrom(text, fragment string, cursor int) int {
	if cursor >= 0 && cursor <= len(text) {
		if offset := strings.Index(text[cursor:], fragment); offset >= 0 {
			return cursor + offset
		}
	}
	return strings.Index(text, fragment)
}

func confidenceLevel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "token", "tokens":
		return "token"
	case "word", "words":
		return "word"
	case "line", "lines":
		return "line"
	case "paragraph", "paragraphs", "block", "blocks", "segment", "segments":
		return "paragraph"
	default:
		return ""
	}
}

func objectConfidence(object map[string]any) (float64, string, bool) {
	for _, key := range []string{"confidence", "score", "probability", "prob"} {
		if value, ok := numericValue(object[key]); ok {
			if normalized, ok := normalizeConfidence(value); ok {
				return normalized, key, true
			}
		}
	}
	if value, ok := numericValue(object["logprob"]); ok && value <= 0 {
		confidence := math.Exp(value)
		if !math.IsNaN(confidence) && !math.IsInf(confidence, 0) {
			return confidence, "logprob", true
		}
	}
	return 0, "", false
}

func confidenceSegment(text string, byteStart, byteEnd int, confidence float64, level, source string) ConfidenceSegment {
	return ConfidenceSegment{
		Text:       text[byteStart:byteEnd],
		Start:      utf16Length(text[:byteStart]),
		End:        utf16Length(text[:byteEnd]),
		Confidence: confidence,
		Level:      level,
		Source:     source,
	}
}

func utf16Length(value string) int {
	length := 0
	for _, r := range value {
		length += utf16.RuneLen(r)
	}
	return length
}

func mergeAdjacentTokenSegments(items []ConfidenceSegment, text string) []ConfidenceSegment {
	if len(items) < 2 {
		return items
	}
	merged := []ConfidenceSegment{items[0]}
	for _, item := range items[1:] {
		last := &merged[len(merged)-1]
		if item.Start == last.End {
			last.Text += item.Text
			last.End = item.End
			if item.Confidence < last.Confidence {
				last.Confidence = item.Confidence
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func deduplicateConfidenceSegments(items []ConfidenceSegment) []ConfidenceSegment {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Start != items[j].Start {
			return items[i].Start < items[j].Start
		}
		if items[i].End != items[j].End {
			return items[i].End < items[j].End
		}
		return items[i].Confidence < items[j].Confidence
	})
	result := make([]ConfidenceSegment, 0, len(items))
	seen := map[[2]int]bool{}
	for _, item := range items {
		key := [2]int{item.Start, item.End}
		if item.End <= item.Start || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) >= 1000 {
			break
		}
	}
	return result
}
