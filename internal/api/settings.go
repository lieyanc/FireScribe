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

type settingsResponse struct {
	RequestTimeoutSeconds int    `json:"request_timeout_seconds"`
	PDFRenderDPI          int    `json:"pdf_render_dpi"`
	PromptPath            string `json:"prompt_path"`
	Prompt                string `json:"prompt"`
}

type settingsUpdateRequest struct {
	RequestTimeoutSeconds *int    `json:"request_timeout_seconds"`
	PDFRenderDPI          *int    `json:"pdf_render_dpi"`
	Prompt                *string `json:"prompt"`
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
		RequestTimeoutSeconds: cfg.RequestTimeoutSeconds,
		PDFRenderDPI:          cfg.PDFRenderDPI,
		PromptPath:            cfg.PromptPath,
		Prompt:                prompt,
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
		if req.RequestTimeoutSeconds != nil {
			if *req.RequestTimeoutSeconds < 10 || *req.RequestTimeoutSeconds > 3600 {
				return fmt.Errorf("parse settings: request_timeout_seconds must be between 10 and 3600")
			}
			cfg.RequestTimeoutSeconds = *req.RequestTimeoutSeconds
		}
		if req.PDFRenderDPI != nil {
			cfg.PDFRenderDPI = *req.PDFRenderDPI
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
