package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) listDocumentExports(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListDocumentExports(r.Context(), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) exportSnapshot(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListExportPageSnapshots(r.Context(), chi.URLParam(r, "exportID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listProjectExports(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListProjectExports(r.Context(), chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) projectExportSnapshot(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.Store.ListProjectExportPageSnapshots(r.Context(), chi.URLParam(r, "exportID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
