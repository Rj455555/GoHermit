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
