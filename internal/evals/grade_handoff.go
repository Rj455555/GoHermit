package evals

import (
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/team"
)

// GradeHandoffScenario drives Mission.Complete with a candidate handoff and
// asserts the accept/reject verdict. Rejections must leave the work item
// running with no handoff attached and no handoff recorded on the mission.
func GradeHandoffScenario(t *testing.T, scenario HandoffScenarioFixture) {
	t.Helper()
	mission, err := team.NewMission("mission-eval", "run-eval", "grade handoff quality", team.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	item := scenario.WorkItem.Build()
	if err = mission.AddWork(item); err != nil {
		t.Fatalf("add work item: %v", err)
	}
	if err = mission.Start(item.ID); err != nil {
		t.Fatalf("start work item: %v", err)
	}
	err = mission.Complete(item.ID, scenario.Handoff.Build())
	if scenario.ExpectAccept {
		if err != nil {
			t.Fatalf("handoff unexpectedly rejected: %v", err)
		}
		got := mission.WorkItems[0]
		if got.Status != team.WorkCompleted || got.HandoffID == "" || len(mission.Handoffs) != 1 {
			t.Fatalf("accepted handoff not recorded: item=%+v handoffs=%d", got, len(mission.Handoffs))
		}
		return
	}
	if err == nil {
		t.Fatal("handoff unexpectedly accepted")
	}
	if scenario.ErrorContains != "" && !strings.Contains(err.Error(), scenario.ErrorContains) {
		t.Fatalf("error %q does not contain %q", err.Error(), scenario.ErrorContains)
	}
	got := mission.WorkItems[0]
	if got.Status != team.WorkRunning || got.HandoffID != "" || len(mission.Handoffs) != 0 {
		t.Fatalf("rejected handoff polluted mission state: item=%+v handoffs=%d", got, len(mission.Handoffs))
	}
}
