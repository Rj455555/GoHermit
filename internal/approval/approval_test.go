package approval

import (
	"strings"
	"testing"
	"time"
)

var testStart = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

func validSpec() CreateSpec {
	return CreateSpec{
		RequestID:         "apr-test",
		SessionID:         "session-1",
		RunID:             "run-1",
		Tool:              "shell",
		ResourcePaths:     []string{"src/main.go"},
		ArgsSummary:       "go build ./...",
		ArgsPayload:       `{"command":"go build ./..."}`,
		PolicyFingerprint: "ws-fingerprint",
		PlanRevision:      1,
	}
}

func mustCreate(t *testing.T, spec CreateSpec) Request {
	t.Helper()
	req, err := Create(spec, testStart)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestCreateValidatesScopeAndStampsExpiry(t *testing.T) {
	spec := validSpec()
	spec.MissionID, spec.WorkItemID, spec.Role = "mission-1", "build", "builder"
	req := mustCreate(t, spec)
	if req.Status != Pending || !req.ExpiresAt.Equal(testStart.Add(TTL)) || !req.CreatedAt.Equal(testStart) {
		t.Fatalf("request=%+v", req)
	}
	if req.ArgsDigest == "" || strings.Contains(req.ArgsDigest, "go build") {
		t.Fatalf("digest must be a sha256 hex, not payload text: %q", req.ArgsDigest)
	}
	derived, err := Create(CreateSpec{SessionID: "s", RunID: "r", Tool: "shell", ResourcePaths: []string{"a.txt"}, PolicyFingerprint: "fp", PlanRevision: 1}, testStart)
	if err != nil || !strings.HasPrefix(derived.RequestID, "apr-") {
		t.Fatalf("derived id=%q err=%v", derived.RequestID, err)
	}

	cases := map[string]func(*CreateSpec){
		"missing session":     func(s *CreateSpec) { s.SessionID = "" },
		"missing run":         func(s *CreateSpec) { s.RunID = " " },
		"missing tool":        func(s *CreateSpec) { s.Tool = "" },
		"no paths":            func(s *CreateSpec) { s.ResourcePaths = nil },
		"too many paths":      func(s *CreateSpec) { s.ResourcePaths = make([]string, MaxResourcePaths+1) },
		"empty path":          func(s *CreateSpec) { s.ResourcePaths = []string{" "} },
		"absolute path":       func(s *CreateSpec) { s.ResourcePaths = []string{"/etc/passwd"} },
		"windows absolute":    func(s *CreateSpec) { s.ResourcePaths = []string{`C:\Windows\system32`} },
		"dotdot path":         func(s *CreateSpec) { s.ResourcePaths = []string{"../outside.txt"} },
		"nested dotdot":       func(s *CreateSpec) { s.ResourcePaths = []string{"a/../../b"} },
		"missing fingerprint": func(s *CreateSpec) { s.PolicyFingerprint = "" },
		"missing revision":    func(s *CreateSpec) { s.PlanRevision = 0 },
	}
	for name, mutate := range cases {
		spec := validSpec()
		mutate(&spec)
		if _, err := Create(spec, testStart); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
	// In-place "../" segments and over-long summaries are rejected/bounded.
	spec = validSpec()
	spec.ResourcePaths = []string{"a/./b"}
	if _, err := Create(spec, testStart); err != nil {
		t.Fatalf("a/./b must stay valid: %v", err)
	}
	spec = validSpec()
	spec.ArgsSummary = strings.Repeat("x", MaxSummaryBytes+100)
	req = mustCreate(t, spec)
	if len(req.ArgsSummary) != MaxSummaryBytes {
		t.Fatalf("summary not clipped: %d", len(req.ArgsSummary))
	}
}

func TestIsExpiredIsTheSingleExpiryPredicate(t *testing.T) {
	req := mustCreate(t, validSpec())
	if IsExpired(&req, testStart.Add(TTL-time.Second)) {
		t.Fatal("not yet expired")
	}
	if !IsExpired(&req, testStart.Add(TTL)) || !IsExpired(&req, testStart.Add(TTL+time.Second)) {
		t.Fatal("deadline must expire at expires_at")
	}
	req.Status = Approved
	if IsExpired(&req, testStart.Add(2*TTL)) {
		t.Fatal("terminal statuses are never lazily expired by the predicate")
	}
	if IsExpired(nil, testStart) {
		t.Fatal("nil request must not expire")
	}
}

func TestDecideTransitionsOnlyFromLivePending(t *testing.T) {
	req := mustCreate(t, validSpec())
	if err := Decide(&req, true, testStart.Add(time.Minute)); err != nil || req.Status != Approved {
		t.Fatalf("approve err=%v status=%s", err, req.Status)
	}
	if err := Decide(&req, false, testStart.Add(time.Minute)); err == nil || req.Status != Approved {
		t.Fatalf("re-decide must fail without changing state: err=%v status=%s", err, req.Status)
	}

	req = mustCreate(t, validSpec())
	if err := Decide(&req, false, testStart.Add(time.Minute)); err != nil || req.Status != Denied {
		t.Fatalf("deny err=%v status=%s", err, req.Status)
	}
	if err := Decide(&req, true, testStart.Add(time.Minute)); err == nil || req.Status != Denied {
		t.Fatalf("denied must be terminal: err=%v status=%s", err, req.Status)
	}
}

func TestDecideOnExpiredPendingMarksExpiredAndRejects(t *testing.T) {
	req := mustCreate(t, validSpec())
	if err := Decide(&req, true, testStart.Add(TTL)); err == nil || req.Status != Expired {
		t.Fatalf("expired pending must become expired and reject approval: err=%v status=%s", err, req.Status)
	}
	// The expiry decision is itself terminal.
	if err := Decide(&req, true, testStart.Add(TTL)); err == nil || req.Status != Expired {
		t.Fatalf("expired must be terminal: err=%v status=%s", err, req.Status)
	}
}

func TestConsumeIsOneShotIrreversibleAndNonReentrant(t *testing.T) {
	req := mustCreate(t, validSpec())
	if err := Consume(&req, testStart.Add(time.Minute)); err == nil || req.Status != Pending {
		t.Fatalf("pending cannot be consumed: err=%v status=%s", err, req.Status)
	}
	if err := Decide(&req, true, testStart.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := Consume(&req, testStart.Add(2*time.Minute)); err != nil || req.Status != Consumed {
		t.Fatalf("consume err=%v status=%s", err, req.Status)
	}
	if err := Consume(&req, testStart.Add(3*time.Minute)); err == nil || req.Status != Consumed {
		t.Fatalf("second consume must fail without changing state: err=%v status=%s", err, req.Status)
	}
	if err := Decide(&req, false, testStart.Add(3*time.Minute)); err == nil || req.Status != Consumed {
		t.Fatalf("consumed must be terminal for decisions: err=%v status=%s", err, req.Status)
	}
}

func TestConsumeOnExpiredApprovedMarksExpiredAndRejects(t *testing.T) {
	req := mustCreate(t, validSpec())
	if err := Decide(&req, true, testStart.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := Consume(&req, testStart.Add(TTL)); err == nil || req.Status != Expired {
		t.Fatalf("expired approved must become expired, never consumable: err=%v status=%s", err, req.Status)
	}
	if err := Consume(&req, testStart.Add(TTL)); err == nil || req.Status != Expired {
		t.Fatalf("expired must be terminal for consumption: err=%v status=%s", err, req.Status)
	}
}

func TestBatchTriggersExpireOnlyTheMatchingPendingRequests(t *testing.T) {
	fresh := func() []Request {
		pendingRun1 := mustCreate(t, validSpec())
		pendingRun1.RequestID = "pending-run1"
		staleRevision := pendingRun1
		staleRevision.RequestID = "stale-revision"
		staleRevision.PlanRevision = 2
		otherRun := pendingRun1
		otherRun.RequestID = "other-run"
		otherRun.RunID = "run-2"
		approved := pendingRun1
		approved.RequestID = "approved-run1"
		approved.Status = Approved
		denied := pendingRun1
		denied.RequestID = "denied-run1"
		denied.Status = Denied
		return []Request{pendingRun1, staleRevision, otherRun, approved, denied}
	}
	now := testStart.Add(time.Minute)

	requests := fresh()
	expired := ExpireRunPending(requests, "run-1", now)
	if strings.Join(expired, ",") != "pending-run1,stale-revision" {
		t.Fatalf("expired=%v", expired)
	}
	for _, req := range requests {
		want := map[string]Status{"pending-run1": Expired, "stale-revision": Expired, "other-run": Pending, "approved-run1": Approved, "denied-run1": Denied}[req.RequestID]
		if req.Status != want {
			t.Fatalf("%s: status=%s want %s", req.RequestID, req.Status, want)
		}
	}

	requests = fresh()
	expired = ExpirePlanRevisionPending(requests, "run-1", 1, now)
	if strings.Join(expired, ",") != "stale-revision" {
		t.Fatalf("expired=%v", expired)
	}
	if requests[0].Status != Pending || requests[1].Status != Expired || requests[2].Status != Pending || requests[3].Status != Approved || requests[4].Status != Denied {
		t.Fatalf("revision trigger touched the wrong requests: %+v", requests)
	}

	requests = fresh()
	expired = ExpirePolicyPending(requests, "rotated-fingerprint", now)
	if strings.Join(expired, ",") != "pending-run1,stale-revision,other-run" {
		t.Fatalf("expired=%v", expired)
	}
	if requests[3].Status != Approved || requests[4].Status != Denied {
		t.Fatalf("policy trigger touched terminal requests: %+v", requests)
	}

	// Idempotent: a second trigger over the same scope expires nothing.
	if again := ExpireRunPending(requests, "run-1", now); len(again) != 0 {
		t.Fatalf("trigger is not idempotent: %v", again)
	}
}
