package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

const maxEmployeeRequestBytes = 512 << 10

type employeeRecordResponse struct {
	Employee        employeeResponse         `json:"employee"`
	ProjectBindings []projectBindingResponse `json:"project_bindings"`
}

type employeeResponse struct {
	employee.Employee
	ProjectCount       int                      `json:"project_count"`
	Responsibilities   []string                 `json:"responsibilities"`
	BehaviorBoundaries []string                 `json:"behavior_boundaries"`
	SkillBindings      []employee.SkillBinding  `json:"skill_bindings"`
	ProjectBindingIDs  []string                 `json:"project_binding_ids"`
	PermissionPolicy   permissionPolicyResponse `json:"permission_policy"`
}

type permissionPolicyResponse struct {
	AllowedCapabilities []string `json:"allowed_capabilities"`
	NetworkAllowed      bool     `json:"network_allowed"`
}

type projectBindingResponse struct {
	employee.ProjectBinding
	AllowedToolCapabilities []string `json:"allowed_tool_capabilities"`
}

func (s *Server) createEmployee(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.EmployeeInput
	if !decodeEmployeeRequest(w, r, &input) {
		return
	}
	record, err := s.svc.CreateEmployee(r.Context(), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeEmployeeRecord(w, http.StatusCreated, record)
}

func (s *Server) listEmployees(w http.ResponseWriter, r *http.Request) {
	options, ok := employeeListOptions(w, r)
	if !ok {
		return
	}
	page, err := s.svc.ListEmployees(r.Context(), options)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getEmployee(w http.ResponseWriter, r *http.Request) {
	record, err := s.svc.GetEmployee(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeEmployeeRecord(w, http.StatusOK, record)
}

func (s *Server) updateEmployee(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.EmployeeUpdateInput
	if !decodeEmployeeRequest(w, r, &input) {
		return
	}
	record, err := s.svc.UpdateEmployee(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeEmployeeRecord(w, http.StatusOK, record)
}

func (s *Server) dryRunEmployee(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if r.ContentLength > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "dry-run request body must be empty"})
		return
	}
	result, err := s.svc.DryRunEmployee(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) disableEmployee(w http.ResponseWriter, r *http.Request) {
	s.transitionEmployee(w, r, s.svc.DisableEmployee)
}

func (s *Server) enableEmployee(w http.ResponseWriter, r *http.Request) {
	s.transitionEmployee(w, r, s.svc.EnableEmployee)
}

func (s *Server) archiveEmployee(w http.ResponseWriter, r *http.Request) {
	s.transitionEmployee(w, r, s.svc.ArchiveEmployee)
}

func (s *Server) transitionEmployee(w http.ResponseWriter, r *http.Request, transition func(context.Context, string, int) (employeestore.Record, error)) {
	if !requireSameOrigin(w, r) {
		return
	}
	var input controlplane.EmployeeTransitionInput
	if !decodeEmployeeRequest(w, r, &input) {
		return
	}
	record, err := transition(r.Context(), r.PathValue("id"), input.ExpectedRevision)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeEmployeeRecord(w, http.StatusOK, record)
}

func writeEmployeeRecord(w http.ResponseWriter, status int, record employeestore.Record) {
	bindings := make([]projectBindingResponse, len(record.ProjectBindings))
	for index, binding := range record.ProjectBindings {
		bindings[index] = projectBindingResponse{
			ProjectBinding:          binding,
			AllowedToolCapabilities: nonNilStrings(binding.AllowedToolCapabilities),
		}
	}
	writeJSON(w, status, employeeRecordResponse{
		Employee: employeeResponse{
			Employee:           record.Employee,
			ProjectCount:       len(record.ProjectBindings),
			Responsibilities:   nonNilStrings(record.Employee.Responsibilities),
			BehaviorBoundaries: nonNilStrings(record.Employee.BehaviorBoundaries),
			SkillBindings:      nonNilSkillBindings(record.Employee.SkillBindings),
			ProjectBindingIDs:  nonNilStrings(record.Employee.ProjectBindingIDs),
			PermissionPolicy: permissionPolicyResponse{
				AllowedCapabilities: nonNilStrings(record.Employee.PermissionPolicy.AllowedCapabilities),
				NetworkAllowed:      record.Employee.PermissionPolicy.NetworkAllowed,
			},
		},
		ProjectBindings: bindings,
	})
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilSkillBindings(values []employee.SkillBinding) []employee.SkillBinding {
	if values == nil {
		return []employee.SkillBinding{}
	}
	return values
}

func (s *Server) employeeActivity(w http.ResponseWriter, r *http.Request) {
	options, ok := employeeListOptions(w, r)
	if !ok {
		return
	}
	page, err := s.svc.EmployeeActivity(r.Context(), r.PathValue("id"), options)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.svc.Projects(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func employeeListOptions(w http.ResponseWriter, r *http.Request) (employeestore.ListOptions, bool) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be an integer"})
			return employeestore.ListOptions{}, false
		}
	}
	return employeestore.ListOptions{
		Limit: limit, Cursor: r.URL.Query().Get("cursor"), State: employee.State(r.URL.Query().Get("state")),
	}, true
}

func decodeEmployeeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEmployeeRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid employee request: " + err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "employee request must contain one JSON value"})
		return false
	}
	return true
}
