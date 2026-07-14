package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/pageproc"
)

func (s *Server) startPageProcessing(w http.ResponseWriter, r *http.Request) {
	req := struct {
		PageIDs []string               `json:"page_ids"`
		Config  pageproc.EnhanceConfig `json:"config"`
	}{Config: pageproc.DefaultEnhanceConfig()}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid processing request: " + err.Error()})
			return
		}
	}
	start, err := s.app.StartPageProcessing(r.Context(), chi.URLParam(r, "documentID"), app.PageProcessingOptions{
		PageIDs: req.PageIDs, Config: req.Config,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, start)
}

func (s *Server) listPageProcessingRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.app.Store.ListPageProcessingRuns(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getPageProcessingRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.app.Store.GetPageProcessingRun(r.Context(), chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) listPageProcessingResults(w http.ResponseWriter, r *http.Request) {
	results, err := s.app.Store.ListPageProcessingResults(r.Context(), chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, err)
		return
	}
	for index := range results {
		results[index].OriginalURL = "/api/pages/" + results[index].PageID + "/image"
		if results[index].OutputAssetID != "" {
			results[index].EnhancedURL = "/api/assets/" + results[index].OutputAssetID + "/download"
		}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) pageProcessingPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := s.app.PageProcessingPreview(r.Context(), chi.URLParam(r, "pageID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
