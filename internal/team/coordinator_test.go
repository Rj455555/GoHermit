package team

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type fakeWorker struct {
	calls []string
	fail  string
}

type concurrencyWorker struct {
	activeReaders int32
	maxReaders    int32
	activeWriters int32
	maxWriters    int32
}

func (w *concurrencyWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	active, maximum := &w.activeReaders, &w.maxReaders
	if assignment.WorkItem.MutatesWorkspace {
		active, maximum = &w.activeWriters, &w.maxWriters
	}
	value := atomic.AddInt32(active, 1)
	for {
		old := atomic.LoadInt32(maximum)
		if value <= old || atomic.CompareAndSwapInt32(maximum, old, value) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	atomic.AddInt32(active, -1)
	return Result{Handoff: Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "done"}}, nil
}

func TestCoordinatorParallelReadersAndSingleWriter(t *testing.T) {
	mission, _ := NewMission("mission-parallel", "run", "goal", DefaultBudget())
	for _, item := range []WorkItem{
		{ID: "read-a", Title: "Read A", Goal: "read", Role: RoleExplorer},
		{ID: "read-b", Title: "Read B", Goal: "read", Role: RoleReviewer},
		{ID: "write-a", Title: "Write A", Goal: "write", Role: RoleBuilder, MutatesWorkspace: true},
		{ID: "write-b", Title: "Write B", Goal: "write", Role: RoleBuilder, MutatesWorkspace: true},
	} {
		if err := mission.AddWork(item); err != nil {
			t.Fatal(err)
		}
	}
	worker := &concurrencyWorker{}
	if err := (&Coordinator{Worker: worker, MaxParallel: 4}).Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	if worker.maxReaders < 2 || worker.maxWriters != 1 {
		t.Fatalf("max readers=%d max writers=%d", worker.maxReaders, worker.maxWriters)
	}
}

func (w *fakeWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	w.calls = append(w.calls, assignment.WorkItem.ID)
	if assignment.WorkItem.ID == w.fail {
		return Result{}, fmt.Errorf("failed %s", w.fail)
	}
	handoff := Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "completed " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == RoleReviewer {
		handoff.Findings = []Finding{{Severity: SeverityBlocking, Summary: "scripted blocking finding"}}
	}
	if assignment.WorkItem.Role == RoleVerifier {
		handoff.Checks = []Check{{Command: "go test ./...", Passed: true, Summary: "ok"}}
	}
	return Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

func TestCoordinatorRunsPersonalDevelopmentWorkflow(t *testing.T) {
	mission, err := DefaultMission("mission-1", "run-1", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	worker := &fakeWorker{}
	checkpoints := 0
	coordinator := Coordinator{Worker: worker, Checkpoint: func(*Mission) error { checkpoints++; return nil }}
	if err = coordinator.Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	if mission.Status != Completed || len(mission.Handoffs) != 6 || checkpoints < 7 {
		t.Fatalf("mission=%+v checkpoints=%d", mission, checkpoints)
	}
	want := []string{"explore", "build", "review", "repair", "verify", "lead"}
	for i := range want {
		if worker.calls[i] != want[i] {
			t.Fatalf("calls=%v", worker.calls)
		}
	}
}

func TestCoordinatorStopsOnWorkerFailure(t *testing.T) {
	mission, _ := DefaultMission("mission-1", "run-1", "build it", DefaultBudget())
	worker := &fakeWorker{fail: "review"}
	err := (&Coordinator{Worker: worker}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed {
		t.Fatalf("status=%s err=%v", mission.Status, err)
	}
}

type unverifiedWorker struct{}

func (unverifiedWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	return Result{Handoff: Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "done"}}, nil
}

func TestCoordinatorBlocksLeadWithoutVerifierEvidence(t *testing.T) {
	mission, _ := DefaultMission("mission-1", "run-1", "build it", DefaultBudget())
	err := (&Coordinator{Worker: unverifiedWorker{}}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed || mission.Error != "independent verification did not pass" {
		t.Fatalf("status=%s error=%q err=%v", mission.Status, mission.Error, err)
	}
}

type overBudgetWorker struct{}

func (overBudgetWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	return Result{
		Handoff:    Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "done"},
		ModelCalls: 2,
		Tokens:     200,
	}, nil
}

type parallelFailureWorker struct{}

func (parallelFailureWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	if assignment.WorkItem.ID == "fail" {
		return Result{}, fmt.Errorf("worker failed")
	}
	time.Sleep(30 * time.Millisecond)
	return Result{Handoff: Handoff{ID: "handoff-slow", WorkItemID: "slow", Role: assignment.WorkItem.Role, Summary: "done"}}, nil
}

func TestCoordinatorFailureLeavesNoRunningWork(t *testing.T) {
	mission, _ := NewMission("mission-failure", "run", "goal", DefaultBudget())
	_ = mission.AddWork(WorkItem{ID: "fail", Title: "Fail", Goal: "fail", Role: RoleExplorer})
	_ = mission.AddWork(WorkItem{ID: "slow", Title: "Slow", Goal: "slow", Role: RoleReviewer})
	err := (&Coordinator{Worker: parallelFailureWorker{}, MaxParallel: 2}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed {
		t.Fatalf("status=%s err=%v", mission.Status, err)
	}
	for _, item := range mission.WorkItems {
		if item.Status == WorkRunning {
			t.Fatalf("work item remained running after terminal failure: %+v", item)
		}
	}
}

func TestCoordinatorStopsWhenWorkerResultExceedsBudget(t *testing.T) {
	budget := DefaultBudget()
	budget.MaxModelCalls = 1
	budget.MaxTokens = 100
	mission, _ := NewMission("mission-budget", "run", "goal", budget)
	if err := mission.AddWork(WorkItem{ID: "explore", Title: "Explore", Goal: "inspect", Role: RoleExplorer}); err != nil {
		t.Fatal(err)
	}
	events := make([]TeamEvent, 0)
	err := (&Coordinator{Worker: overBudgetWorker{}, Sink: func(event TeamEvent) { events = append(events, event) }}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed || mission.Error != "mission model budget exceeded" {
		t.Fatalf("status=%s error=%q err=%v", mission.Status, mission.Error, err)
	}
	if len(events) == 0 || events[len(events)-1].Type != MissionFailed {
		t.Fatalf("events=%+v", events)
	}
}

type retryVerifierWorker struct {
	verifyCalls int
	repairCalls int
}

func (w *retryVerifierWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	handoff := Handoff{ID: fmt.Sprintf("handoff-%s-%d", assignment.WorkItem.ID, assignment.WorkItem.Attempt), WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "done"}
	if assignment.WorkItem.ID == "repair" {
		w.repairCalls++
	}
	if assignment.WorkItem.Role == RoleReviewer {
		handoff.Findings = []Finding{{Severity: SeverityBlocking, Summary: "scripted blocking finding"}}
	}
	if assignment.WorkItem.Role == RoleVerifier {
		w.verifyCalls++
		passed := w.verifyCalls > 1
		handoff.Checks = []Check{{Command: "go test ./...", Passed: passed, Summary: map[bool]string{true: "passed", false: "failed"}[passed]}}
	}
	return Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

func TestCoordinatorRepairsAndReverifiesWithinBound(t *testing.T) {
	mission, err := DefaultMission("mission-repair", "run", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	worker := &retryVerifierWorker{}
	if err = (&Coordinator{Worker: worker, MaxRepairAttempts: 3}).Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	if mission.Status != Completed || worker.verifyCalls != 2 || worker.repairCalls != 2 {
		t.Fatalf("mission=%+v verify=%d repair=%d", mission, worker.verifyCalls, worker.repairCalls)
	}
}
