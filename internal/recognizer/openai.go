package recognizer

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lieyan/firescribe/internal/pageproc"
)

//go:embed default_prompt.txt
var DefaultPrompt string

type OpenAIConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	PromptPath    string
	PromptVersion string
	Temperature   float64
	MaxTokens     int
	MaxImageEdge  int
	RetryAttempts int
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
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 3
	}
	return &OpenAIRecognizer{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
	}
}

func (r *OpenAIRecognizer) Name() string     { return "openai-compatible" }
func (r *OpenAIRecognizer) Provider() string { return "openai-compatible" }
func (r *OpenAIRecognizer) Model() string    { return r.cfg.Model }

// PromptVersion couples the configured version label with a hash of the
// actual prompt text so runs stay auditable after prompt file edits.
func (r *OpenAIRecognizer) PromptVersion() string {
	_, hash := r.promptText()
	version := strings.TrimSpace(r.cfg.PromptVersion)
	if version == "" {
		version = "prompt"
	}
	return version + "#" + hash
}

func (r *OpenAIRecognizer) ConfigJSON() string {
	_, hash := r.promptText()
	raw, _ := json.Marshal(map[string]any{
		"base_url":       r.cfg.BaseURL,
		"model":          r.cfg.Model,
		"prompt_version": r.cfg.PromptVersion,
		"prompt_hash":    hash,
		"temperature":    r.cfg.Temperature,
		"max_tokens":     r.cfg.MaxTokens,
		"max_image_edge": r.cfg.MaxImageEdge,
		"retry_attempts": r.cfg.RetryAttempts,
	})
	return string(raw)
}

// promptText loads the prompt file on every call so edits (via the settings
// API or directly on disk) take effect without a restart; missing or empty
// files fall back to the embedded default prompt.
func (r *OpenAIRecognizer) promptText() (string, string) {
	prompt := ""
	if path := strings.TrimSpace(r.cfg.PromptPath); path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			prompt = strings.TrimSpace(string(raw))
		}
	}
	if prompt == "" {
		prompt = strings.TrimSpace(DefaultPrompt)
	}
	sum := sha256.Sum256([]byte(prompt))
	return prompt, hex.EncodeToString(sum[:4])
}

func (r *OpenAIRecognizer) RecognizePage(ctx context.Context, input PageInput) (RecognitionResult, error) {
	if strings.TrimSpace(r.cfg.APIKey) == "" {
		return RecognitionResult{}, fmt.Errorf("OpenAI compatible API key is not configured")
	}
	if strings.TrimSpace(r.cfg.Model) == "" {
		return RecognitionResult{}, fmt.Errorf("OpenAI compatible model is not configured")
	}

	mimeType, imageData, err := pageproc.PrepareForUpload(input.ImagePath, r.cfg.MaxImageEdge)
	if err != nil {
		return RecognitionResult{}, fmt.Errorf("prepare page image: %w", err)
	}
	imageURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageData)
	prompt, promptHash := r.promptText()

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
	}
	if r.cfg.MaxTokens > 0 {
		body["max_tokens"] = r.cfg.MaxTokens
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return RecognitionResult{}, err
	}
	endpoint := strings.TrimRight(r.cfg.BaseURL, "/") + "/chat/completions"

	var lastErr error
	for attempt := 1; attempt <= r.cfg.RetryAttempts; attempt++ {
		result, retryable, retryAfter, err := r.attempt(ctx, endpoint, payload)
		if err == nil {
			result.Metadata = map[string]any{
				"attempts":    attempt,
				"upload_mime": mimeType,
				"prompt_hash": promptHash,
			}
			return result, nil
		}
		lastErr = err
		if !retryable || attempt == r.cfg.RetryAttempts {
			break
		}
		if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
			return RecognitionResult{}, err
		}
	}
	return RecognitionResult{}, lastErr
}

// attempt performs one provider request. retryable marks transient failures
// (network errors, 408/429/5xx, empty transcriptions) worth another attempt.
func (r *OpenAIRecognizer) attempt(ctx context.Context, endpoint string, payload []byte) (result RecognitionResult, retryable bool, retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return RecognitionResult{}, false, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return RecognitionResult{}, false, 0, ctx.Err()
		}
		return RecognitionResult{}, true, 0, fmt.Errorf("request provider: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return RecognitionResult{}, true, 0, fmt.Errorf("read provider response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == http.StatusRequestTimeout ||
			resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode >= 500
		return RecognitionResult{}, retryable, parseRetryAfter(resp.Header.Get("Retry-After")),
			fmt.Errorf("provider returned %s: %s", resp.Status, bodySnippet(raw))
	}

	text, finishReason, err := extractText(raw)
	if err != nil {
		return RecognitionResult{}, false, 0, err
	}
	if finishReason == "length" {
		return RecognitionResult{}, false, 0,
			fmt.Errorf("transcription truncated by max_tokens limit (finish_reason=length); increase openai.max_tokens in settings")
	}
	if strings.TrimSpace(text) == "" {
		return RecognitionResult{}, true, 0, fmt.Errorf("provider returned an empty transcription")
	}
	return RecognitionResult{Text: text, RawJSON: raw}, false, 0, nil
}

// sleepBackoff waits before the next retry attempt; a variable so tests can
// stub the delay out.
var sleepBackoff = func(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := time.Duration(1<<(2*attempt)) * time.Second / 2 // 2s, 8s, 32s…
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

func bodySnippet(raw []byte) string {
	snippet := strings.TrimSpace(string(raw))
	if len(snippet) > 500 {
		snippet = snippet[:500] + "…"
	}
	if snippet == "" {
		return "(empty body)"
	}
	return snippet
}

func extractText(raw []byte) (string, string, error) {
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", fmt.Errorf("parse provider response: %w: %s", err, bodySnippet(raw))
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", "", fmt.Errorf("provider error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", "", fmt.Errorf("provider response has no choices: %s", bodySnippet(raw))
	}
	choice := parsed.Choices[0]
	switch content := choice.Message.Content.(type) {
	case string:
		return strings.TrimSpace(content), choice.FinishReason, nil
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
			return strings.TrimSpace(strings.Join(parts, "\n")), choice.FinishReason, nil
		}
	}
	return "", "", fmt.Errorf("provider response did not include text content: %s", bodySnippet(raw))
}
