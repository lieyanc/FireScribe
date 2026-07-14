package app

import "testing"

func TestConservativeMergeLineageNormalizesOnlyLineEdgeWhitespace(t *testing.T) {
	results := map[string]RecognitionResult{
		"result": {ID: "result", Text: "头\n低词\n尾"},
	}
	segments := conservativeMergeLineage("merge", "头\n  低词  \n尾", []string{"result"}, results)
	if len(segments) != 3 {
		t.Fatalf("segments = %+v", segments)
	}
	middle := segments[1]
	if middle.Text != "低词" || middle.SourceStart != 2 || middle.SourceEnd != 4 || middle.OutputStart != 4 || middle.OutputEnd != 6 {
		t.Fatalf("middle lineage = %+v", middle)
	}
}
