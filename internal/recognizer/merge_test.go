package recognizer

import (
	"strings"
	"testing"
)

func TestValidateConservativeMergeRejectsRepetitionAndKnownReordering(t *testing.T) {
	candidates := []string{"甲\n乙\n丙", "甲\n乙\n丁"}
	if err := ValidateConservativeMerge("甲\n乙\n丁", candidates); err != nil {
		t.Fatalf("valid conservative merge rejected: %v", err)
	}
	if err := ValidateConservativeMerge("甲\n甲\n乙", candidates); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("repetition error = %v", err)
	}
	if err := ValidateConservativeMerge("乙\n甲", candidates); err == nil || !strings.Contains(err.Error(), "reordered") {
		t.Fatalf("reordering error = %v", err)
	}
}
