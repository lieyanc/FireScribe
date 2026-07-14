package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
)

type crossCheckRequest struct {
	Name           string   `json:"name"`
	PageIDs        []string `json:"page_ids"`
	MergeProfileID string   `json:"merge_profile_id"`
	Variants       []struct {
		Name              string `json:"name"`
		ProfileID         string `json:"recognizer_profile_id"`
		ProviderAdapterID string `json:"provider_adapter_id"`
		PromptVersionID   string `json:"prompt_version_id"`
		ImageSource       string `json:"image_source"`
	} `json:"variants"`
}

func (s *Server) createCrossCheck(w http.ResponseWriter, r *http.Request) {
	var req crossCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse cross check: %w", err))
		return
	}
	variants := make([]app.CrossCheckVariant, 0, len(req.Variants))
	for _, input := range req.Variants {
		variants = append(variants, app.CrossCheckVariant{
			Name: input.Name, ProfileID: input.ProfileID, ProviderAdapterID: input.ProviderAdapterID,
			PromptVersionID: input.PromptVersionID, ImageSource: input.ImageSource,
		})
	}
	started, err := s.app.StartCrossCheck(r.Context(), chi.URLParam(r, "documentID"), app.CrossCheckOptions{
		Name: req.Name, PageIDs: req.PageIDs, Variants: variants, MergeProfileID: req.MergeProfileID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, started)
}

func (s *Server) listCrossChecks(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListCrossChecks(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getCrossCheck(w http.ResponseWriter, r *http.Request) {
	item, err := s.app.Store.GetCrossCheck(r.Context(), chi.URLParam(r, "checkID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adoptCrossCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PageIDs []string `json:"page_ids"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, fmt.Errorf("parse cross check adoption: %w", err))
			return
		}
	}
	adoption, err := s.app.AdoptCrossCheckConsensus(r.Context(), chi.URLParam(r, "checkID"), req.PageIDs)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adoption)
}

func (s *Server) getPageCrossCheck(w http.ResponseWriter, r *http.Request) {
	check, page, err := s.app.Store.LatestCrossCheckForPage(r.Context(), chi.URLParam(r, "pageID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "page has no cross check"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cross_check": check, "page": page})
}
