package controlplane

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

type EmployeeInput struct {
	Employee        employee.Employee         `json:"employee"`
	ProjectBindings []employee.ProjectBinding `json:"project_bindings"`
}

type EmployeeUpdateInput struct {
	ExpectedRevision int                       `json:"expected_revision"`
	Employee         employee.Employee         `json:"employee"`
	ProjectBindings  []employee.ProjectBinding `json:"project_bindings"`
}

type EmployeeTransitionInput struct {
	ExpectedRevision int `json:"expected_revision"`
}

type ReadinessCheck struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail"`
}

type EmployeeDryRunResult struct {
	EmployeeID string           `json:"employee_id"`
	Revision   int              `json:"revision"`
	Ready      bool             `json:"ready"`
	Checks     []ReadinessCheck `json:"checks"`
}

type Project struct {
	ID                   string `json:"id"`
	Label                string `json:"label"`
	WorkspaceRealPath    string `json:"workspace_real_path"`
	WorkspaceFingerprint string `json:"workspace_fingerprint"`
}

func (s *Service) CreateEmployee(_ context.Context, input EmployeeInput) (employeestore.Record, error) {
	if err := s.validateWorkspaceBindings(input.ProjectBindings); err != nil {
		return employeestore.Record{}, classified(KindInvalid, err)
	}
	record, err := s.employees.Create(input.Employee, input.ProjectBindings)
	return record, classifyEmployeeStore(err)
}

func (s *Service) ListEmployees(_ context.Context, options employeestore.ListOptions) (employeestore.Page, error) {
	page, err := s.employees.List(options)
	return page, classifyEmployeeStore(err)
}

func (s *Service) GetEmployee(_ context.Context, id string) (employeestore.Record, error) {
	record, err := s.employees.Get(id)
	return record, classifyEmployeeStore(err)
}

func (s *Service) UpdateEmployee(_ context.Context, id string, input EmployeeUpdateInput) (employeestore.Record, error) {
	if input.Employee.ID != id {
		return employeestore.Record{}, &Error{Kind: KindInvalid, Message: "employee path id and body id must match"}
	}
	if err := s.validateWorkspaceBindings(input.ProjectBindings); err != nil {
		return employeestore.Record{}, classified(KindInvalid, err)
	}
	record, err := s.employees.Update(id, input.ExpectedRevision, input.Employee, input.ProjectBindings)
	return record, classifyEmployeeStore(err)
}

func (s *Service) DisableEmployee(_ context.Context, id string, expected int) (employeestore.Record, error) {
	record, err := s.employees.Disable(id, expected)
	return record, classifyEmployeeStore(err)
}

func (s *Service) EnableEmployee(_ context.Context, id string, expected int) (employeestore.Record, error) {
	record, err := s.employees.Enable(id, expected)
	return record, classifyEmployeeStore(err)
}

func (s *Service) ArchiveEmployee(_ context.Context, id string, expected int) (employeestore.Record, error) {
	record, err := s.employees.Archive(id, expected)
	return record, classifyEmployeeStore(err)
}

func (s *Service) EmployeeActivity(_ context.Context, id string, options employeestore.ListOptions) (employeestore.ActivityPage, error) {
	page, err := s.employees.Activity(id, options)
	return page, classifyEmployeeStore(err)
}

// DryRunEmployee performs configuration/readiness checks only. It never
// builds a runtime, creates a Session or Run, invokes a model, or mutates any
// workspace or employee data.
func (s *Service) DryRunEmployee(ctx context.Context, id string) (EmployeeDryRunResult, error) {
	record, err := s.employees.Get(id)
	if err != nil {
		return EmployeeDryRunResult{}, classifyEmployeeStore(err)
	}
	value := record.Employee
	result := EmployeeDryRunResult{EmployeeID: value.ID, Revision: value.Revision, Ready: true, Checks: []ReadinessCheck{}}
	add := func(name string, ready bool, detail string) {
		result.Checks = append(result.Checks, ReadinessCheck{Name: name, Ready: ready, Detail: detail})
		if !ready {
			result.Ready = false
		}
	}
	add("employee_state", value.State == employee.StateActive, fmt.Sprintf("employee state is %s", value.State))
	_, agentOK := config.AgentProfile(value.AgentProfile)
	add("agent_profile", agentOK, selectionDetail(agentOK, "agent profile is configured", "unknown agent profile"))

	selection := config.RuntimeSelection{
		Company: value.DefaultSelection.Company,
		Access:  value.DefaultSelection.Access,
		Model:   value.DefaultSelection.Model,
		Agent:   value.AgentProfile,
	}
	_, _, selectionErr := config.ResolveSelection(selection)
	add("provider_access_model", selectionErr == nil, errorDetail(selectionErr, "provider, access, and model are configured"))
	access, accessOK := config.AccessProfile(selection.Company, selection.Access)
	credentialsReady := false
	credentialDetail := "access method is unknown"
	if accessOK {
		credentialsReady, _, credentialDetail = s.AccessStatus(ctx, access)
	}
	add("access_readiness", accessOK && credentialsReady, credentialDetail)

	bindingsReady := len(record.ProjectBindings) > 0
	bindingDetail := "at least one project binding is required"
	if bindingsReady {
		bindingDetail = "project bindings are valid"
		for _, binding := range record.ProjectBindings {
			if err := employee.ValidateProjectBinding(binding); err != nil {
				bindingsReady, bindingDetail = false, err.Error()
				break
			}
		}
	}
	add("project_binding", bindingsReady, bindingDetail)
	workspaceErr := s.validateWorkspaceBindings(record.ProjectBindings)
	add("service_workspace", workspaceErr == nil && len(record.ProjectBindings) > 0, errorDetail(workspaceErr, "all project bindings match the current service workspace"))
	policyErr := employee.Validate(value)
	add("policy_configuration", policyErr == nil, errorDetail(policyErr, "employee policy and configuration are complete"))
	return result, nil
}

// Projects exposes exactly the service's startup workspace in v0.7.
func (s *Service) Projects(_ context.Context) ([]Project, error) {
	canonical, err := canonicalWorkspace(s.Workspace)
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	fingerprintSource := employee.ProjectBinding{
		ID: "service-workspace", EmployeeID: "service", Label: filepath.Base(canonical),
		WorkspaceRealPath: canonical, ReadAllowed: true,
	}
	// Use the domain constructor to obtain the canonical path fingerprint;
	// this does not persist a binding.
	binding, err := employee.CreateProjectBinding(fingerprintSource, timeNow())
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	return []Project{{ID: "service-workspace", Label: binding.Label, WorkspaceRealPath: canonical, WorkspaceFingerprint: binding.WorkspaceFingerprint}}, nil
}

var timeNow = func() time.Time { return time.Now().UTC() }

func (s *Service) validateWorkspaceBindings(bindings []employee.ProjectBinding) error {
	canonical, err := canonicalWorkspace(s.Workspace)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if filepath.Clean(binding.WorkspaceRealPath) != canonical {
			return fmt.Errorf("project binding %q does not match the current service workspace", binding.ID)
		}
	}
	return nil
}

func canonicalWorkspace(workspace string) (string, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve service workspace: %w", err)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve service workspace symlinks: %w", err)
	}
	return filepath.Clean(real), nil
}

func classifyEmployeeStore(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, employeestore.ErrNotFound):
		return classified(KindNotFound, err)
	case errors.Is(err, employeestore.ErrConflict), errors.Is(err, employee.ErrArchived), errors.Is(err, employee.ErrInvalidTransition):
		return classified(KindConflict, err)
	default:
		return classified(KindInvalid, err)
	}
}

func errorDetail(err error, success string) string {
	if err == nil {
		return success
	}
	return err.Error()
}

func selectionDetail(ok bool, success, failure string) string {
	if ok {
		return success
	}
	return failure
}
