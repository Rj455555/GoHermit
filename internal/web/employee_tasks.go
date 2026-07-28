package web

import (
	"net/http"
	"strconv"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

const maxEmployeeTaskRequestBytes = 256 << 10

func (s *Server) createEmployeeTask(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.EmployeeTaskCreateInput
	if !decodeBoundedPhase4(w, r, maxEmployeeTaskRequestBytes, &input, "Employee Task") {
		return
	}
	task, err := s.svc.CreateEmployeeTask(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) listEmployeeTasks(w http.ResponseWriter, r *http.Request) {
	options, ok := employeeTaskListOptions(w, r)
	if !ok {
		return
	}
	page, err := s.svc.ListEmployeeTasks(r.Context(), r.PathValue("id"), options)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getEmployeeTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.svc.GetEmployeeTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) cancelEmployeeTask(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if !requirePhase4EmptyBody(w, r, "Employee Task cancellation") {
		return
	}
	task, err := s.svc.CancelEmployeeTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) startEmployeeTask(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if !requirePhase4EmptyBody(w, r, "Employee Task start") {
		return
	}
	task, err := s.svc.StartEmployeeTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) resumeEmployeeTask(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if !requirePhase4EmptyBody(w, r, "Employee Task resume") {
		return
	}
	task, err := s.svc.ResumeEmployeeTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func employeeTaskListOptions(w http.ResponseWriter, r *http.Request) (employeestore.TaskListOptions, bool) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be an integer"})
			return employeestore.TaskListOptions{}, false
		}
	}
	return employeestore.TaskListOptions{
		Limit: limit, Cursor: r.URL.Query().Get("cursor"),
		State: employee.TaskState(r.URL.Query().Get("state")),
	}, true
}
