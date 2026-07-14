package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.app.Store.ListProjects(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	project, err := s.app.CreateProject(r.Context(), request.Name, request.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	detail, err := s.app.Store.GetProjectDetail(r.Context(), chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	project, err := s.app.Store.PatchProject(r.Context(), chi.URLParam(r, "projectID"), request.Name, request.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteProject(r.Context(), chi.URLParam(r, "projectID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listProjectDocuments(w http.ResponseWriter, r *http.Request) {
	documents, err := s.app.Store.ListProjectDocuments(r.Context(), chi.URLParam(r, "projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documents)
}

func (s *Server) addProjectDocument(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DocumentID string `json:"document_id"`
		Position   *int   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(request.DocumentID) == "" {
		writeError(w, errors.New("document_id is required"))
		return
	}
	documents, err := s.app.Store.AddProjectDocument(r.Context(), chi.URLParam(r, "projectID"), request.DocumentID, request.Position)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already belongs") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, documents)
}

func (s *Server) removeProjectDocument(w http.ResponseWriter, r *http.Request) {
	documents, err := s.app.Store.RemoveProjectDocument(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "documentID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documents)
}

func (s *Server) reorderProjectDocuments(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DocumentIDs []string `json:"document_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	documents, err := s.app.Store.ReorderProjectDocuments(r.Context(), chi.URLParam(r, "projectID"), request.DocumentIDs)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documents)
}

func (s *Server) exportProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Format             string `json:"format"`
		IncludePageNumbers bool   `json:"include_page_numbers"`
		TextScope          string `json:"text_scope"`
		IncludeAnnotations bool   `json:"include_annotations"`
		IncludeUncertain   bool   `json:"include_uncertain"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&request)
	}
	start, err := s.app.StartProjectExportWithOptions(r.Context(), chi.URLParam(r, "projectID"), app.ExportOptions{
		Format: request.Format, IncludePageNumbers: request.IncludePageNumbers, TextScope: request.TextScope,
		IncludeAnnotations: request.IncludeAnnotations, IncludeUncertain: request.IncludeUncertain,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, start)
}

func (s *Server) getProjectExport(w http.ResponseWriter, r *http.Request) {
	export, err := s.app.Store.GetProjectExport(r.Context(), chi.URLParam(r, "exportID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, export)
}

func (s *Server) downloadProjectExport(w http.ResponseWriter, r *http.Request) {
	export, err := s.app.Store.GetProjectExport(r.Context(), chi.URLParam(r, "exportID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if export.Status != "succeeded" || export.AssetID == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "project export is not ready"})
		return
	}
	s.serveAsset(w, r, export.AssetID, true)
}
