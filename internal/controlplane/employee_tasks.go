package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/skill"
)

type EmployeeTaskPreparationState string

const EmployeeTaskPrepared EmployeeTaskPreparationState = "prepared"

type EmployeeTaskPreparation struct {
	TaskID                string                       `json:"task_id"`
	EmployeeID            string                       `json:"employee_id"`
	EmployeeRevision      int                          `json:"employee_revision"`
	SessionID             string                       `json:"session_id"`
	TaskSnapshotDigest    string                       `json:"task_snapshot_digest"`
	CompactSnapshotDigest string                       `json:"compact_snapshot_digest"`
	State                 EmployeeTaskPreparationState `json:"state"`
}

var runtimePreparationGlobalCapabilities = []string{
	"execute", "filesystem.list", "filesystem.read", "filesystem.search",
	"filesystem.write", "git.diff", "git.log", "git.status", "network",
	"patch.apply", "read", "read_file", "shell.execute", "test.run", "write", "write_file",
}

// PrepareEmployeeTask performs readiness, seals compact recovery context, and
// reconciles exactly one stable schema-v6 Session. It never builds a runtime,
// creates a Run, invokes a model/provider, executes a Tool, or takes a
// workspace execution lease.
func (s *Service) PrepareEmployeeTask(ctx context.Context, taskID string) (EmployeeTaskPreparation, error) {
	if err := ctx.Err(); err != nil {
		return EmployeeTaskPreparation{}, classified(KindInvalid, err)
	}
	s.prepareMu.Lock()
	defer s.prepareMu.Unlock()

	task, err := s.employees.GetTask(taskID)
	if err != nil {
		return EmployeeTaskPreparation{}, classifyEmployeeStore(err)
	}
	if task.State != employee.TaskQueued {
		return EmployeeTaskPreparation{}, classified(KindConflict, errors.New("only a queued Employee Task can be prepared"))
	}
	current, err := s.employees.Get(task.EmployeeID)
	if err != nil {
		return EmployeeTaskPreparation{}, classifyEmployeeStore(err)
	}
	if current.Employee.State != employee.StateActive {
		return EmployeeTaskPreparation{}, classified(KindConflict, fmt.Errorf("Employee state %s is not ready", current.Employee.State))
	}
	revision, err := s.employees.LoadRevision(task.EmployeeID, task.EmployeeRevision)
	if err != nil {
		return EmployeeTaskPreparation{}, classifyEmployeeStore(err)
	}
	if !reflect.DeepEqual(revision, task.EmployeeSnapshot) {
		return EmployeeTaskPreparation{}, classified(KindInternal, errors.New("Employee Task revision snapshot does not match immutable revision Store"))
	}
	if err := s.validateWorkspaceBindings([]employee.ProjectBinding{task.ProjectBinding}); err != nil {
		return EmployeeTaskPreparation{}, classified(KindConflict, err)
	}
	workspace, err := canonicalWorkspace(s.Workspace)
	if err != nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, err)
	}
	if task.ProjectBinding.WorkspaceRealPath != workspace {
		return EmployeeTaskPreparation{}, classified(KindConflict, errors.New("Employee Task ProjectBinding does not exactly match the current service workspace"))
	}
	sessionWorkspace, err := filepath.Abs(s.Workspace)
	if err != nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, fmt.Errorf("resolve Session workspace: %w", err))
	}

	configuration, err := app.LoadConfig(s.Workspace, s.ConfigPath)
	if err != nil {
		return EmployeeTaskPreparation{}, classified(KindConflict, fmt.Errorf("service configuration is not ready: %w", err))
	}
	selection := config.RuntimeSelection{
		Company: task.EmployeeSnapshot.Employee.DefaultSelection.Company,
		Access:  task.EmployeeSnapshot.Employee.DefaultSelection.Access,
		Model:   task.EmployeeSnapshot.Employee.DefaultSelection.Model,
		Agent:   task.EmployeeSnapshot.Employee.AgentProfile,
	}
	_, agentProfile, err := config.ResolveSelection(selection)
	if err != nil {
		return EmployeeTaskPreparation{}, classified(KindConflict, fmt.Errorf("provider, access, model, or Agent Profile is not ready: %w", err))
	}
	access, ok := config.AccessProfile(selection.Company, selection.Access)
	if !ok {
		return EmployeeTaskPreparation{}, classified(KindConflict, errors.New("Employee Task access method is not configured"))
	}
	ready, _, detail := s.AccessStatus(ctx, access)
	if !ready {
		return EmployeeTaskPreparation{}, classified(KindConflict, errors.New(detail))
	}

	compactSkills, grants, err := s.prepareTaskSkills(task)
	if err != nil {
		return EmployeeTaskPreparation{}, err
	}
	compactKnowledge, err := s.prepareTaskKnowledge(task)
	if err != nil {
		return EmployeeTaskPreparation{}, err
	}
	compactMemory, err := s.prepareTaskMemory(task)
	if err != nil {
		return EmployeeTaskPreparation{}, err
	}
	effective, err := employee.ResolveEffectivePolicy(employee.CapabilityIntersection{
		Global: runtimePreparationGlobalCapabilities, AgentToolPolicy: agentProfile.ToolPolicy,
		Employee: task.EmployeeSnapshot.Employee.PermissionPolicy.AllowedCapabilities,
		Project:  task.ProjectBinding.AllowedToolCapabilities, Task: task.Policy.AllowedCapabilities,
		GlobalNetwork:   configuration.Permissions.AllowNetwork,
		EmployeeNetwork: task.EmployeeSnapshot.Employee.PermissionPolicy.NetworkAllowed,
		ProjectNetwork:  task.ProjectBinding.NetworkAllowed, TaskNetwork: task.Policy.NetworkAllowed,
		EnabledSkillGrants: grants,
	})
	if err != nil {
		return EmployeeTaskPreparation{}, classified(KindConflict, fmt.Errorf("effective capability policy is not ready: %w", err))
	}
	compact := employee.CompactSnapshot{
		SchemaVersion: employee.CompactSnapshotSchemaVersion,
		EmployeeID:    task.EmployeeID, EmployeeRevision: task.EmployeeRevision,
		TaskID: task.ID, TaskSnapshotDigest: task.SnapshotDigest,
		Identity: employee.CompactIdentity{
			Name:               task.EmployeeSnapshot.Employee.Name,
			JobTitle:           task.EmployeeSnapshot.Employee.JobTitle,
			Charter:            task.EmployeeSnapshot.Employee.Charter,
			Responsibilities:   append([]string{}, task.EmployeeSnapshot.Employee.Responsibilities...),
			BehaviorBoundaries: append([]string{}, task.EmployeeSnapshot.Employee.BehaviorBoundaries...),
		},
		EffectivePolicy: effective, Budget: task.Policy.Budget,
		Project: employee.CompactProject{
			BindingID:            task.ProjectBinding.ID,
			WorkspaceFingerprint: task.ProjectBinding.WorkspaceFingerprint,
			ReadAllowed:          task.ProjectBinding.ReadAllowed,
			MutationAllowed:      task.ProjectBinding.MutationAllowed,
			NetworkAllowed:       task.ProjectBinding.NetworkAllowed,
			WorkspaceSummary: fmt.Sprintf(
				"binding=%s; real_path=%s; fingerprint=%s; read=%t; mutation=%t",
				task.ProjectBinding.ID, workspace, task.ProjectBinding.WorkspaceFingerprint,
				task.ProjectBinding.ReadAllowed, task.ProjectBinding.MutationAllowed,
			),
		},
		Skills: compactSkills, Knowledge: compactKnowledge, Memory: compactMemory,
	}
	if err := employee.SealCompactSnapshot(&compact); err != nil {
		return EmployeeTaskPreparation{}, classified(KindConflict, fmt.Errorf("compact Employee context is not ready: %w", err))
	}
	if _, err := contextmgr.EmployeeContextFromCompact(compact); err != nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, fmt.Errorf("compact Employee context contract: %w", err))
	}

	sessionID := stableEmployeeSessionID(task, workspace)
	configDigest := session.ConfigDigest(configuration)
	sessionSelection := session.Selection{
		Company: selection.Company, Access: selection.Access,
		Model: selection.Model, Agent: selection.Agent,
	}
	expectedJournal := employeestore.DispatchRecord{
		SchemaVersion: employeestore.DispatchSchemaVersion,
		EmployeeID:    task.EmployeeID, TaskID: task.ID, SessionID: sessionID,
		TaskSnapshotDigest: task.SnapshotDigest, CompactSnapshotDigest: compact.Digest,
		WorkspaceRealPath: workspace, Stage: employeestore.DispatchPrepared,
	}
	journal, err := s.employees.PrepareDispatch(expectedJournal)
	if err != nil {
		return EmployeeTaskPreparation{}, classifyEmployeeStore(err)
	}
	if err := s.callPrepareStageHook("journal_written"); err != nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, err)
	}
	if s.store == nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, errors.New("Session Store is unavailable"))
	}
	if s.store.Has(sessionID) {
		existing, loadErr := s.store.Load(ctx, sessionID)
		if loadErr != nil {
			return EmployeeTaskPreparation{}, classified(KindInternal, loadErr)
		}
		if !preparedSessionMatches(existing, task, compact, workspace, sessionWorkspace, configDigest, sessionSelection) {
			return EmployeeTaskPreparation{}, classified(KindInternal, errors.New("existing prepared Session does not match dispatch journal"))
		}
	} else {
		prepared, createErr := session.NewPrepared(
			sessionID, task.Prompt, sessionWorkspace, configDigest,
			task.EmployeeID, task.ID, task.EmployeeRevision, task.SnapshotDigest, compact,
		)
		if createErr != nil {
			return EmployeeTaskPreparation{}, classified(KindInternal, createErr)
		}
		prepared.Selection = sessionSelection
		prepared.GitState = session.GitState(ctx, sessionWorkspace)
		if saveErr := s.store.Save(ctx, prepared); saveErr != nil {
			return EmployeeTaskPreparation{}, classified(KindInternal, saveErr)
		}
	}
	if err := s.callPrepareStageHook("session_saved"); err != nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, err)
	}
	if journal.Stage != employeestore.DispatchSessionCreated {
		journal, err = s.employees.MarkDispatchSessionCreated(task.ID)
		if err != nil {
			return EmployeeTaskPreparation{}, classifyEmployeeStore(err)
		}
	}
	if err := s.callPrepareStageHook("journal_advanced"); err != nil {
		return EmployeeTaskPreparation{}, classified(KindInternal, err)
	}
	return EmployeeTaskPreparation{
		TaskID: task.ID, EmployeeID: task.EmployeeID, EmployeeRevision: task.EmployeeRevision,
		SessionID: journal.SessionID, TaskSnapshotDigest: task.SnapshotDigest,
		CompactSnapshotDigest: compact.Digest, State: EmployeeTaskPrepared,
	}, nil
}

func (s *Service) prepareTaskSkills(task employee.EmployeeTask) ([]employee.CompactSkill, []employee.SkillCapabilityGrant, error) {
	catalog, err := s.skillCatalog()
	if err != nil {
		return nil, nil, classified(KindInternal, err)
	}
	result := make([]employee.CompactSkill, 0, len(task.Skills))
	grants := make([]employee.SkillCapabilityGrant, 0, len(task.Skills))
	for _, binding := range task.Skills {
		item, resolveErr := catalog.Resolve(binding.SkillID, binding.Version)
		if errors.Is(resolveErr, fs.ErrNotExist) {
			return nil, nil, classified(KindConflict, fmt.Errorf("pinned Skill %s@%s is missing", binding.SkillID, binding.Version))
		}
		if resolveErr != nil {
			return nil, nil, classifySkillCatalog(resolveErr)
		}
		if item.Manifest.Digest != binding.Digest {
			return nil, nil, classified(KindConflict, fmt.Errorf("pinned Skill %s@%s Digest changed", binding.SkillID, binding.Version))
		}
		if err := skill.ValidateConfiguration(item.Manifest.ConfigurationSchema, binding.Configuration); err != nil {
			return nil, nil, classified(KindConflict, fmt.Errorf("pinned Skill %s@%s configuration: %w", binding.SkillID, binding.Version, err))
		}
		if !binding.Enabled {
			continue
		}
		references := make([]employee.CompactSkillReference, 0, len(item.References))
		for path, content := range item.References {
			references = append(references, employee.CompactSkillReference{Path: path, Content: content})
		}
		sort.Slice(references, func(left, right int) bool { return references[left].Path < references[right].Path })
		result = append(result, employee.CompactSkill{
			SkillID: binding.SkillID, Version: binding.Version, Digest: binding.Digest,
			Instructions: item.Instructions, References: references,
		})
		grants = append(grants, employee.SkillCapabilityGrant{
			Enabled: true, InstructionOnly: item.Kind == skill.KindAdapter,
			Requested: append([]string{}, item.Manifest.RequestedCapabilities...),
		})
	}
	return result, grants, nil
}

func (s *Service) prepareTaskKnowledge(task employee.EmployeeTask) ([]employee.CompactKnowledge, error) {
	if len(task.Knowledge) == 0 {
		return []employee.CompactKnowledge{}, nil
	}
	state, err := s.employees.Knowledge(task.EmployeeID)
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
	result := make([]employee.CompactKnowledge, 0)
	for _, pinned := range task.Knowledge {
		source, sourceOK := sources[pinned.SourceID]
		index, indexOK := indexes[pinned.SourceID]
		if !sourceOK || !indexOK || source.Status != knowledge.StatusReady ||
			source.Digest != pinned.SourceDigest || index.SourceDigest != pinned.SourceDigest {
			return nil, classified(KindConflict, fmt.Errorf("pinned Knowledge source %q changed or is unavailable", pinned.SourceID))
		}
		available := make(map[string]knowledge.Citation)
		for _, document := range index.Documents {
			for _, citation := range document.Citations {
				available[citation.ID] = citation
			}
		}
		for _, reference := range pinned.Citations {
			citation, exists := available[reference.CitationID]
			if !exists || citation.EmployeeID != task.EmployeeID ||
				citation.SourceID != pinned.SourceID || citation.Path != reference.Path ||
				citation.Digest != reference.Digest || citation.StartLine != reference.StartLine ||
				citation.EndLine != reference.EndLine {
				return nil, classified(KindConflict, fmt.Errorf("pinned Citation %q changed or is unavailable", reference.CitationID))
			}
			result = append(result, employee.CompactKnowledge{
				SourceID: source.ID, SourceDigest: source.Digest,
				CitationID: citation.ID, Digest: citation.Digest, Title: source.Title,
				Path: citation.Path, StartLine: citation.StartLine, EndLine: citation.EndLine,
				Snippet: citation.Snippet,
			})
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].SourceID == result[right].SourceID {
			return result[left].CitationID < result[right].CitationID
		}
		return result[left].SourceID < result[right].SourceID
	})
	return result, nil
}

func (s *Service) prepareTaskMemory(task employee.EmployeeTask) ([]employee.CompactMemory, error) {
	if len(task.MemoryFacts) == 0 {
		return []employee.CompactMemory{}, nil
	}
	facts, err := s.employees.Memory(task.EmployeeID)
	if err != nil {
		return nil, classifyMemoryStore(err)
	}
	available := make(map[string]employeememory.Fact, len(facts))
	for _, fact := range facts {
		available[fact.ID] = fact
	}
	result := make([]employee.CompactMemory, 0, len(task.MemoryFacts))
	for _, pinned := range task.MemoryFacts {
		fact, exists := available[pinned.FactID]
		if !exists || fact.EmployeeID != task.EmployeeID || fact.Digest != pinned.Digest {
			return nil, classified(KindConflict, fmt.Errorf("accepted Memory Fact %q changed or is unavailable", pinned.FactID))
		}
		provenance, marshalErr := json.Marshal(fact.Provenance)
		if marshalErr != nil {
			return nil, classified(KindInternal, marshalErr)
		}
		result = append(result, employee.CompactMemory{
			FactID: fact.ID, Digest: fact.Digest, Category: fact.Category,
			Value: fact.Value, Provenance: string(provenance),
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].FactID < result[right].FactID })
	return result, nil
}

func stableEmployeeSessionID(task employee.EmployeeTask, workspace string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"employee-task-session-v1", task.EmployeeID, task.ID, task.SnapshotDigest,
		filepath.Clean(workspace),
	}, "\x00")))
	return "employee-" + hex.EncodeToString(sum[:16])
}

func preparedSessionMatches(
	value *session.Session, task employee.EmployeeTask, compact employee.CompactSnapshot,
	workspaceRealPath, sessionWorkspace, configDigest string, selection session.Selection,
) bool {
	return value != nil && value.SchemaVersion == session.SchemaVersion &&
		value.ID == stableEmployeeSessionID(task, workspaceRealPath) &&
		value.EmployeeID == task.EmployeeID && value.EmployeeTaskID == task.ID &&
		value.EmployeeRevision == task.EmployeeRevision &&
		value.EmployeeTaskSnapshotDigest == task.SnapshotDigest &&
		value.EmployeeContextSnapshot != nil &&
		value.EmployeeContextSnapshot.Digest == compact.Digest &&
		value.Workspace == sessionWorkspace && value.ConfigDigest == configDigest &&
		value.Selection == selection && value.Goal == task.Prompt &&
		value.Status == session.Open && len(value.Runs) == 0 && value.ActiveRunID == ""
}

func (s *Service) callPrepareStageHook(stage string) error {
	if s.prepareStageHook == nil {
		return nil
	}
	return s.prepareStageHook(stage)
}

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
