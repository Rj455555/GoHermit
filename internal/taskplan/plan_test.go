package taskplan

import (
	"fmt"
	"strings"
	"testing"
)

func TestPlanTracksSingleCurrentStepAndCompletion(t *testing.T) {
	plan, err := DefaultSingle("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := plan.Start("analyze", "reading"); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, err = plan.Start("execute", "writing"); err == nil {
		t.Fatal("expected concurrent current-step rejection")
	}
	for _, id := range []string{"analyze", "execute", "verify", "report"} {
		if id != "analyze" {
			if _, err = plan.Start(id, "started"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = plan.Complete(id, "done"); err != nil {
			t.Fatal(err)
		}
	}
	done, total := plan.Progress()
	if plan.Status != Completed || done != total || total != 4 || plan.Current() != nil {
		t.Fatalf("plan=%+v done=%d total=%d", plan, done, total)
	}
}

func TestPlanFailureIsTerminal(t *testing.T) {
	plan, _ := DefaultTeam("run-1")
	_, _ = plan.Start("explore", "started")
	if _, err := plan.Fail("explore", "failed"); err != nil {
		t.Fatal(err)
	}
	if plan.Status != Failed || plan.Steps[0].Status != StepFailed {
		t.Fatalf("plan=%+v", plan)
	}
	if _, err := plan.Start("build", "should not start"); err == nil {
		t.Fatal("failed plan accepted new work")
	}
}

func TestPlanCancellationKeepsCompletedHistory(t *testing.T) {
	plan, _ := DefaultSingle("run-1")
	_, _ = plan.Start("analyze", "started")
	_, _ = plan.Complete("analyze", "done")
	_, _ = plan.Start("execute", "started")
	changed, stepID := plan.Cancel("owner stopped")
	if !changed || stepID != "execute" || plan.Status != Cancelled || plan.Steps[0].Status != StepDone || plan.Steps[1].Status != StepCancelled {
		t.Fatalf("plan=%+v step=%s", plan, stepID)
	}
	if err := Validate(plan); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRejectsDuplicateAndUnsafeSteps(t *testing.T) {
	if _, err := New("plan", []StepSpec{{ID: "same", Title: "one"}, {ID: "same", Title: "two"}}); err == nil {
		t.Fatal("expected duplicate rejection")
	}
	if _, err := New("plan", []StepSpec{{ID: "../bad", Title: "bad"}}); err == nil {
		t.Fatal("expected unsafe id rejection")
	}
}

func TestAddStepsAppendsSubstepsWithoutRewritingHistory(t *testing.T) {
	plan, err := New("plan", []StepSpec{{ID: "explore", Title: "Explore"}, {ID: "lead", Title: "Lead"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = plan.Start("explore", "running")
	_, _ = plan.Complete("explore", "done")
	revision := plan.Revision
	changed, err := plan.AddSteps([]StepSpec{{ID: "inspect_auth", Title: "梳理认证流程"}, {ID: "cross_check", Title: "交叉核对"}})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if plan.Revision != revision+1 {
		t.Fatalf("revision=%d want %d", plan.Revision, revision+1)
	}
	if plan.step("explore").Status != StepDone || plan.step("inspect_auth").Status != Pending || plan.step("cross_check").Status != Pending {
		t.Fatalf("plan=%+v", plan.Steps)
	}
	if err = Validate(plan); err != nil {
		t.Fatal(err)
	}
	if changed, err = plan.AddSteps(nil); changed || err != nil {
		t.Fatalf("empty specs changed=%v err=%v", changed, err)
	}
}

func TestAddStepsRejectsDuplicatesOverflowAndInactivePlan(t *testing.T) {
	plan, _ := New("plan", []StepSpec{{ID: "explore", Title: "Explore"}, {ID: "lead", Title: "Lead"}})
	_, _ = plan.Start("explore", "running")
	_, _ = plan.Complete("explore", "done")
	if _, err := plan.AddSteps([]StepSpec{{ID: "explore", Title: "Rewrite history"}}); err == nil {
		t.Fatal("expected duplicate-with-completed-id rejection")
	}
	if _, err := plan.AddSteps([]StepSpec{{ID: "lead", Title: "Duplicate pending"}}); err == nil {
		t.Fatal("expected duplicate-with-pending-id rejection")
	}
	if _, err := plan.AddSteps([]StepSpec{{ID: "../unsafe", Title: "Unsafe"}}); err == nil {
		t.Fatal("expected unsafe id rejection")
	}
	if plan.Revision != 3 || len(plan.Steps) != 2 {
		t.Fatalf("rejected AddSteps mutated the plan: %+v", plan)
	}

	specs := make([]StepSpec, 0, MaxSteps-1)
	for i := 0; i < MaxSteps-1; i++ {
		specs = append(specs, StepSpec{ID: fmt.Sprintf("step_%d", i), Title: "Step"})
	}
	full, err := New("plan-full", specs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = full.AddSteps([]StepSpec{{ID: "extra_a", Title: "Extra"}, {ID: "extra_b", Title: "Extra"}}); err == nil {
		t.Fatal("expected step-limit rejection")
	}
	if len(full.Steps) != MaxSteps-1 {
		t.Fatalf("overflow attempt mutated the plan: %d steps", len(full.Steps))
	}

	inactive, _ := New("plan-inactive", []StepSpec{{ID: "only", Title: "Only"}})
	_, _ = inactive.Start("only", "running")
	_, _ = inactive.Fail("only", "failed")
	if _, err = inactive.AddSteps([]StepSpec{{ID: "late", Title: "Late"}}); err == nil {
		t.Fatal("expected non-active plan rejection")
	}
	if len(inactive.Steps) != 1 {
		t.Fatalf("non-active AddSteps mutated the plan: %+v", inactive.Steps)
	}
}

func TestPlanRejectsProgressThatSkipsCurrentStep(t *testing.T) {
	plan, _ := DefaultSingle("run-1")
	_, _ = plan.Start("analyze", "started")
	if _, err := plan.Complete("execute", "skipped"); err == nil {
		t.Fatal("expected completion of a pending step to reject another current step")
	}
	if _, err := plan.Fail("execute", "skipped"); err == nil {
		t.Fatal("expected failure of a pending step to reject another current step")
	}
}

func TestValidateRejectsUnknownAndLogicallyCompleteActivePlan(t *testing.T) {
	plan, _ := New("plan", []StepSpec{{ID: "only", Title: "Only step"}})
	plan.Status = Status("mystery")
	if err := Validate(plan); err == nil {
		t.Fatal("expected unknown status rejection")
	}
	plan.Status = Active
	plan.Steps[0].Status = StepDone
	if err := Validate(plan); err == nil {
		t.Fatal("expected active all-completed plan rejection")
	}
}

func TestForGoalBuildsBoundedTaskSpecificPlans(t *testing.T) {
	goal := "修复 Codex 登录后没有流式输出，并增加断线恢复"
	for _, agent := range []string{"coding", "team"} {
		plan, err := ForGoal("run-1", goal, agent)
		if err != nil {
			t.Fatalf("agent=%s err=%v", agent, err)
		}
		if err = Validate(plan); err != nil {
			t.Fatalf("agent=%s invalid plan: %v", agent, err)
		}
		joined := ""
		for _, step := range plan.Steps {
			joined += step.Title
			if len(step.Title) > MaxTitleBytes || strings.Contains(step.Title, "\n") {
				t.Fatalf("unbounded plan title: %q", step.Title)
			}
		}
		if !strings.Contains(joined, "Codex 登录") {
			t.Fatalf("plan is not task-specific: %+v", plan.Steps)
		}
	}
}

func TestParallelPlanCanTrackConcurrentStepsAndReopenVerification(t *testing.T) {
	plan, err := NewParallel("plan-team", []StepSpec{
		{ID: "inspect-code", Title: "Inspect code"},
		{ID: "inspect-tests", Title: "Inspect tests"},
		{ID: "repair", Title: "Repair"},
		{ID: "verify", Title: "Verify"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Start("inspect-code", "running"); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Start("inspect-tests", "running"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"inspect-code", "inspect-tests", "repair", "verify"} {
		if plan.step(id).Status == Pending {
			_, _ = plan.Start(id, "running")
		}
		if _, err = plan.Complete(id, "done"); err != nil {
			t.Fatal(err)
		}
	}
	if plan.Status != Completed {
		t.Fatalf("plan=%+v", plan)
	}
	changed, err := plan.Reopen([]string{"repair", "verify"}, "verification failed")
	if err != nil || !changed || plan.Status != Active || plan.step("repair").Status != Pending || plan.step("verify").Status != Pending {
		t.Fatalf("plan=%+v changed=%v err=%v", plan, changed, err)
	}
	if err = Validate(plan); err != nil {
		t.Fatal(err)
	}
}
