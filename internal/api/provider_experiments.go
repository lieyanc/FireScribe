package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
)

type providerAdapterInput struct {
	Name               string `json:"name"`
	Engine             string `json:"engine"`
	Endpoint           string `json:"endpoint"`
	Model              string `json:"model"`
	AuthType           string `json:"auth_type"`
	Secret             string `json:"secret"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	RequestConfigJSON  string `json:"request_config_json"`
	ResponseConfigJSON string `json:"response_config_json"`
	IsEnabled          *bool  `json:"is_enabled"`
}

func (s *Server) listProviderAdapters(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListProviderAdapters(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createProviderAdapter(w http.ResponseWriter, r *http.Request) {
	var req providerAdapterInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse provider adapter: %w", err))
		return
	}
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	item, err := s.app.SaveProviderAdapter(r.Context(), app.ProviderAdapter{
		Name: req.Name, Engine: req.Engine, Endpoint: req.Endpoint, Model: req.Model,
		AuthType: req.AuthType, Secret: req.Secret, TimeoutSeconds: req.TimeoutSeconds,
		RequestConfigJSON: req.RequestConfigJSON, ResponseConfigJSON: req.ResponseConfigJSON, IsEnabled: enabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateProviderAdapter(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "adapterID")
	current, err := s.app.Store.GetProviderAdapter(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	var req providerAdapterInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse provider adapter: %w", err))
		return
	}
	enabled := current.IsEnabled
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	item, err := s.app.SaveProviderAdapter(r.Context(), app.ProviderAdapter{
		ID: id, Name: req.Name, Engine: req.Engine, Endpoint: req.Endpoint, Model: req.Model,
		AuthType: req.AuthType, Secret: req.Secret, TimeoutSeconds: req.TimeoutSeconds,
		RequestConfigJSON: req.RequestConfigJSON, ResponseConfigJSON: req.ResponseConfigJSON, IsEnabled: enabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteProviderAdapter(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteProviderAdapter(r.Context(), chi.URLParam(r, "adapterID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type recognitionExperimentRequest struct {
	Name     string   `json:"name"`
	PageIDs  []string `json:"page_ids"`
	Variants []struct {
		Name              string `json:"name"`
		ProfileID         string `json:"recognizer_profile_id"`
		ProviderAdapterID string `json:"provider_adapter_id"`
		PromptVersionID   string `json:"prompt_version_id"`
		ImageSource       string `json:"image_source"`
	} `json:"variants"`
}

func (s *Server) createRecognitionExperiment(w http.ResponseWriter, r *http.Request) {
	var req recognitionExperimentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse recognition experiment: %w", err))
		return
	}
	variants := make([]app.RecognitionExperimentVariant, 0, len(req.Variants))
	for _, input := range req.Variants {
		variants = append(variants, app.RecognitionExperimentVariant{
			Name: input.Name, ProfileID: input.ProfileID, ProviderAdapterID: input.ProviderAdapterID,
			PromptVersionID: input.PromptVersionID, ImageSource: input.ImageSource,
		})
	}
	started, err := s.app.StartRecognitionExperiment(r.Context(), chi.URLParam(r, "documentID"), app.RecognitionExperimentOptions{
		Name: req.Name, PageIDs: req.PageIDs, Variants: variants,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, started)
}

func (s *Server) listRecognitionExperiments(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListRecognitionExperiments(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getRecognitionExperiment(w http.ResponseWriter, r *http.Request) {
	item, err := s.app.GetRecognitionExperiment(r.Context(), chi.URLParam(r, "experimentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) selectRecognitionExperimentWinner(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VariantID string `json:"variant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse experiment winner: %w", err))
		return
	}
	id := chi.URLParam(r, "experimentID")
	if err := s.app.Store.SelectRecognitionExperimentWinner(r.Context(), id, req.VariantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "variant does not belong to experiment"})
			return
		}
		writeError(w, err)
		return
	}
	item, err := s.app.GetRecognitionExperiment(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
