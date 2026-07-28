package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		Name: "noop", Permission: tool.PermissionWrite, MutatesWorkspace: false,
		InputSchema: json.RawMessage(`{"type":"object"}`), DefaultTimeout: time.Second,
	}
}

func (t *noReplayTool) Execute(context.Context, tool.Call) (tool.Result, error) {
	t.executions.Add(1)
	return tool.Result{Output: "side effect"}, nil
}

func TestToolCallDigestCanonicalizesJSON(t *testing.T) {
	equivalent := []json.RawMessage{
		json.RawMessage(`{"path":"a","mode":"safe","count":1}`),
		json.RawMessage("{\n  \"mode\" : \"safe\", \"count\": 1.0, \"path\" : \"a\"\n}"),
		json.RawMessage(`{"count":1e0,"mode":"\u0073afe","path":"\u0061"}`),
	}
	var want string
	for i, arguments := range equivalent {
		got, err := toolCallDigest(model.ToolCall{Name: "noop", Arguments: arguments})
		if err != nil {
			t.Fatalf("equivalent input %d: %v", i, err)
		}
		if len(got) != 64 || got != strings.ToLower(got) {
			t.Fatalf("digest is not canonical lowercase SHA-256: %q", got)
		}
		if i == 0 {
			want = got
		} else if got != want {
			t.Fatalf("equivalent input %d digest=%q want %q", i, got, want)
		}
	}
	different, err := toolCallDigest(model.ToolCall{
		Name: "noop", Arguments: json.RawMessage(`{"path":"b","mode":"safe","count":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if different == want {
		t.Fatal("different arguments produced the same digest")
	}
	largeA, err := toolCallDigest(model.ToolCall{
		Name: "noop", Arguments: json.RawMessage(`{"value":9007199254740992}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	largeB, err := toolCallDigest(model.ToolCall{
		Name: "noop", Arguments: json.RawMessage(`{"value":9007199254740993}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if largeA == largeB {
		t.Fatal("distinct large integers produced the same digest")
	}
}

func TestToolCallDigestRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	oversized := append([]byte(`{"value":"`), make([]byte, maxToolArgumentsBytes)...)
	oversized = append(oversized, []byte(`"}`)...)
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "multiple values", raw: []byte(`{} {}`)},
		{name: "trailing content", raw: []byte(`{} trailing`)},
		{name: "invalid UTF-8", raw: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}},
		{name: "duplicate key", raw: []byte(`{"path":"a","path":"b"}`)},
		{name: "isolated surrogate", raw: []byte(`{"path":"\ud800"}`)},
		{name: "oversized", raw: oversized},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := toolCallDigest(model.ToolCall{Name: "noop", Arguments: test.raw}); err == nil {
				t.Fatal("unsafe arguments must be rejected")
			}
		})
	}
}

func TestInvalidToolArgumentsNeverExecute(t *testing.T) {
	provider := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return model.GenerateResponse{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID: "invalid-call", Name: "noop", Arguments: json.RawMessage(`{"path":"a","path":"b"}`),
			}}},
		}, nil
	}}
	runner, value := newRunner(t, provider, 2, 5*time.Second, agentTool{})
	registered := &noReplayTool{}
	registry := tool.NewRegistry()
	if err := registry.Register(registered); err != nil {
		t.Fatal(err)
	}
	runner.Executor.Registry = registry
	if err := runner.Run(context.Background(), value); err == nil {
		t.Fatal("ambiguous tool arguments must fail the Run")
	}
	if registered.executions.Load() != 0 {
		t.Fatalf("invalid tool arguments executed %d times", registered.executions.Load())
	}
	if len(value.ToolCalls) != 0 {
		t.Fatalf("invalid arguments persisted ToolRecords: %+v", value.ToolCalls)
	}
}

func TestInterruptedRunDoesNotReplayCompletedToolCall(t *testing.T) {
	completed := model.ToolCall{
		ID: "call-before-restart", Name: "noop",
		Arguments: json.RawMessage(`{"path":"same","mode":"safe"}`),
	}
	replayed := completed
	replayed.ID = "call-after-restart"
	replayed.Arguments = json.RawMessage(`{"mode":"safe","path":"same"}`)
	executions := runRecoveredToolScenario(t, []session.ToolRecord{
		completedRecord(t, "run-recovery", completed, 3),
	}, 3, [][]model.ToolCall{{replayed}})
	if executions != 0 {
		t.Fatalf("completed tool side effect replayed %d times", executions)
	}
}

func TestRecoveryCanonicalDigestIgnoresWhitespaceAndEquivalentEscapes(t *testing.T) {
	for _, arguments := range []json.RawMessage{
		json.RawMessage("{\n \"path\" : \"same\" }"),
		json.RawMessage(`{"path":"\u0073ame"}`),
	} {
		completed := model.ToolCall{
			ID: "old-call", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
		}
		replayed := model.ToolCall{ID: "new-call", Name: "noop", Arguments: arguments}
		if got := runRecoveredToolScenario(t, []session.ToolRecord{
			completedRecord(t, "run-recovery", completed, 4),
		}, 4, [][]model.ToolCall{{replayed}}); got != 0 {
			t.Fatalf("equivalent call executed %d times", got)
		}
	}
}

func TestRecoveryExecutesGenuinelyDifferentArguments(t *testing.T) {
	completed := model.ToolCall{
		ID: "old-call", Name: "noop", Arguments: json.RawMessage(`{"path":"a"}`),
	}
	different := model.ToolCall{
		ID: "new-call", Name: "noop", Arguments: json.RawMessage(`{"path":"b"}`),
	}
	if got := runRecoveredToolScenario(t, []session.ToolRecord{
		completedRecord(t, "run-recovery", completed, 2),
	}, 2, [][]model.ToolCall{{different}}); got != 1 {
		t.Fatalf("different call executions=%d want 1", got)
	}
}

func TestRecoveryDoesNotConsumeMatchingCallFromEarlierTurn(t *testing.T) {
	completed := model.ToolCall{
		ID: "old-call", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
	}
	replayed := completed
	replayed.ID = "new-call"
	if got := runRecoveredToolScenario(t, []session.ToolRecord{
		completedRecord(t, "run-recovery", completed, 2),
	}, 3, [][]model.ToolCall{{replayed}}); got != 1 {
		t.Fatalf("earlier-turn call suppressed current execution; executions=%d", got)
	}
}

func TestRecoveryNeverSuppressesStartedOrUncertainCall(t *testing.T) {
	call := model.ToolCall{
		ID: "old-call", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
	}
	for _, status := range []string{"started", "uncertain"} {
		t.Run(status, func(t *testing.T) {
			record := completedRecord(t, "run-recovery", call, 3)
			record.Status = status
			record.CompletedAt = nil
			reissued := call
			reissued.ID = "new-call"
			if got := runRecoveredToolScenario(t, []session.ToolRecord{
				record,
			}, 3, [][]model.ToolCall{{reissued}}); got != 1 {
				t.Fatalf("%s call suppressed replanned execution; executions=%d", status, got)
			}
		})
	}
}

func TestRecoveryConsumesEachFrontierCompletionOnce(t *testing.T) {
	call := model.ToolCall{
		ID: "old-call", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
	}
	first := call
	first.ID = "new-call-1"
	second := call
	second.ID = "new-call-2"
	if got := runRecoveredToolScenario(t, []session.ToolRecord{
		completedRecord(t, "run-recovery", call, 5),
	}, 5, [][]model.ToolCall{{first, second}}); got != 1 {
		t.Fatalf("one completion and two reissues executed %d times; want 1", got)
	}
}

func TestRecoveryCountsMultipleFrontierCompletions(t *testing.T) {
	call := model.ToolCall{
		ID: "old-call-1", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
	}
	secondRecordCall := call
	secondRecordCall.ID = "old-call-2"
	reissues := make([]model.ToolCall, 3)
	for i := range reissues {
		reissues[i] = call
		reissues[i].ID = "new-call-" + string(rune('1'+i))
	}
	if got := runRecoveredToolScenario(t, []session.ToolRecord{
		completedRecord(t, "run-recovery", call, 6),
		completedRecord(t, "run-recovery", secondRecordCall, 6),
	}, 6, [][]model.ToolCall{reissues}); got != 1 {
		t.Fatalf("two completions and three reissues executed %d times; want 1", got)
	}
}

func TestRecoveryLegacyRecordUsesExactCallIDOnly(t *testing.T) {
	now := time.Now().UTC()
	legacy := session.ToolRecord{
		Time: now, StartedAt: now, CompletedAt: &now,
		RunID: "run-recovery", CallID: "stable-call", Name: "noop", Status: "completed",
	}
	sameID := model.ToolCall{
		ID: "stable-call", Name: "noop", Arguments: json.RawMessage(`{"path":"changed"}`),
	}
	if got := runRecoveredToolScenario(t, []session.ToolRecord{legacy}, 7, [][]model.ToolCall{{sameID}}); got != 0 {
		t.Fatalf("legacy exact Call ID replayed %d times", got)
	}
	differentID := sameID
	differentID.ID = "new-call"
	if got := runRecoveredToolScenario(t, []session.ToolRecord{legacy}, 7, [][]model.ToolCall{{differentID}}); got != 1 {
		t.Fatalf("legacy empty digest incorrectly matched arguments; executions=%d", got)
	}
}

func TestCorruptToolRecordPreventsRecoveryBeforeToolExecution(t *testing.T) {
	provider := &scriptedProvider{fn: func(_ int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		return model.GenerateResponse{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID: "call-after-restart", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
			}}},
		}, nil
	}}
	runner, value := newRunner(t, provider, 2, 5*time.Second, agentTool{})
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
	value.Turns = 2
	run.EndTurn = 2
	run.Status = session.RunInterrupted
	completed := model.ToolCall{
		ID: "call-before-restart", Name: "noop", Arguments: json.RawMessage(`{"path":"same"}`),
	}
	value.ToolCalls = []session.ToolRecord{completedRecord(t, run.ID, completed, 2)}
	if err = runner.Store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	value.ToolCalls[0].ArgsDigest = strings.Repeat("A", 64)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(value.Workspace, ".gohermit", "sessions", value.ID, "session.json")
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.NewStore(value.Workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Recover(context.Background(), value.ID); err == nil {
		t.Fatal("corrupt ToolRecord must prevent recovery")
	}
	if registered.executions.Load() != 0 {
		t.Fatalf("tool executed despite corrupt recovery evidence: %d", registered.executions.Load())
	}
}

func completedRecord(t *testing.T, runID string, call model.ToolCall, turn int) session.ToolRecord {
	t.Helper()
	digest, err := toolCallDigest(call)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Second)
	completed := started.Add(time.Millisecond)
	return session.ToolRecord{
		Time: started, StartedAt: started, CompletedAt: &completed,
		RunID: runID, Turn: turn, CallID: call.ID, Name: call.Name,
		ArgsDigest: digest, Status: "completed",
	}
}

func runRecoveredToolScenario(
	t *testing.T,
	records []session.ToolRecord,
	frontier int,
	responses [][]model.ToolCall,
) int32 {
	t.Helper()
	provider := &scriptedProvider{fn: func(call int, _ model.GenerateRequest) (model.GenerateResponse, error) {
		if call < len(responses) {
			return model.GenerateResponse{
				Message: model.Message{Role: model.RoleAssistant, ToolCalls: responses[call]},
			}, nil
		}
		return model.GenerateResponse{
			Message: model.Message{Role: model.RoleAssistant, Content: "Done without unsafe replay."},
		}, nil
	}}
	runner, value := newRunner(t, provider, 4, 5*time.Second, agentTool{})
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
	value.Turns = frontier
	run.Status = session.RunInterrupted
	run.EndTurn = frontier
	run.UpdatedAt = time.Now().UTC()
	value.ToolCalls = records
	if err = runner.Run(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if run.Status != session.RunCompleted {
		t.Fatalf("resumed Run status = %s", run.Status)
	}
	return registered.executions.Load()
}
