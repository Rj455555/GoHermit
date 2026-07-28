package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/session"
)

// EmployeeTaskExecutionState reuses the existing string-backed Task state
// type for API compatibility while adding projected Session/Run values.
type EmployeeTaskExecutionState = employee.TaskState

const (
	EmployeeTaskStateQueued       EmployeeTaskExecutionState = "queued"
	EmployeeTaskStatePrepared     EmployeeTaskExecutionState = "prepared"
	EmployeeTaskStateWaitingOwner EmployeeTaskExecutionState = "waiting_owner"
	EmployeeTaskStateRunning      EmployeeTaskExecutionState = "running"
	EmployeeTaskStateVerifying    EmployeeTaskExecutionState = "verifying"
	EmployeeTaskStateInterrupted  EmployeeTaskExecutionState = "interrupted"
	EmployeeTaskStateCompleted    EmployeeTaskExecutionState = "completed"
	EmployeeTaskStateFailed       EmployeeTaskExecutionState = "failed"
	EmployeeTaskStateCancelled    EmployeeTaskExecutionState = "cancelled"
)

type employeeRunLaunch struct {
	TaskID  string
	Context contextmgr.EmployeeContext
}

func stableEmployeeRunID(task employee.EmployeeTask, sessionID string) string {
	sum := sha256.Sum256([]byte("employee-task-run-v1\x00" + task.ID + "\x00" + sessionID + "\x00" + task.SnapshotDigest))
	return "run-" + hex.EncodeToString(sum[:16])
}

// StartEmployeeTask is the only EmployeeTask execution entry. It reconciles
// preparation, fixes one stable Run ID, persists the Run, binds the Task, and
// only then allows the existing Runner to start.
func (s *Service) StartEmployeeTask(ctx context.Context, taskID string) (EmployeeTaskView, error) {
	s.employeeTaskMu.Lock()
	defer s.employeeTaskMu.Unlock()

	task, err := s.employees.GetTask(taskID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	if task.State == employee.TaskCancelled {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("cancelled Employee Task cannot start"))
	}
	record, err := s.employees.Get(task.EmployeeID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	if record.Employee.State != employee.StateActive {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("disabled or archived Employee cannot start execution"))
	}
	if task.SessionID != "" {
		if _, err := s.reconcileBoundEmployeeTask(ctx, task, false); err != nil {
			return EmployeeTaskView{}, err
		}
		if err := s.reconcileBoundDispatch(task); err != nil {
			return EmployeeTaskView{}, err
		}
		return s.reconcileBoundEmployeeTask(ctx, task, true)
	}
	if err := s.ensureEmployeeRunAvailable(ctx, task); err != nil {
		return EmployeeTaskView{}, err
	}

	journal, err := s.employees.LoadDispatch(task.ID)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, employeestore.ErrNotFound) {
		if _, err = s.PrepareEmployeeTask(ctx, task.ID); err != nil {
			return EmployeeTaskView{}, err
		}
		journal, err = s.employees.LoadDispatch(task.ID)
	}
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	if journal.Stage == employeestore.DispatchPrepared {
		if _, err = s.PrepareEmployeeTask(ctx, task.ID); err != nil {
			return EmployeeTaskView{}, err
		}
		journal, err = s.employees.LoadDispatch(task.ID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
	}
	if journal.TaskID != task.ID || journal.EmployeeID != task.EmployeeID ||
		journal.TaskSnapshotDigest != task.SnapshotDigest {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("dispatch journal does not match Employee Task"))
	}
	workspace, err := canonicalWorkspace(s.Workspace)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	if journal.WorkspaceRealPath != workspace || task.ProjectBinding.WorkspaceRealPath != workspace {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("dispatch workspace no longer matches the service workspace"))
	}
	sess, err := s.store.Load(ctx, journal.SessionID)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	if err := validatePreparedExecutionSession(sess, task, journal); err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	sessionWorkspace, err := filepath.Abs(s.Workspace)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	configuration, err := app.LoadConfig(s.Workspace, s.ConfigPath)
	if err != nil {
		return EmployeeTaskView{}, classified(KindConflict, err)
	}
	expectedSelection := session.Selection{
		Company: task.EmployeeSnapshot.Employee.DefaultSelection.Company,
		Access:  task.EmployeeSnapshot.Employee.DefaultSelection.Access,
		Model:   task.EmployeeSnapshot.Employee.DefaultSelection.Model,
		Agent:   task.EmployeeSnapshot.Employee.AgentProfile,
	}
	if sess.Workspace != sessionWorkspace || sess.Goal != task.Prompt ||
		sess.ConfigDigest != session.ConfigDigest(configuration) || sess.Selection != expectedSelection {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("prepared Session execution inputs changed"))
	}
	runID := stableEmployeeRunID(task, sess.ID)
	if len(sess.Runs) > 1 || (len(sess.Runs) == 1 &&
		(sess.Runs[0].ID != runID || sess.Runs[0].Message != task.Prompt)) {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("prepared Session contains an ambiguous Run"))
	}
	if journal.Stage == employeestore.DispatchSessionCreated {
		if err := s.callEmployeeTaskStageHook("session_prepared"); err != nil {
			return EmployeeTaskView{}, classified(KindInternal, err)
		}
		journal, err = s.employees.PrepareDispatchRun(task.ID, runID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
	}
	if journal.RunID != runID {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("dispatch stable Run identity mismatch"))
	}
	if journal.Stage == employeestore.DispatchRunPrepared {
		run, createErr := sess.NewRunWithID(runID, task.Prompt)
		if createErr != nil {
			return EmployeeTaskView{}, classified(KindInternal, createErr)
		}
		if run.ID != runID {
			return EmployeeTaskView{}, classified(KindInternal, errors.New("Session created an unexpected Run"))
		}
		if saveErr := s.store.Save(ctx, sess); saveErr != nil {
			return EmployeeTaskView{}, classified(KindInternal, saveErr)
		}
		if messageErr := s.ensureEmployeeRunMessage(sess.ID, runID, task.Prompt); messageErr != nil {
			return EmployeeTaskView{}, classified(KindInternal, messageErr)
		}
		if err := s.callEmployeeTaskStageHook("run_saved"); err != nil {
			return EmployeeTaskView{}, classified(KindInternal, err)
		}
		journal, err = s.employees.MarkDispatchRunCreated(task.ID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
	}
	if journal.Stage == employeestore.DispatchRunCreated {
		task, err = s.employees.BindTask(task.ID, sess.ID, runID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
		if err := s.callEmployeeTaskStageHook("task_bound"); err != nil {
			return EmployeeTaskView{}, classified(KindInternal, err)
		}
		journal, err = s.employees.MarkDispatchTaskBound(task.ID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
	}
	if journal.Stage != employeestore.DispatchTaskBound ||
		task.SessionID != sess.ID || task.RunID != runID {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Employee Task execution binding reconciliation is incomplete"))
	}
	if err := s.callEmployeeTaskStageHook("journal_task_bound"); err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	if err = s.employees.CompleteDispatch(task.ID); err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	return s.reconcileBoundEmployeeTask(ctx, task, true)
}

func (s *Service) callEmployeeTaskStageHook(stage string) error {
	if s.employeeTaskStageHook == nil {
		return nil
	}
	return s.employeeTaskStageHook(stage)
}

func (s *Service) ensureEmployeeRunMessage(sessionID, runID, prompt string) error {
	messages, err := s.store.Messages(sessionID)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.RunID == runID && message.Role == "user" {
			if message.Content != prompt {
				return errors.New("persisted Run message does not match Employee Task")
			}
			return nil
		}
	}
	return s.store.AppendMessage(sessionID, session.MessageRecord{RunID: runID, Role: "user", Content: prompt})
}

func validatePreparedExecutionSession(sess *session.Session, task employee.EmployeeTask, journal employeestore.DispatchRecord) error {
	if sess == nil || sess.ID != journal.SessionID || sess.EmployeeID != task.EmployeeID ||
		sess.EmployeeTaskID != task.ID || sess.EmployeeRevision != task.EmployeeRevision ||
		sess.EmployeeTaskSnapshotDigest != task.SnapshotDigest || sess.EmployeeContextSnapshot == nil ||
		sess.EmployeeContextSnapshot.Digest != journal.CompactSnapshotDigest {
		return errors.New("prepared Session identity or digest mismatch")
	}
	if sess.EmployeeContextSnapshot.TaskSnapshotDigest != task.SnapshotDigest ||
		sess.EmployeeContextSnapshot.TaskID != task.ID ||
		sess.EmployeeContextSnapshot.EmployeeID != task.EmployeeID {
		return errors.New("compact Employee context identity mismatch")
	}
	if sess.Workspace == "" || journal.WorkspaceRealPath == "" {
		return errors.New("prepared Session workspace is missing")
	}
	return nil
}

func (s *Service) reconcileBoundDispatch(task employee.EmployeeTask) error {
	journal, err := s.employees.LoadDispatch(task.ID)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, employeestore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return classifyEmployeeStore(err)
	}
	if journal.TaskID != task.ID || journal.EmployeeID != task.EmployeeID ||
		journal.SessionID != task.SessionID || journal.RunID != task.RunID ||
		journal.TaskSnapshotDigest != task.SnapshotDigest {
		return classified(KindInternal, errors.New("bound Task and dispatch journal disagree"))
	}
	if journal.Stage == employeestore.DispatchRunCreated {
		if _, err = s.employees.MarkDispatchTaskBound(task.ID); err != nil {
			return classifyEmployeeStore(err)
		}
	} else if journal.Stage != employeestore.DispatchTaskBound {
		return classified(KindInternal, errors.New("bound Task has an incomplete dispatch stage"))
	}
	if err = s.employees.CompleteDispatch(task.ID); err != nil {
		return classifyEmployeeStore(err)
	}
	return nil
}

func (s *Service) ensureEmployeeRunAvailable(ctx context.Context, requested employee.EmployeeTask) error {
	cursor := ""
	for {
		page, err := s.employees.ListTasks(requested.EmployeeID, employeestore.TaskListOptions{Limit: employeestore.MaxTaskPageSize, Cursor: cursor})
		if err != nil {
			return classifyEmployeeStore(err)
		}
		for _, summary := range page.Tasks {
			if summary.ID == requested.ID {
				continue
			}
			task, getErr := s.employees.GetTask(summary.ID)
			if getErr != nil {
				return classifyEmployeeStore(getErr)
			}
			if task.SessionID == "" {
				continue
			}
			sess, loadErr := s.store.Load(ctx, task.SessionID)
			if loadErr != nil {
				return classified(KindInternal, loadErr)
			}
			run := findRun(sess, task.RunID)
			if run == nil {
				return classified(KindInternal, errors.New("bound Employee Task Run is missing"))
			}
			if run.Status == session.RunQueued || run.Status == session.RunRunning || run.Status == session.RunVerifying {
				return classified(KindConflict, errors.New("Employee already has an executing Task"))
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}

func (s *Service) reconcileBoundEmployeeTask(ctx context.Context, task employee.EmployeeTask, launchQueued bool) (EmployeeTaskView, error) {
	sess, err := s.store.Load(ctx, task.SessionID)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	if sess.EmployeeTaskID != task.ID || sess.EmployeeTaskSnapshotDigest != task.SnapshotDigest ||
		sess.EmployeeID != task.EmployeeID {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Task and Session binding mismatch"))
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Task-bound Run is missing"))
	}
	if run.ID != stableEmployeeRunID(task, sess.ID) {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Task-bound Run identity is not stable"))
	}
	if launchQueued {
		switch run.Status {
		case session.RunCompleted, session.RunFailed, session.RunCancelled:
			return EmployeeTaskView{}, classified(KindConflict, errors.New("terminal Employee Task Run cannot start again"))
		case session.RunInterrupted:
			return EmployeeTaskView{}, classified(KindConflict, errors.New("interrupted Employee Task must use resume"))
		}
	}
	if launchQueued && run.Status == session.RunQueued && !(run.PlanMode == session.PlanReview && !run.PlanApproved) {
		compact, compactErr := contextmgr.EmployeeContextFromCompact(*sess.EmployeeContextSnapshot)
		if compactErr != nil {
			return EmployeeTaskView{}, classified(KindInternal, compactErr)
		}
		if _, launchErr := s.launchSessionRunConfigured(sess, "", &employeeRunLaunch{TaskID: task.ID, Context: compact}); launchErr != nil {
			if !errors.Is(launchErr, errRunActive) {
				return EmployeeTaskView{}, classifiedLaunchError(launchErr)
			}
			s.runMu.Lock()
			sameRun := s.activeSession == task.SessionID && s.activeRun == task.RunID
			s.runMu.Unlock()
			if !sameRun {
				return EmployeeTaskView{}, classifiedLaunchError(launchErr)
			}
		}
		return s.waitForEmployeeRunProjection(ctx, task, EmployeeTaskStatePrepared)
	}
	return s.projectEmployeeTask(ctx, task)
}

func (s *Service) ResumeEmployeeTask(ctx context.Context, taskID string) (EmployeeTaskView, error) {
	s.employeeTaskMu.Lock()
	defer s.employeeTaskMu.Unlock()
	task, err := s.employees.GetTask(taskID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	if task.SessionID == "" {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("Employee Task has no Run to resume"))
	}
	if _, err := s.reconcileBoundEmployeeTask(ctx, task, false); err != nil {
		return EmployeeTaskView{}, err
	}
	if err := s.reconcileBoundDispatch(task); err != nil {
		return EmployeeTaskView{}, err
	}
	record, err := s.employees.Get(task.EmployeeID)
	if err != nil {
		return EmployeeTaskView{}, classifyEmployeeStore(err)
	}
	if record.Employee.State != employee.StateActive {
		return EmployeeTaskView{}, classified(KindConflict, errors.New("disabled or archived Employee cannot resume execution"))
	}
	s.runMu.Lock()
	if s.activeSession == task.SessionID && s.activeRun == task.RunID {
		s.runMu.Unlock()
		return s.waitForEmployeeRunProjection(ctx, task, EmployeeTaskStateInterrupted)
	}
	if s.active.Load() {
		s.runMu.Unlock()
		return EmployeeTaskView{}, classified(KindConflict, errRunActive)
	}
	s.runMu.Unlock()
	sess, err := s.store.Recover(ctx, task.SessionID)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Task-bound Run is missing"))
	}
	if run.Status != session.RunInterrupted {
		view, projectErr := s.projectEmployeeTask(ctx, task)
		if projectErr == nil && (view.State == EmployeeTaskStateRunning || view.State == EmployeeTaskStateVerifying) {
			return view, nil
		}
		return EmployeeTaskView{}, classified(KindConflict, errors.New("Employee Task Run is not interrupted"))
	}
	compact, err := contextmgr.EmployeeContextFromCompact(*sess.EmployeeContextSnapshot)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	if _, err = s.launchSessionRunConfigured(sess, "", &employeeRunLaunch{TaskID: task.ID, Context: compact}); err != nil {
		return EmployeeTaskView{}, classifiedLaunchError(err)
	}
	return s.waitForEmployeeRunProjection(ctx, task, EmployeeTaskStateInterrupted)
}

func (s *Service) waitForEmployeeRunProjection(ctx context.Context, task employee.EmployeeTask, previous employee.TaskState) (EmployeeTaskView, error) {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		view, err := s.projectEmployeeTask(ctx, task)
		if err != nil {
			return EmployeeTaskView{}, err
		}
		if view.State != previous {
			return view, nil
		}
		select {
		case <-ctx.Done():
			return EmployeeTaskView{}, classified(KindInvalid, ctx.Err())
		case <-deadline.C:
			return EmployeeTaskView{}, classified(KindInternal, errors.New("Employee Task Run did not persist its next projection"))
		case <-ticker.C:
		}
	}
}

func (s *Service) cancelBoundEmployeeTask(ctx context.Context, task employee.EmployeeTask) (EmployeeTaskView, error) {
	sess, loadErr := s.store.Load(ctx, task.SessionID)
	if loadErr != nil {
		return EmployeeTaskView{}, classified(KindInternal, loadErr)
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Task-bound Run is missing"))
	}
	if run.Status == session.RunInterrupted ||
		(run.Status == session.RunQueued && (run.PlanMode != session.PlanReview || run.PlanApproved)) {
		if err := s.cancelDormantEmployeeRun(ctx, task); err != nil {
			return EmployeeTaskView{}, err
		}
		return s.projectEmployeeTask(ctx, task)
	}
	_, err := s.CancelRun(ctx, task.SessionID, task.RunID)
	if err != nil {
		var serviceErr *Error
		if !errors.As(err, &serviceErr) || serviceErr.Kind != KindConflict {
			return EmployeeTaskView{}, err
		}
		// A terminal Run makes repeat cancellation idempotent.
		view, projectErr := s.projectEmployeeTask(ctx, task)
		if projectErr != nil {
			return EmployeeTaskView{}, projectErr
		}
		switch view.State {
		case EmployeeTaskStateCompleted, EmployeeTaskStateFailed, EmployeeTaskStateCancelled:
			return view, nil
		default:
			return EmployeeTaskView{}, err
		}
	}
	return s.projectEmployeeTask(ctx, task)
}

func (s *Service) cancelDormantEmployeeRun(ctx context.Context, task employee.EmployeeTask) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.activeSession == task.SessionID && s.activeRun == task.RunID {
		return classified(KindConflict, errors.New("Employee Task Run became active"))
	}
	sess, err := s.store.Load(ctx, task.SessionID)
	if err != nil {
		return classified(KindInternal, err)
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		return classified(KindInternal, errors.New("Task-bound Run is missing"))
	}
	if run.Status == session.RunCancelled {
		return nil
	}
	if run.Status != session.RunInterrupted && run.Status != session.RunQueued {
		return classified(KindConflict, errors.New("Employee Task Run is not dormant"))
	}
	var transition runcontrol.Transition
	if run.Plan != nil {
		transition, err = runcontrol.Cancel(run.Plan, "Employee Task execution stopped by Owner")
		if err != nil {
			return classified(KindInternal, err)
		}
	}
	now := time.Now().UTC()
	run.Status, run.CompletedAt, run.UpdatedAt = session.RunCancelled, &now, now
	sess.ActiveRunID = ""
	events := make([]event.Event, 0, 2)
	if transition.Changed {
		events = append(events, s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, transition.Detail))
	}
	cancelled := event.New(event.TaskCancelled, sess.ID)
	cancelled.RunID, cancelled.Message = run.ID, "Employee Task execution stopped by Owner"
	events = append(events, cancelled)
	events = s.appendApprovalExpiredEvents(sess, runcontrol.ExpireRunApprovals(sess.ApprovalRequests, run.ID, now), events)
	if _, err = s.commitAndPublishMany(sess, events); err != nil {
		return classified(KindInternal, err)
	}
	return nil
}

func (s *Service) projectEmployeeTask(ctx context.Context, task employee.EmployeeTask) (EmployeeTaskView, error) {
	view := projectEmployeeTaskMetadata(task)
	if task.SessionID != "" {
		artifacts, err := s.employees.Artifacts(task.ID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
		view.Artifacts = artifacts
	}
	if task.State == employee.TaskCancelled {
		view.State = EmployeeTaskStateCancelled
		return view, nil
	}
	if task.SessionID == "" {
		if _, err := s.employees.LoadDispatch(task.ID); err == nil {
			view.State = EmployeeTaskStatePrepared
		} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, employeestore.ErrNotFound) {
			view.State = EmployeeTaskStateQueued
		} else {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
		return view, nil
	}
	sess, err := s.store.Load(ctx, task.SessionID)
	if err != nil {
		return EmployeeTaskView{}, classified(KindInternal, err)
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		return EmployeeTaskView{}, classified(KindInternal, errors.New("Task-bound Run is missing"))
	}
	if hasPendingApproval(sess, run.ID) || (run.Status == session.RunQueued && run.PlanMode == session.PlanReview && !run.PlanApproved) {
		view.State = EmployeeTaskStateWaitingOwner
		return view, nil
	}
	switch run.Status {
	case session.RunQueued:
		view.State = EmployeeTaskStatePrepared
	case session.RunRunning:
		view.State = EmployeeTaskStateRunning
	case session.RunVerifying:
		view.State = EmployeeTaskStateVerifying
	case session.RunInterrupted:
		view.State = EmployeeTaskStateInterrupted
	case session.RunCompleted:
		view.State = EmployeeTaskStateCompleted
		if err = s.finalizeEmployeeTaskOutcome(task.ID); err != nil {
			return EmployeeTaskView{}, classified(KindInternal, err)
		}
		view.Artifacts, err = s.employees.Artifacts(task.ID)
		if err != nil {
			return EmployeeTaskView{}, classifyEmployeeStore(err)
		}
	case session.RunFailed:
		view.State = EmployeeTaskStateFailed
	case session.RunCancelled:
		view.State = EmployeeTaskStateCancelled
	default:
		return EmployeeTaskView{}, classified(KindInternal, fmt.Errorf("unsupported Run status %q", run.Status))
	}
	return view, nil
}

func findRun(sess *session.Session, runID string) *session.Run {
	for index := range sess.Runs {
		if sess.Runs[index].ID == runID {
			return &sess.Runs[index]
		}
	}
	return nil
}

func hasPendingApproval(sess *session.Session, runID string) bool {
	for _, request := range sess.ApprovalRequests {
		if request.RunID == runID && request.Status == approval.Pending && !approval.IsExpired(&request, time.Now().UTC()) {
			return true
		}
	}
	return false
}

func sortedModifiedFiles(run *session.Run) []string {
	files := append([]string{}, run.ModifiedFiles...)
	sort.Strings(files)
	return files
}

// finalizeEmployeeTaskOutcome derives bounded, idempotent post-run metadata
// only from an existing successfully verified Run. It never promotes Memory.
func (s *Service) finalizeEmployeeTaskOutcome(taskID string) error {
	task, err := s.employees.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.SessionID == "" || task.RunID == "" {
		return errors.New("cannot finalize an unbound Employee Task")
	}
	sess, err := s.store.Load(context.Background(), task.SessionID)
	if err != nil {
		return err
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		return errors.New("cannot finalize a missing Employee Task Run")
	}
	if run.Status != session.RunCompleted || run.CompletedAt == nil {
		return nil
	}
	verifiedAt := run.CompletedAt.UTC()
	if value := strings.TrimSpace(run.FinalMessage); value != "" {
		value = clipUTF8Bytes(value, employeememory.MaxValueBytes)
		sum := sha256.Sum256([]byte(task.EmployeeID + "\x00" + task.ID + "\x00" + task.SessionID + "\x00" + task.RunID))
		candidate, candidateErr := employeememory.NewCandidate(employeememory.Candidate{
			ID: "candidate-run-" + hex.EncodeToString(sum[:12]), EmployeeID: task.EmployeeID,
			Category: "verified-run", Value: value,
			Provenance: []employeememory.Provenance{{
				SourceType: "run", SourceID: task.RunID, SourceTaskID: task.ID,
				SourceSessionID: task.SessionID, SourceRunID: task.RunID, VerifiedAt: verifiedAt,
			}},
		}, verifiedAt)
		if candidateErr == nil {
			if err = s.employees.AddMemoryCandidate(task.EmployeeID, candidate); err != nil {
				if !errors.Is(err, employeestore.ErrCapacity) {
					return err
				}
			}
		}
	}
	files := sortedModifiedFiles(run)
	if len(files) > employeestore.MaxArtifactsPerTask {
		files = files[:employeestore.MaxArtifactsPerTask]
	}
	artifacts := make([]employeestore.Artifact, 0, len(files))
	for _, relative := range files {
		digest, exists := sess.ModifiedFiles[relative]
		if !exists {
			return fmt.Errorf("verified Artifact %q is absent from Session digest metadata", relative)
		}
		artifact, artifactErr := employeestore.NewArtifact(
			task.EmployeeID, task.ID, task.SessionID, task.RunID, relative, digest, verifiedAt,
		)
		if artifactErr != nil {
			return artifactErr
		}
		artifacts = append(artifacts, artifact)
	}
	return s.employees.PutVerifiedArtifacts(task.ID, artifacts)
}

func clipUTF8Bytes(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
