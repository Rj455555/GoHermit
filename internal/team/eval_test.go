package team_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Rj455555/GoHermit/internal/evals"
	"github.com/Rj455555/GoHermit/internal/team"
)

// evalScriptWorker replays the per-work-item, per-attempt script from a
// verification fixture; unlisted (id, attempt) pairs succeed without checks.
type evalScriptWorker struct {
	mu      sync.Mutex
	scripts map[string]map[int]string
	calls   map[string]int
}

func (w *evalScriptWorker) Execute(_ context.Context, assignment team.Assignment) (team.Result, error) {
	w.mu.Lock()
	w.calls[assignment.WorkItem.ID]++
	script := w.scripts[assignment.WorkItem.ID][assignment.WorkItem.Attempt]
	w.mu.Unlock()
	id, attempt := assignment.WorkItem.ID, assignment.WorkItem.Attempt
	if script == "error" {
		return team.Result{}, fmt.Errorf("scripted failure for %s attempt %d", id, attempt)
	}
	handoff := team.Handoff{ID: fmt.Sprintf("eval-%s-%d", id, attempt), WorkItemID: id, Role: assignment.WorkItem.Role, Summary: fmt.Sprintf("%s attempt %d done", id, attempt)}
	switch script {
	case "checks_pass":
		handoff.Checks = []team.Check{{Command: "go test ./...", Passed: true, Summary: "pass"}}
	case "checks_fail":
		handoff.Checks = []team.Check{{Command: "go test ./...", Passed: false, Summary: "fail"}}
	case "findings_blocking":
		handoff.Findings = []team.Finding{{Severity: team.SeverityBlocking, Summary: "必须在交付前修复"}}
	case "findings_advisory":
		handoff.Findings = []team.Finding{{Severity: team.SeverityAdvisory, Summary: "可选改进"}}
	}
	return team.Result{Handoff: handoff, ModelCalls: 1, Tokens: 10}, nil
}

func TestTeamVerificationFixtures(t *testing.T) {
	fixture, err := evals.LoadFixture[evals.TeamVerificationFixture](filepath.Join("..", "evals", "testdata", "team_verification.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			gradeVerificationScenario(t, scenario)
		})
	}
}

func gradeVerificationScenario(t *testing.T, scenario evals.VerificationScenarioFixture) {
	t.Helper()
	mission, err := team.NewMission("mission-eval", "run-eval", "verification eval", team.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range scenario.WorkItems {
		if err = mission.AddWork(item.Build()); err != nil {
			t.Fatalf("add work item %q: %v", item.ID, err)
		}
	}
	worker := &evalScriptWorker{scripts: map[string]map[int]string{}, calls: map[string]int{}}
	for _, entry := range scenario.Script {
		if worker.scripts[entry.ID] == nil {
			worker.scripts[entry.ID] = map[int]string{}
		}
		worker.scripts[entry.ID][entry.Attempt] = entry.Result
	}
	runErr := (&team.Coordinator{Worker: worker, MaxRepairAttempts: scenario.MaxRepairAttempts}).Run(context.Background(), mission)
	if string(mission.Status) != scenario.Expected.MissionStatus {
		t.Fatalf("mission status=%s want %s (err=%v)", mission.Status, scenario.Expected.MissionStatus, runErr)
	}
	if scenario.Expected.MissionStatus == string(team.Completed) && runErr != nil {
		t.Fatalf("completed mission returned error: %v", runErr)
	}
	if scenario.Expected.MissionStatus != string(team.Completed) && runErr == nil {
		t.Fatal("failed mission returned nil error")
	}
	if len(mission.WorkItems) != len(scenario.Expected.Attempts) {
		t.Fatalf("fixture expects attempts for %d items, mission has %d", len(scenario.Expected.Attempts), len(mission.WorkItems))
	}
	for _, item := range mission.WorkItems {
		want, ok := scenario.Expected.Attempts[item.ID]
		if !ok {
			t.Fatalf("fixture has no expected attempt count for %q", item.ID)
		}
		if item.Attempt != want {
			t.Fatalf("work item %q attempts=%d want %d", item.ID, item.Attempt, want)
		}
	}
	for id, want := range scenario.Expected.Statuses {
		var got team.WorkStatus
		for _, item := range mission.WorkItems {
			if item.ID == id {
				got = item.Status
			}
		}
		if string(got) != want {
			t.Fatalf("work item %q status=%s want %s", id, got, want)
		}
	}
	for id, want := range scenario.Expected.WorkerCalls {
		if worker.calls[id] != want {
			t.Fatalf("worker calls for %q=%d want %d", id, worker.calls[id], want)
		}
	}
	if len(mission.Handoffs) != scenario.Expected.Handoffs {
		t.Fatalf("handoffs=%d want %d", len(mission.Handoffs), scenario.Expected.Handoffs)
	}
	if scenario.Expected.PreservedFailedChecks {
		preserved := false
		for _, handoff := range mission.Handoffs {
			if handoff.Role != team.RoleVerifier {
				continue
			}
			for _, check := range handoff.Checks {
				if !check.Passed {
					preserved = true
				}
			}
		}
		if !preserved {
			t.Fatal("failed verifier handoff was not preserved as audit history")
		}
	}
	if scenario.Expected.MissionStatus != string(team.Completed) {
		for _, item := range scenario.WorkItems {
			if item.Role == string(team.RoleLead) && worker.calls[item.ID] != 0 {
				t.Fatalf("lead %q ran without passing verification", item.ID)
			}
		}
	}
}
