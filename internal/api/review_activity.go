package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) recordReviewActivity(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SessionID     string  `json:"session_id"`
		ActiveSeconds float64 `json:"active_seconds"`
		Finished      bool    `json:"finished"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(input.SessionID) == "" || input.ActiveSeconds < 0 || input.ActiveSeconds > 24*60*60 || math.IsNaN(input.ActiveSeconds) || math.IsInf(input.ActiveSeconds, 0) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required and active_seconds must be between 0 and 86400"})
		return
	}
	item, err := s.app.Store.RecordReviewActivity(r.Context(), chi.URLParam(r, "pageID"), strings.TrimSpace(input.SessionID), input.ActiveSeconds, input.Finished)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
