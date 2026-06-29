package recognizer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type OpenAIConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	Prompt        string
	PromptVersion string
	Temperature   float64
	MaxTokens     int
	Timeout       time.Duration
}

type OpenAIRecognizer struct {
	cfg    OpenAIConfig
	client *http.Client
}

func NewOpenAI(cfg OpenAIConfig) *OpenAIRecognizer {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &OpenAIRecognizer{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}
}

func (r *OpenAIRecognizer) Name() string          { return "openai-compatible" }
func (r *OpenAIRecognizer) Provider() string      { return "openai-compatible" }
func (r *OpenAIRecognizer) Model() string         { return r.cfg.Model }
func (r *OpenAIRecognizer) PromptVersion() string { return r.cfg.PromptVersion }

func (r *OpenAIRecognizer) ConfigJSON() string {
	raw, _ := json.Marshal(map[string]any{
		"base_url":       r.cfg.BaseURL,
		"model":          r.cfg.Model,
		"prompt_version": r.cfg.PromptVersion,
		"temperature":    r.cfg.Temperature,
		"max_tokens":     r.cfg.MaxTokens,
	})
	return string(raw)
}

func (r *OpenAIRecognizer) RecognizePage(ctx context.Context, input PageInput) (RecognitionResult, error) {
	if strings.TrimSpace(r.cfg.APIKey) == "" {
		return RecognitionResult{}, fmt.Errorf("OpenAI compatible API key is not configured")
	}
	if strings.TrimSpace(r.cfg.Model) == "" {
		return RecognitionResult{}, fmt.Errorf("OpenAI compatible model is not configured")
	}
	imageURL, err := dataURL(input.ImagePath)
	if err != nil {
		return RecognitionResult{}, err
	}

	prompt := strings.TrimSpace(r.cfg.Prompt)
	if prompt == "" {
		prompt = "Transcribe the visible handwritten text faithfully. Return only the transcription."
	}

	body := map[string]any{
		"model": r.cfg.Model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
				},
			},
		},
		"temperature": r.cfg.Temperature,
		"max_tokens":  r.cfg.MaxTokens,
	}
	payload, _ := json.Marshal(body)
	endpoint := strings.TrimRight(r.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return RecognitionResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return RecognitionResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return RecognitionResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RecognitionResult{}, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	text, err := extractText(raw)
	if err != nil {
		return RecognitionResult{}, err
	}
	return RecognitionResult{Text: text, RawJSON: raw, Metadata: map[string]any{"status": resp.StatusCode}}, nil
}

func dataURL(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func extractText(raw []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("provider response has no choices")
	}
	switch content := parsed.Choices[0].Message.Content.(type) {
	case string:
		return strings.TrimSpace(content), nil
	case []any:
		var parts []string
		for _, item := range content {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := obj["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.TrimSpace(strings.Join(parts, "\n")), nil
		}
	}
	return "", fmt.Errorf("provider response did not include text content")
}
