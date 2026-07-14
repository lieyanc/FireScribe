package api

import (
	"net/http"
	"strings"
)

func (s *Server) evaluationMetrics(w http.ResponseWriter, r *http.Request) {
	benchmarkOnly := !strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("benchmark_only")), "false")
	metrics, err := s.app.Store.GetEvaluationMetrics(r.Context(), benchmarkOnly)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
