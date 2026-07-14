package recognizer

import (
	"math"
	"testing"
)

func TestExtractConfidenceSegmentsFromOpenAILogprobs(t *testing.T) {
	raw := []byte(`{"choices":[{"logprobs":{"content":[{"token":"火","logprob":-0.1},{"token":"🔥","logprob":-2.0},{"token":"光","logprob":-1.5},{"token":"照","logprob":-0.05}]}}]}`)
	items := ExtractConfidenceSegments(raw, "火🔥光照", 0.5)
	if len(items) != 1 {
		t.Fatalf("segments = %+v, want one merged segment", items)
	}
	if items[0].Text != "🔥光" || items[0].Start != 1 || items[0].End != 4 {
		t.Fatalf("segment = %+v, want UTF-16 range [1,4]", items[0])
	}
	if math.Abs(items[0].Confidence-math.Exp(-2)) > 1e-9 {
		t.Fatalf("confidence = %v", items[0].Confidence)
	}
}

func TestExtractConfidenceSegmentsFromStructuredLines(t *testing.T) {
	raw := []byte(`{"result":{"lines":[{"text":"第一行","confidence":0.92},{"text":"第二行","confidence":31}]}}`)
	items := ExtractConfidenceSegments(raw, "第一行\n第二行", 0.5)
	if len(items) != 1 || items[0].Text != "第二行" || items[0].Level != "line" || items[0].Confidence != 0.31 {
		t.Fatalf("segments = %+v", items)
	}
}

func TestExtractConfidenceSegmentsDoesNotInventPageSegments(t *testing.T) {
	raw := []byte(`{"text":"整页文本","confidence":0.2}`)
	if items := ExtractConfidenceSegments(raw, "整页文本", 0.8); len(items) != 0 {
		t.Fatalf("segments = %+v, want page fallback", items)
	}
}

func TestExtractConfidenceSegmentsUsesOrderedFinestStructuredCollection(t *testing.T) {
	raw := []byte(`{"result":{"paragraphs":[{"text":"重复重复","confidence":0.2}],"words":[{"text":"重复","confidence":0.2},{"text":"重复","confidence":0.3}]}}`)
	items := ExtractConfidenceSegments(raw, "重复重复", 0.5)
	if len(items) != 2 {
		t.Fatalf("segments = %+v, want two word occurrences", items)
	}
	if items[0].Level != "word" || items[0].Start != 0 || items[0].End != 2 || items[1].Start != 2 || items[1].End != 4 {
		t.Fatalf("ordered word offsets = %+v", items)
	}
}

func TestExtractConfidenceSegmentsHonorsExplicitStructuredOffsets(t *testing.T) {
	raw := []byte(`{"words":[{"text":"重复","start":2,"end":4,"confidence":0.2},{"text":"重复","start":0,"end":2,"confidence":0.3}]}`)
	items := ExtractConfidenceSegments(raw, "重复重复", 0.5)
	if len(items) != 2 || items[0].Start != 0 || items[0].Confidence != 0.3 || items[1].Start != 2 || items[1].Confidence != 0.2 {
		t.Fatalf("explicit offset segments = %+v", items)
	}
}
