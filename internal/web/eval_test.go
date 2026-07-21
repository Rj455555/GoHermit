package web

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/evals"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
)

// evalHandoffWorker returns the fixture handoff recorded for each work item.
type evalHandoffWorker struct {
	handoffs map[string]team.Handoff
}

func (w evalHandoffWorker) Execute(_ context.Context, assignment team.Assignment) (team.Result, error) {
	handoff, ok := w.handoffs[assignment.WorkItem.ID]
	if !ok {
		return team.Result{}, fmt.Errorf("no fixture handoff for %s", assignment.WorkItem.ID)
	}
	return team.Result{Handoff: handoff, ModelCalls: 1, Tokens: 10}, nil
}

func TestOwnerSummaryFixtures(t *testing.T) {
	fixture, err := evals.LoadFixture[evals.OwnerSummaryFixture](filepath.Join("..", "evals", "testdata", "owner_summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			gradeOwnerSummary(t, scenario)
			if scenario.Run {
				gradeOwnerSummaryRun(t, scenario)
			}
		})
	}
}

func gradeOwnerSummary(t *testing.T, scenario evals.OwnerSummaryScenarioFixture) {
	t.Helper()
	mission := &team.Mission{ID: "mission-eval", RunID: "run-eval"}
	for _, handoff := range scenario.Handoffs {
		mission.Handoffs = append(mission.Handoffs, handoff.Build())
	}
	if got := finalTeamHandoff(mission).Summary; got != scenario.Expected.FinalSummary {
		t.Fatalf("final summary=%q want %q", got, scenario.Expected.FinalSummary)
	}
	assertStringSlicesEqual(t, "modified files", missionModifiedFiles(mission), scenario.Expected.ModifiedFiles)
}

// gradeOwnerSummaryRun drives a full team run with the fixture handoffs and
// asserts the owner-facing summary, aggregated checks, deduped file list, and
// that the durable completion event carries no prompt or unbounded content.
func gradeOwnerSummaryRun(t *testing.T, scenario evals.OwnerSummaryScenarioFixture) {
	t.Helper()
	server := testServer(t)
	handoffs := make(map[string]team.Handoff, len(scenario.Handoffs))
	for _, fixtureHandoff := range scenario.Handoffs {
		handoffs[fixtureHandoff.WorkItemID] = fixtureHandoff.Build()
	}
	server.teamWorker = evalHandoffWorker{handoffs: handoffs}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Owner summary eval", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	const promptMarker = "PROMPT-MARKER-9f3e"
	run, err := sess.NewRun("build the owner summary eval widget " + promptMarker)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget()); err != nil {
		t.Fatal(err)
	}
	if run.Plan, err = taskplan.DefaultTeam(run.ID); err != nil {
		t.Fatal(err)
	}
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	server.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)
	loaded, err := server.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Runs) != 1 {
		t.Fatalf("runs=%d want 1", len(loaded.Runs))
	}
	gotRun := loaded.Runs[0]
	if gotRun.Status != session.RunCompleted {
		t.Fatalf("run status=%s want completed", gotRun.Status)
	}
	if gotRun.FinalMessage != scenario.Expected.FinalSummary {
		t.Fatalf("final message=%q want %q", gotRun.FinalMessage, scenario.Expected.FinalSummary)
	}
	assertStringSlicesEqual(t, "run modified files", gotRun.ModifiedFiles, scenario.Expected.ModifiedFiles)
	if len(loaded.TestResults) != len(scenario.Expected.Checks) {
		t.Fatalf("test results=%d want %d", len(loaded.TestResults), len(scenario.Expected.Checks))
	}
	for i, want := range scenario.Expected.Checks {
		got := loaded.TestResults[i]
		if got.Command != want.Command || got.Passed != want.Passed || got.Summary != want.Summary {
			t.Fatalf("test result %d=%+v want %+v", i, got, want)
		}
	}
	events, err := server.store.Events(sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var completed *event.Event
	for i := range events {
		if events[i].Type == event.TaskCompleted {
			completed = &events[i]
		}
	}
	if completed == nil {
		t.Fatal("no durable task completion event")
	}
	if completed.Message != scenario.Expected.FinalSummary {
		t.Fatalf("completion event message=%q want %q", completed.Message, scenario.Expected.FinalSummary)
	}
	if strings.Contains(completed.Message, promptMarker) || strings.Contains(string(completed.Data), promptMarker) {
		t.Fatal("completion event payload leaks prompt content")
	}
	if len(completed.Message) > team.MaxTextBytes {
		t.Fatalf("completion event message is unbounded: %d bytes", len(completed.Message))
	}
}

func assertStringSlicesEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s=%v want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s=%v want %v", label, got, want)
		}
	}
}
