package controlplane

import (
	"context"
	"errors"

	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

type EmployeeMemoryResult struct {
	EmployeeID string                `json:"employee_id"`
	Facts      []employeememory.Fact `json:"facts"`
}

type EmployeeMemoryCandidatesResult struct {
	EmployeeID string                     `json:"employee_id"`
	Candidates []employeememory.Candidate `json:"candidates"`
}

type EmployeeMemoryEditInput struct {
	Value string `json:"value"`
}

func (s *Service) EmployeeMemory(_ context.Context, employeeID string) (EmployeeMemoryResult, error) {
	facts, err := s.employees.Memory(employeeID)
	return EmployeeMemoryResult{EmployeeID: employeeID, Facts: facts}, classifyMemoryStore(err)
}

func (s *Service) EmployeeMemoryCandidates(_ context.Context, employeeID string) (EmployeeMemoryCandidatesResult, error) {
	candidates, err := s.employees.MemoryCandidates(employeeID)
	return EmployeeMemoryCandidatesResult{EmployeeID: employeeID, Candidates: candidates}, classifyMemoryStore(err)
}

func (s *Service) AcceptEmployeeMemoryCandidate(_ context.Context, employeeID, candidateID string) (employeememory.Fact, error) {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return employeememory.Fact{}, err
	}
	fact, err := s.employees.AcceptMemoryCandidate(employeeID, candidateID)
	return fact, classifyMemoryStore(err)
}

func (s *Service) RejectEmployeeMemoryCandidate(_ context.Context, employeeID, candidateID string) error {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return err
	}
	return classifyMemoryStore(s.employees.RejectMemoryCandidate(employeeID, candidateID))
}

func (s *Service) EditEmployeeMemory(_ context.Context, employeeID, factID, value string) (employeememory.Fact, error) {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return employeememory.Fact{}, err
	}
	fact, err := s.employees.EditMemory(employeeID, factID, value)
	return fact, classifyMemoryStore(err)
}

func (s *Service) ForgetEmployeeMemory(_ context.Context, employeeID, factID string) error {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return err
	}
	return classifyMemoryStore(s.employees.ForgetMemory(employeeID, factID))
}

func classifyMemoryStore(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, employeestore.ErrNotFound), errors.Is(err, employeememory.ErrMissing):
		return classified(KindNotFound, err)
	case errors.Is(err, employeestore.ErrCorrupt), errors.Is(err, employeememory.ErrCorrupt):
		return classified(KindInternal, err)
	case errors.Is(err, employeestore.ErrConflict):
		return classified(KindConflict, err)
	default:
		return classified(KindInvalid, err)
	}
}
