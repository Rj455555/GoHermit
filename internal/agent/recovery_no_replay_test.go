package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type noReplayTool struct{ executions atomic.Int32 }

func (*noReplayTool) Definition() tool.Definition {
	return tool.Definition{
		Name: "noop", Permission: tool.PermissionWrite, MutatesWorkspace: true,
		InputSchema: json.RawMessage(`{"type":"object"}`), DefaultTimeout: time.Second,
	}
}

func (t *noReplayTool) Execute(context.Context, tool.Call) (tool.Result, error) {
	t.executions.Add(1)
	return tool.Result{Output: "side effect"}, nil
}

func TestInterruptedRunDoesNotReplayCompletedToolCall(t *testing.T) {
	provider := &scriptedProvider{fn: func(call int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		switch call {
		case 0:
			return model.GenerateResponse{
				Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
					ID: "call-after-restart", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
				}}},
			}, nil
		case 1:
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "Done without replay."}}, nil
		default:
			return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{"summary":"recovered"}`}}, nil
		}
	}}
	runner, value := newRunner(t, provider, 3, 5*time.Second, agentTool{})
	registered := &noReplayTool{}
	registry := tool.NewRegistry()
	if err := registry.Register(registered); err != nil {
		t.Fatal(err)
	}
	runner.Executor.Registry = registry
	run, err := value.NewRunWithID("run-recovery", "resume safely")
	if err != nil {
		t.Fatal(err)
	}
	run.Status = session.RunInterrupted
	completed := model.ToolCall{ID: "call-before-restart", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`)}
	value.ToolCalls = []session.ToolRecord{{
		RunID: run.ID, CallID: completed.ID, Name: completed.Name,
		ArgsDigest: toolCallDigest(completed), Status: "completed",
	}}
	if err = runner.Run(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if registered.executions.Load() != 0 {
		t.Fatalf("completed tool side effect replayed %d times", registered.executions.Load())
	}
	if run.Status != session.RunCompleted {
		t.Fatalf("resumed Run status = %s", run.Status)
	}
}
