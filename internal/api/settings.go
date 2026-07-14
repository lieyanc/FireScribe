package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/lieyan/firescribe/internal/config"
	"github.com/lieyan/firescribe/internal/recognizer"
)

type settingsOpenAI struct {
	BaseURL       string  `json:"base_url"`
	Model         string  `json:"model"`
	APIKeySet     bool    `json:"api_key_set"`
	PromptVersion string  `json:"prompt_version"`
	Temperature   float64 `json:"temperature"`
	MaxTokens     int     `json:"max_tokens"`
	MaxImageEdge  int     `json:"max_image_edge"`
	RetryAttempts int     `json:"retry_attempts"`
}

type settingsResponse struct {
	UseMockOCR            bool           `json:"use_mock_ocr"`
	RequestTimeoutSeconds int            `json:"request_timeout_seconds"`
	PDFRenderDPI          int            `json:"pdf_render_dpi"`
	PromptPath            string         `json:"prompt_path"`
	Prompt                string         `json:"prompt"`
	OpenAI                settingsOpenAI `json:"openai"`
}

type settingsUpdateOpenAI struct {
	BaseURL       *string  `json:"base_url"`
	Model         *string  `json:"model"`
	APIKey        *string  `json:"api_key"`
	PromptVersion *string  `json:"prompt_version"`
	Temperature   *float64 `json:"temperature"`
	MaxTokens     *int     `json:"max_tokens"`
	MaxImageEdge  *int     `json:"max_image_edge"`
	RetryAttempts *int     `json:"retry_attempts"`
}

type settingsUpdateRequest struct {
	UseMockOCR            *bool                 `json:"use_mock_ocr"`
	RequestTimeoutSeconds *int                  `json:"request_timeout_seconds"`
	PDFRenderDPI          *int                  `json:"pdf_render_dpi"`
	Prompt                *string               `json:"prompt"`
	OpenAI                *settingsUpdateOpenAI `json:"openai"`
}

func settingsFromConfig(cfg config.Config) settingsResponse {
	prompt := ""
	if raw, err := os.ReadFile(cfg.PromptPath); err == nil {
		prompt = string(raw)
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = recognizer.DefaultPrompt
	}
	return settingsResponse{
		UseMockOCR:            cfg.UseMockOCR,
		RequestTimeoutSeconds: cfg.RequestTimeoutSeconds,
		PDFRenderDPI:          cfg.PDFRenderDPI,
		PromptPath:            cfg.PromptPath,
		Prompt:                prompt,
		OpenAI: settingsOpenAI{
			BaseURL:       cfg.OpenAI.BaseURL,
			Model:         cfg.OpenAI.Model,
			APIKeySet:     strings.TrimSpace(cfg.OpenAI.APIKey) != "",
			PromptVersion: cfg.OpenAI.PromptVersion,
			Temperature:   cfg.OpenAI.Temperature,
			MaxTokens:     cfg.OpenAI.MaxTokens,
			MaxImageEdge:  cfg.OpenAI.MaxImageEdge,
			RetryAttempts: cfg.OpenAI.RetryAttempts,
		},
	}
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "settings runtime is not configured"})
		return
	}
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	cfg := s.runtime.Config()
	if _, err := s.syncCurrentPrompt(r.Context(), cfg); err != nil {
		writeError(w, fmt.Errorf("sync prompt library: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, settingsFromConfig(cfg))
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "settings runtime is not configured"})
		return
	}
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	var req settingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse settings: %w", err))
		return
	}

	// Prompt constraints are checked up front, but the file is only written
	// after Apply validates the whole request: a rejected PUT must not leave a
	// half-applied prompt behind (the recognizer hot-reads the file).
	if req.Prompt != nil {
		cfg := s.runtime.Config()
		if strings.TrimSpace(cfg.PromptPath) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt_path is not configured"})
			return
		}
		if strings.TrimSpace(*req.Prompt) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt must not be empty"})
			return
		}
	}

	next, err := s.runtime.Apply(func(cfg *config.Config) error {
		if req.UseMockOCR != nil {
			cfg.UseMockOCR = *req.UseMockOCR
		}
		if req.RequestTimeoutSeconds != nil {
			if *req.RequestTimeoutSeconds < 10 || *req.RequestTimeoutSeconds > 3600 {
				return fmt.Errorf("parse settings: request_timeout_seconds must be between 10 and 3600")
			}
			cfg.RequestTimeoutSeconds = *req.RequestTimeoutSeconds
		}
		if req.PDFRenderDPI != nil {
			cfg.PDFRenderDPI = *req.PDFRenderDPI
		}
		if req.OpenAI != nil {
			openAI := req.OpenAI
			if openAI.BaseURL != nil {
				trimmed := strings.TrimSpace(*openAI.BaseURL)
				if trimmed != "" && !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
					return fmt.Errorf("parse settings: base_url must start with http:// or https://")
				}
				cfg.OpenAI.BaseURL = trimmed
			}
			if openAI.Model != nil {
				cfg.OpenAI.Model = strings.TrimSpace(*openAI.Model)
			}
			if openAI.APIKey != nil && strings.TrimSpace(*openAI.APIKey) != "" {
				cfg.OpenAI.APIKey = strings.TrimSpace(*openAI.APIKey)
			}
			if openAI.PromptVersion != nil {
				cfg.OpenAI.PromptVersion = strings.TrimSpace(*openAI.PromptVersion)
			}
			if openAI.Temperature != nil {
				cfg.OpenAI.Temperature = *openAI.Temperature
			}
			if openAI.MaxTokens != nil {
				cfg.OpenAI.MaxTokens = *openAI.MaxTokens
			}
			if openAI.MaxImageEdge != nil {
				cfg.OpenAI.MaxImageEdge = *openAI.MaxImageEdge
			}
			if openAI.RetryAttempts != nil {
				cfg.OpenAI.RetryAttempts = *openAI.RetryAttempts
			}
		}
		if !cfg.UseMockOCR {
			if strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
				return fmt.Errorf("parse settings: openai.api_key is required when use_mock_ocr is false")
			}
			if strings.TrimSpace(cfg.OpenAI.Model) == "" {
				return fmt.Errorf("parse settings: openai.model is required when use_mock_ocr is false")
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if req.Prompt != nil {
		if err := os.WriteFile(next.PromptPath, []byte(*req.Prompt), 0o644); err != nil {
			writeError(w, fmt.Errorf("settings saved but writing prompt file failed: %w", err))
			return
		}
	}
	if _, err := s.syncCurrentPrompt(r.Context(), next); err != nil {
		writeError(w, fmt.Errorf("settings saved but syncing prompt library failed: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, settingsFromConfig(next))
}
