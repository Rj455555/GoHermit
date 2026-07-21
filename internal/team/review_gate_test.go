package team

import (
	"context"
	"strings"
	"testing"
)

// reviewGateMission returns a running mutation mission whose build and review
// completed, leaving the repair stage queued behind the review.
func reviewGateMission(t *testing.T, findings []Finding) *Mission {
	t.Helper()
	m, err := NewMission("mission-gate", "run-1", "build the feature", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []WorkItem{
		{ID: "build", Title: "Build", Goal: "implement", Role: RoleBuilder, MutatesWorkspace: true},
		{ID: "review", Title: "Review", Goal: "review", Role: RoleReviewer, DependsOn: []string{"build"}},
		{ID: "repair", Title: "Repair", Goal: "repair", Role: RoleBuilder, DependsOn: []string{"review"}, MutatesWorkspace: true},
		{ID: "verify", Title: "Verify", Goal: "verify", Role: RoleVerifier, DependsOn: []string{"repair"}},
		{ID: "lead", Title: "Lead", Goal: "synthesize", Role: RoleLead, DependsOn: []string{"verify"}},
	} {
		if err = m.AddWork(item); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.Start("build"); err != nil {
		t.Fatal(err)
	}
	if err = m.Complete("build", Handoff{ID: "handoff-build", WorkItemID: "build", Role: RoleBuilder, Summary: "implemented"}); err != nil {
		t.Fatal(err)
	}
	if err = m.Start("review"); err != nil {
		t.Fatal(err)
	}
	if err = m.Complete("review", Handoff{ID: "handoff-review", WorkItemID: "review", Role: RoleReviewer, Summary: "reviewed", Findings: findings}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSkipRepairsAfterReviewMarksQueuedMutatingDependents(t *testing.T) {
	m := reviewGateMission(t, nil)
	skipped := m.SkipRepairsAfterReview("review")
	if len(skipped) != 1 || skipped[0] != "repair" {
		t.Fatalf("skipped=%v", skipped)
	}
	repair := m.work("repair")
	if repair.Status != WorkSkipped || repair.Attempt != 0 || repair.StartedAt != nil || repair.CompletedAt != nil {
		t.Fatalf("repair=%+v", repair)
	}
	if ready := m.Ready(); len(ready) != 1 || ready[0] != "verify" {
		t.Fatalf("skipped repair must satisfy its dependents: ready=%v", ready)
	}
}

func TestSkipRepairsAfterReviewNoMatch(t *testing.T) {
	m := reviewGateMission(t, nil)
	if skipped := m.SkipRepairsAfterReview("build"); len(skipped) != 0 {
		t.Fatalf("non-review dependency must not skip: %v", skipped)
	}
	if skipped := m.SkipRepairsAfterReview("ghost"); len(skipped) != 0 {
		t.Fatalf("unknown review id must not skip: %v", skipped)
	}
	if m.work("repair").Status != WorkQueued {
		t.Fatalf("repair=%+v", m.work("repair"))
	}
	// A completed mutating dependent is never rewritten.
	if err := m.Start("repair"); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("repair", Handoff{ID: "handoff-repair", WorkItemID: "repair", Role: RoleBuilder, Summary: "fixed"}); err != nil {
		t.Fatal(err)
	}
	if skipped := m.SkipRepairsAfterReview("review"); len(skipped) != 0 || m.work("repair").Status != WorkCompleted {
		t.Fatalf("completed repair must not be skipped: %v", skipped)
	}
}

func TestSkipRepairsAfterReviewIgnoresPreflightReview(t *testing.T) {
	// The mutation topology runs a preflight Reviewer before any mutation; its
	// advisory-only outcome must never skip the primary build it gates.
	m, err := AdaptiveMission("mission-preflight", "run-1", "implement the widget", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if m.work("preflight") == nil || m.work("preflight").Role != RoleReviewer {
		t.Fatalf("expected a preflight reviewer in the mutation topology: %+v", m.WorkItems)
	}
	if skipped := m.SkipRepairsAfterReview("preflight"); len(skipped) != 0 {
		t.Fatalf("preflight review must not skip anything: %v", skipped)
	}
	if item := m.work("build"); item.Status != WorkQueued || !item.MutatesWorkspace {
		t.Fatalf("build was touched by a preflight skip: %+v", item)
	}
}

func TestSkippedRepairAllowsMissionCompletion(t *testing.T) {
	m := reviewGateMission(t, []Finding{{Severity: SeverityAdvisory, Summary: "可选改进"}})
	m.SkipRepairsAfterReview("review")
	if err := m.Start("verify"); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("verify", Handoff{ID: "handoff-verify", WorkItemID: "verify", Role: RoleVerifier, Summary: "verified", Checks: []Check{{Command: "go test ./...", Passed: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("lead"); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("lead", Handoff{ID: "handoff-lead", WorkItemID: "lead", Role: RoleLead, Summary: "delivered"}); err != nil {
		t.Fatal(err)
	}
	if m.Status != Completed {
		t.Fatalf("skipped repair must count as done: status=%s", m.Status)
	}
}

func TestCancelLeavesSkippedItemsSkipped(t *testing.T) {
	m := reviewGateMission(t, nil)
	m.SkipRepairsAfterReview("review")
	m.Cancel("owner stopped")
	repair := m.work("repair")
	if repair.Status != WorkSkipped || repair.CompletedAt != nil {
		t.Fatalf("cancel must not rewrite a skipped repair: %+v", repair)
	}
	if m.work("verify").Status != WorkCancelled || m.Status != Cancelled {
		t.Fatalf("mission=%s verify=%+v", m.Status, m.work("verify"))
	}
}

func TestRequeueAfterVerificationUnskipsSkippedRepair(t *testing.T) {
	m := reviewGateMission(t, []Finding{{Severity: SeverityAdvisory, Summary: "可选改进"}})
	m.SkipRepairsAfterReview("review")
	if err := m.Start("verify"); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("verify", Handoff{ID: "handoff-verify", WorkItemID: "verify", Role: RoleVerifier, Summary: "failed", Checks: []Check{{Command: "go test ./...", Passed: false}}}); err != nil {
		t.Fatal(err)
	}
	requeued, err := m.RequeueAfterVerification("verify", 3)
	if err != nil || !requeued {
		t.Fatalf("requeued=%v err=%v", requeued, err)
	}
	repair := m.work("repair")
	if repair.Status != WorkQueued || repair.StartedAt != nil || repair.CompletedAt != nil {
		t.Fatalf("skipped repair was not reset to queued: %+v", repair)
	}
	if ready := m.Ready(); len(ready) != 1 || ready[0] != "repair" {
		t.Fatalf("requeued repair must be runnable: ready=%v", ready)
	}
}

func TestValidateHandoffFindingsBounds(t *testing.T) {
	oversized := make([]Finding, 0, 129)
	for i := 0; i <= 128; i++ {
		oversized = append(oversized, Finding{Severity: SeverityAdvisory, Summary: "finding"})
	}
	cases := []struct {
		name     string
		findings []Finding
		want     string
	}{
		{"blocking accepted", []Finding{{Severity: SeverityBlocking, Summary: "必须修复"}}, ""},
		{"advisory accepted", []Finding{{Severity: SeverityAdvisory, Summary: "可选改进"}}, ""},
		{"invalid severity fails closed", []Finding{{Severity: "critical", Summary: "未知级别"}}, "blocking or advisory severity"},
		{"empty severity fails closed", []Finding{{Summary: "缺少级别"}}, "blocking or advisory severity"},
		{"empty summary rejected", []Finding{{Severity: SeverityBlocking, Summary: " "}}, "blocking or advisory severity"},
		{"oversized summary rejected", []Finding{{Severity: SeverityBlocking, Summary: strings.Repeat("s", MaxTextBytes+1)}}, "blocking or advisory severity"},
		{"too many findings rejected", oversized, "bounded limits"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			m, err := NewMission("mission-findings", "run-1", "goal", DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			if err = m.AddWork(WorkItem{ID: "review", Title: "Review", Goal: "review", Role: RoleReviewer}); err != nil {
				t.Fatal(err)
			}
			if err = m.Start("review"); err != nil {
				t.Fatal(err)
			}
			handoff := Handoff{ID: "handoff-review", WorkItemID: "review", Role: RoleReviewer, Summary: "reviewed", Findings: testCase.findings}
			err = m.Complete("review", handoff)
			if testCase.want == "" {
				if err != nil {
					t.Fatalf("findings unexpectedly rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("err=%v want substring %q", err, testCase.want)
			}
			if m.work("review").Status != WorkRunning || len(m.Handoffs) != 0 {
				t.Fatalf("rejected handoff polluted mission state: %+v", m)
			}
		})
	}
}

func TestHasBlockingFindings(t *testing.T) {
	cases := []struct {
		findings []Finding
		want     bool
	}{
		{nil, false},
		{[]Finding{{Severity: SeverityAdvisory, Summary: "可选"}}, false},
		{[]Finding{{Severity: SeverityAdvisory, Summary: "可选"}, {Severity: SeverityBlocking, Summary: "必须"}}, true},
	}
	for _, testCase := range cases {
		if got := (Handoff{Findings: testCase.findings}).HasBlockingFindings(); got != testCase.want {
			t.Fatalf("findings=%+v got=%v want=%v", testCase.findings, got, testCase.want)
		}
	}
}

// reviewGateWorker reviews with blocking or advisory-only findings and always
// verifies with passing checks.
type reviewGateWorker struct {
	blocking bool
	calls    []string
}

func (w *reviewGateWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	w.calls = append(w.calls, assignment.WorkItem.ID)
	handoff := Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "completed " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == RoleReviewer {
		severity := SeverityAdvisory
		if w.blocking {
			severity = SeverityBlocking
		}
		handoff.Findings = []Finding{{Severity: severity, Summary: "scripted finding"}}
	}
	if assignment.WorkItem.Role == RoleVerifier {
		handoff.Checks = []Check{{Command: "go test ./...", Passed: true, Summary: "ok"}}
	}
	return Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

func TestCoordinatorSkipsRepairAfterAdvisoryReview(t *testing.T) {
	mission, err := DefaultMission("mission-advisory", "run-1", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	worker := &reviewGateWorker{}
	events := make([]TeamEvent, 0)
	checkpoints := 0
	coordinator := Coordinator{Worker: worker, Sink: func(event TeamEvent) { events = append(events, event) }, Checkpoint: func(*Mission) error { checkpoints++; return nil }}
	if err = coordinator.Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	repair := mission.work("repair")
	if repair.Status != WorkSkipped || repair.Attempt != 0 {
		t.Fatalf("repair=%+v", repair)
	}
	for _, call := range worker.calls {
		if call == "repair" {
			t.Fatalf("skipped repair was executed: calls=%v", worker.calls)
		}
	}
	if mission.Status != Completed || len(mission.Handoffs) != 5 {
		t.Fatalf("mission=%s handoffs=%d", mission.Status, len(mission.Handoffs))
	}
	skipEvent := false
	for _, event := range events {
		if event.Type == WorkItemDone && event.WorkItemID == "repair" && event.Role == RoleBuilder && event.Message == "审查无 blocking 发现，跳过修复" {
			skipEvent = true
		}
	}
	if !skipEvent || checkpoints == 0 {
		t.Fatalf("skipEvent=%v checkpoints=%d events=%+v", skipEvent, checkpoints, events)
	}
}

func TestCoordinatorRunsRepairAfterBlockingReview(t *testing.T) {
	mission, err := DefaultMission("mission-blocking", "run-1", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	worker := &reviewGateWorker{blocking: true}
	if err = (&Coordinator{Worker: worker}).Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	repair := mission.work("repair")
	if repair.Status != WorkCompleted || repair.Attempt != 1 {
		t.Fatalf("repair=%+v", repair)
	}
	calls := 0
	for _, call := range worker.calls {
		if call == "repair" {
			calls++
		}
	}
	if calls != 1 || mission.Status != Completed || len(mission.Handoffs) != 6 {
		t.Fatalf("calls=%v mission=%s handoffs=%d", worker.calls, mission.Status, len(mission.Handoffs))
	}
}
