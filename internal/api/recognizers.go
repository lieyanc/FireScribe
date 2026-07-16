package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
)

type recognizerProfileInput struct {
	Name            string `json:"name"`
	ProviderID      string `json:"provider_id"`
	Driver          string `json:"driver"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	ParamsJSON      string `json:"params_json"`
	PromptVersionID string `json:"prompt_version_id"`
	IsDefault       bool   `json:"is_default"`
}

// listRecognizerProfiles remains as a flat model list for run selectors.
func (s *Server) listRecognizerProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.app.Store.ListRecognizerProfiles(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (s *Server) createRecognizerProfile(w http.ResponseWriter, r *http.Request) {
	var req recognizerProfileInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse recognizer profile: %w", err))
		return
	}
	profile, err := s.app.SaveRecognizerProfile(r.Context(), app.RecognizerProfile{
		ProviderID: req.ProviderID, Name: req.Name, Driver: req.Driver, BaseURL: req.BaseURL, APIKey: req.APIKey, Model: req.Model,
		ParamsJSON: req.ParamsJSON, PromptVersionID: req.PromptVersionID, IsDefault: req.IsDefault,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, profile)
}

func (s *Server) updateRecognizerProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "profileID")
	current, err := s.app.Store.GetRecognizerProfile(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	var req recognizerProfileInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse recognizer profile: %w", err))
		return
	}
	providerID := req.ProviderID
	if providerID == "" {
		providerID = current.ProviderID
	}
	profile, err := s.app.SaveRecognizerProfile(r.Context(), app.RecognizerProfile{
		ID: id, ProviderID: providerID, Name: req.Name, Driver: req.Driver, BaseURL: req.BaseURL, APIKey: req.APIKey, Model: req.Model,
		ParamsJSON: req.ParamsJSON, PromptVersionID: req.PromptVersionID, IsDefault: req.IsDefault,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) deleteRecognizerProfile(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteRecognizerProfile(r.Context(), chi.URLParam(r, "profileID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mergeRecognitionCandidates(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ResultIDs []string                           `json:"result_ids"`
		ProfileID string                             `json:"recognizer_profile_id"`
		Segments  []app.AlignedCandidateSegmentInput `json:"segments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse candidate merge: %w", err))
		return
	}
	var merge app.CandidateMerge
	var err error
	if len(req.Segments) > 0 {
		merge, err = s.app.MergeAlignedCandidates(r.Context(), chi.URLParam(r, "pageID"), req.Segments)
	} else {
		merge, err = s.app.MergeRecognitionCandidates(r.Context(), chi.URLParam(r, "pageID"), req.ResultIDs, req.ProfileID)
	}
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "at least two") || strings.Contains(message, "aligned segment") || strings.Contains(message, "invalid UTF-16") || strings.Contains(message, "does not match") || strings.Contains(message, "distinct") || strings.Contains(message, "does not belong") || strings.Contains(message, "introduced a line") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, merge)
}

func (s *Server) getCandidateMerge(w http.ResponseWriter, r *http.Request) {
	merge, err := s.app.Store.GetCandidateMergeByTextVersion(r.Context(), chi.URLParam(r, "textVersionID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "candidate merge not found"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, merge)
}
