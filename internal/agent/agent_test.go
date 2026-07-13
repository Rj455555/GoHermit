package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
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
	if s.Status != session.Completed || s.Turns != 2 {
		t.Fatalf("session=%+v", s)
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
	if s.Status != session.Failed || s.Turns != 2 {
		t.Fatalf("status=%s turns=%d", s.Status, s.Turns)
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
	if s.Status != session.Failed && s.Status != session.Cancelled {
		t.Fatalf("status=%s", s.Status)
	}
}
func TestToolErrorReturnedToModel(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, r model.GenerateRequest) (model.GenerateResponse, error) {
		if n == 0 {
			return toolResponse("bad"), nil
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
