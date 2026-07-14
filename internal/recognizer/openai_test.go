package recognizer

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stubBackoff(t *testing.T) *[]int {
	t.Helper()
	attempts := &[]int{}
	original := sleepBackoff
	sleepBackoff = func(ctx context.Context, attempt int, retryAfter time.Duration) error {
		*attempts = append(*attempts, attempt)
		return ctx.Err()
	}
	t.Cleanup(func() { sleepBackoff = original })
	return attempts
}

func testImage(t *testing.T, dir string, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}
	path := filepath.Join(dir, "page.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestRecognizer(t *testing.T, serverURL string, mutate func(*OpenAIConfig)) *OpenAIRecognizer {
	t.Helper()
	cfg := OpenAIConfig{
		BaseURL:       serverURL,
		APIKey:        "test-key",
		Model:         "test-vlm",
		PromptVersion: "v1",
		MaxTokens:     32768,
		RetryAttempts: 3,
		Timeout:       5 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return NewOpenAI(cfg)
}

func chatResponse(text, finishReason string) string {
	raw, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{
				"finish_reason": finishReason,
				"message":       map[string]any{"content": text},
			},
		},
	})
	return string(raw)
}

func recognize(t *testing.T, rec *OpenAIRecognizer, imagePath string) (RecognitionResult, error) {
	t.Helper()
	return rec.RecognizePage(context.Background(), PageInput{
		DocumentID: "doc", PageID: "pag", PageNo: 1, ImagePath: imagePath,
	})
}

func TestRecognizePageSuccessAndRequestShape(t *testing.T) {
	imagePath := testImage(t, t.TempDir(), 20, 20)
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("X-Request-Id", "req_test_123")
		fmt.Fprint(w, chatResponse("识别文本", "stop"))
	}))
	defer server.Close()

	result, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err != nil {
		t.Fatalf("RecognizePage() error = %v", err)
	}
	if result.Text != "识别文本" {
		t.Fatalf("Text = %q", result.Text)
	}
	if body["max_tokens"].(float64) != 32768 {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
		t.Fatalf("image url prefix = %q", imageURL[:40])
	}
	if result.Metadata["attempts"].(int) != 1 {
		t.Fatalf("attempts metadata = %v", result.Metadata["attempts"])
	}
	headers, ok := result.Metadata["response_headers"].(map[string]string)
	if !ok || headers["x-request-id"] != "req_test_123" {
		t.Fatalf("response headers metadata = %#v", result.Metadata["response_headers"])
	}
}

func TestRecognizePageRetriesOn429ThenSucceeds(t *testing.T) {
	backoffs := stubBackoff(t)
	imagePath := testImage(t, t.TempDir(), 20, 20)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests <= 2 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, chatResponse("ok text", "stop"))
	}))
	defer server.Close()

	result, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err != nil {
		t.Fatalf("RecognizePage() error = %v", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if len(*backoffs) != 2 {
		t.Fatalf("backoff calls = %d, want 2", len(*backoffs))
	}
	if result.Metadata["attempts"].(int) != 3 {
		t.Fatalf("attempts = %v", result.Metadata["attempts"])
	}
}

func TestRecognizePageExtractsExplicitConfidence(t *testing.T) {
	imagePath := testImage(t, t.TempDir(), 20, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","confidence":87.5,"message":{"content":"识别文本"}}]}`)
	}))
	defer server.Close()

	result, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence == nil || math.Abs(*result.Confidence-0.875) > 1e-9 {
		t.Fatalf("confidence = %v, want 0.875", result.Confidence)
	}
	if result.Metadata["confidence_source"] != "choices[0].confidence" {
		t.Fatalf("confidence source = %v", result.Metadata["confidence_source"])
	}
}

func TestRecognizePageDerivesConfidenceFromTokenLogprobs(t *testing.T) {
	imagePath := testImage(t, t.TempDir(), 20, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"识别文本"},"logprobs":{"content":[{"logprob":-0.1},{"logprob":-0.3}]}}]}`)
	}))
	defer server.Close()

	result, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err != nil {
		t.Fatal(err)
	}
	want := math.Exp(-0.2)
	if result.Confidence == nil || math.Abs(*result.Confidence-want) > 1e-9 {
		t.Fatalf("confidence = %v, want %v", result.Confidence, want)
	}
	if result.Metadata["confidence_source"] != "choices[0].logprobs.content" {
		t.Fatalf("confidence source = %v", result.Metadata["confidence_source"])
	}
}

func TestRecognizePageDoesNotRetryClientErrors(t *testing.T) {
	stubBackoff(t)
	imagePath := testImage(t, t.TempDir(), 20, 20)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("error = %v, want provider 400 message", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (no retry on 4xx)", requests)
	}
}

func TestRecognizePageDetectsTruncation(t *testing.T) {
	stubBackoff(t)
	imagePath := testImage(t, t.TempDir(), 20, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse("被截断的文本", "length"))
	}))
	defer server.Close()

	_, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("error = %v, want truncation error", err)
	}
}

func TestRecognizePageRejectsEmptyTranscription(t *testing.T) {
	stubBackoff(t)
	imagePath := testImage(t, t.TempDir(), 20, 20)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, chatResponse("  ", "stop"))
	}))
	defer server.Close()

	_, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err == nil || !strings.Contains(err.Error(), "empty transcription") {
		t.Fatalf("error = %v, want empty transcription error", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3 (empty text is retryable)", requests)
	}
}

func TestRecognizePageSurfacesBodyOnMissingChoices(t *testing.T) {
	stubBackoff(t)
	imagePath := testImage(t, t.TempDir(), 20, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"chat.completion","choices":[]}`)
	}))
	defer server.Close()

	_, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err == nil || !strings.Contains(err.Error(), "chat.completion") {
		t.Fatalf("error = %v, want body snippet in error", err)
	}
}

func TestRecognizePageSurfacesErrorBodyOn200(t *testing.T) {
	stubBackoff(t)
	imagePath := testImage(t, t.TempDir(), 20, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":{"message":"upstream quota exceeded"}}`)
	}))
	defer server.Close()

	_, err := recognize(t, newTestRecognizer(t, server.URL, nil), imagePath)
	if err == nil || !strings.Contains(err.Error(), "upstream quota exceeded") {
		t.Fatalf("error = %v, want provider error message", err)
	}
}

func TestRecognizePageDownscalesLargeImages(t *testing.T) {
	imagePath := testImage(t, t.TempDir(), 1200, 300)
	var imageURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
		imageURL = content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
		fmt.Fprint(w, chatResponse("text", "stop"))
	}))
	defer server.Close()

	rec := newTestRecognizer(t, server.URL, func(cfg *OpenAIConfig) { cfg.MaxImageEdge = 512 })
	if _, err := recognize(t, rec, imagePath); err != nil {
		t.Fatalf("RecognizePage() error = %v", err)
	}
	if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
		t.Fatalf("downscaled image should be JPEG, got prefix %q", imageURL[:30])
	}
}

func TestPromptVersionTracksFileContent(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(promptPath, []byte("第一版提示词"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newTestRecognizer(t, "http://unused", func(cfg *OpenAIConfig) { cfg.PromptPath = promptPath })

	v1 := rec.PromptVersion()
	if !strings.HasPrefix(v1, "v1#") {
		t.Fatalf("PromptVersion = %q", v1)
	}
	if err := os.WriteFile(promptPath, []byte("第二版提示词"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v2 := rec.PromptVersion(); v2 == v1 {
		t.Fatalf("PromptVersion did not change after prompt edit: %q", v2)
	}
}

func TestPromptFallsBackToEmbeddedDefault(t *testing.T) {
	rec := newTestRecognizer(t, "http://unused", func(cfg *OpenAIConfig) {
		cfg.PromptPath = filepath.Join(t.TempDir(), "missing.txt")
	})
	prompt, _ := rec.promptText()
	if !strings.Contains(prompt, "忠实转录") {
		t.Fatalf("fallback prompt = %q", prompt)
	}
}

func TestPromptSnapshotOverrideIsSentWithoutChangingBaseRecognizer(t *testing.T) {
	imagePath := testImage(t, t.TempDir(), 20, 20)
	var sentPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		messages := body["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		sentPrompt = content[0].(map[string]any)["text"].(string)
		fmt.Fprint(w, chatResponse("识别文本", "stop"))
	}))
	defer server.Close()

	base := newTestRecognizer(t, server.URL, func(cfg *OpenAIConfig) {
		cfg.PromptText = "A 版本提示词"
		cfg.PromptVersion = "a"
	})
	overridden := base.WithPromptSnapshot("B 版本提示词", "b")
	if _, err := overridden.RecognizePage(context.Background(), PageInput{PageID: "page", PageNo: 1, ImagePath: imagePath}); err != nil {
		t.Fatal(err)
	}
	if sentPrompt != "B 版本提示词" || !strings.HasPrefix(overridden.PromptVersion(), "b#") {
		t.Fatalf("sent prompt=%q version=%q", sentPrompt, overridden.PromptVersion())
	}
	if prompt, _ := base.promptText(); prompt != "A 版本提示词" {
		t.Fatalf("base recognizer was mutated: %q", prompt)
	}
}
