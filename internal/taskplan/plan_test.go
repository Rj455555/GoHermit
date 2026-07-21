package taskplan

import (
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
