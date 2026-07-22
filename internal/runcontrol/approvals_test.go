package runcontrol

import (
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/approval"
)

var approvalStart = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

func approvalFixture(t *testing.T) []approval.Request {
	t.Helper()
	create := func(id, runID string, revision int, status approval.Status) approval.Request {
		req, err := approval.Create(approval.CreateSpec{
			RequestID: id, SessionID: "session-1", RunID: runID, Tool: "shell",
			ResourcePaths: []string{"a.txt"}, ArgsPayload: `{"command":"x"}`,
			PolicyFingerprint: "fp-1", PlanRevision: revision,
		}, approvalStart)
		if err != nil {
			t.Fatal(err)
		}
		req.Status = status
		return req
	}
	return []approval.Request{
		create("pending-run1", "run-1", 1, approval.Pending),
		create("stale-run1", "run-1", 2, approval.Pending),
		create("pending-run2", "run-2", 1, approval.Pending),
		create("approved-run1", "run-1", 1, approval.Approved),
		create("consumed-run1", "run-1", 1, approval.Consumed),
	}
}

func TestExpireRunApprovalsExpiresOnlyTheRunsPendingRequests(t *testing.T) {
	requests := approvalFixture(t)
	expired := ExpireRunApprovals(requests, "run-1", approvalStart.Add(time.Minute))
	if strings.Join(expired, ",") != "pending-run1,stale-run1" {
		t.Fatalf("expired=%v", expired)
	}
	want := map[string]approval.Status{
		"pending-run1": approval.Expired, "stale-run1": approval.Expired,
		"pending-run2": approval.Pending, "approved-run1": approval.Approved,
		"consumed-run1": approval.Consumed,
	}
	for _, req := range requests {
		if req.Status != want[req.RequestID] {
			t.Fatalf("%s: status=%s want %s", req.RequestID, req.Status, want[req.RequestID])
		}
	}
}

func TestExpireApprovalsForPlanRevisionExpiresOnlyStalePending(t *testing.T) {
	requests := approvalFixture(t)
	expired := ExpireApprovalsForPlanRevision(requests, "run-1", 1, approvalStart.Add(time.Minute))
	if strings.Join(expired, ",") != "stale-run1" {
		t.Fatalf("expired=%v", expired)
	}
	if requests[0].Status != approval.Pending || requests[1].Status != approval.Expired || requests[3].Status != approval.Approved || requests[4].Status != approval.Consumed {
		t.Fatalf("revision trigger touched the wrong requests: %+v", requests)
	}
}

func TestExpireApprovalsForPolicyExpiresOnlyStaleFingerprintPending(t *testing.T) {
	requests := approvalFixture(t)
	expired := ExpireApprovalsForPolicy(requests, "fp-rotated", approvalStart.Add(time.Minute))
	if strings.Join(expired, ",") != "pending-run1,stale-run1,pending-run2" {
		t.Fatalf("expired=%v", expired)
	}
	if requests[3].Status != approval.Approved || requests[4].Status != approval.Consumed {
		t.Fatalf("policy trigger touched terminal requests: %+v", requests)
	}
	// Same fingerprint expires nothing; triggers are idempotent.
	if again := ExpireApprovalsForPolicy(requests, "fp-1", approvalStart.Add(time.Minute)); len(again) != 0 {
		t.Fatalf("unchanged fingerprint expired requests: %v", again)
	}
}
