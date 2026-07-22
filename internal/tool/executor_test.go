package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeTool struct {
	def Definition
	run func(context.Context, Call) (Result, error)
}

func (f fakeTool) Definition() Definition                            { return f.def }
func (f fakeTool) Execute(c context.Context, x Call) (Result, error) { return f.run(c, x) }
func TestExecutorTimeoutAndTruncation(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeTool{def: Definition{Name: "slow", DefaultTimeout: 10 * time.Millisecond, MaxOutputBytes: 5, InputSchema: json.RawMessage(`{}`)}, run: func(ctx context.Context, c Call) (Result, error) { <-ctx.Done(); return Result{}, ctx.Err() }})
	res, _ := (Executor{Registry: r}).Execute(context.Background(), Call{Name: "slow"})
	if res.Error == nil || res.Error.Code != "tool_timeout" {
		t.Fatalf("result=%+v", res)
	}
	r = NewRegistry()
	_ = r.Register(fakeTool{def: Definition{Name: "large", MaxOutputBytes: 5, InputSchema: json.RawMessage(`{}`)}, run: func(context.Context, Call) (Result, error) { return Result{Output: "123456789"}, nil }})
	res, _ = (Executor{Registry: r}).Execute(context.Background(), Call{Name: "large"})
	if !res.Truncated || res.Output != "12345" || res.OriginalSize != 9 || res.ReturnedSize != 5 {
		t.Fatalf("result=%+v", res)
	}
}

// TestExecuteApprovedMarksOnlyTheSingleInvocation: the approved marker is
// visible to the tool only inside ExecuteApproved and can never leak into a
// plain Execute — the unexported key is set by the executor alone.
func TestExecuteApprovedMarksOnlyTheSingleInvocation(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeTool{def: Definition{Name: "probe", InputSchema: json.RawMessage(`{}`)}, run: func(ctx context.Context, c Call) (Result, error) {
		if IsApproved(ctx) {
			return Result{Output: "approved"}, nil
		}
		return Result{Error: &Error{Code: CodeApprovalRequired, Message: "parked"}, Approval: &ApprovalHint{Paths: []string{"a.txt"}, Summary: "probe a.txt"}}, nil
	}})
	executor := Executor{Registry: r}
	res, _ := executor.Execute(context.Background(), Call{Name: "probe"})
	if res.Error == nil || res.Error.Code != CodeApprovalRequired || res.Approval == nil || res.Approval.Paths[0] != "a.txt" {
		t.Fatalf("plain execute result=%+v", res)
	}
	res, _ = executor.ExecuteApproved(context.Background(), Call{Name: "probe"})
	if res.Error != nil || res.Output != "approved" {
		t.Fatalf("approved execute result=%+v", res)
	}
	res, _ = executor.Execute(context.Background(), Call{Name: "probe"})
	if res.Error == nil || res.Error.Code != CodeApprovalRequired {
		t.Fatalf("marker leaked beyond the single invocation: %+v", res)
	}
}
