package controlplane

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/knowledge"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
	"github.com/Rj455555/GoHermit/internal/session"
)

const failEmployeeReadiness = "employee_not_ready"

func (s *Service) startEmployeeLoopInvocation(ctx context.Context, invocation loop.Invocation) (loop.Invocation, error) {
	input, err := s.employeeLoopTaskInput(invocation.DefinitionSnapshot, invocation.TaskSnapshot)
	if err != nil {
		if blockErr := invocation.Block(failEmployeeReadiness, clipSummary(err.Error()), time.Now().UTC()); blockErr == nil {
			_ = s.loopStore.SaveInvocation(invocation)
		}
		return invocation, nil
	}
	task, err := s.CreateEmployeeTask(ctx, invocation.DefinitionSnapshot.EmployeeID, input)
	if err != nil {
		if skipErr := invocation.Skip("Employee Task creation failed: "+err.Error(), time.Now().UTC()); skipErr == nil {
			_ = s.loopStore.SaveInvocation(invocation)
		}
		return invocation, err
	}
	invocation.EmployeeTaskID = task.ID
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation, classified(KindInternal, err)
	}
	prepared, err := s.PrepareEmployeeTask(ctx, task.ID)
	if err != nil {
		if skipErr := invocation.Skip("Employee Task preparation failed: "+err.Error(), time.Now().UTC()); skipErr == nil {
			_ = s.loopStore.SaveInvocation(invocation)
		}
		return invocation, err
	}
	sess, err := s.store.Load(ctx, prepared.SessionID)
	if err != nil {
		return invocation, classified(KindInternal, err)
	}
	recipe := deepCopyRecipe(invocation.DefinitionSnapshot.VerificationRecipe)
	sess.VerificationRecipe = &recipe
	planMode, err := session.NormalizePlanMode(invocation.DefinitionSnapshot.PlanMode)
	if err != nil {
		return invocation, classified(KindInternal, err)
	}
	sess.PlanMode = planMode
	if err = s.store.Save(ctx, sess); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = invocation.Dispatch(); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation, classified(KindInternal, err)
	}
	task, err = s.StartEmployeeTask(ctx, task.ID)
	if err != nil {
		if failErr := invocation.Fail(failRunStart, err.Error(), time.Now().UTC()); failErr == nil {
			_ = s.loopStore.SaveInvocation(invocation)
		}
		return invocation, err
	}
	if err = invocation.Attach(task.SessionID, task.RunID, time.Now().UTC()); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation, classified(KindInternal, err)
	}
	return invocation, nil
}

func (s *Service) employeeLoopTaskInput(definition loop.Definition, prompt string) (EmployeeTaskCreateInput, error) {
	record, err := s.employees.Get(definition.EmployeeID)
	if err != nil {
		return EmployeeTaskCreateInput{}, classifyEmployeeStore(err)
	}
	if record.Employee.State != employee.StateActive {
		return EmployeeTaskCreateInput{}, errors.New("Employee owner is not active")
	}
	workspace, err := canonicalWorkspace(s.Workspace)
	if err != nil {
		return EmployeeTaskCreateInput{}, err
	}
	var project *employee.ProjectBinding
	for index := range record.ProjectBindings {
		if record.ProjectBindings[index].MatchesCanonicalWorkspace(workspace) {
			value := record.ProjectBindings[index]
			project = &value
			break
		}
	}
	if project == nil {
		return EmployeeTaskCreateInput{}, errors.New("Employee has no ProjectBinding for this workspace")
	}

	skills := make([]EmployeeTaskSkillSelection, 0, len(record.Employee.SkillBindings))
	for _, binding := range record.Employee.SkillBindings {
		if binding.Enabled {
			skills = append(skills, EmployeeTaskSkillSelection{SkillID: binding.SkillID, Version: binding.Version})
		}
	}
	knowledgeState, err := s.employees.Knowledge(record.Employee.ID)
	if err != nil {
		return EmployeeTaskCreateInput{}, err
	}
	readySources := make(map[string]bool, len(knowledgeState.Sources))
	for _, source := range knowledgeState.Sources {
		readySources[source.ID] = source.Status == knowledge.StatusReady
	}
	knowledgeSelections := make([]EmployeeTaskKnowledgeSelection, 0, len(knowledgeState.Indexes))
	for _, index := range knowledgeState.Indexes {
		if !readySources[index.SourceID] {
			continue
		}
		selection := EmployeeTaskKnowledgeSelection{SourceID: index.SourceID}
		for _, document := range index.Documents {
			for _, citation := range document.Citations {
				selection.CitationIDs = append(selection.CitationIDs, citation.ID)
			}
		}
		if len(selection.CitationIDs) > 0 {
			knowledgeSelections = append(knowledgeSelections, selection)
		}
	}
	facts, err := s.employees.Memory(record.Employee.ID)
	if err != nil {
		return EmployeeTaskCreateInput{}, err
	}
	memoryIDs := make([]string, 0, len(facts))
	for _, fact := range facts {
		memoryIDs = append(memoryIDs, fact.ID)
	}
	capabilities := intersectLoopCapabilities(
		record.Employee.PermissionPolicy.AllowedCapabilities,
		project.AllowedToolCapabilities,
		definition.WorkspacePolicy.ReadOnly,
	)
	budget := record.Employee.BudgetPolicy
	if project.BudgetOverride != nil {
		budget = minimumLoopBudget(budget, *project.BudgetOverride)
	}
	budget = minimumLoopBudget(budget, employee.BudgetPolicy{
		MaxModelCalls:  definition.Budget.MaxModelCalls,
		MaxTokens:      definition.Budget.MaxTokens,
		TimeoutSeconds: definition.Budget.TimeoutSeconds,
	})
	return EmployeeTaskCreateInput{
		Prompt: prompt, Skills: skills, Knowledge: knowledgeSelections, MemoryFactIDs: memoryIDs,
		ProjectBindingID: project.ID,
		Policy: employee.TaskPolicy{
			AllowedCapabilities: capabilities,
			NetworkAllowed: record.Employee.PermissionPolicy.NetworkAllowed &&
				project.NetworkAllowed && !definition.WorkspacePolicy.ReadOnly,
			Budget: budget,
		},
	}, nil
}

func minimumLoopBudget(left, right employee.BudgetPolicy) employee.BudgetPolicy {
	return employee.BudgetPolicy{
		MaxModelCalls:  min(left.MaxModelCalls, right.MaxModelCalls),
		MaxTokens:      min(left.MaxTokens, right.MaxTokens),
		TimeoutSeconds: min(left.TimeoutSeconds, right.TimeoutSeconds),
	}
}

func intersectLoopCapabilities(employeeValues, projectValues []string, readOnly bool) []string {
	project := make(map[string]struct{}, len(projectValues))
	for _, value := range projectValues {
		project[value] = struct{}{}
	}
	result := make([]string, 0, len(employeeValues))
	for _, value := range employeeValues {
		if _, ok := project[value]; !ok {
			continue
		}
		if readOnly && isMutationCapability(value) {
			continue
		}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func isMutationCapability(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "write") || strings.Contains(lower, "execute") ||
		strings.Contains(lower, "delete") || strings.Contains(lower, "mutation") ||
		strings.Contains(lower, "network")
}

// LoopRuntimeState rebuilds the bounded state projection from Invocation
// history and persists it. Invocation/Session/Run remain execution truth.
func (s *Service) LoopRuntimeState(ctx context.Context, loopID string) (loop.RuntimeState, error) {
	s.loopScheduleMu.Lock()
	defer s.loopScheduleMu.Unlock()
	if err := s.loopStoreAvailable(); err != nil {
		return loop.RuntimeState{}, err
	}
	definition, err := s.loopStore.GetDefinition(loopID)
	if err != nil {
		return loop.RuntimeState{}, classified(KindNotFound, err)
	}
	now := time.Now().UTC()
	state, stateErr := s.loopStore.GetRuntimeState(loopID)
	if errors.Is(stateErr, loopstore.ErrRuntimeStateNotFound) {
		state = loop.NewRuntimeState(definition, now)
	} else if stateErr != nil {
		return loop.RuntimeState{}, classified(KindInternal, stateErr)
	}
	if state.DefinitionRevision != definition.Revision {
		state = alignLoopRuntimeDefinition(state, definition, now)
	}
	invocations, err := s.loopStore.ListInvocations(loopID)
	if err != nil {
		return loop.RuntimeState{}, classified(KindInternal, err)
	}
	start := 0
	if state.LastInvocationID != "" {
		found := false
		for index, stored := range invocations {
			if stored.ID != state.LastInvocationID {
				continue
			}
			found = true
			start = index + 1
			if !state.LastStatus.Terminal() {
				start = index
			}
			break
		}
		if !found {
			// A persisted frontier that is absent from the authoritative
			// invocation journal is corruption, not a reason to double-count.
			return loop.RuntimeState{}, classified(KindInternal, errors.New("loop runtime state invocation frontier is missing"))
		}
	}
	for _, stored := range invocations[start:] {
		projected := s.reconcileInvocation(ctx, stored)
		if !projected.Status.Terminal() {
			state.LastInvocationID = projected.ID
			state.LastStatus = projected.Status
			state.LastRunAt = timePointerControl(projected.CreatedAt.UTC())
			continue
		}
		state, err = loop.ProjectRuntimeState(state, projected, now)
		if err != nil {
			return loop.RuntimeState{}, classified(KindInternal, err)
		}
	}
	if state.DefinitionRevision != definition.Revision {
		state = alignLoopRuntimeDefinition(state, definition, now)
	}
	if err = s.loopStore.SaveRuntimeState(state); err != nil {
		return loop.RuntimeState{}, classified(KindInternal, err)
	}
	return state, nil
}

// RunDueLoops claims due schedules by advancing next_run_at before dispatch.
// This service remains the single scheduler/writer for the configured
// workspace; a second service instance is intentionally unsupported.
func (s *Service) RunDueLoops(ctx context.Context, now time.Time) ([]loop.Invocation, error) {
	s.loopScheduleMu.Lock()
	defer s.loopScheduleMu.Unlock()
	definitions, err := s.ListLoops()
	if err != nil {
		return nil, err
	}
	var launched []loop.Invocation
	for _, definition := range definitions {
		if !definition.Enabled || definition.Schedule.Kind != loop.ScheduleDaily {
			continue
		}
		state, stateErr := s.loopStore.GetRuntimeState(definition.ID)
		if errors.Is(stateErr, loopstore.ErrRuntimeStateNotFound) {
			state = loop.NewRuntimeState(definition, now)
			if saveErr := s.loopStore.SaveRuntimeState(state); saveErr != nil {
				return launched, classified(KindInternal, saveErr)
			}
			continue
		}
		if stateErr != nil {
			return launched, classified(KindInternal, stateErr)
		}
		if state.DefinitionRevision != definition.Revision {
			state = alignLoopRuntimeDefinition(state, definition, now)
			if saveErr := s.loopStore.SaveRuntimeState(state); saveErr != nil {
				return launched, classified(KindInternal, saveErr)
			}
			continue
		}
		if state.NextRunAt == nil || state.NextRunAt.After(now) {
			continue
		}
		next, nextErr := loop.NextScheduledTime(definition.Schedule, *state.NextRunAt)
		if nextErr != nil {
			return launched, classified(KindInternal, nextErr)
		}
		state.NextRunAt = &next
		state.UpdatedAt = now.UTC()
		if saveErr := s.loopStore.SaveRuntimeState(state); saveErr != nil {
			return launched, classified(KindInternal, saveErr)
		}
		invocation, launchErr := s.startLoopInvocation(ctx, definition.ID, loop.TriggerSchedule)
		if launchErr != nil {
			return launched, launchErr
		}
		state.LastInvocationID = invocation.ID
		state.LastStatus = invocation.Status
		state.LastRunAt = timePointerControl(invocation.CreatedAt.UTC())
		state.UpdatedAt = now.UTC()
		if saveErr := s.loopStore.SaveRuntimeState(state); saveErr != nil {
			return launched, classified(KindInternal, saveErr)
		}
		launched = append(launched, invocation)
	}
	return launched, nil
}

func alignLoopRuntimeDefinition(state loop.RuntimeState, definition loop.Definition, now time.Time) loop.RuntimeState {
	state.DefinitionRevision = definition.Revision
	state.UpdatedAt = now.UTC()
	if next, err := loop.NextScheduledTime(definition.Schedule, now); err == nil {
		state.NextRunAt = timePointerControl(next)
	} else {
		state.NextRunAt = nil
	}
	return state
}

func (s *Service) StartLoopScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			_, _ = s.RunDueLoops(ctx, time.Now().UTC())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func timePointerControl(value time.Time) *time.Time {
	copy := value
	return &copy
}
