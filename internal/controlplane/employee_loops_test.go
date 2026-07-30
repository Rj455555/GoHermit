package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
)

func employeeLoopTestDefinition(workspace string) loop.Definition {
	value := loopTestDefinition(workspace)
	value.ID = "knowledge-daily"
	value.Name = "私人知识管家"
	value.EmployeeID = "employee-a"
	value.Contract = loop.Contract{
		Goal:       "Archive new owner knowledge with durable provenance.",
		Boundaries: []string{"Never invent facts.", "Never persist credentials."},
		SOP:        []string{"Inspect new inputs.", "Deduplicate.", "Write a cited report."},
	}
	value.Schedule = loop.Schedule{
		Kind: loop.ScheduleDaily, LocalTime: "02:00", Timezone: "Asia/Shanghai",
	}
	value.WorkspacePolicy.ReadOnly = true
	value.WorkspacePolicy.RequireCleanGit = false
	value.ApprovalPolicy.RequireForMutation = false
	return value
}

func TestStartEmployeeOwnedLoopCreatesOneEmployeeTaskAndReusesItsRun(t *testing.T) {
	fixture := newPhase6Fixture(t)
	provider := &phase7Provider{}
	configurePhase7Runtime(t, fixture, provider)
	store, err := loopstore.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(employeeLoopTestDefinition(fixture.workspace)); err != nil {
		t.Fatal(err)
	}
	fixture.service.loopStore = store

	invocation, err := fixture.service.StartLoopInvocation(context.Background(), "knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.EmployeeTaskID == "" || invocation.SessionID == "" || invocation.RunID == "" {
		t.Fatalf("employee loop binding = %+v", invocation)
	}
	task, err := fixture.employees.GetTask(invocation.EmployeeTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.EmployeeID != "employee-a" || task.SessionID != invocation.SessionID || task.RunID != invocation.RunID {
		t.Fatalf("task/invocation mismatch: task=%+v invocation=%+v", task, invocation)
	}
	if len(task.Skills) != 1 || len(task.Knowledge) != 1 || len(task.MemoryFacts) != 1 {
		t.Fatalf("employee loop did not snapshot employee context: %+v", task)
	}
	waitForEmployeeTaskState(t, fixture.service, task.ID, EmployeeTaskStateCompleted)
	projected, err := fixture.service.GetInvocation(context.Background(), invocation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != loop.Completed {
		t.Fatalf("projected invocation = %+v", projected)
	}
}

func TestRunDueLoopsCreatesAtMostOneScheduledInvocationPerDueState(t *testing.T) {
	fixture := newPhase6Fixture(t)
	provider := &phase7Provider{}
	configurePhase7Runtime(t, fixture, provider)
	store, err := loopstore.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(employeeLoopTestDefinition(fixture.workspace)); err != nil {
		t.Fatal(err)
	}
	definition, err := store.GetDefinition("knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	state := loop.NewRuntimeState(definition, due.Add(-24*time.Hour))
	state.NextRunAt = &due
	if err := store.SaveRuntimeState(state); err != nil {
		t.Fatal(err)
	}
	fixture.service.loopStore = store

	first, err := fixture.service.RunDueLoops(context.Background(), due)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.RunDueLoops(context.Background(), due)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 0 || first[0].Trigger != loop.TriggerSchedule {
		t.Fatalf("scheduled results first=%+v second=%+v", first, second)
	}
	waitForEmployeeTaskState(t, fixture.service, first[0].EmployeeTaskID, EmployeeTaskStateCompleted)
	waitForEmployeeTaskIdle(t, fixture.service)
}

func TestRunDueLoopsRealignsChangedDefinitionBeforeDispatch(t *testing.T) {
	fixture := newPhase6Fixture(t)
	store, err := loopstore.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveDefinition(employeeLoopTestDefinition(fixture.workspace)); err != nil {
		t.Fatal(err)
	}
	original, err := store.GetDefinition("knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	state := loop.NewRuntimeState(original, due.Add(-24*time.Hour))
	state.NextRunAt = &due
	if err = store.SaveRuntimeState(state); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Schedule.LocalTime = "23:59"
	if err = store.SaveDefinition(changed); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetDefinition("knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.loopStore = store

	launched, err := fixture.service.RunDueLoops(context.Background(), due)
	if err != nil {
		t.Fatal(err)
	}
	realigned, err := store.GetRuntimeState("knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	if len(launched) != 0 || realigned.DefinitionRevision != current.Revision ||
		realigned.NextRunAt == nil || !realigned.NextRunAt.After(due) {
		t.Fatalf("launched=%+v realigned=%+v current=%+v", launched, realigned, current)
	}
}
