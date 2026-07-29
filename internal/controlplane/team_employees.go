package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/knowledge"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

type teamEmployeeRole struct {
	runtime          app.RoleRuntime
	compact          employee.CompactSnapshot
	snapshotDigest   string
	projectBindingID string
	workspaceDigest  string
	policyDigest     string
}

func (s *Service) resolveTeamEmployeeRole(
	ctx context.Context,
	role team.Role,
	roleSelection teamtemplate.RoleSelection,
) (teamEmployeeRole, error) {
	record, err := s.employees.Get(roleSelection.EmployeeID)
	if err != nil {
		return teamEmployeeRole{}, classifyEmployeeStore(err)
	}
	if record.Employee.State != employee.StateActive {
		return teamEmployeeRole{}, fmt.Errorf("Employee %q is %s, not active", roleSelection.EmployeeID, record.Employee.State)
	}
	revision, err := s.employees.LoadRevision(record.Employee.ID, record.Employee.Revision)
	if err != nil {
		return teamEmployeeRole{}, classifyEmployeeStore(err)
	}
	if revision.EmployeeID != record.Employee.ID || revision.Revision != record.Employee.Revision ||
		revision.Digest == "" || !reflect.DeepEqual(revision.Employee, record.Employee) ||
		!reflect.DeepEqual(revision.ProjectBindings, record.ProjectBindings) {
		return teamEmployeeRole{}, errors.New("Employee revision snapshot identity mismatch")
	}
	workspace, err := canonicalWorkspace(s.Workspace)
	if err != nil {
		return teamEmployeeRole{}, err
	}
	var binding *employee.ProjectBinding
	for index := range revision.ProjectBindings {
		candidate := &revision.ProjectBindings[index]
		if candidate.WorkspaceRealPath == workspace {
			if binding != nil {
				return teamEmployeeRole{}, errors.New("Employee has ambiguous bindings for the current Service Workspace")
			}
			binding = candidate
		}
	}
	if binding == nil || !binding.MatchesCanonicalWorkspace(workspace) {
		return teamEmployeeRole{}, errors.New("Employee ProjectBinding does not exactly match the current Service Workspace")
	}

	selection := config.RuntimeSelection{Agent: app.TeamAgentProfile(role)}
	if strings.TrimSpace(roleSelection.Company) != "" {
		selection.Company, selection.Access, selection.Model =
			roleSelection.Company, roleSelection.Access, roleSelection.Model
	} else {
		selection.Company = revision.Employee.DefaultSelection.Company
		selection.Access = revision.Employee.DefaultSelection.Access
		selection.Model = revision.Employee.DefaultSelection.Model
	}
	resolved, apiKey, models, err := s.resolveTeamRoleSelection(ctx, selection)
	if err != nil {
		return teamEmployeeRole{}, err
	}
	_, agentProfile, err := config.ResolveSelectionWithModels(resolved, models)
	if err != nil {
		return teamEmployeeRole{}, err
	}
	configuration, err := app.LoadConfig(s.Workspace, s.ConfigPath)
	if err != nil {
		return teamEmployeeRole{}, fmt.Errorf("service configuration is not ready: %w", err)
	}

	skills, grants, err := s.prepareTaskSkills(employee.EmployeeTask{
		EmployeeID: record.Employee.ID,
		Skills:     append([]employee.SkillBinding{}, revision.Employee.SkillBindings...),
	})
	if err != nil {
		return teamEmployeeRole{}, err
	}
	knowledgeContext, err := s.teamEmployeeKnowledge(record.Employee.ID)
	if err != nil {
		return teamEmployeeRole{}, err
	}
	memoryContext, err := s.teamEmployeeMemory(record.Employee.ID)
	if err != nil {
		return teamEmployeeRole{}, err
	}
	effective, err := employee.ResolveEffectivePolicy(employee.CapabilityIntersection{
		Global: runtimePreparationGlobalCapabilities, AgentToolPolicy: agentProfile.ToolPolicy,
		Employee: revision.Employee.PermissionPolicy.AllowedCapabilities,
		Project:  binding.AllowedToolCapabilities, Task: runtimePreparationGlobalCapabilities,
		GlobalNetwork: configuration.Permissions.AllowNetwork, EmployeeNetwork: revision.Employee.PermissionPolicy.NetworkAllowed,
		ProjectNetwork: binding.NetworkAllowed, TaskNetwork: true, EnabledSkillGrants: grants,
	})
	if err != nil {
		return teamEmployeeRole{}, fmt.Errorf("effective capability policy is not ready: %w", err)
	}
	if len(effective.AllowedCapabilities) == 0 ||
		(!containsString(effective.AllowedCapabilities, "filesystem.read") &&
			!containsString(effective.AllowedCapabilities, "read")) {
		return teamEmployeeRole{}, errors.New("effective capability policy does not permit required workspace reads")
	}
	if role == team.RoleBuilder && (!binding.MutationAllowed ||
		(!containsString(effective.AllowedCapabilities, "filesystem.write") &&
			!containsString(effective.AllowedCapabilities, "write"))) {
		return teamEmployeeRole{}, errors.New("Builder Employee does not have effective workspace mutation permission")
	}
	budget := revision.Employee.BudgetPolicy
	if binding.BudgetOverride != nil {
		budget = *binding.BudgetOverride
	}
	compact := employee.CompactSnapshot{
		SchemaVersion: employee.CompactSnapshotSchemaVersion,
		EmployeeID:    record.Employee.ID, EmployeeRevision: record.Employee.Revision,
		TaskID: "team-role-" + string(role), TaskSnapshotDigest: revision.Digest,
		Identity: employee.CompactIdentity{
			Name: revision.Employee.Name, JobTitle: revision.Employee.JobTitle,
			Charter:            revision.Employee.Charter,
			Responsibilities:   append([]string{}, revision.Employee.Responsibilities...),
			BehaviorBoundaries: append([]string{}, revision.Employee.BehaviorBoundaries...),
		},
		EffectivePolicy: effective, Budget: budget,
		Project: employee.CompactProject{
			BindingID: binding.ID, WorkspaceFingerprint: binding.WorkspaceFingerprint,
			ReadAllowed: binding.ReadAllowed, MutationAllowed: binding.MutationAllowed,
			NetworkAllowed: binding.NetworkAllowed,
			WorkspaceSummary: fmt.Sprintf(
				"binding=%s; fingerprint=%s; read=%t; mutation=%t",
				binding.ID, binding.WorkspaceFingerprint, binding.ReadAllowed, binding.MutationAllowed,
			),
		},
		Skills: skills, Knowledge: knowledgeContext, Memory: memoryContext,
	}
	if err := employee.SealCompactSnapshot(&compact); err != nil {
		return teamEmployeeRole{}, fmt.Errorf("compact Team Employee context is not ready: %w", err)
	}
	if _, err := contextmgr.EmployeeContextFromCompact(compact); err != nil {
		return teamEmployeeRole{}, fmt.Errorf("compact Team Employee context contract: %w", err)
	}
	policyDigest, err := digestJSON(effective)
	if err != nil {
		return teamEmployeeRole{}, err
	}
	return teamEmployeeRole{
		runtime: app.RoleRuntime{Selection: resolved, APIKey: apiKey, Models: models},
		compact: compact, snapshotDigest: revision.Digest,
		projectBindingID: binding.ID, workspaceDigest: binding.WorkspaceFingerprint,
		policyDigest: policyDigest,
	}, nil
}

func (s *Service) teamEmployeeKnowledge(employeeID string) ([]employee.CompactKnowledge, error) {
	state, err := s.employees.Knowledge(employeeID)
	if err != nil {
		return nil, classifyPhase4Store(err)
	}
	indexes := make(map[string]knowledge.Index, len(state.Indexes))
	for _, index := range state.Indexes {
		indexes[index.SourceID] = index
	}
	result := []employee.CompactKnowledge{}
	for _, source := range state.Sources {
		index, ok := indexes[source.ID]
		if source.Status != knowledge.StatusReady || !ok ||
			index.EmployeeID != employeeID || index.SourceDigest != source.Digest {
			return nil, fmt.Errorf("Knowledge source %q is stale or unavailable", source.ID)
		}
		for _, document := range index.Documents {
			for _, citation := range document.Citations {
				result = append(result, employee.CompactKnowledge{
					SourceID: source.ID, SourceDigest: source.Digest,
					CitationID: citation.ID, Digest: citation.Digest, Title: source.Title,
					Path: citation.Path, StartLine: citation.StartLine, EndLine: citation.EndLine,
					Snippet: citation.Snippet,
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SourceID == result[j].SourceID {
			return result[i].CitationID < result[j].CitationID
		}
		return result[i].SourceID < result[j].SourceID
	})
	return result, nil
}

func (s *Service) teamEmployeeMemory(employeeID string) ([]employee.CompactMemory, error) {
	facts, err := s.employees.Memory(employeeID)
	if err != nil {
		return nil, classifyMemoryStore(err)
	}
	result := make([]employee.CompactMemory, 0, len(facts))
	for _, fact := range facts {
		if fact.EmployeeID != employeeID {
			return nil, errors.New("Memory Fact belongs to another Employee")
		}
		provenance, marshalErr := json.Marshal(fact.Provenance)
		if marshalErr != nil {
			return nil, marshalErr
		}
		result = append(result, employee.CompactMemory{
			FactID: fact.ID, Digest: fact.Digest, Category: fact.Category,
			Value: fact.Value, Provenance: string(provenance),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FactID < result[j].FactID })
	return result, nil
}

func (s *Service) materializeTeamEmployeeAssignments(
	ctx context.Context,
	sess *session.Session,
	plan *teamRolePlan,
) error {
	if plan == nil || len(plan.employeeRoles) == 0 {
		return nil
	}
	if sess == nil || sess.Mission == nil || s.store == nil {
		return errors.New("Team Employee assignment requires a Mission and Session Store")
	}
	if sess.Mission.EmployeeAssignments == nil {
		sess.Mission.EmployeeAssignments = map[string]team.TeamEmployeeAssignment{}
	}
	if plan.workItems == nil {
		plan.workItems = map[string]app.RoleRuntime{}
	}
	for index := range sess.Mission.WorkItems {
		if err := s.materializeTeamEmployeeWorkItem(ctx, sess, plan, &sess.Mission.WorkItems[index]); err != nil {
			return err
		}
	}
	return sess.Mission.ValidateEmployeeAssignments()
}

func (s *Service) materializeTeamEmployeeWorkItem(
	ctx context.Context,
	sess *session.Session,
	plan *teamRolePlan,
	item *team.WorkItem,
) error {
	if plan == nil || item == nil {
		return nil
	}
	source, ok := plan.employeeRoles[string(item.Role)]
	if !ok {
		return nil
	}
	if existing, ok := sess.Mission.EmployeeAssignments[item.ID]; ok {
		runtime, runtimeOK := plan.workItems[item.ID]
		if !runtimeOK || runtime.EmployeeAssignment == nil ||
			runtime.EmployeeAssignment.Digest != existing.Digest {
			return errors.New("existing Team Employee assignment runtime mismatch")
		}
		return nil
	}
	if item.ExecutionSessionID == "" {
		item.ExecutionSessionID = "worker-" + sess.Mission.ID + "-" + item.ID
	}
	compact := source.compact.Clone()
	compact.TaskID, compact.TaskSnapshotDigest = item.ID, source.snapshotDigest
	if err := employee.SealCompactSnapshot(&compact); err != nil {
		return err
	}
	assignment := team.TeamEmployeeAssignment{
		SchemaVersion: team.EmployeeAssignmentSchemaVersion,
		WorkItemID:    item.ID, Role: item.Role,
		EmployeeID: compact.EmployeeID, EmployeeRevision: compact.EmployeeRevision,
		EmployeeSnapshotDigest: source.snapshotDigest,
		ProjectBindingID:       source.projectBindingID, WorkspaceFingerprint: source.workspaceDigest,
		Company: source.runtime.Selection.Company, Access: source.runtime.Selection.Access,
		Model: source.runtime.Selection.Model, AgentProfile: source.runtime.Selection.Agent,
		EffectivePolicyDigest: source.policyDigest, ContextDigest: compact.Digest,
	}
	if err := team.SealTeamEmployeeAssignment(&assignment); err != nil {
		return err
	}
	sess.Mission.EmployeeAssignments[item.ID] = assignment
	runtime := source.runtime
	assignmentCopy, contextCopy := assignment, compact
	runtime.EmployeeAssignment, runtime.EmployeeContext = &assignmentCopy, &contextCopy
	plan.workItems[item.ID] = runtime
	if err := sess.Mission.ValidateEmployeeAssignments(); err != nil {
		delete(sess.Mission.EmployeeAssignments, item.ID)
		delete(plan.workItems, item.ID)
		return err
	}

	exists, err := s.store.CheckTarget(item.ExecutionSessionID)
	if err != nil {
		return fmt.Errorf("hidden Worker Session target: %w", err)
	}
	if exists {
		child, loadErr := s.store.Load(ctx, item.ExecutionSessionID)
		if loadErr != nil {
			return loadErr
		}
		if child.TeamEmployeeAssignment == nil || child.TeamEmployeeContextSnapshot == nil ||
			child.TeamEmployeeAssignment.Digest != assignment.Digest ||
			child.TeamEmployeeContextSnapshot.Digest != compact.Digest {
			return errors.New("existing hidden Worker Employee snapshot mismatch")
		}
		return nil
	}
	child, err := session.NewTeamWorker(
		item.ExecutionSessionID, item.Goal, item.Title, sess.Workspace, sess.ConfigDigest,
		sess.ID, sess.Mission.RunID,
		session.Selection{
			Company: assignment.Company, Access: assignment.Access,
			Model: assignment.Model, Agent: assignment.AgentProfile,
		},
		assignment, compact,
	)
	if err != nil {
		return err
	}
	child.GitState = session.GitState(ctx, sess.Workspace)
	if err = s.store.Save(ctx, child); err != nil {
		return err
	}
	return sess.Mission.ValidateEmployeeAssignments()
}

func (s *Service) restoreTeamEmployeePlan(
	ctx context.Context,
	sess *session.Session,
) (*teamRolePlan, error) {
	if sess == nil || sess.Mission == nil || len(sess.Mission.EmployeeAssignments) == 0 {
		return nil, nil
	}
	plan := &teamRolePlan{
		overrides: map[string]app.RoleRuntime{}, workItems: map[string]app.RoleRuntime{},
		roleLimits: sess.Mission.Budget.RoleLimits, employeeRoles: map[string]teamEmployeeRole{},
	}
	// Preserve the pre-Phase-9 mixed-template behavior for roles without an
	// Employee assignment: they continue to resolve through the existing
	// RoleSelection path. Employee-assigned roles below are restored only
	// from hidden snapshots and never from the mutable Employee Store.
	if template, err := s.loadTeamTemplate(); err == nil && !template.Empty() {
		selections := teamtemplate.EffectiveSelections(template)
		for _, role := range teamValidationRoles {
			roleSelection := selections[role]
			if roleSelection.EmployeeID != "" {
				continue
			}
			selection := config.RuntimeSelection{
				Company: roleSelection.Company, Access: roleSelection.Access,
				Model: roleSelection.Model, Agent: sess.Selection.Agent,
			}
			resolved, apiKey, models, resolveErr := s.resolveTeamRoleSelection(ctx, selection)
			if resolveErr != nil {
				return nil, fmt.Errorf("team role %q: %w", role, resolveErr)
			}
			plan.overrides[role] = app.RoleRuntime{Selection: resolved, APIKey: apiKey, Models: models}
		}
	} else if err != nil {
		return nil, err
	}
	for _, item := range sess.Mission.WorkItems {
		assignment, ok := sess.Mission.EmployeeAssignments[item.ID]
		if !ok {
			continue
		}
		if item.ExecutionSessionID == "" {
			return nil, errors.New("assigned Team WorkItem has no hidden Session")
		}
		child, err := s.store.Load(ctx, item.ExecutionSessionID)
		if err != nil {
			return nil, err
		}
		if child.TeamEmployeeAssignment == nil || child.TeamEmployeeContextSnapshot == nil ||
			child.TeamEmployeeAssignment.Digest != assignment.Digest {
			return nil, errors.New("hidden Worker assignment snapshot mismatch")
		}
		selection := config.RuntimeSelection{
			Company: assignment.Company, Access: assignment.Access,
			Model: assignment.Model, Agent: assignment.AgentProfile,
		}
		resolved, apiKey, models, err := s.resolveTeamRoleSelection(ctx, selection)
		if err != nil {
			return nil, err
		}
		assignmentCopy, compactCopy := assignment, child.TeamEmployeeContextSnapshot.Clone()
		plan.workItems[item.ID] = app.RoleRuntime{
			Selection: resolved, APIKey: apiKey, Models: models,
			EmployeeAssignment: &assignmentCopy, EmployeeContext: &compactCopy,
		}
		roleKey := string(item.Role)
		if _, exists := plan.employeeRoles[roleKey]; !exists {
			plan.employeeRoles[roleKey] = teamEmployeeRole{
				runtime: plan.workItems[item.ID], compact: compactCopy,
				snapshotDigest:   assignment.EmployeeSnapshotDigest,
				projectBindingID: assignment.ProjectBindingID,
				workspaceDigest:  assignment.WorkspaceFingerprint,
				policyDigest:     assignment.EffectivePolicyDigest,
			}
		}
	}
	return plan, nil
}

func digestJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
