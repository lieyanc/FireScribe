package recognizer

import (
	"context"
	"encoding/json"
	"fmt"
)

type PageInput struct {
	DocumentID string
	PageID     string
	PageNo     int
	ImagePath  string
	Width      int
	Height     int
}

type RecognitionResult struct {
	Text       string
	Confidence *float64
	RawJSON    []byte
	Metadata   map[string]any
}

type Recognizer interface {
	Name() string
	Provider() string
	Model() string
	PromptVersion() string
	ConfigJSON() string
	RecognizePage(ctx context.Context, input PageInput) (RecognitionResult, error)
}

type MockRecognizer struct{}

func (MockRecognizer) Name() string          { return "mock" }
func (MockRecognizer) Provider() string      { return "mock" }
func (MockRecognizer) Model() string         { return "mock-vlm" }
func (MockRecognizer) PromptVersion() string { return "mock" }
func (MockRecognizer) ConfigJSON() string    { return `{"mode":"mock"}` }

func (MockRecognizer) RecognizePage(_ context.Context, input PageInput) (RecognitionResult, error) {
	text := fmt.Sprintf("（模拟识别）第 %d 页\n\n这里会保存 OpenAI 兼容 OCR/VLM 返回的候选文本。请在右侧校对区替换为真实转录内容。", input.PageNo)
	raw, _ := json.Marshal(map[string]any{
		"provider": "mock",
		"model":    "mock-vlm",
		"page_id":  input.PageID,
		"text":     text,
	})
	confidence := 0.5
	return RecognitionResult{
		Text:       text,
		Confidence: &confidence,
		RawJSON:    raw,
		Metadata:   map[string]any{"mock": true},
	}, nil
}
