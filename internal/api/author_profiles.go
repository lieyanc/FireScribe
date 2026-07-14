package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lieyan/firescribe/internal/app"
)

func (s *Server) listAuthorProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.app.Store.ListAuthorProfiles(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (s *Server) createAuthorProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	profile, err := s.app.Store.CreateAuthorProfile(r.Context(), req.Name, req.Notes)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, profile)
}

func (s *Server) getAuthorProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.app.Store.GetAuthorProfile(r.Context(), chi.URLParam(r, "profileID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) patchAuthorProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  *string `json:"name"`
		Notes *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	profile, err := s.app.Store.PatchAuthorProfile(r.Context(), chi.URLParam(r, "profileID"), req.Name, req.Notes)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) deleteAuthorProfile(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteAuthorProfile(r.Context(), chi.URLParam(r, "profileID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAuthorTerms(w http.ResponseWriter, r *http.Request) {
	terms, err := s.app.Store.ListAuthorTerms(r.Context(), chi.URLParam(r, "profileID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, terms)
}

func (s *Server) createAuthorTerm(w http.ResponseWriter, r *http.Request) {
	var term app.AuthorTerm
	if err := json.NewDecoder(r.Body).Decode(&term); err != nil {
		writeError(w, err)
		return
	}
	created, err := s.app.Store.CreateAuthorTerm(r.Context(), chi.URLParam(r, "profileID"), term)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) patchAuthorTerm(w http.ResponseWriter, r *http.Request) {
	var term app.AuthorTerm
	if err := json.NewDecoder(r.Body).Decode(&term); err != nil {
		writeError(w, err)
		return
	}
	updated, err := s.app.Store.PatchAuthorTerm(r.Context(), chi.URLParam(r, "termID"), term)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteAuthorTerm(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteAuthorTerm(r.Context(), chi.URLParam(r, "termID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAuthorProfileDocuments(w http.ResponseWriter, r *http.Request) {
	documents, err := s.app.Store.ListAuthorProfileDocuments(r.Context(), chi.URLParam(r, "profileID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documents)
}

func (s *Server) getDocumentAuthorProfile(w http.ResponseWriter, r *http.Request) {
	profile, ok, err := s.app.Store.GetDocumentAuthorProfile(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) setDocumentAuthorProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProfileID string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	profile, err := s.app.Store.SetDocumentAuthorProfile(r.Context(), chi.URLParam(r, "documentID"), req.ProfileID)
	if err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.ProfileID) == "" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) syncAuthorCorrections(w http.ResponseWriter, r *http.Request) {
	documents, err := s.app.Store.ListAuthorProfileDocuments(r.Context(), chi.URLParam(r, "profileID"))
	if err != nil {
		writeError(w, err)
		return
	}
	added := 0
	for _, document := range documents {
		count, err := s.app.Store.SyncAuthorCorrections(r.Context(), document.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		added += count
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

func (s *Server) listAuthorCorrections(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	corrections, err := s.app.Store.ListAuthorCorrections(r.Context(), chi.URLParam(r, "profileID"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, corrections)
}

func (s *Server) getAuthorRecognitionMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.app.Store.GetAuthorRecognitionMetrics(r.Context(), chi.URLParam(r, "profileID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) downloadAuthorTrainingData(w http.ResponseWriter, r *http.Request) {
	profile, err := s.app.Store.GetAuthorProfile(r.Context(), chi.URLParam(r, "profileID"))
	if err != nil {
		writeError(w, err)
		return
	}
	corrections, err := s.app.Store.ListAuthorCorrections(r.Context(), profile.ID, 10000)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="author-training-%s.jsonl"`, profile.ID))
	encoder := json.NewEncoder(w)
	for _, correction := range corrections {
		record := map[string]any{
			"author_profile_id": profile.ID,
			"author_name":       profile.Name,
			"document_id":       correction.DocumentID,
			"document_title":    correction.DocumentTitle,
			"page_id":           correction.PageID,
			"page_no":           correction.PageNo,
			"image_asset_id":    correction.ImageAssetID,
			"image_url":         "/api/pages/" + correction.PageID + "/image",
			"source_result_id":  correction.SourceResultID,
			"provider":          correction.Provider,
			"model":             correction.Model,
			"prompt_version":    correction.PromptVersion,
			"text_version_id":   correction.TextVersionID,
			"input":             correction.SourceText,
			"output":            correction.CorrectedText,
			"kind":              correction.Kind,
			"created_at":        correction.CreatedAt,
		}
		if err := encoder.Encode(record); err != nil {
			return
		}
	}
}
