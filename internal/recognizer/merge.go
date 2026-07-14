package recognizer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const ConservativeMergePromptVersion = "vlm_merge_candidates_v1"

const ConservativeMergePrompt = `你是手稿转录候选稿的保守合并器。
只能输出候选文本中逐字可见的完整行；可以从不同候选中选择行、去除重复行或省略冲突内容。
严禁改写、润色、补全、推测、解释或创造任何候选中没有原样出现的行。
不要输出标题、说明、Markdown 代码块或候选编号。若有冲突，选择证据最一致的一行；无法判断时省略。`

type CandidateMergeInput struct {
	PageID     string
	Candidates []string
}

type CandidateMergeResult struct {
	Text        string
	RawResponse []byte
}

type CandidateMerger interface {
	MergeCandidates(context.Context, CandidateMergeInput) (CandidateMergeResult, error)
}

func MergePromptHash() string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ConservativeMergePrompt)))
	return hex.EncodeToString(sum[:])
}

func (MockRecognizer) MergeCandidates(_ context.Context, input CandidateMergeInput) (CandidateMergeResult, error) {
	if len(input.Candidates) < 2 {
		return CandidateMergeResult{}, fmt.Errorf("at least two candidates are required")
	}
	text := strings.TrimSpace(input.Candidates[0])
	if text == "" {
		return CandidateMergeResult{}, fmt.Errorf("candidate text must not be empty")
	}
	raw, _ := json.Marshal(map[string]any{
		"driver":         "mock",
		"prompt_version": ConservativeMergePromptVersion,
		"text":           text,
	})
	return CandidateMergeResult{Text: text, RawResponse: raw}, nil
}

func ValidateConservativeMerge(text string, candidates []string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("merge returned empty text")
	}
	visibleLines := map[string]bool{}
	maxOccurrences := map[string]int{}
	candidateLines := make([][]string, 0, len(candidates))
	for _, candidate := range candidates {
		lines := make([]string, 0)
		counts := map[string]int{}
		for _, line := range strings.Split(strings.ReplaceAll(candidate, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				visibleLines[line] = true
				lines = append(lines, line)
				counts[line]++
			}
		}
		for line, count := range counts {
			if count > maxOccurrences[line] {
				maxOccurrences[line] = count
			}
		}
		candidateLines = append(candidateLines, lines)
	}
	outputLines := make([]string, 0)
	outputCounts := map[string]int{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !visibleLines[line] {
			return fmt.Errorf("merge introduced a line that is not present verbatim in any source candidate")
		}
		if line != "" {
			outputLines = append(outputLines, line)
			outputCounts[line]++
			if outputCounts[line] > maxOccurrences[line] {
				return fmt.Errorf("merge repeated a source line more times than any candidate")
			}
		}
	}
	for i := 0; i < len(outputLines); i++ {
		for j := i + 1; j < len(outputLines); j++ {
			if outputLines[i] == outputLines[j] {
				continue
			}
			sameOrder, oppositeOrder := false, false
			for _, lines := range candidateLines {
				first, second := -1, -1
				for index, line := range lines {
					if first < 0 && line == outputLines[i] {
						first = index
					}
					if second < 0 && line == outputLines[j] {
						second = index
					}
				}
				if first >= 0 && second >= 0 {
					if first < second {
						sameOrder = true
					} else {
						oppositeOrder = true
					}
				}
			}
			if oppositeOrder && !sameOrder {
				return fmt.Errorf("merge reordered source lines against every candidate that contains them")
			}
		}
	}
	return nil
}
