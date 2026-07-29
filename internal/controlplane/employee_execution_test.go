package controlplane

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type phase7Provider struct {
	calls   atomic.Int32
	runs    atomic.Int32
	block   <-chan struct{}
	mu      sync.Mutex
	request model.GenerateRequest
}

func (p *phase7Provider) Generate(ctx context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	p.calls.Add(1)
	compression := len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "Compress the visible coding-session facts")
	if !compression {
		p.runs.Add(1)
		p.mu.Lock()
		p.request = request
		p.mu.Unlock()
	}
	if p.block != nil {
		select {
		case <-ctx.Done():
			return model.GenerateResponse{}, ctx.Err()
		case <-p.block:
		}
	}
	content := "Verified bounded outcome."
	if compression {
		content = `{"summary":"# Current goal\n\nVerified\n\n# Remaining work\n\nNone"}`
	}
	return model.GenerateResponse{
		Message:      model.Message{Role: model.RoleAssistant, Content: content},
		FinishReason: "stop", Attempts: 1,
	}, nil
}

func (*phase7Provider) Capabilities() model.Capabilities {
	return model.Capabilities{ToolCalls: true}
}

func configurePhase7Runtime(t *testing.T, fixture *phase6Fixture, provider model.Provider) {
	t.Helper()
	configuration, err := app.LoadConfig(fixture.workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.approvals = newApprovalBroker()
	ownerStore, err := owner.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.owner = ownerStore
	fixture.service.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
		return &app.Runtime{Workspace: fixture.workspace, Config: configuration}, nil
	}
	fixture.service.buildEmployee = func(
		_ context.Context, _ string, _ string, selection config.RuntimeSelection,
		_ string, _ []config.ModelOption, _ employee.EffectivePolicy,
	) (*app.Runtime, error) {
		manager, managerErr := contextmgr.New(contextmgr.Config{
			MaxTokens: 32_000, CompressionThreshold: .8, HardLimitThreshold: .95,
			ReserveOutputTokens: 2_000,
		})
		if managerErr != nil {
			return nil, managerErr
		}
		return &app.Runtime{
			Workspace: fixture.workspace, Config: configuration,
			Runner: &agent.Runner{
				Provider: provider,
				Executor: tool.Executor{Registry: tool.NewRegistry()},
				Context:  manager, Store: fixture.sessions,
				Config:    agent.Config{MaxTurns: 2, Timeout: 10 * time.Second, Model: selection.Model},
				Approvals: fixture.service.approvals,
			},
		}, nil
	}
}

func TestEmployeeTaskConcurrentStartCreatesAndStartsOneStableRun(t *testing.T) {
	fixture := newPhase6Fixture(t)
	release := make(chan struct{})
	provider := &phase7Provider{block: release}
	configurePhase7Runtime(t, fixture, provider)
	knowledgeState, err := fixture.employees.Knowledge("employee-a")
	if err != nil {
		t.Fatal(err)
	}
	secondTask, err := fixture.service.CreateEmployeeTask(context.Background(), "employee-a", EmployeeTaskCreateInput{
		Prompt: "Second queued Task.",
		Skills: []EmployeeTaskSkillSelection{{SkillID: "review", Version: "1.0.0"}},
		Knowledge: []EmployeeTaskKnowledgeSelection{{
			SourceID: fixture.sourceID, CitationIDs: []string{knowledgeState.Indexes[0].Documents[0].Citations[0].ID},
		}},
		MemoryFactIDs: []string{fixture.memoryID}, ProjectBindingID: "project-a",
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget:              employee.BudgetPolicy{MaxModelCalls: 2, MaxTokens: 20_000, TimeoutSeconds: 900},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var wait sync.WaitGroup
	wait.Add(workers)
	results := make(chan EmployeeTaskView, workers)
	failures := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			result, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatalf("concurrent Start failed: %v", err)
	}
	var sessionID, runID string
	for result := range results {
		if result.State != EmployeeTaskStateRunning || result.SessionID == "" || result.RunID == "" {
			t.Fatalf("Start projection = %#v", result)
		}
		if sessionID == "" {
			sessionID, runID = result.SessionID, result.RunID
		}
		if result.SessionID != sessionID || result.RunID != runID {
			t.Fatalf("Start returned multiple bindings: %#v", result)
		}
	}
	loaded, err := fixture.sessions.Load(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Runs) != 1 || loaded.Runs[0].ID != runID {
		t.Fatalf("at-most-once Runs = %#v", loaded.Runs)
	}
	task, err := fixture.employees.GetTask(fixture.taskID)
	if err != nil || task.SessionID != sessionID || task.RunID != runID ||
		task.SnapshotDigest != fixture.taskDigest {
		t.Fatalf("Task binding = %#v, %v", task, err)
	}
	if _, err = fixture.employees.LoadDispatch(fixture.taskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed dispatch journal = %v, want not exist", err)
	}
	if _, err = fixture.service.StartEmployeeTask(context.Background(), secondTask.ID); serviceErrorKind(err) != KindConflict {
		t.Fatalf("per-Employee/workspace concurrency gate = %v", err)
	}
	close(release)
	waitForEmployeeTaskState(t, fixture.service, fixture.taskID, EmployeeTaskStateCompleted)
	if provider.runs.Load() != 1 {
		t.Fatalf("execution model calls = %d, want 1", provider.runs.Load())
	}
}

func TestEmployeeTaskStartReconcilesBindingCrashPoints(t *testing.T) {
	for _, crash := range []string{"run_saved", "task_bound", "journal_task_bound"} {
		t.Run(crash, func(t *testing.T) {
			fixture := newPhase6Fixture(t)
			provider := &phase7Provider{}
			configurePhase7Runtime(t, fixture, provider)
			failed := false
			fixture.service.employeeTaskStageHook = func(stage string) error {
				if stage == crash && !failed {
					failed = true
					return errors.New("simulated Phase 7 crash")
				}
				return nil
			}
			if _, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID); err == nil {
				t.Fatal("expected simulated crash")
			}
			fixture.service.employeeTaskStageHook = nil
			result, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID)
			if err != nil {
				t.Fatal(err)
			}
			if result.SessionID == "" || result.RunID == "" {
				t.Fatalf("reconciled binding = %#v", result)
			}
			waitForEmployeeTaskState(t, fixture.service, fixture.taskID, EmployeeTaskStateCompleted)
			waitForEmployeeTaskIdle(t, fixture.service)
			loaded, err := fixture.sessions.Load(context.Background(), result.SessionID)
			if err != nil || len(loaded.Runs) != 1 || loaded.Runs[0].ID != result.RunID {
				t.Fatalf("reconciled Runs = %#v, %v", loaded.Runs, err)
			}
			if provider.runs.Load() != 1 {
				t.Fatalf("reconciled execution model calls = %d", provider.runs.Load())
			}
		})
	}
}

func TestEmployeeTaskVerifiedOutcomeCreatesCandidateWithoutPromotion(t *testing.T) {
	fixture := newPhase6Fixture(t)
	provider := &phase7Provider{}
	configurePhase7Runtime(t, fixture, provider)
	factsBefore, err := fixture.employees.Memory("employee-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.service.StartEmployeeTask(context.Background(), fixture.taskID); err != nil {
		t.Fatal(err)
	}
	completed := waitForEmployeeTaskState(t, fixture.service, fixture.taskID, EmployeeTaskStateCompleted)
	candidates, err := fixture.employees.MemoryCandidates("employee-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Provenance[0].SourceTaskID != fixture.taskID ||
		candidates[0].Provenance[0].SourceSessionID != completed.SessionID ||
		candidates[0].Provenance[0].SourceRunID != completed.RunID {
		t.Fatalf("verified Candidate = %#v", candidates)
	}
	factsAfter, err := fixture.employees.Memory("employee-a")
	if err != nil || !reflect.DeepEqual(factsAfter, factsBefore) {
		t.Fatalf("Candidate was automatically accepted: %#v / %#v, %v", factsBefore, factsAfter, err)
	}
	if len(completed.Artifacts) != 0 {
		t.Fatalf("no-mutation Run produced Artifacts: %#v", completed.Artifacts)
	}
	again, err := fixture.service.GetEmployeeTask(context.Background(), fixture.taskID)
	if err != nil || again.State != EmployeeTaskStateCompleted {
		t.Fatalf("completed projection = %#v, %v", again, err)
	}
	candidatesAgain, _ := fixture.employees.MemoryCandidates("employee-a")
	if len(candidatesAgain) != 1 {
		t.Fatalf("terminal reconciliation duplicated Candidate: %#v", candidatesAgain)
	}
	provider.mu.Lock()
	request := provider.request
	provider.mu.Unlock()
	joined := ""
	for _, message := range request.Messages {
		joined += message.Content + "\n"
	}
	for _, expected := range []string{"Employee identity", "Pinned Skill", "Knowledge reference", "Private Employee Memory"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("sealed compact context omitted %q", expected)
		}
	}
}

func TestEmployeeTaskPreparedAndWaitingOwnerCancellationIsIdempotent(t *testing.T) {
	t.Run("prepared", func(t *testing.T) {
		fixture := newPhase6Fixture(t)
		preparation, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
		if err != nil {
			t.Fatal(err)
		}
		first, err := fixture.service.CancelEmployeeTask(context.Background(), fixture.taskID)
		if err != nil || first.State != EmployeeTaskStateCancelled {
			t.Fatalf("prepared cancel = %#v, %v", first, err)
		}
		second, err := fixture.service.CancelEmployeeTask(context.Background(), fixture.taskID)
		if err != nil || second.State != EmployeeTaskStateCancelled {
			t.Fatalf("repeat prepared cancel = %#v, %v", second, err)
		}
		prepared, err := fixture.sessions.Load(context.Background(), preparation.SessionID)
		if err != nil || len(prepared.Runs) != 0 {
			t.Fatalf("prepared cancellation created execution: %#v, %v", prepared, err)
		}
	})

	t.Run("waiting-owner", func(t *testing.T) {
		fixture := newPhase6Fixture(t)
		provider := &phase7Provider{}
		configurePhase7Runtime(t, fixture, provider)
		prepared, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
		if err != nil {
			t.Fatal(err)
		}
		sess, err := fixture.sessions.Load(context.Background(), prepared.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		sess.PlanMode = session.PlanReview
		if err = fixture.sessions.Save(context.Background(), sess); err != nil {
			t.Fatal(err)
		}
		started, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID)
		if err != nil || started.State != EmployeeTaskStateWaitingOwner {
			t.Fatalf("waiting-owner Start = %#v, %v", started, err)
		}
		cancelled, err := fixture.service.CancelEmployeeTask(context.Background(), fixture.taskID)
		if err != nil || cancelled.State != EmployeeTaskStateCancelled {
			t.Fatalf("waiting-owner cancel = %#v, %v", cancelled, err)
		}
		again, err := fixture.service.CancelEmployeeTask(context.Background(), fixture.taskID)
		if err != nil || again.State != EmployeeTaskStateCancelled || provider.runs.Load() != 0 {
			t.Fatalf("repeat waiting-owner cancel = %#v, %v; calls=%d", again, err, provider.runs.Load())
		}
	})
}

func TestEmployeeTaskInterruptedResumeUsesOriginalRun(t *testing.T) {
	fixture := newPhase6Fixture(t)
	release := make(chan struct{})
	provider := &phase7Provider{block: release}
	configurePhase7Runtime(t, fixture, provider)
	fixture.service.employeeTaskStageHook = func(stage string) error {
		if stage == "journal_task_bound" {
			return errors.New("simulated exit before launch")
		}
		return nil
	}
	if _, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("expected pre-launch interruption")
	}
	fixture.service.employeeTaskStageHook = nil
	task, err := fixture.employees.GetTask(fixture.taskID)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := fixture.sessions.Load(context.Background(), task.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	run := findRun(sess, task.RunID)
	if run == nil {
		t.Fatal("bound Run is missing")
	}
	run.Status = session.RunInterrupted
	if err = fixture.sessions.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	resumed, err := fixture.service.ResumeEmployeeTask(context.Background(), task.ID)
	if err != nil || resumed.RunID != task.RunID || resumed.State != EmployeeTaskStateRunning {
		t.Fatalf("resume = %#v, %v", resumed, err)
	}
	repeated, err := fixture.service.ResumeEmployeeTask(context.Background(), task.ID)
	if err != nil || repeated.RunID != task.RunID || repeated.State != EmployeeTaskStateRunning {
		t.Fatalf("repeat resume = %#v, %v", repeated, err)
	}
	close(release)
	waitForEmployeeTaskState(t, fixture.service, task.ID, EmployeeTaskStateCompleted)
	loaded, err := fixture.sessions.Load(context.Background(), task.SessionID)
	if err != nil || len(loaded.Runs) != 1 || loaded.Runs[0].ID != task.RunID || provider.runs.Load() != 1 {
		t.Fatalf("resumed Run = %#v, %v; execution calls=%d", loaded.Runs, err, provider.runs.Load())
	}
}

func TestEmployeeTaskVerificationFailureCreatesNoCandidateOrArtifact(t *testing.T) {
	fixture := newPhase6Fixture(t)
	provider := &phase7Provider{}
	configurePhase7Runtime(t, fixture, provider)
	fixture.service.employeeTaskStageHook = func(stage string) error {
		if stage == "journal_task_bound" {
			return errors.New("pause before launch")
		}
		return nil
	}
	if _, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("expected pause")
	}
	fixture.service.employeeTaskStageHook = nil
	task, _ := fixture.employees.GetTask(fixture.taskID)
	sess, _ := fixture.sessions.Load(context.Background(), task.SessionID)
	run := findRun(sess, task.RunID)
	run.LastMutationTurn = 1
	run.ModifiedFiles = []string{"code.go"}
	sess.ModifiedFiles["code.go"] = strings.Repeat("a", 64)
	if err := fixture.sessions.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.StartEmployeeTask(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	waitForEmployeeTaskState(t, fixture.service, task.ID, EmployeeTaskStateFailed)
	candidates, err := fixture.employees.MemoryCandidates(task.EmployeeID)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := fixture.employees.Artifacts(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 || len(artifacts) != 0 {
		t.Fatalf("failed verification produced Candidate/Artifact: %#v / %#v", candidates, artifacts)
	}
}

func TestEmployeeTaskStartRejectsDisabledAndArchivedEmployeeWithoutDispatch(t *testing.T) {
	for _, state := range []employee.State{employee.StateDisabled, employee.StateArchived} {
		t.Run(string(state), func(t *testing.T) {
			fixture := newPhase6Fixture(t)
			record, err := fixture.employees.Get("employee-a")
			if err != nil {
				t.Fatal(err)
			}
			if state == employee.StateDisabled {
				_, err = fixture.employees.Disable("employee-a", record.Employee.Revision)
			} else {
				_, err = fixture.employees.Archive("employee-a", record.Employee.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = fixture.service.StartEmployeeTask(context.Background(), fixture.taskID); serviceErrorKind(err) != KindConflict {
				t.Fatalf("Start gate = %v", err)
			}
			if ids, listErr := fixture.sessions.List(); listErr != nil || len(ids) != 0 {
				t.Fatalf("disabled/archived Start created Session: %#v, %v", ids, listErr)
			}
			if _, err = fixture.employees.LoadDispatch(fixture.taskID); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("disabled/archived Start created dispatch: %v", err)
			}
			if view, getErr := fixture.service.GetEmployeeTask(context.Background(), fixture.taskID); getErr != nil || view.State != EmployeeTaskStateQueued {
				t.Fatalf("historical Task is not readable: %#v, %v", view, getErr)
			}
		})
	}
}

func TestEmployeeTaskBindingMismatchFailsClosedWithoutExecution(t *testing.T) {
	fixture := newPhase6Fixture(t)
	provider := &phase7Provider{}
	configurePhase7Runtime(t, fixture, provider)
	fixture.service.employeeTaskStageHook = func(stage string) error {
		if stage == "journal_task_bound" {
			return errors.New("pause before launch")
		}
		return nil
	}
	if _, err := fixture.service.StartEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("expected pause")
	}
	fixture.service.employeeTaskStageHook = nil
	task, _ := fixture.employees.GetTask(fixture.taskID)
	sess, _ := fixture.sessions.Load(context.Background(), task.SessionID)
	run := findRun(sess, task.RunID)
	run.ID = "run-mismatched"
	sess.ActiveRunID = run.ID
	if err := fixture.sessions.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.StartEmployeeTask(context.Background(), task.ID); serviceErrorKind(err) != KindInternal {
		t.Fatalf("Run mismatch error = %v", err)
	}
	if provider.runs.Load() != 0 || fixture.service.Active() {
		t.Fatalf("mismatch started execution: calls=%d active=%t", provider.runs.Load(), fixture.service.Active())
	}
}

func waitForEmployeeTaskState(t *testing.T, service *Service, taskID string, expected employee.TaskState) EmployeeTaskView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, err := service.GetEmployeeTask(context.Background(), taskID)
		if err == nil && view.State == expected {
			return view
		}
		time.Sleep(10 * time.Millisecond)
	}
	view, err := service.GetEmployeeTask(context.Background(), taskID)
	t.Fatalf("Task did not reach %s: %#v, %v", expected, view, err)
	return EmployeeTaskView{}
}

func waitForEmployeeTaskIdle(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !service.Active() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Employee Task runner did not release the service gate")
}
