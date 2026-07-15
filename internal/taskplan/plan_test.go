package taskplan

import "testing"

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
