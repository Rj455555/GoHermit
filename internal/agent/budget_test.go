package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
)

func setModelCallBudget(t *testing.T, runner *Runner, maximum int) {
	t.Helper()
	field := reflect.ValueOf(&runner.Config).Elem().FieldByName("MaxModelCalls")
	if !field.IsValid() {
		t.Fatal("Runner.Config.MaxModelCalls is not implemented")
	}
	field.SetInt(int64(maximum))
}

func TestModelCallBudgetOneAllowsDirectFinal(t *testing.T) {
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}, nil
	}}
	runner, s := newRunner(t, p, 3, 30*time.Second, agentTool{})
	setModelCallBudget(t, runner, 1)

	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || s.Runs[0].ModelCalls != 1 || s.Runs[0].Status != session.RunCompleted {
		t.Fatalf("calls=%d run=%+v", p.calls, s.Runs[0])
	}
}

type countingBudgetTool struct {
	calls *atomic.Int32
	block bool
}

func (t countingBudgetTool) Definition() tool.Definition {
	return tool.Definition{
		Name: "noop", InputSchema: json.RawMessage(`{"type":"object"}`),
		DefaultTimeout: time.Second, MaxOutputBytes: 100,
	}
}

func (t countingBudgetTool) Execute(ctx context.Context, _ tool.Call) (tool.Result, error) {
	t.calls.Add(1)
	if t.block {
		<-ctx.Done()
		return tool.Result{}, ctx.Err()
	}
	return tool.Result{Output: "ok"}, nil
}

func TestModelCallBudgetOneStopsBeforePostBudgetToolAndProvider(t *testing.T) {
	var toolCalls atomic.Int32
	p := &scriptedProvider{fn: func(n int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		if n > 0 {
			t.Errorf("provider entered after budget exhaustion: call=%d", n+1)
		}
		return toolResponse("c1"), nil
	}}
	runner, s := newRunner(t, p, 3, 30*time.Second, agentTool{})
	registry := tool.NewRegistry()
	if err := registry.Register(countingBudgetTool{calls: &toolCalls}); err != nil {
		t.Fatal(err)
	}
	runner.Executor.Registry = registry
	setModelCallBudget(t, runner, 1)

	err := runner.Run(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "model call budget exhausted") {
		t.Fatalf("err=%v", err)
	}
	if p.calls != 1 || toolCalls.Load() != 0 || s.Runs[0].Status == session.RunRunning || s.ActiveRunID != "" {
		t.Fatalf("calls=%d tools=%d session=%+v run=%+v", p.calls, toolCalls.Load(), s, s.Runs[0])
	}
}

func TestModelCallBudgetTwoAllowsToolThenFinal(t *testing.T) {
	var toolCalls atomic.Int32
	p := &scriptedProvider{fn: func(n int, request model.GenerateRequest) (model.GenerateResponse, error) {
		if n == 0 {
			return toolResponse("c1"), nil
		}
		if n != 1 {
			t.Errorf("unexpected provider call %d", n+1)
		}
		found := false
		for _, message := range request.Messages {
			found = found || message.Role == model.RoleTool && message.ToolCallID == "c1"
		}
		if !found {
			t.Error("second model call did not receive the tool result")
		}
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}, nil
	}}
	runner, s := newRunner(t, p, 3, 30*time.Second, agentTool{})
	registry := tool.NewRegistry()
	if err := registry.Register(countingBudgetTool{calls: &toolCalls}); err != nil {
		t.Fatal(err)
	}
	runner.Executor.Registry = registry
	setModelCallBudget(t, runner, 2)

	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 || toolCalls.Load() != 1 || s.Runs[0].ModelCalls != 2 || s.Runs[0].Status != session.RunCompleted {
		t.Fatalf("calls=%d tools=%d run=%+v", p.calls, toolCalls.Load(), s.Runs[0])
	}
}

func TestProviderCallBudgetCountsApplicationEntriesNotProviderAttempts(t *testing.T) {
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return model.GenerateResponse{}, &model.ProviderError{
			Kind: model.ErrorUnavailable, Status: 503, Retryable: false,
			Attempts: 7, Message: "unavailable",
		}
	}}
	runner, s := newRunner(t, p, 2, 30*time.Second, agentTool{})
	setModelCallBudget(t, runner, 2)

	err := runner.Run(context.Background(), s)
	if err == nil || s.Runs[0].ModelCalls != 1 || p.calls != 1 {
		t.Fatalf("err=%v calls=%d run=%+v", err, p.calls, s.Runs[0])
	}
}

type contextBudgetProvider struct {
	calls atomic.Int32
	fn    func(context.Context, model.GenerateRequest) (model.GenerateResponse, error)
}

func (p *contextBudgetProvider) Generate(ctx context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	p.calls.Add(1)
	return p.fn(ctx, request)
}

func (*contextBudgetProvider) Capabilities() model.Capabilities {
	return model.Capabilities{ToolCalls: true}
}

func TestProviderThatIgnoresContextCannotKeepRunRunning(t *testing.T) {
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		select {}
	}}
	runner, s := newRunner(t, p, 3, 500*time.Millisecond, agentTool{})
	setModelCallBudget(t, runner, 1)

	started := time.Now()
	err := runner.Run(context.Background(), s)
	if time.Since(started) > 2*time.Second {
		t.Fatal("run did not terminate within the bounded deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) || s.Runs[0].Status != session.RunInterrupted ||
		s.ActiveRunID == "" || s.Runs[0].ModelCalls != 1 {
		t.Fatalf("err=%v session=%+v run=%+v", err, s, s.Runs[0])
	}
}

type blockingBudgetTool struct {
	started chan struct{}
}

func (blockingBudgetTool) Definition() tool.Definition {
	return tool.Definition{
		Name: "noop", InputSchema: json.RawMessage(`{"type":"object"}`),
		DefaultTimeout: time.Second, MaxOutputBytes: 100,
	}
}

func (t blockingBudgetTool) Execute(context.Context, tool.Call) (tool.Result, error) {
	close(t.started)
	select {}
}

func TestToolThatIgnoresContextCannotKeepRunRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return toolResponse("c1"), nil
	}}
	runner, s := newRunner(t, p, 3, 5*time.Second, agentTool{})
	registry := tool.NewRegistry()
	if err := registry.Register(blockingBudgetTool{started: started}); err != nil {
		t.Fatal(err)
	}
	runner.Executor.Registry = registry
	setModelCallBudget(t, runner, 2)

	result := make(chan error, 1)
	go func() {
		result <- runner.Run(ctx, s)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("tool execution did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || p.calls != 1 ||
			s.Runs[0].Status != session.RunCancelled || s.Runs[0].ModelCalls != 1 ||
			s.ActiveRunID != "" {
			t.Fatalf("err=%v calls=%d session=%+v run=%+v", err, p.calls, s, s.Runs[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tool execution kept the Run running after cancellation")
	}
}

func TestLateProviderResultCannotCompleteTimedOutRun(t *testing.T) {
	p := &contextBudgetProvider{fn: func(ctx context.Context, _ model.GenerateRequest) (model.GenerateResponse, error) {
		<-ctx.Done()
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "late"}}, nil
	}}
	runner, s := newRunner(t, p, 2, 500*time.Millisecond, agentTool{})
	setModelCallBudget(t, runner, 1)

	err := runner.Run(context.Background(), s)
	if !errors.Is(err, context.DeadlineExceeded) || s.Runs[0].Status != session.RunInterrupted ||
		s.Runs[0].FinalMessage != "" {
		t.Fatalf("err=%v run=%+v", err, s.Runs[0])
	}
}

func TestRetryableProviderErrorUsesNextApplicationBudgetSlot(t *testing.T) {
	p := &scriptedProvider{fn: func(n int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		if n == 0 {
			return model.GenerateResponse{}, &model.ProviderError{
				Kind: model.ErrorUnavailable, Status: 503, Retryable: true,
				Attempts: 9, Message: "temporary failure",
			}
		}
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}, nil
	}}
	runner, s := newRunner(t, p, 3, 30*time.Second, agentTool{})
	setModelCallBudget(t, runner, 2)

	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 || s.Runs[0].ModelCalls != 2 || s.Runs[0].Status != session.RunCompleted {
		t.Fatalf("calls=%d run=%+v", p.calls, s.Runs[0])
	}
}

func TestResumeDoesNotResetModelCallBudget(t *testing.T) {
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		t.Error("resume entered Provider after the persisted budget was exhausted")
		return model.GenerateResponse{}, nil
	}}
	runner, s := newRunner(t, p, 2, 30*time.Second, agentTool{})
	run, err := s.NewRun("goal")
	if err != nil {
		t.Fatal(err)
	}
	run.Status = session.RunInterrupted
	run.ModelCalls = 1
	if err := runner.Store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	setModelCallBudget(t, runner, 1)

	err = runner.Run(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "model call budget exhausted") ||
		p.calls != 0 || s.Runs[0].ModelCalls != 1 || s.Runs[0].Status != session.RunFailed ||
		s.ActiveRunID != "" {
		t.Fatalf("err=%v calls=%d run=%+v active=%q", err, p.calls, s.Runs[0], s.ActiveRunID)
	}
}

func TestToolTimeoutStopsBeforeNextProviderCall(t *testing.T) {
	p := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return toolResponse("timeout"), nil
	}}
	runner, s := newRunner(t, p, 3, 30*time.Second, agentTool{delay: time.Second})
	setModelCallBudget(t, runner, 2)

	err := runner.Run(context.Background(), s)
	if err == nil || p.calls != 1 || s.Runs[0].Status != session.RunFailed ||
		s.ActiveRunID != "" || s.Runs[0].ModelCalls != 1 {
		t.Fatalf("err=%v calls=%d session=%+v run=%+v", err, p.calls, s, s.Runs[0])
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Status != "completed" || !s.ToolCalls[0].IsError {
		t.Fatalf("timed out tool record=%+v", s.ToolCalls)
	}
}
