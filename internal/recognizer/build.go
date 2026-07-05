package recognizer

import (
	"log"

	"github.com/lieyan/firescribe/internal/config"
)

// Build constructs the recognizer described by cfg. It is used at startup and
// again whenever settings change at runtime.
func Build(cfg config.Config) Recognizer {
	if cfg.UseMockOCR {
		log.Printf("OCR recognizer: mock (set use_mock_ocr=false, openai.model and openai.api_key in %s to use OpenAI compatible OCR)", cfg.Path)
		return MockRecognizer{}
	}
	log.Printf("OCR recognizer: OpenAI compatible model=%s base_url=%s max_tokens=%d max_image_edge=%d retry_attempts=%d",
		cfg.OpenAI.Model, cfg.OpenAI.BaseURL, cfg.OpenAI.MaxTokens, cfg.OpenAI.MaxImageEdge, cfg.OpenAI.RetryAttempts)
	return NewOpenAI(OpenAIConfig{
		BaseURL:       cfg.OpenAI.BaseURL,
		APIKey:        cfg.OpenAI.APIKey,
		Model:         cfg.OpenAI.Model,
		PromptPath:    cfg.PromptPath,
		PromptVersion: cfg.OpenAI.PromptVersion,
		Temperature:   cfg.OpenAI.Temperature,
		MaxTokens:     cfg.OpenAI.MaxTokens,
		MaxImageEdge:  cfg.OpenAI.MaxImageEdge,
		RetryAttempts: cfg.OpenAI.RetryAttempts,
		Timeout:       cfg.RequestTimeout,
	})
}
