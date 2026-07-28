package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

type EmployeeTaskSkillSelection struct {
	SkillID string `json:"skill_id"`
	Version string `json:"version"`
}

type EmployeeTaskKnowledgeSelection struct {
	SourceID    string   `json:"source_id"`
	CitationIDs []string `json:"citation_ids"`
}

type EmployeeTaskCreateInput struct {
	Prompt           string                           `json:"prompt"`
	Skills           []EmployeeTaskSkillSelection     `json:"skills"`
	Knowledge        []EmployeeTaskKnowledgeSelection `json:"knowledge"`
	MemoryFactIDs    []string                         `json:"memory_fact_ids"`
	ProjectBindingID string                           `json:"project_binding_id"`
	Policy           employee.TaskPolicy              `json:"policy"`
}

type EmployeeTaskSnapshotMetadata struct {
	SchemaVersion int       `json:"schema_version"`
	EmployeeID    string    `json:"employee_id"`
	Revision      int       `json:"revision"`
	CapturedAt    time.Time `json:"captured_at"`
	Digest        string    `json:"digest"`
}

// EmployeeTaskProjectProjection deliberately omits the local workspace path.
type EmployeeTaskProjectProjection struct {
	ID                      string                 `json:"id"`
	Label                   string                 `json:"label"`
	WorkspaceFingerprint    string                 `json:"workspace_fingerprint"`
	ReadAllowed             bool                   `json:"read_allowed"`
	MutationAllowed         bool                   `json:"mutation_allowed"`
	AllowedToolCapabilities []string               `json:"allowed_tool_capabilities"`
	NetworkAllowed          bool                   `json:"network_allowed"`
	BudgetOverride          *employee.BudgetPolicy `json:"budget_override,omitempty"`
}

// EmployeeTaskView exposes bounded immutable selection metadata, not the
// complete Employee revision file.
type EmployeeTaskView struct {
	SchemaVersion    int                               `json:"schema_version"`
	ID               string                            `json:"id"`
	EmployeeID       string                            `json:"employee_id"`
	EmployeeRevision int                               `json:"employee_revision"`
	Prompt           string                            `json:"prompt"`
	State            employee.TaskState                `json:"state"`
	CreatedAt        time.Time                         `json:"created_at"`
	UpdatedAt        time.Time                         `json:"updated_at"`
	CancelledAt      *time.Time                        `json:"cancelled_at,omitempty"`
	EmployeeSnapshot EmployeeTaskSnapshotMetadata      `json:"employee_snapshot"`
	Skills           []employee.SkillBinding           `json:"skills"`
	Knowledge        []employee.TaskKnowledgeSnapshot  `json:"knowledge"`
	MemoryFacts      []employee.TaskMemoryFactSnapshot `json:"memory_facts"`
	ProjectBinding   EmployeeTaskProjectProjection     `json:"project_binding"`
	Policy           employee.TaskPolicy               `json:"policy"`
	SnapshotDigest   string                            `json:"snapshot_digest"`
	SessionID        string                            `json:"session_id"`
	RunID            string                            `json:"run_id"`
}

func (s *Service) CreateEmployeeTask(_ context.Context, employeeID string, input EmployeeTaskCreateInput) (EmployeeTaskView, error) {
	record, err := s.employees.Get(employeeID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	if record.Employee.State != employee.StateActive {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("only an active Employee can create a Task"))
	}
	snapshot, err := employee.NewRevisionSnapshot(record.Employee, record.ProjectBindings)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	skills, err := selectTaskSkills(record.Employee.SkillBindings, input.Skills)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInvalid, err)
	}
	selectedKnowledge, err := s.selectTaskKnowledge(employeeID, input.Knowledge)
	if err != nil {
		return EmployeeTaskView{}, err
	}
	selectedMemory, err := s.selectTaskMemory(employeeID, input.MemoryFactIDs)
	if err != nil {
		return EmployeeTaskView{}, err
	}
	project, err := selectTaskProject(record.ProjectBindings, input.ProjectBindingID)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInvalid, err)
	}
	if err := s.validateWorkspaceBindings([]employee.ProjectBinding{project}); err != nil {
		return EmployeeTaskView{}, classified(KindInvalid, err)
	}
	created, err := s.employees.CreateTask(employeeID, employee.EmployeeTask{
		EmployeeID:       employeeID,
		EmployeeRevision: record.Employee.Revision,
		Prompt:           input.Prompt,
		EmployeeSnapshot: snapshot,
		Skills:           skills,
		Knowledge:        selectedKnowledge,
		MemoryFacts:      selectedMemory,
		ProjectBinding:   project,
		Policy:           input.Policy,
	})
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	return projectEmployeeTask(created), nil
}

func (s *Service) ListEmployeeTasks(_ context.Context, employeeID string, options employeestore.TaskListOptions) (employeestore.TaskPage, error) {
	page, err := s.employees.ListTasks(employeeID, options)
	return page, classifyEmployeeStore(err)
}

func (s *Service) GetEmployeeTask(_ context.Context, taskID string) (EmployeeTaskView, error) {
	task, err := s.employees.GetTask(taskID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	return projectEmployeeTask(task), nil
}

func (s *Service) CancelEmployeeTask(_ context.Context, taskID string) (EmployeeTaskView, error) {
	task, err := s.employees.CancelTask(taskID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	return projectEmployeeTask(task), nil
}

func selectTaskSkills(bindings []employee.SkillBinding, selections []EmployeeTaskSkillSelection) ([]employee.SkillBinding, error) {
	available := make(map[string]employee.SkillBinding, len(bindings))
	for _, binding := range bindings {
		available[binding.SkillID+"\x00"+binding.Version] = binding
	}
	selected := make([]employee.SkillBinding, 0, len(selections))
	seen := make(map[string]struct{}, len(selections))
	for _, selection := range selections {
		key := selection.SkillID + "\x00" + selection.Version
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("duplicate Employee Task Skill selection")
		}
		seen[key] = struct{}{}
		binding, exists := available[key]
		if !exists {
			return nil, fmt.Errorf("Employee Task Skill %s@%s is not bound to this Employee revision", selection.SkillID, selection.Version)
		}
		selected = append(selected, binding)
	}
	sort.Slice(selected, func(left, right int) bool {
		if selected[left].SkillID == selected[right].SkillID {
			return selected[left].Version < selected[right].Version
		}
		return selected[left].SkillID < selected[right].SkillID
	})
	if selected == nil {
		selected = []employee.SkillBinding{}
	}
	return selected, nil
}

func (s *Service) selectTaskKnowledge(employeeID string, selections []EmployeeTaskKnowledgeSelection) ([]employee.TaskKnowledgeSnapshot, error) {
	if len(selections) == 0 {
		return []employee.TaskKnowledgeSnapshot{}, nil
	}
	state, err := s.employees.Knowledge(employeeID)
	if err != nil {
		return nil, classifyPhase4Store(err)
	}
	sources := make(map[string]knowledge.Source, len(state.Sources))
	indexes := make(map[string]knowledge.Index, len(state.Indexes))
	for _, source := range state.Sources {
		sources[source.ID] = source
	}
	for _, index := range state.Indexes {
		indexes[index.SourceID] = index
	}
	result := make([]employee.TaskKnowledgeSnapshot, 0, len(selections))
	seenSources := make(map[string]struct{}, len(selections))
	for _, selection := range selections {
		if _, duplicate := seenSources[selection.SourceID]; duplicate {
			return nil, classified(KindInvalid, errors.New("duplicate Employee Task Knowledge source selection"))
		}
		seenSources[selection.SourceID] = struct{}{}
		source, exists := sources[selection.SourceID]
		index, indexed := indexes[selection.SourceID]
		if !exists || !indexed {
			return nil, classified(KindInvalid, fmt.Errorf("Knowledge source %q is not available", selection.SourceID))
		}
		if len(selection.CitationIDs) == 0 {
			return nil, classified(KindInvalid, fmt.Errorf("Knowledge source %q requires at least one Citation", selection.SourceID))
		}
		availableCitations := make(map[string]knowledge.Citation)
		for _, document := range index.Documents {
			for _, citation := range document.Citations {
				availableCitations[citation.ID] = citation
			}
		}
		citations := make([]employee.TaskCitationReference, 0, len(selection.CitationIDs))
		seenCitations := make(map[string]struct{}, len(selection.CitationIDs))
		for _, citationID := range selection.CitationIDs {
			if _, duplicate := seenCitations[citationID]; duplicate {
				return nil, classified(KindInvalid, errors.New("duplicate Employee Task Citation selection"))
			}
			seenCitations[citationID] = struct{}{}
			citation, exists := availableCitations[citationID]
			if !exists {
				return nil, classified(KindInvalid, fmt.Errorf("Citation %q is not in Knowledge source %q", citationID, selection.SourceID))
			}
			citations = append(citations, employee.TaskCitationReference{
				CitationID: citation.ID,
				Path:       citation.Path,
				Digest:     citation.Digest,
				StartLine:  citation.StartLine,
				EndLine:    citation.EndLine,
			})
		}
		sort.Slice(citations, func(left, right int) bool { return citations[left].CitationID < citations[right].CitationID })
		result = append(result, employee.TaskKnowledgeSnapshot{
			SourceID: source.ID, SourceDigest: source.Digest, Citations: citations,
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].SourceID < result[right].SourceID })
	return result, nil
}

func (s *Service) selectTaskMemory(employeeID string, factIDs []string) ([]employee.TaskMemoryFactSnapshot, error) {
	if len(factIDs) == 0 {
		return []employee.TaskMemoryFactSnapshot{}, nil
	}
	facts, err := s.employees.Memory(employeeID)
	if err != nil {
		return nil, classifyMemoryStore(err)
	}
	available := make(map[string]string, len(facts))
	for _, fact := range facts {
		available[fact.ID] = fact.Digest
	}
	result := make([]employee.TaskMemoryFactSnapshot, 0, len(factIDs))
	seen := make(map[string]struct{}, len(factIDs))
	for _, factID := range factIDs {
		if _, duplicate := seen[factID]; duplicate {
			return nil, classified(KindInvalid, errors.New("duplicate Employee Task Memory Fact selection"))
		}
		seen[factID] = struct{}{}
		digest, exists := available[factID]
		if !exists {
			return nil, classified(KindInvalid, fmt.Errorf("accepted Memory Fact %q is not available", factID))
		}
		result = append(result, employee.TaskMemoryFactSnapshot{FactID: factID, Digest: digest})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].FactID < result[right].FactID })
	return result, nil
}

func selectTaskProject(bindings []employee.ProjectBinding, bindingID string) (employee.ProjectBinding, error) {
	if bindingID == "" {
		return employee.ProjectBinding{}, errors.New("Employee Task ProjectBinding is required")
	}
	for _, binding := range bindings {
		if binding.ID == bindingID {
			return binding, nil
		}
	}
	return employee.ProjectBinding{}, fmt.Errorf("ProjectBinding %q is not in this Employee revision", bindingID)
}

func projectEmployeeTask(task employee.EmployeeTask) EmployeeTaskView {
	project := EmployeeTaskProjectProjection{
		ID: task.ProjectBinding.ID, Label: task.ProjectBinding.Label,
		WorkspaceFingerprint: task.ProjectBinding.WorkspaceFingerprint,
		ReadAllowed:          task.ProjectBinding.ReadAllowed, MutationAllowed: task.ProjectBinding.MutationAllowed,
		AllowedToolCapabilities: append([]string{}, task.ProjectBinding.AllowedToolCapabilities...),
		NetworkAllowed:          task.ProjectBinding.NetworkAllowed,
	}
	if task.ProjectBinding.BudgetOverride != nil {
		copy := *task.ProjectBinding.BudgetOverride
		project.BudgetOverride = &copy
	}
	return EmployeeTaskView{
		SchemaVersion: task.SchemaVersion, ID: task.ID, EmployeeID: task.EmployeeID,
		EmployeeRevision: task.EmployeeRevision, Prompt: task.Prompt, State: task.State,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, CancelledAt: cloneControlPlaneTime(task.CancelledAt),
		EmployeeSnapshot: EmployeeTaskSnapshotMetadata{
			SchemaVersion: task.EmployeeSnapshot.SchemaVersion,
			EmployeeID:    task.EmployeeSnapshot.EmployeeID, Revision: task.EmployeeSnapshot.Revision,
			CapturedAt: task.EmployeeSnapshot.CapturedAt, Digest: task.EmployeeSnapshot.Digest,
		},
		Skills:         append([]employee.SkillBinding{}, task.Skills...),
		Knowledge:      append([]employee.TaskKnowledgeSnapshot{}, task.Knowledge...),
		MemoryFacts:    append([]employee.TaskMemoryFactSnapshot{}, task.MemoryFacts...),
		ProjectBinding: project, Policy: task.Policy, SnapshotDigest: task.SnapshotDigest,
		SessionID: task.SessionID, RunID: task.RunID,
	}
}

func cloneControlPlaneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
