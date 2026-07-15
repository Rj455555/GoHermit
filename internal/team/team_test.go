package team

import (
	"testing"
)

func TestMissionDependencyAndWriterRules(t *testing.T) {
	m, err := NewMission("mission-1", "run-1", "build the feature", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	items := []WorkItem{
		{ID: "explore", Title: "Explore", Goal: "inspect", Role: RoleExplorer},
		{ID: "build", Title: "Build", Goal: "implement", Role: RoleBuilder, DependsOn: []string{"explore"}, MutatesWorkspace: true},
		{ID: "review", Title: "Review", Goal: "review", Role: RoleReviewer, DependsOn: []string{"build"}},
	}
	for _, item := range items {
		if err = m.AddWork(item); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.Ready(); len(got) != 1 || got[0] != "explore" {
		t.Fatalf("ready=%v", got)
	}
	if err = m.Start("build"); err == nil {
		t.Fatal("expected dependency failure")
	}
	if err = m.Start("explore"); err != nil {
		t.Fatal(err)
	}
	if err = m.Complete("explore", Handoff{ID: "handoff-1", WorkItemID: "explore", Role: RoleExplorer, Summary: "inspected"}); err != nil {
		t.Fatal(err)
	}
	if err = m.Start("build"); err != nil {
		t.Fatal(err)
	}
}

func TestMissionRequiresStructuredHandoff(t *testing.T) {
	m, _ := NewMission("mission-1", "run-1", "goal", DefaultBudget())
	if err := m.AddWork(WorkItem{ID: "review", Title: "Review", Goal: "review", Role: RoleReviewer}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("review"); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("review", Handoff{ID: "bad", WorkItemID: "review", Role: RoleBuilder, Summary: "wrong role"}); err == nil {
		t.Fatal("expected handoff validation failure")
	}
	if err := m.Complete("review", Handoff{ID: "ok", WorkItemID: "review", Role: RoleReviewer, Summary: "approved"}); err != nil {
		t.Fatal(err)
	}
	if m.Status != Completed {
		t.Fatalf("status=%s", m.Status)
	}
}

func TestCancelIsTerminalAndInterruptIsResumable(t *testing.T) {
	interrupted, _ := NewMission("interrupt", "run", "goal", DefaultBudget())
	_ = interrupted.AddWork(WorkItem{ID: "work", Title: "Work", Goal: "work", Role: RoleBuilder, MutatesWorkspace: true})
	_ = interrupted.Start("work")
	interrupted.Interrupt("restart")
	if interrupted.Status != Interrupted || len(interrupted.Ready()) != 1 {
		t.Fatalf("interrupted mission should be resumable: %+v", interrupted)
	}

	cancelled, _ := NewMission("cancel", "run", "goal", DefaultBudget())
	_ = cancelled.AddWork(WorkItem{ID: "work", Title: "Work", Goal: "work", Role: RoleBuilder, MutatesWorkspace: true})
	_ = cancelled.Start("work")
	cancelled.Cancel("owner stopped")
	if cancelled.Status != Cancelled || cancelled.WorkItems[0].Status != WorkCancelled || len(cancelled.Ready()) != 0 {
		t.Fatalf("cancelled mission should be terminal: %+v", cancelled)
	}
}
