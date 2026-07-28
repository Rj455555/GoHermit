package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/skill"
)

type SkillCatalogItem struct {
	SkillID               string          `json:"skill_id"`
	Version               string          `json:"version"`
	Digest                string          `json:"digest"`
	Kind                  skill.Kind      `json:"kind"`
	Title                 string          `json:"title"`
	Description           string          `json:"description"`
	RequestedCapabilities []string        `json:"requested_capabilities"`
	ConfigurationSchema   json.RawMessage `json:"configuration_schema"`
}

type EmployeeSkillBindingStatus struct {
	Binding employee.SkillBinding `json:"binding"`
	Status  string                `json:"status"`
	Kind    skill.Kind            `json:"kind,omitempty"`
}

type EmployeeSkillsResult struct {
	EmployeeID string                       `json:"employee_id"`
	Revision   int                          `json:"revision"`
	Bindings   []EmployeeSkillBindingStatus `json:"bindings"`
}

type EmployeeSkillsUpdateInput struct {
	ExpectedRevision int                     `json:"expected_revision"`
	Bindings         []employee.SkillBinding `json:"bindings"`
}

func (s *Service) ListSkills(_ context.Context) ([]SkillCatalogItem, error) {
	catalog, err := s.skillCatalog()
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	items, err := catalog.List()
	if err != nil {
		return nil, classifySkillCatalog(err)
	}
	result := make([]SkillCatalogItem, 0, len(items))
	for _, item := range items {
		result = append(result, catalogProjection(item))
	}
	return result, nil
}

func (s *Service) EmployeeSkills(_ context.Context, employeeID string) (EmployeeSkillsResult, error) {
	record, err := s.employees.Get(employeeID)
	if err != nil {
		return EmployeeSkillsResult{}, classifyEmployeeStore(err)
	}
	catalog, err := s.skillCatalog()
	if err != nil {
		return EmployeeSkillsResult{}, classified(KindInternal, err)
	}
	items, err := catalog.List()
	if err != nil {
		return EmployeeSkillsResult{}, classifySkillCatalog(err)
	}
	available := make(map[string]skill.Skill, len(items))
	for _, item := range items {
		available[item.Manifest.SkillID+"\x00"+item.Manifest.Version] = item
	}
	result := EmployeeSkillsResult{
		EmployeeID: employeeID, Revision: record.Employee.Revision,
		Bindings: make([]EmployeeSkillBindingStatus, 0, len(record.Employee.SkillBindings)),
	}
	for _, binding := range record.Employee.SkillBindings {
		status := EmployeeSkillBindingStatus{Binding: binding, Status: "missing"}
		if item, exists := available[binding.SkillID+"\x00"+binding.Version]; exists {
			status.Kind = item.Kind
			if item.Manifest.Digest == binding.Digest {
				status.Status = "current"
			} else {
				status.Status = "digest_drift"
			}
		}
		result.Bindings = append(result.Bindings, status)
	}
	sort.Slice(result.Bindings, func(i, j int) bool {
		return result.Bindings[i].Binding.SkillID < result.Bindings[j].Binding.SkillID
	})
	return result, nil
}

func (s *Service) UpdateEmployeeSkills(_ context.Context, employeeID string, input EmployeeSkillsUpdateInput) (employeestore.Record, error) {
	if input.ExpectedRevision < 1 {
		return employeestore.Record{}, &Error{Kind: KindInvalid, Message: "expected_revision must be positive"}
	}
	if len(input.Bindings) > employee.MaxSkillBindings {
		return employeestore.Record{}, &Error{Kind: KindInvalid, Message: fmt.Sprintf("Skill bindings exceed %d items", employee.MaxSkillBindings)}
	}
	record, err := s.employees.Get(employeeID)
	if err != nil {
		return employeestore.Record{}, classifyEmployeeStore(err)
	}
	if record.Employee.State != employee.StateActive {
		return employeestore.Record{}, &Error{Kind: KindConflict, Message: "Skill bindings can only be changed for an active Employee"}
	}
	catalog, err := s.skillCatalog()
	if err != nil {
		return employeestore.Record{}, classified(KindInternal, err)
	}
	seen := make(map[string]struct{}, len(input.Bindings))
	bindings := make([]employee.SkillBinding, len(input.Bindings))
	for index, binding := range input.Bindings {
		key := binding.SkillID + "\x00" + binding.Version
		if _, duplicate := seen[key]; duplicate {
			return employeestore.Record{}, &Error{Kind: KindInvalid, Message: "duplicate Skill binding"}
		}
		seen[key] = struct{}{}
		item, resolveErr := catalog.Resolve(binding.SkillID, binding.Version)
		if errors.Is(resolveErr, fs.ErrNotExist) {
			return employeestore.Record{}, &Error{Kind: KindInvalid, Message: fmt.Sprintf("Skill %s@%s is not in the configured catalog", binding.SkillID, binding.Version)}
		}
		if resolveErr != nil {
			return employeestore.Record{}, classifySkillCatalog(resolveErr)
		}
		if item.Manifest.Digest != binding.Digest {
			return employeestore.Record{}, &Error{Kind: KindInvalid, Message: fmt.Sprintf("Skill %s@%s digest does not match the catalog", binding.SkillID, binding.Version)}
		}
		if err := skill.ValidateConfiguration(item.Manifest.ConfigurationSchema, binding.Configuration); err != nil {
			return employeestore.Record{}, &Error{Kind: KindInvalid, Message: fmt.Sprintf("Skill %s@%s configuration: %v", binding.SkillID, binding.Version, err)}
		}
		binding.Configuration = append(json.RawMessage(nil), binding.Configuration...)
		bindings[index] = binding
	}
	proposed := record.Employee
	proposed.SkillBindings = bindings
	updated, err := s.employees.Update(employeeID, input.ExpectedRevision, proposed, record.ProjectBindings)
	if err != nil {
		return employeestore.Record{}, classifyEmployeeStore(err)
	}
	if err := s.employees.RecordActivity(employeestore.ActivityEvent{
		EmployeeID: employeeID, Type: employeestore.ActivitySkillBinding,
		EmployeeRevision: updated.Employee.Revision,
		SubjectID:        fmt.Sprintf("skill-bindings-r%d", updated.Employee.Revision),
	}); err != nil {
		return employeestore.Record{}, classifyEmployeeStore(err)
	}
	return updated, nil
}

func (s *Service) skillCatalog() (*skill.Catalog, error) {
	if s.skills != nil {
		return s.skills, nil
	}
	return skill.NewCatalog("")
}

func catalogProjection(item skill.Skill) SkillCatalogItem {
	return SkillCatalogItem{
		SkillID: item.Manifest.SkillID, Version: item.Manifest.Version,
		Digest: item.Manifest.Digest, Kind: item.Kind, Title: item.Manifest.Title,
		Description:           item.Manifest.Description,
		RequestedCapabilities: append([]string(nil), item.Manifest.RequestedCapabilities...),
		ConfigurationSchema:   append(json.RawMessage(nil), item.Manifest.ConfigurationSchema...),
	}
}

func classifySkillCatalog(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, skill.ErrCorrupt) {
		return classified(KindInternal, err)
	}
	return classified(KindInvalid, err)
}
