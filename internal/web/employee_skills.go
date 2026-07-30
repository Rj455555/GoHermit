package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Rj455555/GoHermit/internal/controlplane"
)

const maxSkillBindingRequestBytes = 64 << 10

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.svc.ListSkills(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items})
}

func (s *Server) employeeSkills(w http.ResponseWriter, r *http.Request) {
	result, err := s.svc.EmployeeSkills(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) updateEmployeeSkills(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.EmployeeSkillsUpdateInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSkillBindingRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Employee Skill request: " + err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Employee Skill request must contain one JSON value"})
		return
	}
	record, err := s.svc.UpdateEmployeeSkills(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeEmployeeRecord(w, http.StatusOK, record)
}
