package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	DataDir        string
	DatabasePath   string
	WebDir         string
	OpenAI         OpenAIConfig
	PromptPath     string
	UseMockOCR     bool
	RequestTimeout time.Duration
}

type OpenAIConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	PromptVersion string
	Temperature   float64
	MaxTokens     int
}

func Load() Config {
	dataDir := env("FIRESCRIBE_DATA_DIR", "data")
	timeoutSeconds := envInt("FIRESCRIBE_OCR_TIMEOUT_SECONDS", 120)
	apiKey := os.Getenv(env("FIRESCRIBE_OPENAI_API_KEY_ENV", "OPENAI_API_KEY"))
	baseURL := strings.TrimRight(env("FIRESCRIBE_OPENAI_BASE_URL", env("OPENAI_BASE_URL", "https://api.openai.com/v1")), "/")
	model := env("FIRESCRIBE_OPENAI_MODEL", env("OPENAI_MODEL", ""))

	return Config{
		Addr:           env("FIRESCRIBE_ADDR", ":8080"),
		DataDir:        dataDir,
		DatabasePath:   env("FIRESCRIBE_DB_PATH", filepath.Join(dataDir, "firescribe.db")),
		WebDir:         env("FIRESCRIBE_WEB_DIR", filepath.Join("web", "dist")),
		PromptPath:     env("FIRESCRIBE_PROMPT_PATH", filepath.Join("prompts", "vlm_transcribe_page_v1.txt")),
		UseMockOCR:     envBool("FIRESCRIBE_USE_MOCK_OCR", apiKey == "" || model == ""),
		RequestTimeout: time.Duration(timeoutSeconds) * time.Second,
		OpenAI: OpenAIConfig{
			BaseURL:       baseURL,
			APIKey:        apiKey,
			Model:         model,
			PromptVersion: env("FIRESCRIBE_PROMPT_VERSION", "vlm_transcribe_page_v1"),
			Temperature:   envFloat("FIRESCRIBE_OPENAI_TEMPERATURE", 0),
			MaxTokens:     envInt("FIRESCRIBE_OPENAI_MAX_TOKENS", 4096),
		},
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
