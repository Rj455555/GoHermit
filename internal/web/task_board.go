package web

import (
	"net/http"

	"github.com/Rj455555/GoHermit/internal/controlplane"
)

const maxTaskBoardRequestBytes = 256 << 10

func (s *Server) getTaskBoard(w http.ResponseWriter, r *http.Request) {
	board, err := s.svc.GetTaskBoard(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) updateTaskBoard(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.TaskBoardSettingsInput
	if !decodeBoundedPhase4(w, r, maxTaskBoardRequestBytes, &input, "Task Board") {
		return
	}
	board, err := s.svc.UpdateTaskBoardSettings(r.Context(), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) updateTaskBoardCard(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.TaskBoardCardInput
	if !decodeBoundedPhase4(w, r, maxTaskBoardRequestBytes, &input, "Task Board card") {
		return
	}
	board, err := s.svc.UpdateTaskBoardCard(r.Context(), r.PathValue("taskID"), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) createTaskBoardNote(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.TaskBoardNoteInput
	if !decodeBoundedPhase4(w, r, maxTaskBoardRequestBytes, &input, "Task Board note") {
		return
	}
	board, err := s.svc.CreateTaskBoardNote(r.Context(), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, board)
}
