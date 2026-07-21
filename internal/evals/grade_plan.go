package evals

import (
	"fmt"
	"strings"
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

// GradeSubstepProposalScript applies an Explorer substep proposal to a mission
// snapshot and, for accepted proposals, extends the Live Plan. Accepted
// substeps must become read-only queued work items wired into the queued lead
// and map 1:1 onto new pending plan steps with identical IDs; pre-existing
// steps, including completed history, stay untouched and the plan revision
// increments exactly once. Rejections must leave mission and plan unchanged.
func GradeSubstepProposalScript(t *testing.T, script SubstepProposalScriptFixture) {
	t.Helper()
	mission := script.Mission.Build()
	preExisting := make(map[string]team.WorkStatus, len(mission.WorkItems))
	leadID := ""
	for _, item := range mission.WorkItems {
		preExisting[item.ID] = item.Status
		if item.Role == team.RoleLead {
			leadID = item.ID
		}
	}
	specs := make([]taskplan.StepSpec, 0, len(script.PlanSteps))
	for _, step := range script.PlanSteps {
		specs = append(specs, taskplan.StepSpec{ID: step.ID, Title: step.Title})
	}
	plan, err := taskplan.New("plan-eval", specs)
	if err != nil {
		t.Fatalf("build fixture plan: %v", err)
	}
	for _, step := range script.PlanSteps {
		if step.Status == string(taskplan.StepDone) {
			if _, err = plan.Complete(step.ID, "done"); err != nil {
				t.Fatalf("complete pre-existing step %q: %v", step.ID, err)
			}
		}
	}
	priorStatus := stepStatuses(plan)
	revisionBefore := plan.Revision
	proposal := make([]team.SubstepSpec, 0, len(script.Proposal))
	for _, spec := range script.Proposal {
		proposal = append(proposal, spec.Build())
	}
	err = mission.AddSubsteps(proposal)
	if !script.Expected.Accept {
		if err == nil {
			t.Fatal("proposal unexpectedly accepted")
		}
		if script.Expected.ErrorContains != "" && !strings.Contains(err.Error(), script.Expected.ErrorContains) {
			t.Fatalf("error %q does not contain %q", err.Error(), script.Expected.ErrorContains)
		}
		if len(mission.WorkItems) != len(preExisting) {
			t.Fatalf("rejected proposal changed work items from %d to %d", len(preExisting), len(mission.WorkItems))
		}
		for id, status := range preExisting {
			if item := findWorkItem(mission, id); item == nil || item.Status != status {
				t.Fatalf("rejected proposal mutated work item %q", id)
			}
		}
		if plan.Revision != revisionBefore || len(plan.Steps) != len(script.PlanSteps) {
			t.Fatalf("rejected proposal touched the plan: %+v", plan)
		}
		return
	}
	if err != nil {
		t.Fatalf("proposal unexpectedly rejected: %v", err)
	}
	for _, spec := range proposal {
		item := findWorkItem(mission, spec.ID)
		if item == nil || item.MutatesWorkspace || item.Status != team.WorkQueued || item.Role != spec.Role {
			t.Fatalf("accepted substep %q is not a read-only queued work item: %+v", spec.ID, item)
		}
		if strings.Join(item.DependsOn, ",") != strings.Join(spec.DependsOn, ",") {
			t.Fatalf("substep %q depends_on=%v want %v", spec.ID, item.DependsOn, spec.DependsOn)
		}
	}
	if leadID != "" {
		lead := findWorkItem(mission, leadID)
		if lead == nil || strings.Join(lead.DependsOn, ",") != strings.Join(script.Expected.LeadDependsOn, ",") {
			t.Fatalf("lead depends_on=%v want %v", lead.DependsOn, script.Expected.LeadDependsOn)
		}
	}
	for id, status := range preExisting {
		if item := findWorkItem(mission, id); item == nil || item.Status != status {
			t.Fatalf("accepted proposal mutated pre-existing work item %q", id)
		}
	}
	stepSpecs := make([]taskplan.StepSpec, 0, len(proposal))
	for _, spec := range proposal {
		stepSpecs = append(stepSpecs, taskplan.StepSpec{ID: spec.ID, Title: spec.Title})
	}
	changed, err := plan.AddSteps(stepSpecs)
	if err != nil || !changed {
		t.Fatalf("plan.AddSteps changed=%v err=%v", changed, err)
	}
	if plan.Revision != revisionBefore+1 {
		t.Fatalf("plan revision=%d want exactly %d", plan.Revision, revisionBefore+1)
	}
	for id, status := range priorStatus {
		if step := findStep(plan, id); step == nil || step.Status != status {
			t.Fatalf("pre-existing plan step %q changed status", id)
		}
	}
	for _, spec := range proposal {
		count := 0
		for _, step := range plan.Steps {
			if step.ID == spec.ID {
				count++
				if step.Title != spec.Title || step.Status != taskplan.Pending {
					t.Fatalf("plan step %q does not mirror the accepted substep: %+v", spec.ID, step)
				}
			}
		}
		if count != 1 {
			t.Fatalf("substep %q maps to %d plan steps, want exactly one", spec.ID, count)
		}
	}
	if err = taskplan.Validate(plan); err != nil {
		t.Fatalf("plan invalid after accepted substeps: %v", err)
	}
}

func findWorkItem(mission *team.Mission, id string) *team.WorkItem {
	for i := range mission.WorkItems {
		if mission.WorkItems[i].ID == id {
			return &mission.WorkItems[i]
		}
	}
	return nil
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
