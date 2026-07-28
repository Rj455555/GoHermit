package web

import (
	"net/http"

	"github.com/Rj455555/GoHermit/internal/controlplane"
)

const maxMemoryRequestBytes = 16 << 10

func (s *Server) employeeMemory(w http.ResponseWriter, r *http.Request) {
	result, err := s.svc.EmployeeMemory(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) employeeMemoryCandidates(w http.ResponseWriter, r *http.Request) {
	result, err := s.svc.EmployeeMemoryCandidates(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) acceptEmployeeMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if !requirePhase4EmptyBody(w, r, "Memory acceptance") {
		return
	}
	fact, err := s.svc.AcceptEmployeeMemoryCandidate(r.Context(), r.PathValue("id"), r.PathValue("candidateID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fact)
}

func (s *Server) rejectEmployeeMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if !requirePhase4EmptyBody(w, r, "Memory Candidate rejection") {
		return
	}
	if err := s.svc.RejectEmployeeMemoryCandidate(r.Context(), r.PathValue("id"), r.PathValue("candidateID")); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) editEmployeeMemory(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.EmployeeMemoryEditInput
	if !decodeBoundedPhase4(w, r, maxMemoryRequestBytes, &input, "Memory") {
		return
	}
	fact, err := s.svc.EditEmployeeMemory(r.Context(), r.PathValue("id"), r.PathValue("factID"), input.Value)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fact)
}

func (s *Server) forgetEmployeeMemory(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if !requirePhase4EmptyBody(w, r, "Memory Forget") {
		return
	}
	if err := s.svc.ForgetEmployeeMemory(r.Context(), r.PathValue("id"), r.PathValue("factID")); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
