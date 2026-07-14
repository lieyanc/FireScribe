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
	"math"
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
	PromptText    string
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

func (r *OpenAIRecognizer) WithPromptSnapshot(text, version string) Recognizer {
	cfg := r.cfg
	cfg.PromptPath = ""
	cfg.PromptText = text
	cfg.PromptVersion = version
	return NewOpenAI(cfg)
}

func (r *OpenAIRecognizer) PromptSnapshotText() string {
	prompt, _ := r.promptText()
	return prompt
}

func (r *OpenAIRecognizer) RetrySecret() string { return r.cfg.APIKey }

// promptText loads the prompt file on every call so edits (via the settings
// API or directly on disk) take effect without a restart; missing or empty
// files fall back to the embedded default prompt.
func (r *OpenAIRecognizer) promptText() (string, string) {
	prompt := strings.TrimSpace(r.cfg.PromptText)
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

func hashPrompt(prompt string) (string, string) {
	prompt = strings.TrimSpace(prompt)
	sum := sha256.Sum256([]byte(prompt))
	return prompt, hex.EncodeToString(sum[:4])
}

func promptVersionWithHash(version, prompt string) string {
	_, hash := hashPrompt(prompt)
	version = strings.TrimSpace(version)
	if version == "" {
		version = "prompt"
	}
	return version + "#" + hash
}

func (r *OpenAIRecognizer) MergeCandidates(ctx context.Context, input CandidateMergeInput) (CandidateMergeResult, error) {
	if len(input.Candidates) < 2 {
		return CandidateMergeResult{}, fmt.Errorf("at least two candidates are required")
	}
	if strings.TrimSpace(r.cfg.APIKey) == "" || strings.TrimSpace(r.cfg.Model) == "" {
		return CandidateMergeResult{}, fmt.Errorf("OpenAI compatible API key and model are required")
	}
	var user strings.Builder
	for index, candidate := range input.Candidates {
		fmt.Fprintf(&user, "候选 %d：\n%s\n\n", index+1, candidate)
	}
	body := map[string]any{
		"model": r.cfg.Model,
		"messages": []map[string]any{
			{"role": "system", "content": ConservativeMergePrompt},
			{"role": "user", "content": strings.TrimSpace(user.String())},
		},
		"temperature": 0,
	}
	if r.cfg.MaxTokens > 0 {
		body["max_tokens"] = r.cfg.MaxTokens
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return CandidateMergeResult{}, err
	}
	endpoint := strings.TrimRight(r.cfg.BaseURL, "/") + "/chat/completions"
	var lastErr error
	for attempt := 1; attempt <= r.cfg.RetryAttempts; attempt++ {
		result, retryable, retryAfter, err := r.attempt(ctx, endpoint, payload)
		if err == nil {
			if err := ValidateConservativeMerge(result.Text, input.Candidates); err != nil {
				return CandidateMergeResult{}, err
			}
			return CandidateMergeResult{Text: strings.TrimSpace(result.Text), RawResponse: result.RawJSON}, nil
		}
		lastErr = err
		if !retryable || attempt == r.cfg.RetryAttempts {
			break
		}
		if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
			return CandidateMergeResult{}, err
		}
	}
	return CandidateMergeResult{}, lastErr
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
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			result.Metadata["attempts"] = attempt
			result.Metadata["upload_mime"] = mimeType
			result.Metadata["prompt_hash"] = promptHash
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
	metadata := map[string]any{}
	if headers := auditResponseHeaders(resp.Header); len(headers) > 0 {
		metadata["response_headers"] = headers
	}
	confidence, confidenceSource := extractConfidence(raw)
	if confidenceSource != "" {
		metadata["confidence_source"] = confidenceSource
	}
	return RecognitionResult{Text: text, Confidence: confidence, RawJSON: raw, Metadata: metadata}, false, 0, nil
}

// auditResponseHeaders keeps provider correlation identifiers without
// persisting authorization, cookies, or unrelated response headers. Different
// OpenAI-compatible providers use different names for the same concept.
func auditResponseHeaders(header http.Header) map[string]string {
	keys := []string{
		"x-request-id",
		"request-id",
		"openai-request-id",
		"x-trace-id",
		"traceparent",
		"cf-ray",
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			result[key] = value
		}
	}
	return result
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

// extractConfidence recognizes common OpenAI-compatible confidence shapes.
// Providers are inconsistent here, so only explicit confidence/score fields
// and mathematically derived token probabilities are accepted.
func extractConfidence(raw []byte) (*float64, string) {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return nil, ""
	}

	paths := []struct {
		name string
		keys []string
	}{
		{"confidence", []string{"confidence"}},
		{"score", []string{"score"}},
		{"result.confidence", []string{"result", "confidence"}},
		{"data.confidence", []string{"data", "confidence"}},
		{"choices[0].confidence", []string{"choices", "0", "confidence"}},
		{"choices[0].score", []string{"choices", "0", "score"}},
		{"choices[0].message.confidence", []string{"choices", "0", "message", "confidence"}},
	}
	for _, path := range paths {
		if value, ok := jsonPath(root, path.keys...); ok {
			if confidence, ok := normalizeConfidence(value); ok {
				return &confidence, path.name
			}
		}
	}

	choiceValue, ok := jsonPath(root, "choices", "0")
	if !ok {
		return nil, ""
	}
	choice, ok := choiceValue.(map[string]any)
	if !ok {
		return nil, ""
	}
	if avg, ok := numericValue(choice["avg_logprob"]); ok && avg <= 0 {
		confidence := math.Exp(avg)
		if !math.IsNaN(confidence) && !math.IsInf(confidence, 0) {
			return &confidence, "choices[0].avg_logprob"
		}
	}
	logprobs, _ := choice["logprobs"].(map[string]any)
	content, _ := logprobs["content"].([]any)
	var sum float64
	count := 0
	for _, item := range content {
		entry, _ := item.(map[string]any)
		logprob, ok := numericValue(entry["logprob"])
		if !ok || math.IsNaN(logprob) || math.IsInf(logprob, 0) {
			continue
		}
		sum += logprob
		count++
	}
	if count > 0 {
		confidence := math.Exp(sum / float64(count))
		return &confidence, "choices[0].logprobs.content"
	}
	return nil, ""
}

func jsonPath(root any, keys ...string) (any, bool) {
	current := root
	for _, key := range keys {
		switch value := current.(type) {
		case map[string]any:
			current, _ = value[key]
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
		if current == nil {
			return nil, false
		}
	}
	return current, true
}

func normalizeConfidence(value any) (float64, bool) {
	number, ok := numericValue(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return 0, false
	}
	if number <= 1 {
		return number, true
	}
	if number <= 100 {
		return number / 100, true
	}
	return 0, false
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
