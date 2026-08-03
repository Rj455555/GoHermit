package web

import (
	"net/http"
	"strconv"

	"github.com/Rj455555/GoHermit/internal/loopstore"
)

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be between 1 and 100"})
			return
		}
		limit = parsed
	}
	reports, err := s.svc.ListReports(r.Context(), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if reports == nil {
		reports = []loopstore.ReportRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "limit": limit})
}

func (s *Server) retryReport(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if r.ContentLength > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "retry request body must be empty"})
		return
	}
	report, err := s.svc.RetryReport(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, report)
}
