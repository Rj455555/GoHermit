package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
)

// countingLoopProvider is a scripted provider with a call counter (the
// no-replay witness) and an optional one-shot gate that parks the first
// Generate call so a test can inspect mid-run state.
type countingLoopProvider struct {
	mu    sync.Mutex
	calls int
	gate  <-chan struct{}
	fn    func(n int, r model.GenerateRequest) (model.GenerateResponse, error)
}

func (p *countingLoopProvider) Generate(ctx context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	p.mu.Lock()
	n := p.calls
	p.calls++
	gate := p.gate
	p.mu.Unlock()
	if gate != nil && n == 0 {
		select {
		case <-gate:
		case <-ctx.Done():
			return model.GenerateResponse{}, ctx.Err()
		}
	}
	return p.fn(n, request)
}

func (*countingLoopProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true, ToolCalls: true}
}

func (p *countingLoopProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func stopLoopResponse(string) (model.GenerateResponse, error) {
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

// fakeLoopBuild routes runtime construction through the svc.build seam with
// a scripted provider, exactly like the existing server tests; it never
// touches the network.
func fakeLoopBuild(svc *Service, provider model.Provider, builds *int64) {
	svc.build = func(_ context.Context, workspace, _ string, selection config.RuntimeSelection, _ string, _ []config.ModelOption) (*app.Runtime, error) {
		atomic.AddInt64(builds, 1)
		manager, err := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if err != nil {
			return nil, err
		}
		return &app.Runtime{
			Workspace: workspace,
			Store:     svc.store,
			Runner: &agent.Runner{
				Provider: provider,
				Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second},
				Context:  manager,
				Store:    svc.store,
				Config:   agent.Config{MaxTurns: 4, Timeout: time.Minute, Model: selection.Model},
			},
		}, nil
	}
}

// readOnlyInvocationDefinition is a docs-maintenance style definition: it
// inspects the workspace and, per its workspace policy, needs no clean git
// tree.
func readOnlyInvocationDefinition(workspace string) loop.Definition {
	definition := loopTestDefinition(workspace)
	definition.ID = "loop-ro"
	definition.Name = "docs-maintenance"
	definition.TaskSource.Prompt = "summarize the documentation"
	definition.VerificationRecipe = loop.VerificationRecipe{}
	definition.ApprovalPolicy = loop.ApprovalPolicy{}
	definition.WorkspacePolicy = loop.WorkspacePolicy{ReadOnly: true, RequireCleanGit: false}
	definition.OutputPolicy.IncludeDiff = false
	return definition
}

func newInvocationService(t *testing.T, definitions ...loop.Definition) *Service {
	t.Helper()
	svc := newTestService(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectLoopStore(t, svc, definitions...)
	return svc
}

func waitForProviderCall(t *testing.T, provider *countingLoopProvider, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for provider.callCount() < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if provider.callCount() < want {
		t.Fatalf("provider calls=%d, want at least %d", provider.callCount(), want)
	}
}

func TestStartLoopInvocationHappyPath(t *testing.T) {
	svc := newInvocationService(t)
	injectLoopStore(t, svc, readOnlyInvocationDefinition(svc.Workspace))
	provider := &countingLoopProvider{fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
		return stopLoopResponse("")
	}}
	var builds int64
	fakeLoopBuild(svc, provider, &builds)

	invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
	if err != nil {
		t.Fatalf("start err = %v", err)
	}
	if invocation.Status != loop.Attached || invocation.SessionID == "" || invocation.RunID == "" || invocation.StartedAt == nil {
		t.Fatalf("invocation=%+v", invocation)
	}
	if invocation.DefinitionRevision != 1 || invocation.TaskSnapshot != "summarize the documentation" {
		t.Fatalf("invocation=%+v", invocation)
	}
	waitForRun(t, svc)

	invocation, err = svc.GetInvocation(context.Background(), invocation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Status != loop.Completed || invocation.FinishedAt == nil {
		t.Fatalf("invocation=%+v", invocation)
	}
	// Exactly one Session and one Run exist, and the binding matches them.
	ids, err := svc.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != invocation.SessionID {
		t.Fatalf("sessions=%v binding=%q", ids, invocation.SessionID)
	}
	sess, err := svc.store.Load(context.Background(), invocation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Runs) != 1 || sess.Runs[0].ID != invocation.RunID || sess.Runs[0].Status != session.RunCompleted {
		t.Fatalf("runs=%+v", sess.Runs)
	}
	if sess.Title != "docs-maintenance" || sess.Selection.Agent != "coding" {
		t.Fatalf("session=%+v", sess)
	}
	listed, err := svc.ListInvocations(context.Background(), "loop-ro")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Status != loop.Completed || listed[0].ID != invocation.ID {
		t.Fatalf("listed=%+v", listed)
	}
	if builds == 0 || provider.callCount() == 0 {
		t.Fatalf("builds=%d calls=%d", builds, provider.callCount())
	}
}

// TestStartLoopInvocationDefinitionEditedMidRun proves failure-path 4
// (contract immutability): editing the definition while an invocation runs
// changes nothing for the in-flight invocation — its snapshot and task stay
// at the old revision — and the next invocation picks up the new revision.
func TestStartLoopInvocationDefinitionEditedMidRun(t *testing.T) {
	svc := newInvocationService(t)
	injectLoopStore(t, svc, readOnlyInvocationDefinition(svc.Workspace))
	gate := make(chan struct{})
	var mu sync.Mutex
	var tasks []string
	provider := &countingLoopProvider{gate: gate, fn: func(_ int, request model.GenerateRequest) (model.GenerateResponse, error) {
		mu.Lock()
		tasks = append(tasks, request.Messages[len(request.Messages)-1].Content)
		mu.Unlock()
		return stopLoopResponse("")
	}}
	var builds int64
	fakeLoopBuild(svc, provider, &builds)

	first, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
	if err != nil {
		t.Fatalf("start err = %v", err)
	}
	waitForProviderCall(t, provider, 1)

	// Edit the definition mid-run: the store bumps it to revision 2.
	stored, err := svc.loopStore.GetDefinition("loop-ro")
	if err != nil {
		t.Fatal(err)
	}
	stored.TaskSource.Prompt = "audit the changelog"
	if err = svc.loopStore.SaveDefinition(stored); err != nil {
		t.Fatal(err)
	}
	close(gate)
	waitForRun(t, svc)

	first, err = svc.GetInvocation(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != loop.Completed {
		t.Fatalf("first=%+v", first)
	}
	if first.DefinitionRevision != 1 || first.DefinitionSnapshot.Revision != 1 || first.TaskSnapshot != "summarize the documentation" || first.DefinitionSnapshot.TaskSource.Prompt != "summarize the documentation" {
		t.Fatalf("in-flight invocation drifted to the new revision: %+v", first)
	}

	second, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
	if err != nil {
		t.Fatalf("second start err = %v", err)
	}
	if second.DefinitionRevision != 2 || second.TaskSnapshot != "audit the changelog" {
		t.Fatalf("second=%+v", second)
	}
	waitForRun(t, svc)
	mu.Lock()
	defer mu.Unlock()
	if len(tasks) < 2 || tasks[0] != "summarize the documentation" {
		t.Fatalf("provider tasks=%v", tasks)
	}
	found := false
	for _, task := range tasks[1:] {
		if task == "audit the changelog" {
			found = true
		}
	}
	if !found {
		t.Fatalf("provider tasks=%v", tasks)
	}
}

// TestLoopInvocationCrashRecovery proves failure-path 5 (crash recovery /
// AC-06): after a restart — a fresh service and loop store over the same
// directories — reconciliation reads the bound Session/Run and never
// duplicates it or replays completed provider calls.
func TestLoopInvocationCrashRecovery(t *testing.T) {
	// freshService simulates a process restart over the same directories;
	// its build seam fails the test if reconciliation ever constructs a
	// runtime.
	freshService := func(t *testing.T, svc *Service) (*Service, *int64) {
		fresh, err := New(svc.Workspace, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		store, err := loopstore.NewStore(svc.loopStore.Dir())
		if err != nil {
			t.Fatal(err)
		}
		fresh.loopStore = store
		builds := new(int64)
		fresh.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
			atomic.AddInt64(builds, 1)
			return nil, errors.New("reconciliation must never build a runtime")
		}
		return fresh, builds
	}

	t.Run("completed run reconciles to completed without duplicates", func(t *testing.T) {
		svc := newInvocationService(t)
		injectLoopStore(t, svc, readOnlyInvocationDefinition(svc.Workspace))
		provider := &countingLoopProvider{fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
			return stopLoopResponse("")
		}}
		var builds int64
		fakeLoopBuild(svc, provider, &builds)
		invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
		if err != nil {
			t.Fatalf("start err = %v", err)
		}
		waitForRun(t, svc)
		callsBefore := provider.callCount()

		fresh, freshBuilds := freshService(t, svc)
		got, err := fresh.GetInvocation(context.Background(), invocation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != loop.Completed || got.SessionID != invocation.SessionID || got.RunID != invocation.RunID {
			t.Fatalf("got=%+v", got)
		}
		listed, err := fresh.ListInvocations(context.Background(), "loop-ro")
		if err != nil || len(listed) != 1 || listed[0].Status != loop.Completed {
			t.Fatalf("listed=%+v err=%v", listed, err)
		}
		ids, err := fresh.store.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 {
			t.Fatalf("recovery created sessions: %v", ids)
		}
		sess, err := fresh.store.Load(context.Background(), invocation.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(sess.Runs) != 1 {
			t.Fatalf("recovery created runs: %+v", sess.Runs)
		}
		if *freshBuilds != 0 || provider.callCount() != callsBefore {
			t.Fatalf("recovery replayed work: builds=%d calls %d→%d", *freshBuilds, callsBefore, provider.callCount())
		}
	})

	t.Run("interrupted run stays attached and is never replayed", func(t *testing.T) {
		svc := newInvocationService(t)
		injectLoopStore(t, svc, readOnlyInvocationDefinition(svc.Workspace))
		gate := make(chan struct{})
		provider := &countingLoopProvider{gate: gate, fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
			return stopLoopResponse("")
		}}
		var builds int64
		fakeLoopBuild(svc, provider, &builds)
		invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
		if err != nil {
			t.Fatalf("start err = %v", err)
		}
		waitForProviderCall(t, provider, 1)

		// Crash mid-run: the fresh service's recovery marks the in-flight
		// run interrupted; the invocation must stay attached and resumable.
		fresh, freshBuilds := freshService(t, svc)
		got, err := fresh.GetInvocation(context.Background(), invocation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != loop.Attached {
			t.Fatalf("interrupted invocation must stay attached: %+v", got)
		}
		ids, err := fresh.store.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 || *freshBuilds != 0 || provider.callCount() != 1 {
			t.Fatalf("sessions=%v builds=%d calls=%d", ids, *freshBuilds, provider.callCount())
		}

		// The original run finishes; reconciliation then maps it to
		// completed without replaying anything itself.
		close(gate)
		waitForRun(t, svc)
		callsAfterRun := provider.callCount()
		got, err = fresh.GetInvocation(context.Background(), invocation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != loop.Completed {
			t.Fatalf("got=%+v", got)
		}
		if provider.callCount() != callsAfterRun {
			t.Fatalf("reconciliation re-ran provider calls: %d→%d", callsAfterRun, provider.callCount())
		}
		if sess, loadErr := fresh.store.Load(context.Background(), invocation.SessionID); loadErr != nil || len(sess.Runs) != 1 {
			t.Fatalf("runs=%+v err=%v", sess.Runs, loadErr)
		}
	})
}

// TestStartLoopInvocationDirtyWorkspaceBlocked proves failure-path 6: a
// mutation definition against a dirty git workspace is blocked before any
// provider or session work happens.
func TestStartLoopInvocationDirtyWorkspaceBlocked(t *testing.T) {
	svc := newInvocationService(t)
	initGitRepo(t, svc.Workspace)
	injectLoopStore(t, svc, loopTestDefinition(svc.Workspace))
	var builds int64
	svc.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
		atomic.AddInt64(&builds, 1)
		return nil, errors.New("a blocked invocation must never build a runtime")
	}
	if err := os.WriteFile(filepath.Join(svc.Workspace, "dirty.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	invocation, err := svc.StartLoopInvocation(context.Background(), "loop-1")
	if err != nil {
		t.Fatalf("a recorded block is not an operational error: %v", err)
	}
	if invocation.Status != loop.Blocked || invocation.FailureCode != failWorkspaceNotClean || !strings.Contains(invocation.FailureSummary, "dirty") {
		t.Fatalf("invocation=%+v", invocation)
	}
	if builds != 0 {
		t.Fatalf("blocked invocation built %d runtimes", builds)
	}
	ids, err := svc.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("blocked invocation created sessions: %v", ids)
	}
	persisted, err := svc.loopStore.GetInvocation(invocation.ID)
	if err != nil || persisted.Status != loop.Blocked {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}

func TestStartLoopInvocationGates(t *testing.T) {
	setup := func(t *testing.T, mutate func(*loop.Definition)) (*Service, *int64) {
		svc := newInvocationService(t)
		definition := readOnlyInvocationDefinition(svc.Workspace)
		mutate(&definition)
		injectLoopStore(t, svc, definition)
		builds := new(int64)
		svc.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
			atomic.AddInt64(builds, 1)
			return nil, errors.New("a gated invocation must never build a runtime")
		}
		return svc, builds
	}
	startBlocked := func(t *testing.T, svc *Service) loop.Invocation {
		invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
		if err != nil {
			t.Fatalf("a recorded block is not an operational error: %v", err)
		}
		return invocation
	}

	t.Run("disabled definition is blocked", func(t *testing.T) {
		svc, builds := setup(t, func(d *loop.Definition) { d.Enabled = false })
		invocation := startBlocked(t, svc)
		if invocation.Status != loop.Blocked || invocation.FailureCode != failDefinitionDisabled {
			t.Fatalf("invocation=%+v", invocation)
		}
		if *builds != 0 {
			t.Fatalf("builds=%d", *builds)
		}
		if ids, _ := svc.store.List(); len(ids) != 0 {
			t.Fatalf("sessions=%v", ids)
		}
	})
	t.Run("workspace identity mismatch is blocked", func(t *testing.T) {
		svc, builds := setup(t, func(d *loop.Definition) { d.WorkspaceIdentity = "/somewhere/else" })
		invocation := startBlocked(t, svc)
		if invocation.Status != loop.Blocked || invocation.FailureCode != failWorkspaceMismatch {
			t.Fatalf("invocation=%+v", invocation)
		}
		if *builds != 0 {
			t.Fatalf("builds=%d", *builds)
		}
	})
	t.Run("read-only definition without clean-git requirement skips the git gate", func(t *testing.T) {
		// No git repository at all; the read-only policy must not refuse.
		svc, builds := setup(t, func(d *loop.Definition) {})
		provider := &countingLoopProvider{fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
			return stopLoopResponse("")
		}}
		fakeLoopBuild(svc, provider, builds)
		invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
		if err != nil {
			t.Fatalf("start err = %v", err)
		}
		if invocation.Status != loop.Attached {
			t.Fatalf("invocation=%+v", invocation)
		}
		waitForRun(t, svc)
	})
	t.Run("unknown loop id is not found and records nothing", func(t *testing.T) {
		svc := newInvocationService(t)
		injectLoopStore(t, svc)
		_, err := svc.StartLoopInvocation(context.Background(), "missing")
		var serviceErr *Error
		if !errors.As(err, &serviceErr) || serviceErr.Kind != KindNotFound {
			t.Fatalf("err=%v", err)
		}
		invocations, err := svc.loopStore.ListInvocations("")
		if err != nil || len(invocations) != 0 {
			t.Fatalf("invocations=%+v err=%v", invocations, err)
		}
	})
}

func TestCancelLoopInvocationPreparedSkips(t *testing.T) {
	svc := newInvocationService(t)
	injectLoopStore(t, svc, readOnlyInvocationDefinition(svc.Workspace))
	stored, err := svc.loopStore.GetDefinition("loop-ro")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := loop.NewInvocation(stored, loop.TriggerManual, stored.TaskSource.Prompt, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.loopStore.SaveInvocation(invocation); err != nil {
		t.Fatal(err)
	}
	got, err := svc.CancelLoopInvocation(context.Background(), invocation.ID)
	if err != nil {
		t.Fatalf("cancel err = %v", err)
	}
	if got.Status != loop.Skipped {
		t.Fatalf("got=%+v", got)
	}
	if ids, _ := svc.store.List(); len(ids) != 0 {
		t.Fatalf("sessions=%v", ids)
	}
}

// TestCancelLoopInvocationAttachedCancelsRun cancels an invocation whose run
// is a queued review plan: the durable cancel lands synchronously, and the
// reconciliation maps the cancelled run onto the invocation.
func TestCancelLoopInvocationAttachedCancelsRun(t *testing.T) {
	svc := newInvocationService(t)
	definition := readOnlyInvocationDefinition(svc.Workspace)
	definition.PlanMode = loop.PlanReview
	injectLoopStore(t, svc, definition)
	provider := &countingLoopProvider{fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
		return stopLoopResponse("")
	}}
	var builds int64
	fakeLoopBuild(svc, provider, &builds)

	invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro")
	if err != nil {
		t.Fatalf("start err = %v", err)
	}
	if invocation.Status != loop.Attached {
		t.Fatalf("invocation=%+v", invocation)
	}
	got, err := svc.CancelLoopInvocation(context.Background(), invocation.ID)
	if err != nil {
		t.Fatalf("cancel err = %v", err)
	}
	if got.Status != loop.Cancelled {
		t.Fatalf("got=%+v", got)
	}
	sess, err := svc.store.Load(context.Background(), invocation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Runs) != 1 || sess.Runs[0].Status != session.RunCancelled {
		t.Fatalf("runs=%+v", sess.Runs)
	}
	// Cancelling a terminal invocation is a conflict.
	if _, err = svc.CancelLoopInvocation(context.Background(), invocation.ID); err == nil {
		t.Fatal("expected a conflict cancelling a terminal invocation")
	} else {
		var serviceErr *Error
		if !errors.As(err, &serviceErr) || serviceErr.Kind != KindConflict {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestCancelLoopInvocationUnknown(t *testing.T) {
	svc := newInvocationService(t)
	injectLoopStore(t, svc)
	_, err := svc.CancelLoopInvocation(context.Background(), "missing")
	var serviceErr *Error
	if !errors.As(err, &serviceErr) || serviceErr.Kind != KindNotFound {
		t.Fatalf("err=%v", err)
	}
}
