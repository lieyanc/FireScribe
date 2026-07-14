package api

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) reviewQueue(w http.ResponseWriter, r *http.Request) {
	threshold := 0.8
	if raw := strings.TrimSpace(r.URL.Query().Get("max_confidence")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_confidence must be between 0 and 1"})
			return
		}
		threshold = value
	}
	items, err := s.app.Store.ListReviewQueue(r.Context(), threshold, strings.TrimSpace(r.URL.Query().Get("document_id")))
	if err != nil {
		writeError(w, err)
		return
	}
	for i := range items {
		items[i].ThumbnailURL = "/api/pages/" + items[i].PageID + "/thumbnail"
	}
	writeJSON(w, http.StatusOK, items)
}
