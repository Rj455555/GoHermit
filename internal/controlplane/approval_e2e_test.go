package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
	"github.com/Rj455555/GoHermit/internal/tool/builtin"
)

// approvalE2EProvider drives one shell.execute call with the scripted
// command, then finishes with a final answer once the tool result comes
// back. It captures the last tool message the model received.
type approvalE2EProvider struct {
	command         string
	lastToolMessage atomic.Value
}

func (p *approvalE2EProvider) Generate(_ context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	for _, message := range request.Messages {
		if message.Role == model.RoleTool {
			p.lastToolMessage.Store(message.Content)
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop", Attempts: 1}, nil
		}
	}
	arguments, _ := json.Marshal(map[string]string{"command": p.command})
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "shell.execute", Arguments: arguments}}}, FinishReason: "tool_calls", Attempts: 1}, nil
}

func (*approvalE2EProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true, ToolCalls: true}
}

// installApprovalRuntime points the service at a runtime with the REAL
// workspace builtin tools (including the gated shell) and the scripted
// provider, with the service's approval broker wired into the runner.
func installApprovalRuntime(t *testing.T, svc *Service, provider model.Provider, ttl time.Duration) config.Config {
	t.Helper()
	conf, err := app.LoadConfig(svc.Workspace, svc.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .9, ReserveOutputTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := builtin.NewWorkspace(svc.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err = builtin.RegisterAll(registry, workspace, 5*time.Second, 4096, 4096, false); err != nil {
		t.Fatal(err)
	}
	runner := &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: registry, DefaultTimeout: 5 * time.Second}, Context: manager, Store: svc.store, Config: agent.Config{MaxTurns: 4, Timeout: 30 * time.Second, Model: "test", CheckpointEveryTurns: 1, ApprovalTTL: ttl}, Approvals: svc.approvals}
	svc.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
		return &app.Runtime{Workspace: svc.Workspace, Config: conf, Store: svc.store, Runner: runner}, nil
	}
	return conf
}

func launchApprovalSession(t *testing.T, svc *Service, conf config.Config) *session.Session {
	t.Helper()
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "coding"}
	sess, err := session.NewConversation("Approval E2E", svc.Workspace, session.ConfigDigest(conf), selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.launchSessionRun(sess, "run the gated command"); err != nil {
		t.Fatal(err)
	}
	return sess
}

// decideApproval drives the service decide operation the pre-refactor tests
// reached through the HTTP API; a conflict-kind error is the 409 equivalent.
func decideApproval(t *testing.T, svc *Service, sessionID, requestID string, approve bool) error {
	t.Helper()
	_, _, err := svc.DecideApproval(context.Background(), sessionID, requestID, approve)
	return err
}

func isConflict(t *testing.T, err error) bool {
	t.Helper()
	var serviceErr *Error
	return errors.As(err, &serviceErr) && serviceErr.Kind == KindConflict
}

// waitForParkedApproval polls until the pending request is durable AND the
// runner's waiter is registered with the broker, so the decide below is
// guaranteed to take the active-run rendezvous path.
func waitForParkedApproval(t *testing.T, svc *Service, sessionID string) approval.Request {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := svc.store.Load(context.Background(), sessionID)
		if err == nil {
			for _, req := range loaded.ApprovalRequests {
				if req.Status == approval.Pending && svc.approvals.waiterFor(req.RequestID) != nil {
					return req
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no durable pending approval with a registered waiter")
	return approval.Request{}
}

func loadFreshSession(t *testing.T, svc *Service, sessionID string) *session.Session {
	t.Helper()
	fresh, err := session.NewStore(svc.Workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fresh.Load(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func approvalLifecycleEvents(t *testing.T, svc *Service, sessionID string) []event.Event {
	t.Helper()
	var lifecycle []event.Event
	for _, e := range approvalEvents(t, svc, sessionID) {
		switch e.Type {
		case event.ApprovalRequested, event.ApprovalDecided, event.ApprovalExpired, event.ApprovalConsumed:
			lifecycle = append(lifecycle, e)
		}
	}
	return lifecycle
}

func assertEventOrder(t *testing.T, events []event.Event, want ...event.Type) {
	t.Helper()
	prev := uint64(0)
	at := 0
	for _, e := range events {
		if at < len(want) && e.Type == want[at] {
			if e.Sequence <= prev {
				t.Fatalf("events out of order: %+v", events)
			}
			prev = e.Sequence
			at++
		}
	}
	if at != len(want) {
		t.Fatalf("events=%v missing from %+v", want, events)
	}
}

func onlyRequest(t *testing.T, sess *session.Session) approval.Request {
	t.Helper()
	if len(sess.ApprovalRequests) != 1 {
		t.Fatalf("requests=%+v", sess.ApprovalRequests)
	}
	return sess.ApprovalRequests[0]
}

func modelToolMessage(t *testing.T, p *approvalE2EProvider) string {
	t.Helper()
	value := p.lastToolMessage.Load()
	if value == nil {
		t.Fatal("the model never received a tool message")
	}
	return value.(string)
}

// TestApprovalApprovedExecutesGatedShellCommandEndToEnd proves the full C3
// rendezvous: a real ConfirmationRequired shell command parks the run, the
// owner approves through the decide operation, the decision travels the
// broker to the parked runner, and the command executes exactly once. The
// final checkpoint from a FRESH store shows both sides merged: the consumed
// request AND the run's own progress (model calls, completion) — no writer
// overwrote the other.
func TestApprovalApprovedExecutesGatedShellCommandEndToEnd(t *testing.T) {
	svc := newTestService(t)
	provider := &approvalE2EProvider{command: "touch c3-approved-proof.txt"}
	conf := installApprovalRuntime(t, svc, provider, 0)
	sess := launchApprovalSession(t, svc, conf)

	req := waitForParkedApproval(t, svc, sess.ID)
	if req.Tool != "shell.execute" || req.SessionID != sess.ID || req.RunID == "" || req.PolicyFingerprint != sess.ConfigDigest || req.PlanRevision < 1 {
		t.Fatalf("request scope=%+v", req)
	}
	if len(req.ResourcePaths) != 1 || req.ResourcePaths[0] != "c3-approved-proof.txt" {
		t.Fatalf("request paths=%v", req.ResourcePaths)
	}
	if req.ArgsSummary != "touch c3-approved-proof.txt" || strings.Contains(req.ArgsDigest, "touch") || req.ArgsDigest == "" {
		t.Fatalf("request summary=%q digest=%q", req.ArgsSummary, req.ArgsDigest)
	}

	if err := decideApproval(t, svc, sess.ID, req.RequestID, true); err != nil {
		t.Fatalf("decide err = %v", err)
	}
	waitForRun(t, svc)

	if _, err := os.Stat(filepath.Join(svc.Workspace, "c3-approved-proof.txt")); err != nil {
		t.Fatalf("approved command did not execute: %v", err)
	}
	loaded := loadFreshSession(t, svc, sess.ID)
	run := loaded.Runs[len(loaded.Runs)-1]
	if run.Status != session.RunCompleted || run.ModelCalls < 2 {
		t.Fatalf("run status=%s model_calls=%d error=%q", run.Status, run.ModelCalls, run.Error)
	}
	if got := onlyRequest(t, loaded); got.Status != approval.Consumed || got.RequestID != req.RequestID {
		t.Fatalf("request=%+v", got)
	}
	assertEventOrder(t, approvalLifecycleEvents(t, svc, sess.ID), event.ApprovalRequested, event.ApprovalDecided, event.ApprovalConsumed)

	// One-shot at the API too: a second decide on the same id conflicts.
	if err := decideApproval(t, svc, sess.ID, req.RequestID, true); !isConflict(t, err) {
		t.Fatalf("re-decide err = %v, want a conflict", err)
	}
	if got := onlyRequest(t, loadFreshSession(t, svc, sess.ID)); got.Status != approval.Consumed {
		t.Fatalf("failed re-decide changed state: %+v", got)
	}
}

// TestApprovalDeniedContinuesRunWithStructuredDenial closes the C2 gap: a
// real deny through the decide operation reaches the parked runner, the gated
// command never executes, and the run still completes — the model received
// structured denial data instead of the run failing blindly (ADR 0011).
func TestApprovalDeniedContinuesRunWithStructuredDenial(t *testing.T) {
	svc := newTestService(t)
	provider := &approvalE2EProvider{command: "touch c3-denied-proof.txt"}
	conf := installApprovalRuntime(t, svc, provider, 0)
	sess := launchApprovalSession(t, svc, conf)

	req := waitForParkedApproval(t, svc, sess.ID)
	if err := decideApproval(t, svc, sess.ID, req.RequestID, false); err != nil {
		t.Fatalf("decide err = %v", err)
	}
	waitForRun(t, svc)

	if _, err := os.Stat(filepath.Join(svc.Workspace, "c3-denied-proof.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied command executed: %v", err)
	}
	loaded := loadFreshSession(t, svc, sess.ID)
	if run := loaded.Runs[len(loaded.Runs)-1]; run.Status != session.RunCompleted {
		t.Fatalf("run status=%s error=%q", run.Status, run.Error)
	}
	if got := onlyRequest(t, loaded); got.Status != approval.Denied {
		t.Fatalf("request=%+v", got)
	}
	if got := modelToolMessage(t, provider); !strings.Contains(got, tool.CodeApprovalDenied) {
		t.Fatalf("structured denial did not reach the model: %s", got)
	}
	assertEventOrder(t, approvalLifecycleEvents(t, svc, sess.ID), event.ApprovalRequested, event.ApprovalDecided)
}

// TestApprovalExpiryDeniesUnattendedDecision: with a test-shortened TTL and
// no owner decision, the parked wait expires, the request becomes durably
// expired (the unattended default is deny), and the run completes without
// the side effect.
func TestApprovalExpiryDeniesUnattendedDecision(t *testing.T) {
	svc := newTestService(t)
	provider := &approvalE2EProvider{command: "touch c3-expired-proof.txt"}
	conf := installApprovalRuntime(t, svc, provider, 100*time.Millisecond)
	sess := launchApprovalSession(t, svc, conf)

	waitForRun(t, svc)

	if _, err := os.Stat(filepath.Join(svc.Workspace, "c3-expired-proof.txt")); !os.IsNotExist(err) {
		t.Fatalf("expired command executed: %v", err)
	}
	loaded := loadFreshSession(t, svc, sess.ID)
	if run := loaded.Runs[len(loaded.Runs)-1]; run.Status != session.RunCompleted {
		t.Fatalf("run status=%s error=%q", run.Status, run.Error)
	}
	if got := onlyRequest(t, loaded); got.Status != approval.Expired {
		t.Fatalf("request=%+v", got)
	}
	if got := modelToolMessage(t, provider); !strings.Contains(got, tool.CodeApprovalDenied) {
		t.Fatalf("structured denial did not reach the model: %s", got)
	}
	assertEventOrder(t, approvalLifecycleEvents(t, svc, sess.ID), event.ApprovalRequested, event.ApprovalExpired)
}

// TestApprovalBlockedCommandNeverProducesARequest: a Blocked-classified
// command stays hard-denied exactly as before C3 — identical denial data,
// no approval request, no approval events, and the run continues.
func TestApprovalBlockedCommandNeverProducesARequest(t *testing.T) {
	svc := newTestService(t)
	provider := &approvalE2EProvider{command: "rm -rf c3-blocked-proof"}
	conf := installApprovalRuntime(t, svc, provider, 0)
	sess := launchApprovalSession(t, svc, conf)

	waitForRun(t, svc)

	loaded := loadFreshSession(t, svc, sess.ID)
	if run := loaded.Runs[len(loaded.Runs)-1]; run.Status != session.RunCompleted {
		t.Fatalf("run status=%s error=%q", run.Status, run.Error)
	}
	if len(loaded.ApprovalRequests) != 0 {
		t.Fatalf("blocked command produced a request: %+v", loaded.ApprovalRequests)
	}
	if got := modelToolMessage(t, provider); !strings.Contains(got, `"code":"blocked"`) || !strings.Contains(got, "destructive operation") {
		t.Fatalf("blocked denial data changed: %s", got)
	}
	if lifecycle := approvalLifecycleEvents(t, svc, sess.ID); len(lifecycle) != 0 {
		t.Fatalf("blocked command emitted approval events: %+v", lifecycle)
	}
}
