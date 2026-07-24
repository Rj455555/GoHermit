package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
)

// errRunActive reports that the workspace run gate is already taken.
var errRunActive = errors.New("another run is already active in this workspace")

// CreateSessionInput is everything needed to open a persistent session.
type CreateSessionInput struct {
	Title    string
	Company  string
	Access   string
	Model    string
	Agent    string
	PlanMode string
}

// CreateSession validates the selection and credentials, records the
// config digest fingerprint (ADR 0011), and persists a new conversation.
// Team sessions additionally validate every template role before any
// session state exists.
func (s *Service) CreateSession(ctx context.Context, in CreateSessionInput) (*session.Session, error) {
	selection := config.RuntimeSelection{Company: in.Company, Access: in.Access, Model: in.Model, Agent: in.Agent}
	planMode, err := session.NormalizePlanMode(in.PlanMode)
	if err != nil {
		return nil, classified(KindInvalid, err)
	}
	liveModels, err := s.validateSelection(ctx, selection)
	if err != nil {
		return nil, classified(KindInvalid, err)
	}
	apiKey, err := s.resolveCredential(ctx, selection)
	if err != nil {
		return nil, classified(KindInvalid, err)
	}
	if selection.Agent == "team" {
		if err := s.validateTeamSelections(ctx, selection); err != nil {
			return nil, classified(KindInvalid, err)
		}
	}
	runtime, err := s.build(ctx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		return nil, classified(KindInvalid, err)
	}
	digest := session.ConfigDigest(runtime.Config)
	runtime.Close()
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "New conversation"
	}
	sess, err := session.NewConversation(title, runtime.Workspace, digest, session.Selection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent})
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	sess.PlanMode = planMode
	sess.GitState = session.GitState(ctx, runtime.Workspace)
	if err := s.store.Save(ctx, sess); err != nil {
		return nil, classified(KindInternal, err)
	}
	return sess, nil
}

// StartRun loads a session and launches a run carrying the given user
// message. The workspace run gate makes a concurrent launch a KindConflict.
func (s *Service) StartRun(ctx context.Context, sessionID, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 16<<10 {
		return "", &Error{Kind: KindInvalid, Message: "message must contain 1 to 16384 bytes"}
	}
	sess, err := s.store.Load(ctx, sessionID)
	if err != nil {
		return "", classified(KindNotFound, err)
	}
	runID, err := s.launchSessionRun(sess, message)
	if err != nil {
		return "", classifiedLaunchError(err)
	}
	return runID, nil
}

// ResumeRun recovers an interrupted run and relaunches it. Recover
// interrupted this run (e.g. the process stopped mid-run); its pending
// approvals died with it (ADR 0011) and the resumed run requests fresh
// ones.
func (s *Service) ResumeRun(ctx context.Context, sessionID, runID string) (string, error) {
	sess, err := s.store.Recover(ctx, sessionID)
	if err != nil {
		return "", classified(KindNotFound, err)
	}
	run := sess.ActiveRun()
	if run == nil || run.ID != runID || run.Status != session.RunInterrupted {
		return "", &Error{Kind: KindConflict, Message: "run is not interrupted or resumable"}
	}
	if expired := runcontrol.ExpireRunApprovals(sess.ApprovalRequests, run.ID, time.Now().UTC()); len(expired) > 0 {
		if _, err = s.commitAndPublishMany(sess, s.appendApprovalExpiredEvents(sess, expired, nil)); err != nil {
			return "", classified(KindInternal, err)
		}
	}
	launched, err := s.launchSessionRun(sess, "")
	if err != nil {
		return "", classifiedLaunchError(err)
	}
	return launched, nil
}

// ApprovePlan marks a queued review-plan run approved and relaunches it.
func (s *Service) ApprovePlan(ctx context.Context, sessionID, runID string) (string, error) {
	sess, err := s.store.Load(ctx, sessionID)
	if err != nil {
		return "", classified(KindNotFound, err)
	}
	run := sess.ActiveRun()
	if run == nil || run.ID != runID || run.Status != session.RunQueued || run.PlanMode != session.PlanReview || run.Plan == nil {
		return "", &Error{Kind: KindConflict, Message: "run has no review plan awaiting approval"}
	}
	if !run.PlanApproved {
		now := time.Now().UTC()
		run.PlanApproved, run.PlanApprovedAt, run.UpdatedAt = true, &now, now
		approved := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, "", "计划已批准，准备执行")
		if _, err = s.commitAndPublish(sess, approved); err != nil {
			return "", classified(KindInternal, err)
		}
	}
	launched, err := s.launchSessionRun(sess, "")
	if err != nil {
		return "", classifiedLaunchError(err)
	}
	return launched, nil
}

// classifiedLaunchError maps launch failures the way the pre-refactor HTTP
// handlers did: an occupied workspace is a conflict, everything else an
// input failure.
func classifiedLaunchError(err error) error {
	if errors.Is(err, errRunActive) {
		return classified(KindConflict, err)
	}
	return classified(KindInvalid, err)
}

// CancelRun stops a run. An executing run is signalled through its context
// and the call reports activeCancelled=true; a queued review-plan run is
// cancelled durably through the commit path and the call reports false.
func (s *Service) CancelRun(ctx context.Context, sessionID, runID string) (activeCancelled bool, err error) {
	s.runMu.Lock()
	if s.activeSession != sessionID || s.activeRun != runID || s.cancelRun == nil {
		sess, loadErr := s.store.Load(ctx, sessionID)
		if loadErr == nil {
			run := sess.ActiveRun()
			if run != nil && run.ID == runID && run.Status == session.RunQueued && run.PlanMode == session.PlanReview && !run.PlanApproved {
				transition, cancelErr := runcontrol.Cancel(run.Plan, "计划未执行，已由用户停止")
				if cancelErr == nil {
					now := time.Now().UTC()
					run.Status, run.CompletedAt, run.UpdatedAt = session.RunCancelled, &now, now
					sess.ActiveRunID = ""
					runtimeEvents := make([]event.Event, 0, 2)
					if transition.Changed {
						runtimeEvents = append(runtimeEvents, s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, transition.Detail))
					}
					cancelled := event.New(event.TaskCancelled, sess.ID)
					cancelled.RunID, cancelled.Message = run.ID, "计划已停止"
					runtimeEvents = append(runtimeEvents, cancelled)
					runtimeEvents = s.appendApprovalExpiredEvents(sess, runcontrol.ExpireRunApprovals(sess.ApprovalRequests, run.ID, now), runtimeEvents)
					_, cancelErr = s.commitAndPublishMany(sess, runtimeEvents)
				}
				s.runMu.Unlock()
				if cancelErr != nil {
					return false, classified(KindInternal, cancelErr)
				}
				return false, nil
			}
		}
		s.runMu.Unlock()
		return false, &Error{Kind: KindConflict, Message: "run is not active"}
	}
	cancel := s.cancelRun
	s.runMu.Unlock()
	cancel()
	return true, nil
}

func (s *Service) launchSessionRun(sess *session.Session, message string) (string, error) {
	selection := config.RuntimeSelection{Company: sess.Selection.Company, Access: sess.Selection.Access, Model: sess.Selection.Model, Agent: sess.Selection.Agent}
	liveModels, err := s.validateSelection(context.Background(), selection)
	if err != nil {
		return "", err
	}
	apiKey, err := s.resolveCredential(context.Background(), selection)
	if err != nil {
		return "", err
	}
	// Policy-fingerprint trigger (ADR 0011): recompute the config digest
	// exactly as CreateSession does; when it differs from the digest stored
	// on the session, approvals recorded under the old fingerprint are void.
	// This runs before the run starts executing.
	policyRuntime, err := s.build(context.Background(), s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		return "", err
	}
	digest := session.ConfigDigest(policyRuntime.Config)
	policyRuntime.Close()
	if digest != sess.ConfigDigest {
		if expired := runcontrol.ExpireApprovalsForPolicy(sess.ApprovalRequests, digest, time.Now().UTC()); len(expired) > 0 {
			if _, err = s.commitAndPublishMany(sess, s.appendApprovalExpiredEvents(sess, expired, nil)); err != nil {
				return "", err
			}
		}
	}
	s.runMu.Lock()
	if !s.active.CompareAndSwap(false, true) {
		s.runMu.Unlock()
		return "", errRunActive
	}
	if message != "" {
		run, runErr := sess.NewRun(message)
		if runErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", runErr
		}
		if sess.Title == "New conversation" {
			sess.Title = compactTitle(message)
		}
		if runErr = s.store.AppendMessage(sess.ID, session.MessageRecord{RunID: run.ID, Role: "user", Content: message}); runErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", runErr
		}
	}
	run := sess.ActiveRun()
	if run == nil {
		s.active.Store(false)
		s.runMu.Unlock()
		return "", errors.New("session has no active run")
	}
	if selection.Agent == "team" && (sess.Mission == nil || sess.Mission.RunID != run.ID) {
		budget := team.DefaultBudget()
		rolePlan, planErr := s.resolveTeamRolePlan(context.Background(), selection)
		if planErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", planErr
		}
		if rolePlan != nil && len(rolePlan.roleLimits) > 0 {
			budget.RoleLimits = rolePlan.roleLimits
		}
		mission, missionErr := team.AdaptiveMission("mission-"+run.ID, run.ID, run.Message, budget)
		if missionErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", missionErr
		}
		sess.Mission = mission
	}
	var createdPlanEvent event.Event
	if run.Plan == nil {
		if selection.Agent == "team" {
			run.Plan, err = planForMission(run.ID, sess.Mission)
		} else {
			run.Plan, err = taskplan.ForGoal(run.ID, run.Message, selection.Agent)
		}
		if err != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", err
		}
		message := "执行计划已创建"
		if run.PlanMode == session.PlanReview && !run.PlanApproved {
			message = "执行计划已创建，等待确认"
		}
		createdPlanEvent = s.planRuntimeEvent(sess.ID, run, event.PlanCreated, "", message)
	}
	if createdPlanEvent.Type != "" {
		createdPlanEvent, err = s.store.CommitEvent(context.Background(), sess, createdPlanEvent)
	} else {
		err = s.store.Save(context.Background(), sess)
	}
	if err != nil {
		s.active.Store(false)
		s.runMu.Unlock()
		return "", err
	}
	if run.PlanMode == session.PlanReview && !run.PlanApproved {
		runID := run.ID
		s.active.Store(false)
		s.runMu.Unlock()
		if createdPlanEvent.Type != "" {
			s.emit(createdPlanEvent)
		}
		return runID, nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	s.activeSession, s.activeRun, s.cancelRun = sess.ID, run.ID, cancel
	runID := run.ID
	s.runMu.Unlock()
	if createdPlanEvent.Type != "" {
		s.emit(createdPlanEvent)
	}
	go func() {
		defer func() {
			s.runMu.Lock()
			s.activeSession, s.activeRun, s.cancelRun = "", "", nil
			s.active.Store(false)
			s.runMu.Unlock()
			// The run ended: release its decided approval waiters so late
			// decides take the C2 path against the checkpointed state.
			s.approvals.Release(sess.ID)
		}()
		if selection.Agent == "team" {
			s.runTeam(runCtx, sess, runID, selection, apiKey, liveModels)
			return
		}
		runtime, buildErr := s.build(runCtx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
		if buildErr != nil {
			s.failLaunchedRun(sess, runID, buildErr)
			return
		}
		s.applyOwner(runtime)
		defer runtime.Close()
		runtime.Runner.Sink = s.emit
		// The runner persists and emits its own terminal state; every return
		// from Run — completion, failure, cancellation, or deadline
		// interruption — ends the run's pending approvals (ADR 0011).
		_ = runtime.Runner.Run(runCtx, sess)
		if expired := runcontrol.ExpireRunApprovals(sess.ApprovalRequests, runID, time.Now().UTC()); len(expired) > 0 {
			if _, err := s.commitAndPublishMany(sess, s.appendApprovalExpiredEvents(sess, expired, nil)); err != nil {
				sess.LastError = err.Error()
			}
		}
	}()
	return runID, nil
}

func planForMission(runID string, mission *team.Mission) (*taskplan.Plan, error) {
	if mission == nil || len(mission.WorkItems) == 0 {
		return nil, errors.New("team mission is required before creating its plan")
	}
	specs := make([]taskplan.StepSpec, 0, len(mission.WorkItems))
	for _, item := range mission.WorkItems {
		specs = append(specs, taskplan.StepSpec{ID: item.ID, Title: item.Title})
	}
	return taskplan.NewParallel("plan-"+runID, specs)
}

func (s *Service) planRuntimeEvent(sessionID string, run *session.Run, eventType event.Type, stepID, message string) event.Event {
	runtimeEvent := event.New(eventType, sessionID)
	runtimeEvent.RunID, runtimeEvent.PlanStepID, runtimeEvent.Message = run.ID, stepID, message
	runtimeEvent.Data, _ = json.Marshal(map[string]any{"plan": run.Plan})
	return runtimeEvent
}

// commitAndPublish durably commits one event before making it visible
// through the Publisher port: a subscriber can never see an event a crash
// would lose.
func (s *Service) commitAndPublish(sess *session.Session, runtimeEvent event.Event) (event.Event, error) {
	committed, err := s.commitAndPublishMany(sess, []event.Event{runtimeEvent})
	if err != nil {
		return event.Event{}, err
	}
	return committed[0], nil
}

// commitAndPublishMany durably commits an ordered event batch before
// publishing each committed event in order.
func (s *Service) commitAndPublishMany(sess *session.Session, runtimeEvents []event.Event) ([]event.Event, error) {
	committed, err := s.store.CommitEvents(context.Background(), sess, runtimeEvents)
	if err != nil {
		return nil, err
	}
	for _, runtimeEvent := range committed {
		s.emit(runtimeEvent)
	}
	return committed, nil
}
