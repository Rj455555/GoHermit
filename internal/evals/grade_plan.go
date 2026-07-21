package evals

import (
	"fmt"
	"testing"

	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
)

// GradeTransitionScript replays a plan op script, asserting the plan stays
// valid after every op, the revision increments exactly when state changed,
// and rejected ops leave every step untouched.
func GradeTransitionScript(t *testing.T, script TransitionScriptFixture) {
	t.Helper()
	plan := newFixturePlan(t, script.Steps, script.AllowParallel)
	for i, op := range script.Ops {
		before := plan.Revision
		statuses := stepStatuses(plan)
		changed, err := applyPlanOp(plan, op)
		label := fmt.Sprintf("op %d (%s %s)", i, op.Op, op.ID)
		if op.ExpectOK && err != nil {
			t.Fatalf("%s unexpectedly failed: %v", label, err)
		}
		if !op.ExpectOK {
			if err == nil {
				t.Fatalf("%s unexpectedly succeeded", label)
			}
			if changed {
				t.Fatalf("%s was rejected but reported a change", label)
			}
			for id, status := range statuses {
				if step := findStep(plan, id); step == nil || step.Status != status {
					t.Fatalf("%s was rejected but mutated step %q", label, id)
				}
			}
		}
		if changed != op.ExpectChanged {
			t.Fatalf("%s changed=%v want %v", label, changed, op.ExpectChanged)
		}
		if err = taskplan.Validate(plan); err != nil {
			t.Fatalf("%s left the plan invalid: %v", label, err)
		}
		wantRevision := before
		if changed {
			wantRevision++
		}
		if plan.Revision != wantRevision {
			t.Fatalf("%s revision=%d want %d", label, plan.Revision, wantRevision)
		}
	}
	assertPlanState(t, plan, script.Expected)
}

// GradeTeamEventScript replays team events through runcontrol.ApplyTeamEvent
// against a mission snapshot and asserts every transition and the final plan.
func GradeTeamEventScript(t *testing.T, script TeamEventScriptFixture) {
	t.Helper()
	plan := newFixturePlan(t, script.Steps, script.AllowParallel)
	mission := script.Mission.Build()
	if len(script.Events) != len(script.Expected.Transitions) {
		t.Fatalf("fixture lists %d events but %d expected transitions", len(script.Events), len(script.Expected.Transitions))
	}
	for i, fixtureEvent := range script.Events {
		runtimeEvent := team.TeamEvent{Type: team.TeamEventType(fixtureEvent.Type), MissionID: mission.ID, WorkItemID: fixtureEvent.WorkItemID, Role: team.Role(fixtureEvent.Role), Message: fixtureEvent.Message}
		transition, err := runcontrol.ApplyTeamEvent(plan, runtimeEvent, mission)
		if err != nil {
			t.Fatalf("event %d (%s %s) failed: %v", i, fixtureEvent.Type, fixtureEvent.WorkItemID, err)
		}
		want := script.Expected.Transitions[i]
		if transition.Changed != want.Changed || transition.StepID != want.StepID {
			t.Fatalf("event %d (%s %s) transition=%+v want %+v", i, fixtureEvent.Type, fixtureEvent.WorkItemID, transition, want)
		}
		if err = taskplan.Validate(plan); err != nil {
			t.Fatalf("event %d (%s %s) left the plan invalid: %v", i, fixtureEvent.Type, fixtureEvent.WorkItemID, err)
		}
	}
	assertPlanState(t, plan, script.Expected.Final)
}

func newFixturePlan(t *testing.T, steps []StepSpecFixture, allowParallel bool) *taskplan.Plan {
	t.Helper()
	specs := make([]taskplan.StepSpec, 0, len(steps))
	for _, step := range steps {
		specs = append(specs, taskplan.StepSpec{ID: step.ID, Title: step.Title})
	}
	var (
		plan *taskplan.Plan
		err  error
	)
	if allowParallel {
		plan, err = taskplan.NewParallel("plan-eval", specs)
	} else {
		plan, err = taskplan.New("plan-eval", specs)
	}
	if err != nil {
		t.Fatalf("build fixture plan: %v", err)
	}
	return plan
}

func applyPlanOp(plan *taskplan.Plan, op PlanOpFixture) (bool, error) {
	switch op.Op {
	case "start":
		return plan.Start(op.ID, op.Detail)
	case "complete":
		return plan.Complete(op.ID, op.Detail)
	case "fail":
		return plan.Fail(op.ID, op.Detail)
	case "note":
		return plan.Note(op.ID, op.Detail)
	case "reopen":
		return plan.Reopen(op.IDs, op.Detail)
	case "cancel":
		changed, _ := plan.Cancel(op.Detail)
		return changed, nil
	default:
		return false, fmt.Errorf("unknown plan op %q", op.Op)
	}
}

func stepStatuses(plan *taskplan.Plan) map[string]taskplan.StepStatus {
	statuses := make(map[string]taskplan.StepStatus, len(plan.Steps))
	for _, step := range plan.Steps {
		statuses[step.ID] = step.Status
	}
	return statuses
}

func findStep(plan *taskplan.Plan, id string) *taskplan.Step {
	for i := range plan.Steps {
		if plan.Steps[i].ID == id {
			return &plan.Steps[i]
		}
	}
	return nil
}

func assertPlanState(t *testing.T, plan *taskplan.Plan, want PlanStateFixture) {
	t.Helper()
	if string(plan.Status) != want.Status {
		t.Fatalf("plan status=%s want %s", plan.Status, want.Status)
	}
	if plan.Revision != want.Revision {
		t.Fatalf("plan revision=%d want %d", plan.Revision, want.Revision)
	}
	if len(plan.Steps) != len(want.Steps) {
		t.Fatalf("plan has %d steps, fixture expects %d", len(plan.Steps), len(want.Steps))
	}
	for _, step := range plan.Steps {
		wantStatus, ok := want.Steps[step.ID]
		if !ok {
			t.Fatalf("fixture has no expected status for step %q", step.ID)
		}
		if string(step.Status) != wantStatus {
			t.Fatalf("step %q status=%s want %s", step.ID, step.Status, wantStatus)
		}
	}
}
