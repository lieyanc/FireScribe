package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/recognizer"
)

type llmProviderInput struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	BaseURL string `json:"base_url"`
	APIKey string `json:"api_key"`
}

type llmModelInput struct {
	Name            string `json:"name"`
	Model           string `json:"model"`
	ParamsJSON      string `json:"params_json"`
	PromptVersionID string `json:"prompt_version_id"`
	IsDefault       bool   `json:"is_default"`
}

func (s *Server) listLLMProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListLLMProviders(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	includeModels := r.URL.Query().Get("include_models") == "1" || r.URL.Query().Get("include_models") == "true"
	if includeModels {
		for i := range items {
			models, err := s.app.Store.ListRecognizerProfilesByProvider(r.Context(), items[i].ID)
			if err != nil {
				writeError(w, err)
				return
			}
			items[i].Models = models
			items[i].ModelCount = len(models)
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createLLMProvider(w http.ResponseWriter, r *http.Request) {
	var req llmProviderInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse provider: %w", err))
		return
	}
	item, err := s.app.SaveLLMProvider(r.Context(), app.LLMProvider{
		Name: req.Name, Driver: req.Driver, BaseURL: req.BaseURL, APIKey: req.APIKey,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateLLMProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerID")
	if _, err := s.app.Store.GetLLMProvider(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	var req llmProviderInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse provider: %w", err))
		return
	}
	item, err := s.app.SaveLLMProvider(r.Context(), app.LLMProvider{
		ID: id, Name: req.Name, Driver: req.Driver, BaseURL: req.BaseURL, APIKey: req.APIKey,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteLLMProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteLLMProvider(r.Context(), chi.URLParam(r, "providerID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listLLMModels(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(chi.URLParam(r, "providerID"))
	if providerID != "" {
		if _, err := s.app.Store.GetLLMProvider(r.Context(), providerID); err != nil {
			writeError(w, err)
			return
		}
		items, err := s.app.Store.ListRecognizerProfilesByProvider(r.Context(), providerID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	items, err := s.app.Store.ListRecognizerProfiles(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createLLMModel(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerID")
	if _, err := s.app.Store.GetLLMProvider(r.Context(), providerID); err != nil {
		writeError(w, err)
		return
	}
	var req llmModelInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse model: %w", err))
		return
	}
	item, err := s.app.SaveRecognizerProfile(r.Context(), app.RecognizerProfile{
		ProviderID: providerID, Name: req.Name, Model: req.Model,
		ParamsJSON: req.ParamsJSON, PromptVersionID: req.PromptVersionID, IsDefault: req.IsDefault,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateLLMModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "modelID")
	current, err := s.app.Store.GetRecognizerProfile(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	var req llmModelInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse model: %w", err))
		return
	}
	providerID := current.ProviderID
	if nested := chi.URLParam(r, "providerID"); nested != "" {
		providerID = nested
	}
	item, err := s.app.SaveRecognizerProfile(r.Context(), app.RecognizerProfile{
		ID: id, ProviderID: providerID, Name: req.Name, Model: req.Model,
		ParamsJSON: req.ParamsJSON, PromptVersionID: req.PromptVersionID, IsDefault: req.IsDefault,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteLLMModel(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteRecognizerProfile(r.Context(), chi.URLParam(r, "modelID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listRecognizerDrivers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]string{
		{"id": recognizer.DriverOpenAICompatible, "name": "OpenAI Compatible"},
		{"id": recognizer.DriverMock, "name": "Mock"},
	})
}
