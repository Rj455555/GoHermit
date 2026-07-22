package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
)

// approvalProbeTool parks every call with the approval-required marker until
// the executor's approved override re-executes it; it counts real executions.
type approvalProbeTool struct{ executions int32 }

func (t *approvalProbeTool) Definition() tool.Definition {
	return tool.Definition{Name: "shell.execute", InputSchema: json.RawMessage(`{"type":"object"}`), DefaultTimeout: time.Second, MaxOutputBytes: 1024}
}

func (t *approvalProbeTool) Execute(ctx context.Context, toolCall tool.Call) (tool.Result, error) {
	if !tool.IsApproved(ctx) {
		return tool.Result{
			Error:    &tool.Error{Code: tool.CodeApprovalRequired, Message: "command is not in the non-interactive allowlist"},
			Approval: &tool.ApprovalHint{Paths: []string{"proof.txt"}, Summary: "touch proof.txt"},
		}, nil
	}
	atomic.AddInt32(&t.executions, 1)
	return tool.Result{Output: "ran"}, nil
}

type staticDecisions struct{ approved bool }

func (d staticDecisions) Wait(context.Context, string, string) (bool, error) { return d.approved, nil }

// expiryDecisions never decides; the wait ends only at the request deadline.
type expiryDecisions struct{}

func (expiryDecisions) Wait(ctx context.Context, _, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

// approvalCallProvider issues one shell.execute call, then finishes; it
// captures the last tool message the model received.
type approvalCallProvider struct {
	lastToolMessage atomic.Value
}

func (p *approvalCallProvider) Generate(_ context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	for _, message := range request.Messages {
		if message.Role == model.RoleTool {
			p.lastToolMessage.Store(message.Content)
		}
	}
	for _, message := range request.Messages {
		if message.Role == model.RoleTool {
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop", Attempts: 1}, nil
		}
	}
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "shell.execute", Arguments: json.RawMessage(`{"command":"touch proof.txt"}`)}}}, FinishReason: "tool_calls", Attempts: 1}, nil
}

func (*approvalCallProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true, ToolCalls: true}
}

func newApprovalRunner(t *testing.T, provider model.Provider, decisions ApprovalDecisions, ttl time.Duration, probe *approvalProbeTool) (*Runner, *session.Session) {
	t.Helper()
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .9, ReserveOutputTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err = registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	s, err := session.New("goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Provider: provider, Executor: tool.Executor{Registry: registry, DefaultTimeout: time.Second}, Context: manager, Store: store, Config: Config{MaxTurns: 4, Timeout: 30 * time.Second, Model: "test", CheckpointEveryTurns: 1, ApprovalTTL: ttl}, Approvals: decisions}
	return runner, s
}

func approvalEventTypes(t *testing.T, store *session.Store, sessionID string) []event.Type {
	t.Helper()
	events, err := store.Events(sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []event.Type
	for _, e := range events {
		switch e.Type {
		case event.ApprovalRequested, event.ApprovalDecided, event.ApprovalExpired, event.ApprovalConsumed:
			types = append(types, e.Type)
		}
	}
	return types
}

func lastToolMessage(t *testing.T, p *approvalCallProvider) string {
	t.Helper()
	value := p.lastToolMessage.Load()
	if value == nil {
		t.Fatal("the model never received a tool message")
	}
	return value.(string)
}

// TestNilBrokerKeepsPreC3DenialPath: without an ApprovalDecisions source the
// approval-required result passes straight to the model as denial data, no
// request is created, and the run completes — exactly the pre-C3 behavior.
func TestNilBrokerKeepsPreC3DenialPath(t *testing.T) {
	probe := &approvalProbeTool{}
	provider := &approvalCallProvider{}
	runner, s := newApprovalRunner(t, provider, nil, 0, probe)
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if len(s.ApprovalRequests) != 0 {
		t.Fatalf("nil broker created a request: %+v", s.ApprovalRequests)
	}
	if atomic.LoadInt32(&probe.executions) != 0 {
		t.Fatal("parked call executed without approval")
	}
	if got := lastToolMessage(t, provider); !strings.Contains(got, tool.CodeApprovalRequired) {
		t.Fatalf("denial data did not reach the model: %s", got)
	}
	if types := approvalEventTypes(t, runner.Store, s.ID); len(types) != 0 {
		t.Fatalf("nil broker emitted approval events: %v", types)
	}
}

// TestApprovedCallConsumesOneShotAndExecutesOnce: an approved request is
// durably decided and consumed before the call re-executes exactly once
// through the approved override; the consumed request can never be spent
// again.
func TestApprovedCallConsumesOneShotAndExecutesOnce(t *testing.T) {
	probe := &approvalProbeTool{}
	provider := &approvalCallProvider{}
	runner, s := newApprovalRunner(t, provider, staticDecisions{approved: true}, 0, probe)
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&probe.executions) != 1 {
		t.Fatalf("executions=%d, want exactly one approved re-execution", probe.executions)
	}
	if len(s.ApprovalRequests) != 1 || s.ApprovalRequests[0].Status != approval.Consumed {
		t.Fatalf("requests=%+v", s.ApprovalRequests)
	}
	req := s.ApprovalRequests[0]
	if req.Tool != "shell.execute" || req.RunID == "" || req.SessionID != s.ID || req.PolicyFingerprint != "digest" || req.PlanRevision < 1 {
		t.Fatalf("request scope=%+v", req)
	}
	if len(req.ResourcePaths) != 1 || req.ResourcePaths[0] != "proof.txt" || req.ArgsSummary != "touch proof.txt" {
		t.Fatalf("request hint=%+v", req)
	}
	if strings.Contains(req.ArgsDigest, "touch") || req.ArgsDigest == "" {
		t.Fatalf("args must persist as a digest only: %q", req.ArgsDigest)
	}
	// One-shot: the consumed request rejects any second spend.
	if err := approval.Consume(&req, time.Now().UTC()); err == nil {
		t.Fatal("consumed request was spent twice")
	}
	types := approvalEventTypes(t, runner.Store, s.ID)
	want := []event.Type{event.ApprovalRequested, event.ApprovalDecided, event.ApprovalConsumed}
	if len(types) != len(want) {
		t.Fatalf("approval events=%v want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("approval events=%v want %v", types, want)
		}
	}
}

// TestDeniedApprovalContinuesRunWithStructuredDenial: the owner denial is
// durably recorded, the call never executes, and the model receives
// structured denial data so the run completes instead of failing.
func TestDeniedApprovalContinuesRunWithStructuredDenial(t *testing.T) {
	probe := &approvalProbeTool{}
	provider := &approvalCallProvider{}
	runner, s := newApprovalRunner(t, provider, staticDecisions{approved: false}, 0, probe)
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&probe.executions) != 0 {
		t.Fatal("denied call executed")
	}
	if len(s.ApprovalRequests) != 1 || s.ApprovalRequests[0].Status != approval.Denied {
		t.Fatalf("requests=%+v", s.ApprovalRequests)
	}
	if got := lastToolMessage(t, provider); !strings.Contains(got, tool.CodeApprovalDenied) {
		t.Fatalf("structured denial did not reach the model: %s", got)
	}
	types := approvalEventTypes(t, runner.Store, s.ID)
	if len(types) != 2 || types[0] != event.ApprovalRequested || types[1] != event.ApprovalDecided {
		t.Fatalf("approval events=%v", types)
	}
}

// TestExpiredApprovalContinuesRunWithStructuredDenial: with no decision
// before the (test-shortened) deadline the request expires — the unattended
// default is deny — and the run still completes.
func TestExpiredApprovalContinuesRunWithStructuredDenial(t *testing.T) {
	probe := &approvalProbeTool{}
	provider := &approvalCallProvider{}
	runner, s := newApprovalRunner(t, provider, expiryDecisions{}, 50*time.Millisecond, probe)
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&probe.executions) != 0 {
		t.Fatal("expired call executed")
	}
	if len(s.ApprovalRequests) != 1 || s.ApprovalRequests[0].Status != approval.Expired {
		t.Fatalf("requests=%+v", s.ApprovalRequests)
	}
	if got := lastToolMessage(t, provider); !strings.Contains(got, tool.CodeApprovalDenied) {
		t.Fatalf("structured denial did not reach the model: %s", got)
	}
	types := approvalEventTypes(t, runner.Store, s.ID)
	if len(types) != 2 || types[0] != event.ApprovalRequested || types[1] != event.ApprovalExpired {
		t.Fatalf("approval events=%v", types)
	}
}
