package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

func (s *Service) runTeam(ctx context.Context, sess *session.Session, runID string, selection config.RuntimeSelection, apiKey string, liveModels []config.ModelOption) {
	run := sess.ActiveRun()
	if run == nil || run.ID != runID || sess.Mission == nil {
		s.failLaunchedRun(sess, runID, errors.New("team mission is missing"))
		return
	}
	// A non-empty team template pins roles to their own validated runtimes; a
	// load or resolution failure fails the launch closed, like creation.
	rolePlan, planErr := s.resolveTeamRolePlan(ctx, selection)
	if planErr != nil {
		s.failLaunchedRun(sess, runID, planErr)
		return
	}
	now := time.Now().UTC()
	run.Status, run.UpdatedAt = session.RunRunning, now
	started := event.New(event.TaskStarted, sess.ID)
	started.RunID, started.MissionID, started.AgentID = runID, sess.Mission.ID, string(team.RoleLead)
	if _, err := s.commitAndPublish(sess, started); err != nil {
		s.failLaunchedRun(sess, runID, err)
		return
	}
	profile, _ := s.owner.Load()
	teamWorker := &app.TeamWorker{
		Workspace: s.Workspace, ConfigPath: s.ConfigPath, Selection: selection, APIKey: apiKey, Models: liveModels,
		OwnerContext: owner.Markdown(profile), ParentSessionID: sess.ID, ParentRunID: runID, ParentStore: s.store, Sink: s.emit,
		Approvals: s.approvals,
		Build: func(ctx context.Context, workspace, configPath string, options app.RuntimeOptions) (*app.Runtime, error) {
			runtime, buildErr := s.build(ctx, workspace, configPath, *options.Selection, options.APIKey, options.Models)
			if buildErr == nil && runtime != nil && runtime.Runner != nil && options.Approvals != nil {
				runtime.Runner.Approvals = options.Approvals
			}
			return runtime, buildErr
		},
	}
	if rolePlan != nil {
		teamWorker.RoleSelections = rolePlan.overrides
	}
	// A loop invocation's verification recipe feeds the existing verifier:
	// the worker runs the declared checks deterministically before the
	// verifier result is mapped, so the evidence travels the existing
	// handoff/Checks channel rather than a second framework.
	if sess.VerificationRecipe != nil {
		teamWorker.VerificationRecipe = sess.VerificationRecipe
	}
	var worker team.Worker = teamWorker
	if s.teamWorker != nil {
		worker = s.teamWorker
	}
	var sinkErr error
	coordinator := &team.Coordinator{
		Worker: worker,
		Sink: func(teamEvent team.TeamEvent) {
			if sinkErr != nil {
				return
			}
			runtimeEvent := event.New(teamEventType(teamEvent.Type), sess.ID)
			runtimeEvent.RunID, runtimeEvent.MissionID, runtimeEvent.WorkItemID = runID, sess.Mission.ID, teamEvent.WorkItemID
			runtimeEvent.AgentID, runtimeEvent.Message = string(teamEvent.Role), teamEvent.Message
			runtimeEvents := []event.Event{runtimeEvent}
			planRevision := 0
			if run.Plan != nil {
				planRevision = run.Plan.Revision
			}
			transition, transitionErr := runcontrol.ApplyTeamEvent(run.Plan, teamEvent, sess.Mission)
			if transitionErr != nil {
				sinkErr = transitionErr
				return
			}
			if transition.Changed {
				planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, transition.Detail)
				planEvent.MissionID = sess.Mission.ID
				runtimeEvents = append(runtimeEvents, planEvent)
			}
			if run.Plan != nil && run.Plan.Revision != planRevision {
				// The plan moved to a new revision: pending approvals recorded
				// against an older revision are stale (ADR 0011); requests
				// recorded against the new revision survive.
				expired := runcontrol.ExpireApprovalsForPlanRevision(sess.ApprovalRequests, runID, run.Plan.Revision, time.Now().UTC())
				runtimeEvents = s.appendApprovalExpiredEvents(sess, expired, runtimeEvents)
			}
			_, sinkErr = s.commitAndPublishMany(sess, runtimeEvents)
		},
		Checkpoint: func(mission *team.Mission) error {
			if sinkErr != nil {
				return sinkErr
			}
			sess.Mission = mission
			if active := sess.ActiveRun(); active != nil && active.ID == runID {
				active.ModelCalls = mission.Usage.ModelCalls
				active.TotalTokens = mission.Usage.Tokens
				active.UpdatedAt = mission.UpdatedAt
			}
			sess.GitState = session.GitState(ctx, s.Workspace)
			return s.store.Save(context.WithoutCancel(ctx), sess)
		},
	}
	// The recipe's repair bound caps the verifier requeue loop; zero keeps
	// the coordinator's existing default. Loop validation already bounds the
	// value; the clamp is defense in depth.
	if sess.VerificationRecipe != nil && sess.VerificationRecipe.MaxRepairAttempts > 0 {
		coordinator.MaxRepairAttempts = min(sess.VerificationRecipe.MaxRepairAttempts, loop.MaxRepairAttempts)
	}
	err := coordinator.Run(ctx, sess.Mission)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.finishTeamCancelled(sess, run, err)
			return
		}
		s.failLaunchedRun(sess, runID, err)
		return
	}
	if run.Plan == nil || run.Plan.Status != taskplan.Completed {
		s.failLaunchedRun(sess, runID, errors.New("team live plan completion gate failed"))
		return
	}
	final := finalTeamHandoff(sess.Mission)
	now = time.Now().UTC()
	run.Status, run.FinalMessage, run.CompletedAt, run.UpdatedAt = session.RunCompleted, final.Summary, &now, now
	run.ModelCalls, run.TotalTokens = sess.Mission.Usage.ModelCalls, sess.Mission.Usage.Tokens
	run.ModifiedFiles = missionModifiedFiles(sess.Mission)
	for _, handoff := range sess.Mission.Handoffs {
		for _, check := range handoff.Checks {
			sess.TestResults = append(sess.TestResults, session.TestResult{Command: check.Command, Passed: check.Passed, Summary: check.Summary, ExitCode: check.ExitCode, DurationMS: check.DurationMS, Time: handoff.CreatedAt, RunID: runID})
		}
	}
	sess.ActiveRunID, sess.LastError = "", ""
	sess.CompletedSteps = append(sess.CompletedSteps, "Personal Agent Team completed mission "+sess.Mission.ID)
	if final.Summary == "" {
		final.Summary = "团队任务已完成并通过独立验证。"
		run.FinalMessage = final.Summary
	}
	_ = s.store.AppendMessage(sess.ID, session.MessageRecord{RunID: runID, Role: "assistant", Content: final.Summary})
	completed := event.New(event.TaskCompleted, sess.ID)
	completed.RunID, completed.MissionID, completed.AgentID, completed.Message = runID, sess.Mission.ID, string(team.RoleLead), final.Summary
	// Normal completion terminates the run's pending approvals too (ADR 0011).
	runtimeEvents := s.appendApprovalExpiredEvents(sess, runcontrol.ExpireRunApprovals(sess.ApprovalRequests, runID, now), []event.Event{completed})
	if _, err := s.commitAndPublishMany(sess, runtimeEvents); err != nil {
		sess.LastError = err.Error()
	}
}

func (s *Service) finishTeamCancelled(sess *session.Session, run *session.Run, cause error) {
	now := time.Now().UTC()
	status, eventType := session.RunCancelled, event.TaskCancelled
	var runtimeEvents []event.Event
	if errors.Is(cause, context.DeadlineExceeded) {
		status, eventType = session.RunInterrupted, event.RunInterrupted
		if sess.Mission != nil && sess.Mission.Status != team.Interrupted {
			sess.Mission.Interrupt(cause.Error())
		}
		if run.Plan != nil && run.Plan.Current() != nil {
			if transition, noteErr := runcontrol.Interrupt(run.Plan, "运行已中断，可从当前步骤恢复"); noteErr == nil && transition.Changed {
				planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, "运行已中断，可恢复")
				planEvent.MissionID = sess.Mission.ID
				runtimeEvents = append(runtimeEvents, planEvent)
			}
		}
	} else if sess.Mission != nil {
		sess.Mission.Cancel(cause.Error())
		if run.Plan != nil {
			if transition, cancelErr := runcontrol.Cancel(run.Plan, "任务已由用户停止"); cancelErr == nil && transition.Changed {
				planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, "任务已停止")
				planEvent.MissionID = sess.Mission.ID
				runtimeEvents = append(runtimeEvents, planEvent)
			}
		}
	}
	run.Status, run.Error, run.UpdatedAt = status, cause.Error(), now
	if sess.Mission != nil {
		run.ModelCalls, run.TotalTokens = sess.Mission.Usage.ModelCalls, sess.Mission.Usage.Tokens
	}
	if status == session.RunInterrupted {
		run.CompletedAt = nil
	} else {
		run.CompletedAt = &now
		sess.ActiveRunID = ""
	}
	runtimeEvent := event.New(eventType, sess.ID)
	runtimeEvent.RunID, runtimeEvent.MissionID, runtimeEvent.Error = run.ID, sess.Mission.ID, cause.Error()
	runtimeEvents = append(runtimeEvents, runtimeEvent)
	// ADR 0011: interruption ends pending approvals just like cancellation;
	// a resumed run must request fresh ones.
	runtimeEvents = s.appendApprovalExpiredEvents(sess, runcontrol.ExpireRunApprovals(sess.ApprovalRequests, run.ID, now), runtimeEvents)
	if _, err := s.commitAndPublishMany(sess, runtimeEvents); err != nil {
		sess.LastError = err.Error()
	}
}

func (s *Service) failLaunchedRun(sess *session.Session, runID string, cause error) {
	if run := sess.ActiveRun(); run != nil && run.ID == runID {
		var runtimeEvents []event.Event
		if run.Plan != nil && run.Plan.Status == taskplan.Active {
			step := run.Plan.Current()
			if step == nil {
				step = run.Plan.NextPending()
			}
			if step != nil {
				if changed, planErr := run.Plan.Fail(step.ID, cause.Error()); planErr == nil && changed {
					planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, step.ID, cause.Error())
					runtimeEvents = append(runtimeEvents, planEvent)
				}
			}
		}
		now := time.Now().UTC()
		run.Status = session.RunFailed
		run.Error = cause.Error()
		run.CompletedAt = &now
		run.UpdatedAt = now
		sess.ActiveRunID = ""
		sess.LastError = cause.Error()
		e := event.New(event.TaskFailed, sess.ID)
		e.RunID = runID
		e.Error = cause.Error()
		runtimeEvents = append(runtimeEvents, e)
		runtimeEvents = s.appendApprovalExpiredEvents(sess, runcontrol.ExpireRunApprovals(sess.ApprovalRequests, runID, now), runtimeEvents)
		if _, err := s.commitAndPublishMany(sess, runtimeEvents); err != nil {
			sess.LastError = err.Error()
		}
	}
}

func teamEventType(value team.TeamEventType) event.Type {
	switch value {
	case team.MissionStarted:
		return event.MissionStarted
	case team.MissionFinished:
		return event.MissionCompleted
	case team.MissionFailed:
		return event.MissionFailed
	case team.WorkItemStarted:
		return event.WorkItemStarted
	case team.WorkItemDone:
		return event.WorkItemCompleted
	case team.WorkItemFailed:
		return event.WorkItemFailed
	default:
		return event.SessionUpdated
	}
}

func finalTeamHandoff(mission *team.Mission) team.Handoff {
	if mission == nil {
		return team.Handoff{}
	}
	for i := len(mission.Handoffs) - 1; i >= 0; i-- {
		if mission.Handoffs[i].Role == team.RoleLead {
			return mission.Handoffs[i]
		}
	}
	return team.Handoff{}
}

func missionModifiedFiles(mission *team.Mission) []string {
	seen := map[string]bool{}
	var files []string
	if mission == nil {
		return files
	}
	for _, handoff := range mission.Handoffs {
		for _, file := range handoff.ModifiedFiles {
			if file != "" && !seen[file] {
				seen[file] = true
				files = append(files, file)
			}
		}
	}
	sort.Strings(files)
	return files
}

// teamValidationRoles fixes a stable order so the first reported validation
// failure is deterministic.
var teamValidationRoles = []string{
	string(team.RoleLead), string(team.RoleExplorer), string(team.RoleBuilder),
	string(team.RoleReviewer), string(team.RoleVerifier),
}

// loadTeamTemplate reads the stored team template. Resolution failures are
// fail-closed and deferred from service construction, so a missing store is
// a request-time error, not a startup error.
func (s *Service) loadTeamTemplate() (teamtemplate.Template, error) {
	if s.teamTemplatesErr != nil {
		return teamtemplate.Template{}, fmt.Errorf("team template store unavailable: %w", s.teamTemplatesErr)
	}
	if s.teamTemplates == nil {
		return teamtemplate.Template{}, errors.New("team template store unavailable")
	}
	template, err := s.teamTemplates.Load()
	if err != nil {
		return teamtemplate.Template{}, fmt.Errorf("load team template: %w", err)
	}
	return template, nil
}

// validateTeamSelections checks every team role's effective provider/model
// selection from the team template before any session state exists. An empty
// template keeps the legacy behavior: every role runs on the session-level
// selection, which CreateSession already validated.
func (s *Service) validateTeamSelections(ctx context.Context, sessionSelection config.RuntimeSelection) error {
	template, err := s.loadTeamTemplate()
	if err != nil {
		return err
	}
	if template.Empty() {
		return nil
	}
	selections := teamtemplate.EffectiveSelections(template)
	checked := map[string]bool{}
	for _, role := range teamValidationRoles {
		roleSelection := selections[role]
		selection := config.RuntimeSelection{Company: roleSelection.Company, Access: roleSelection.Access, Model: roleSelection.Model, Agent: sessionSelection.Agent}
		key := selection.Company + "\x00" + selection.Access + "\x00" + selection.Model
		if checked[key] {
			continue
		}
		checked[key] = true
		if err := s.validateTeamRoleSelection(ctx, selection); err != nil {
			return fmt.Errorf("team role %q: %w", role, err)
		}
	}
	return nil
}

// teamRolePlan holds the per-role execution overrides and budget limits a
// non-empty team template implies for one launch.
type teamRolePlan struct {
	overrides  map[string]app.RoleRuntime
	roleLimits map[team.Role]team.Usage
}

// resolveTeamRolePlan loads the team template and resolves every role's
// effective selection into validated runtime inputs plus per-role budget
// limits. A nil plan means the template is empty and the launch keeps legacy
// session-level behavior. Like creation-time validation, load and resolution
// failures are fail-closed.
func (s *Service) resolveTeamRolePlan(ctx context.Context, sessionSelection config.RuntimeSelection) (*teamRolePlan, error) {
	template, err := s.loadTeamTemplate()
	if err != nil {
		return nil, err
	}
	if template.Empty() {
		return nil, nil
	}
	selections := teamtemplate.EffectiveSelections(template)
	resolved := map[string]app.RoleRuntime{}
	plan := &teamRolePlan{overrides: map[string]app.RoleRuntime{}}
	for _, role := range teamValidationRoles {
		roleSelection := selections[role]
		selection := config.RuntimeSelection{Company: roleSelection.Company, Access: roleSelection.Access, Model: roleSelection.Model, Agent: sessionSelection.Agent}
		key := selection.Company + "\x00" + selection.Access + "\x00" + selection.Model
		runtime, ok := resolved[key]
		if !ok {
			_, apiKey, liveModels, resolveErr := s.resolveTeamRoleSelection(ctx, selection)
			if resolveErr != nil {
				return nil, fmt.Errorf("team role %q: %w", role, resolveErr)
			}
			runtime = app.RoleRuntime{Selection: selection, APIKey: apiKey, Models: liveModels}
			resolved[key] = runtime
		}
		plan.overrides[role] = runtime
		if roleSelection.MaxModelCalls > 0 || roleSelection.MaxTokens > 0 {
			if plan.roleLimits == nil {
				plan.roleLimits = map[team.Role]team.Usage{}
			}
			plan.roleLimits[team.Role(role)] = team.Usage{ModelCalls: roleSelection.MaxModelCalls, Tokens: roleSelection.MaxTokens}
		}
	}
	return plan, nil
}

// resolveTeamRoleSelection validates one role's effective selection against
// the live catalog and resolves its credential, returning the validated
// selection with everything execution needs to build the role's runtime.
func (s *Service) resolveTeamRoleSelection(ctx context.Context, selection config.RuntimeSelection) (config.RuntimeSelection, string, []config.ModelOption, error) {
	liveModels, err := s.validateSelection(ctx, selection)
	if err != nil {
		return config.RuntimeSelection{}, "", nil, err
	}
	access, ok := config.AccessProfile(selection.Company, selection.Access)
	if !ok {
		return config.RuntimeSelection{}, "", nil, errors.New("未知的接入方式")
	}
	configured, _, detail := s.accessStatus(ctx, access)
	if !configured {
		return config.RuntimeSelection{}, "", nil, fmt.Errorf("%s: %s", access.Label, detail)
	}
	apiKey, err := s.resolveCredential(ctx, selection)
	if err != nil {
		return config.RuntimeSelection{}, "", nil, err
	}
	return selection, apiKey, liveModels, nil
}

// validateTeamRoleSelection reruns the session-level catalog, credential, and
// provider capability checks for one role's effective selection.
func (s *Service) validateTeamRoleSelection(ctx context.Context, selection config.RuntimeSelection) error {
	selection, apiKey, liveModels, err := s.resolveTeamRoleSelection(ctx, selection)
	if err != nil {
		return err
	}
	preset, _, err := config.ResolveSelectionWithModels(selection, liveModels)
	if err != nil {
		return err
	}
	runtime, err := s.build(ctx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		return err
	}
	defer runtime.Close()
	// Team roles always send tools, so a provider without tool-call support
	// can never serve them.
	if runtime.Runner == nil || runtime.Runner.Provider == nil || !runtime.Runner.Provider.Capabilities().ToolCalls {
		return fmt.Errorf("provider %q does not support the tool calls every team role requires", preset.Provider)
	}
	return nil
}
