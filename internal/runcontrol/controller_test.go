package runcontrol

import (
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
)

func TestApplyTeamEventDrivesPlanOutsidePresentation(t *testing.T) {
	plan, err := taskplan.New("plan-run", []taskplan.StepSpec{
		{ID: "inspect", Title: "Inspect the task"},
		{ID: "verify", Title: "Verify the result"},
	})
	if err != nil {
		t.Fatal(err)
	}

	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.WorkItemStarted, WorkItemID: "inspect", Message: "reading"}, nil)
	if err != nil || !transition.Changed || transition.StepID != "inspect" {
		t.Fatalf("start transition=%+v err=%v", transition, err)
	}
	transition, err = ApplyTeamEvent(plan, team.TeamEvent{Type: team.WorkItemDone, WorkItemID: "inspect", Message: "understood"}, nil)
	if err != nil || !transition.Changed || plan.Steps[0].Status != taskplan.StepDone {
		t.Fatalf("complete transition=%+v plan=%+v err=%v", transition, plan, err)
	}
}

func TestVerifierCannotCompleteWithoutPassingChecks(t *testing.T) {
	plan, err := taskplan.New("plan-run", []taskplan.StepSpec{{ID: "verify", Title: "Verify"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plan.Start("verify", "running checks")
	mission := &team.Mission{Handoffs: []team.Handoff{{
		ID: "handoff-1", WorkItemID: "verify", Role: team.RoleVerifier, CreatedAt: time.Now().UTC(),
		Checks: []team.Check{{Command: "go test ./...", Passed: false, Summary: "failed"}},
	}}}

	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.WorkItemDone, WorkItemID: "verify", Role: team.RoleVerifier}, mission)
	if err != nil || !transition.Changed {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	if plan.Status != taskplan.Failed || plan.Steps[0].Status != taskplan.StepFailed {
		t.Fatalf("verifier failure must fail plan: %+v", plan)
	}
}

func TestInterruptAndCancelHaveDistinctRecoverySemantics(t *testing.T) {
	plan, err := taskplan.New("plan-run", []taskplan.StepSpec{{ID: "build", Title: "Build"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plan.Start("build", "working")

	transition, err := Interrupt(plan, "timeout; resume available")
	if err != nil || !transition.Changed || plan.Status != taskplan.Active || plan.Current() == nil {
		t.Fatalf("interrupt transition=%+v plan=%+v err=%v", transition, plan, err)
	}
	transition, err = Cancel(plan, "stopped by owner")
	if err != nil || !transition.Changed || plan.Status != taskplan.Cancelled {
		t.Fatalf("cancel transition=%+v plan=%+v err=%v", transition, plan, err)
	}
}

func TestVerifierFailureReopensControllerPlanWhenMissionQueuedRepair(t *testing.T) {
	plan, err := taskplan.NewParallel("plan-run", []taskplan.StepSpec{{ID: "repair", Title: "Repair"}, {ID: "verify", Title: "Verify"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plan.Start("repair", "repair")
	_, _ = plan.Complete("repair", "done")
	_, _ = plan.Start("verify", "verify")
	mission := &team.Mission{
		WorkItems: []team.WorkItem{{ID: "repair", Status: team.WorkQueued, MutatesWorkspace: true, Attempt: 1}, {ID: "verify", Role: team.RoleVerifier, Status: team.WorkQueued, DependsOn: []string{"repair"}, Attempt: 1}},
		Handoffs:  []team.Handoff{{ID: "failed", WorkItemID: "verify", Role: team.RoleVerifier, Checks: []team.Check{{Command: "go test ./...", Passed: false}}}},
	}
	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.WorkItemDone, WorkItemID: "verify", Role: team.RoleVerifier}, mission)
	if err != nil || !transition.Changed || transition.StepID != "repair" || plan.Status != taskplan.Active || plan.Steps[0].Status != taskplan.Pending || plan.Steps[1].Status != taskplan.Pending {
		t.Fatalf("transition=%+v plan=%+v err=%v", transition, plan, err)
	}
}

func TestVerifierFailureReopensPlanForUnskippedRepairWithZeroAttempts(t *testing.T) {
	// After an advisory-only review the repair stage is skipped; when the
	// verifier then fails, RequeueAfterVerification un-skips it with Attempt
	// still 0. The plan must reopen that repair step instead of failing.
	plan, err := taskplan.NewParallel("plan-run", []taskplan.StepSpec{{ID: "repair", Title: "Repair"}, {ID: "verify", Title: "Verify"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plan.Complete("repair", "审查无 blocking 发现，跳过修复")
	_, _ = plan.Start("verify", "verify")
	mission := &team.Mission{
		WorkItems: []team.WorkItem{{ID: "repair", Status: team.WorkQueued, MutatesWorkspace: true}, {ID: "verify", Role: team.RoleVerifier, Status: team.WorkQueued, DependsOn: []string{"repair"}, Attempt: 1}},
		Handoffs:  []team.Handoff{{ID: "failed", WorkItemID: "verify", Role: team.RoleVerifier, Checks: []team.Check{{Command: "go test ./...", Passed: false}}}},
	}
	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.WorkItemDone, WorkItemID: "verify", Role: team.RoleVerifier}, mission)
	if err != nil || !transition.Changed || transition.StepID != "repair" || plan.Status != taskplan.Active || plan.Steps[0].Status != taskplan.Pending || plan.Steps[1].Status != taskplan.Pending {
		t.Fatalf("transition=%+v plan=%+v err=%v", transition, plan, err)
	}
}

func TestSubstepsAcceptedExtendsPlanFromExplorerProposal(t *testing.T) {
	plan, err := taskplan.New("plan-run", []taskplan.StepSpec{{ID: "explore", Title: "Explore"}, {ID: "lead", Title: "Lead"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plan.Start("explore", "running")
	message := `[{"id":"inspect_auth","title":"梳理认证流程"},{"id":"cross_check","title":"交叉核对"}]`
	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.SubstepsAccepted, WorkItemID: "explore", Role: team.RoleExplorer, Message: message}, nil)
	if err != nil || !transition.Changed || transition.StepID != "inspect_auth" || transition.Detail == "" {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	if len(plan.Steps) != 4 || plan.Steps[2].Status != taskplan.Pending || plan.Steps[3].ID != "cross_check" {
		t.Fatalf("plan=%+v", plan.Steps)
	}
	if err = taskplan.Validate(plan); err != nil {
		t.Fatal(err)
	}
}

func TestSubstepsRejectedLeavesPlanUntouched(t *testing.T) {
	plan, err := taskplan.New("plan-run", []taskplan.StepSpec{{ID: "explore", Title: "Explore"}})
	if err != nil {
		t.Fatal(err)
	}
	revision := plan.Revision
	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.SubstepsRejected, WorkItemID: "explore", Role: team.RoleExplorer, Message: "substep id \"explore\" already exists in the mission"}, nil)
	if err != nil || transition.Changed || transition.StepID != "" {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	if plan.Revision != revision || len(plan.Steps) != 1 {
		t.Fatalf("rejected proposal changed the plan: %+v", plan)
	}
}

func TestSubstepsAcceptedWithMalformedMessageDoesNotPanic(t *testing.T) {
	plan, err := taskplan.New("plan-run", []taskplan.StepSpec{{ID: "explore", Title: "Explore"}})
	if err != nil {
		t.Fatal(err)
	}
	revision := plan.Revision
	transition, err := ApplyTeamEvent(plan, team.TeamEvent{Type: team.SubstepsAccepted, WorkItemID: "explore", Role: team.RoleExplorer, Message: "not-json"}, nil)
	if err != nil || transition.Changed {
		t.Fatalf("transition=%+v err=%v", transition, err)
	}
	if plan.Revision != revision || len(plan.Steps) != 1 {
		t.Fatalf("malformed message changed the plan: %+v", plan)
	}
}
