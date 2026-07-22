package team

import (
	"context"
	"sync"
	"testing"
)

// readOnlyVerifierWorker scripts a read-only-topology Mission (explore,
// review, verify, lead — no mutation) where the Verifier has nothing a
// deterministic Check could run against, matching a purely informational
// question like "hello, what model are you" against an empty workspace.
// explore and review are independent siblings and run concurrently in the
// same batch, so calls is mutex-guarded (mirrors concurrencyWorker's atomic
// counters elsewhere in this package for the same reason).
type readOnlyVerifierWorker struct {
	verifierIssues []string
	mu             sync.Mutex
	calls          []string
}

func (w *readOnlyVerifierWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	w.mu.Lock()
	w.calls = append(w.calls, assignment.WorkItem.ID)
	w.mu.Unlock()
	handoff := Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "completed " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == RoleVerifier {
		// No Checks: there is nothing to run against a plain informational
		// question in an empty, non-code workspace.
		handoff.Issues = w.verifierIssues
	}
	return Result{Handoff: handoff, ModelCalls: 1, Tokens: 50}, nil
}

// TestReadOnlyMissionPassesVerificationWithNoChecksAndNoIssues reproduces the
// owner-reported bug: a read-only Team Run (e.g. "hello, 你是什么模型" against
// an empty workspace) used to fail with "independent verification did not
// pass" because the Verifier legitimately had no command to run, and
// verificationPassed/handoffChecksPassed treated an empty Checks list as
// unconditionally unverified — with no repair path in the read-only
// topology, the Mission could never recover. An empty Issues list on the
// Verifier's own independent cross-check must count as passed instead.
func TestReadOnlyMissionPassesVerificationWithNoChecksAndNoIssues(t *testing.T) {
	mission, err := AdaptiveMission("mission-readonly", "run-1", "hello, 你是什么模型", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if missionHasMutation(mission) {
		t.Fatalf("goal without a mutation marker must select the read-only topology: %+v", mission.WorkItems)
	}
	worker := &readOnlyVerifierWorker{}
	if err = (&Coordinator{Worker: worker}).Run(context.Background(), mission); err != nil {
		t.Fatalf("mission unexpectedly failed: %v (status=%s error=%q)", err, mission.Status, mission.Error)
	}
	if mission.Status != Completed {
		t.Fatalf("status=%s error=%q", mission.Status, mission.Error)
	}
	// explore and review are independent read-only siblings and may run in
	// either order within a batch; verify depends on both, lead depends on
	// verify, so only their relative order is fixed.
	if len(worker.calls) != 4 || worker.calls[2] != "verify" || worker.calls[3] != "lead" {
		t.Fatalf("calls=%v", worker.calls)
	}
	seenFirstTwo := map[string]bool{worker.calls[0]: true, worker.calls[1]: true}
	if !seenFirstTwo["explore"] || !seenFirstTwo["review"] {
		t.Fatalf("calls=%v", worker.calls)
	}
}

// TestReadOnlyMissionFailsVerificationWhenVerifierReportsIssues confirms the
// fix only changes the "genuinely nothing to check" case: a Verifier that
// actually finds an unsupported or incorrect claim during its independent
// cross-check must still fail the mission, even with no Checks — read-only
// missions have no repair path, so this is a straight failure, not a retry.
func TestReadOnlyMissionFailsVerificationWhenVerifierReportsIssues(t *testing.T) {
	mission, err := AdaptiveMission("mission-readonly-issues", "run-1", "hello, 你是什么模型", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	worker := &readOnlyVerifierWorker{verifierIssues: []string{"reviewer claim is not supported by any workspace evidence"}}
	err = (&Coordinator{Worker: worker}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed || mission.Error != "independent verification did not pass" {
		t.Fatalf("status=%s error=%q err=%v", mission.Status, mission.Error, err)
	}
}

// TestMutationMissionStillFailsVerificationWithNoChecks is the regression
// guard for the safety-critical path: a mission where a Builder actually
// mutated the workspace must never treat an empty Checks list as verified,
// even after this fix. TestCoordinatorBlocksLeadWithoutVerifierEvidence in
// coordinator_test.go already covers this against DefaultMission; this adds
// the same guard against the AdaptiveMission mutation topology specifically,
// since that is the code path selectBatch/handoffChecksPassed actually shares
// with the read-only topology this fix touches.
func TestMutationMissionStillFailsVerificationWithNoChecks(t *testing.T) {
	mission, err := AdaptiveMission("mission-mutation-unverified", "run-1", "implement the requested fix", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if !missionHasMutation(mission) {
		t.Fatalf("goal with a mutation marker must select the mutation topology: %+v", mission.WorkItems)
	}
	worker := &readOnlyVerifierWorker{}
	err = (&Coordinator{Worker: worker, MaxRepairAttempts: 3}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed || mission.Error != "independent verification did not pass" {
		t.Fatalf("status=%s error=%q err=%v — a mutation mission must never treat an empty Checks list as verified", mission.Status, mission.Error, err)
	}
}
