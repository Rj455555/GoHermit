package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type scriptedProvider struct {
	mu    sync.Mutex
	fn    func(int, model.GenerateRequest) (model.GenerateResponse, error)
	calls int
}

func (p *scriptedProvider) Generate(ctx context.Context, r model.GenerateRequest) (model.GenerateResponse, error) {
	p.mu.Lock()
	n := p.calls
	p.calls++
	p.mu.Unlock()
	return p.fn(n, r)
}
func (*scriptedProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true, ToolCalls: true}
}

type agentTool struct {
	err   error
	delay time.Duration
}

type namedAgentTool struct {
	name    string
	mutates bool
}

func (t namedAgentTool) Definition() tool.Definition {
	return tool.Definition{Name: t.name, InputSchema: json.RawMessage(`{"type":"object"}`), MutatesWorkspace: t.mutates, DefaultTimeout: time.Second, MaxOutputBytes: 100}
}
func (t namedAgentTool) Execute(context.Context, tool.Call) (tool.Result, error) {
	return tool.Result{Output: "ok"}, nil
}

func (t agentTool) Definition() tool.Definition {
	return tool.Definition{Name: "noop", InputSchema: json.RawMessage(`{"type":"object"}`), DefaultTimeout: 20 * time.Millisecond, MaxOutputBytes: 100}
}
func (t agentTool) Execute(ctx context.Context, c tool.Call) (tool.Result, error) {
	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			return tool.Result{}, ctx.Err()
		}
	}
	return tool.Result{Output: "ok"}, t.err
}
func newRunner(t *testing.T, p model.Provider, max int, timeout time.Duration, toolImpl agentTool) (*Runner, *session.Session) {
	t.Helper()
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .9, ReserveOutputTokens: 512})
	registry := tool.NewRegistry()
	if err = registry.Register(toolImpl); err != nil {
		t.Fatal(err)
	}
	s, _ := session.New("goal", root, "digest")
	return &Runner{Provider: p, Executor: tool.Executor{Registry: registry, DefaultTimeout: time.Second}, Context: manager, Store: store, Config: Config{MaxTurns: max, Timeout: timeout, Model: "test", CheckpointEveryTurns: 5, CheckpointOnToolCompletion: true}}, s
}
func toolResponse(id string) model.GenerateResponse {
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: id, Name: "noop", Arguments: json.RawMessage(`{}`)}}}, FinishReason: "tool_calls"}
}
func TestNormalStopAndToolResultReturned(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, r model.GenerateRequest) (model.GenerateResponse, error) {
		if n == 0 {
			return toolResponse("c1"), nil
		}
		if n > 1 {
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{}`}}, nil
		}
		found := false
		for _, m := range r.Messages {
			if m.Role == model.RoleTool && m.ToolCallID == "c1" {
				found = true
			}
		}
		if !found {
			t.Fatal("tool result not returned to model")
		}
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
	}}
	runner, s := newRunner(t, p, 5, time.Second, agentTool{})
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.Status != session.Open || s.Turns != 2 || len(s.Runs) != 1 || s.Runs[0].Status != session.RunCompleted {
		t.Fatalf("session=%+v", s)
	}
	if s.Runs[0].Plan == nil || s.Runs[0].Plan.Status != taskplan.Completed {
		t.Fatalf("plan=%+v", s.Runs[0].Plan)
	}
	events, err := runner.Store.Events(s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	created, updated := false, 0
	for _, runtimeEvent := range events {
		created = created || runtimeEvent.Type == event.PlanCreated
		if runtimeEvent.Type == event.PlanUpdated {
			updated++
		}
		if runtimeEvent.Type == event.ToolStarted && len(runtimeEvent.Data) != 0 {
			t.Fatalf("persisted tool arguments: %s", runtimeEvent.Data)
		}
	}
	if !created || updated < 4 {
		t.Fatalf("plan events created=%v updated=%d", created, updated)
	}
}

func TestPersistentEventsAreDurableBeforeSinkDelivery(t *testing.T) {
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
	}}
	runner, s := newRunner(t, p, 2, time.Second, agentTool{})
	durable := true
	seen := false
	runner.Sink = func(runtimeEvent event.Event) {
		if runtimeEvent.Type != event.PlanCreated {
			return
		}
		seen = true
		events, err := runner.Store.Events(s.ID, 0)
		if err != nil {
			durable = false
			return
		}
		found := false
		for _, stored := range events {
			found = found || stored.Sequence == runtimeEvent.Sequence
		}
		durable = durable && found
	}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !seen || !durable {
		t.Fatalf("plan creation reached sink before durable commit: seen=%v durable=%v", seen, durable)
	}
}
func TestMaximumTurns(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, r model.GenerateRequest) (model.GenerateResponse, error) {
		return toolResponse(string(rune('a' + n))), nil
	}}
	runner, s := newRunner(t, p, 2, time.Second, agentTool{})
	err := runner.Run(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "maximum turns") {
		t.Fatalf("err=%v", err)
	}
	if s.Status != session.Open || len(s.Runs) != 1 || s.Runs[0].Status != session.RunFailed || s.Turns != 2 {
		t.Fatalf("session status=%s run=%+v turns=%d", s.Status, s.Runs, s.Turns)
	}
	if s.Runs[0].Plan == nil || s.Runs[0].Plan.Status != taskplan.Failed {
		t.Fatalf("plan=%+v", s.Runs[0].Plan)
	}
}
func TestTotalTimeout(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, r model.GenerateRequest) (model.GenerateResponse, error) {
		time.Sleep(50 * time.Millisecond)
		return model.GenerateResponse{}, context.DeadlineExceeded
	}}
	runner, s := newRunner(t, p, 2, 10*time.Millisecond, agentTool{})
	err := runner.Run(context.Background(), s)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if s.Status != session.Open || len(s.Runs) != 1 || s.Runs[0].Status != session.RunInterrupted || s.ActiveRunID == "" || s.Runs[0].CompletedAt != nil {
		t.Fatalf("session status=%s runs=%+v", s.Status, s.Runs)
	}
	if s.Runs[0].Plan == nil || s.Runs[0].Plan.Status != taskplan.Active || s.Runs[0].Plan.Current() == nil {
		t.Fatalf("plan=%+v", s.Runs[0].Plan)
	}
}

func TestExplicitCancellationIsTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		cancel()
		return model.GenerateResponse{}, context.Canceled
	}}
	runner, s := newRunner(t, p, 2, time.Second, agentTool{})
	if err := runner.Run(ctx, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(s.Runs) != 1 || s.Runs[0].Status != session.RunCancelled || s.ActiveRunID != "" || s.Runs[0].CompletedAt == nil {
		t.Fatalf("runs=%+v active=%q", s.Runs, s.ActiveRunID)
	}
	if s.Runs[0].Plan == nil || s.Runs[0].Plan.Status != taskplan.Cancelled {
		t.Fatalf("plan=%+v", s.Runs[0].Plan)
	}
}
func TestToolErrorReturnedToModel(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, r model.GenerateRequest) (model.GenerateResponse, error) {
		if n == 0 {
			return toolResponse("bad"), nil
		}
		if n > 1 {
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{}`}}, nil
		}
		last := r.Messages[len(r.Messages)-1].Content
		if !strings.Contains(last, "tool_error") {
			t.Fatalf("tool error missing: %s", last)
		}
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "handled"}}, nil
	}}
	runner, s := newRunner(t, p, 3, time.Second, agentTool{err: errors.New("boom")})
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func TestMutationRequiresSuccessfulTestBeforeCompletion(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, r model.GenerateRequest) (model.GenerateResponse, error) {
		switch n {
		case 0:
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "write", Name: "workspace.mutate", Arguments: json.RawMessage(`{}`)}}}}, nil
		case 1:
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "premature"}}, nil
		case 2:
			found := false
			for _, message := range r.Messages {
				if strings.Contains(message.Content, "run test.run") {
					found = true
				}
			}
			if !found {
				t.Fatal("verification failure was not returned to the model")
			}
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "test", Name: "test.run", Arguments: json.RawMessage(`{}`)}}}}, nil
		default:
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "verified"}}, nil
		}
	}}
	runner, s := newRunner(t, p, 6, 3*time.Second, agentTool{})
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = s.Workspace
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := runner.Executor.Registry.Register(namedAgentTool{name: "workspace.mutate", mutates: true}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Executor.Registry.Register(namedAgentTool{name: "test.run"}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if len(s.Runs) != 1 || s.Runs[0].Status != session.RunCompleted || s.Runs[0].VerificationAttempts != 1 || s.Runs[0].LastVerificationTurn < s.Runs[0].LastMutationTurn {
		t.Fatalf("run=%+v", s.Runs)
	}
}
