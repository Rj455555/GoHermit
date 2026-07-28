package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Rj455555/GoHermit/internal/knowledge"
)

const maxKnowledgeRequestBytes = 96 << 10

func (s *Server) employeeKnowledge(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be an integer"})
			return
		}
	}
	result, err := s.svc.EmployeeKnowledge(r.Context(), r.PathValue("id"), r.URL.Query().Get("query"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) addEmployeeKnowledge(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var source knowledge.Source
	if !decodeBoundedPhase4(w, r, maxKnowledgeRequestBytes, &source, "Knowledge") {
		return
	}
	result, err := s.svc.AddEmployeeKnowledge(r.Context(), r.PathValue("id"), source)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) refreshEmployeeKnowledge(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if r.ContentLength > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Knowledge refresh request body must be empty"})
		return
	}
	result, err := s.svc.RefreshEmployeeKnowledge(r.Context(), r.PathValue("id"), r.PathValue("sourceID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteEmployeeKnowledge(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if err := s.svc.DeleteEmployeeKnowledge(r.Context(), r.PathValue("id"), r.PathValue("sourceID")); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeBoundedPhase4(w http.ResponseWriter, r *http.Request, maximum int64, target any, label string) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maximum))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid " + label + " request: " + err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": label + " request must contain one JSON value"})
		return false
	}
	return true
}
