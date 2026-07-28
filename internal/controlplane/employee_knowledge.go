package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

type EmployeeKnowledgeResult struct {
	EmployeeID string             `json:"employee_id"`
	Sources    []knowledge.Source `json:"sources"`
	Indexes    []knowledge.Index  `json:"indexes"`
	Results    []knowledge.Result `json:"results,omitempty"`
}

func (s *Service) EmployeeKnowledge(_ context.Context, employeeID, query string, limit int) (EmployeeKnowledgeResult, error) {
	state, err := s.employees.Knowledge(employeeID)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	results, err := knowledge.Search(state.Sources, state.Indexes, query, limit)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	return EmployeeKnowledgeResult{EmployeeID: employeeID, Sources: state.Sources, Indexes: state.Indexes, Results: results}, nil
}

func (s *Service) AddEmployeeKnowledge(_ context.Context, employeeID string, source knowledge.Source) (EmployeeKnowledgeResult, error) {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return EmployeeKnowledgeResult{}, err
	}
	if source.EmployeeID != "" && source.EmployeeID != employeeID {
		return EmployeeKnowledgeResult{}, &Error{Kind: KindInvalid, Message: "Knowledge path Employee and body Employee must match"}
	}
	if source.Digest != "" || source.Status != "" || source.Error != "" {
		return EmployeeKnowledgeResult{}, &Error{Kind: KindInvalid, Message: "Knowledge digest and status are store-assigned"}
	}
	if source.SchemaVersion != 0 && source.SchemaVersion != knowledge.SchemaVersion {
		return EmployeeKnowledgeResult{}, &Error{Kind: KindInvalid, Message: "unsupported Knowledge request schema"}
	}
	catalog, err := s.knowledgeCatalog()
	if err != nil {
		return EmployeeKnowledgeResult{}, classified(KindInternal, err)
	}
	source.EmployeeID = employeeID
	indexed, index, err := catalog.Index(source)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	state, err := s.employees.SaveKnowledge(employeeID, indexed, index)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	return EmployeeKnowledgeResult{EmployeeID: employeeID, Sources: state.Sources, Indexes: state.Indexes}, nil
}

func (s *Service) RefreshEmployeeKnowledge(_ context.Context, employeeID, sourceID string) (EmployeeKnowledgeResult, error) {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return EmployeeKnowledgeResult{}, err
	}
	state, err := s.employees.Knowledge(employeeID)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	var source *knowledge.Source
	for index := range state.Sources {
		if state.Sources[index].ID == sourceID {
			copySource := state.Sources[index]
			source = &copySource
			break
		}
	}
	if source == nil {
		return EmployeeKnowledgeResult{}, classified(KindNotFound, knowledge.ErrMissing)
	}
	catalog, err := s.knowledgeCatalog()
	if err != nil {
		return EmployeeKnowledgeResult{}, classified(KindInternal, err)
	}
	indexed, index, err := catalog.Index(*source)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	state, err = s.employees.SaveKnowledge(employeeID, indexed, index)
	if err != nil {
		return EmployeeKnowledgeResult{}, classifyPhase4Store(err)
	}
	return EmployeeKnowledgeResult{EmployeeID: employeeID, Sources: state.Sources, Indexes: state.Indexes}, nil
}

func (s *Service) DeleteEmployeeKnowledge(_ context.Context, employeeID, sourceID string) error {
	if err := s.requireMutableEmployee(employeeID); err != nil {
		return err
	}
	return classifyPhase4Store(s.employees.DeleteKnowledge(employeeID, sourceID))
}

func (s *Service) knowledgeCatalog() (*knowledge.Catalog, error) {
	if s.knowledge != nil {
		return s.knowledge, nil
	}
	return knowledge.NewCatalog("")
}

func (s *Service) requireMutableEmployee(employeeID string) error {
	record, err := s.employees.Get(employeeID)
	if err != nil {
		return classifyEmployeeStore(err)
	}
	if record.Employee.State == employee.StateArchived {
		return &Error{Kind: KindConflict, Message: "archived Employee Knowledge and Memory are read-only"}
	}
	return nil
}

func classifyPhase4Store(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, employeestore.ErrNotFound), errors.Is(err, knowledge.ErrMissing):
		return classified(KindNotFound, err)
	case errors.Is(err, employeestore.ErrCorrupt), errors.Is(err, knowledge.ErrCorrupt):
		return classified(KindInternal, err)
	case errors.Is(err, employeestore.ErrConflict):
		return classified(KindConflict, err)
	case errors.Is(err, knowledge.ErrInvalid):
		return classified(KindInvalid, err)
	default:
		return &Error{Kind: KindInvalid, Message: fmt.Sprintf("%v", err)}
	}
}
