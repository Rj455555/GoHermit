package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type teamProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *teamProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{"summary":"inspected","evidence":["workspace"]}`}, FinishReason: "stop", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}

func (*teamProvider) Capabilities() model.Capabilities { return model.Capabilities{} }

func TestTeamWorkerReusesCompletedExecutionSession(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	provider := &teamProvider{}
	build := func(context.Context, string, string, RuntimeOptions) (*Runtime, error) {
		manager, managerErr := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if managerErr != nil {
			return nil, managerErr
		}
		return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second}, Context: manager, Store: store, Config: agent.Config{MaxTurns: 2, Timeout: time.Minute, Model: "test"}}}, nil
	}
	worker := TeamWorker{Workspace: root, ParentSessionID: "parent", ParentRunID: "run", ParentStore: store, Build: build}
	assignment := team.Assignment{MissionID: "mission", Goal: "inspect", WorkItem: team.WorkItem{ID: "explore", Role: team.RoleExplorer, Title: "Explore", Goal: "inspect", ExecutionSessionID: "worker-mission-explore"}, MaxTokens: 1000, MaxDuration: time.Minute}
	first, err := worker.Execute(context.Background(), assignment)
	if err != nil || first.Handoff.Summary != "inspected" || first.Tokens != 30 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := worker.Execute(context.Background(), assignment)
	if err != nil || second.Handoff.Summary != "inspected" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls=%d, completed worker was replayed", provider.calls)
	}
}

// verifierNoCheckProvider scripts a Verifier turn that runs no tool and
// reports no checks — exactly what a real model produces for a read-only
// Team Run's Verifier per the "Verifier checks on read-only Team Runs"
// prompt guidance (prompts/coding.md), since there is nothing a deterministic
// command could check against a plain informational question.
type verifierNoCheckProvider struct{}

func (verifierNoCheckProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{"summary":"cross-checked the claims","issues":[]}`}, FinishReason: "stop", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}

func (verifierNoCheckProvider) Capabilities() model.Capabilities { return model.Capabilities{} }

// TestWorkerResultLeavesVerifierChecksEmptyWhenNoneRan is the end-to-end
// regression guard for the read-only-verification fix: workerResult used to
// force a synthetic failing Check onto any Verifier handoff with no real
// TestResults, which defeated internal/team.handoffChecksPassed's read-only
// path entirely (an owner-reported bug: "team" agent + a plain question
// against an empty workspace always failed "independent verification did not
// pass", even after that coordinator-level fix, because this layer injected
// a fake failing Check before the Handoff ever reached it). A Verifier that
// genuinely ran nothing must produce genuinely empty Checks — the mission
// layer, not this one, decides what that means.
func TestWorkerResultLeavesVerifierChecksEmptyWhenNoneRan(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	provider := verifierNoCheckProvider{}
	build := func(context.Context, string, string, RuntimeOptions) (*Runtime, error) {
		manager, managerErr := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if managerErr != nil {
			return nil, managerErr
		}
		return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second}, Context: manager, Store: store, Config: agent.Config{MaxTurns: 2, Timeout: time.Minute, Model: "test"}}}, nil
	}
	worker := TeamWorker{Workspace: root, ParentSessionID: "parent", ParentRunID: "run", ParentStore: store, Build: build}
	assignment := team.Assignment{MissionID: "mission", Goal: "hello, 你是什么模型", WorkItem: team.WorkItem{ID: "verify", Role: team.RoleVerifier, Title: "Verify", Goal: "cross-check", ExecutionSessionID: "worker-mission-verify"}, MaxTokens: 1000, MaxDuration: time.Minute}
	result, err := worker.Execute(context.Background(), assignment)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Handoff.Checks) != 0 {
		t.Fatalf("checks=%+v, want genuinely empty — no synthetic entry should be fabricated", result.Handoff.Checks)
	}
	if len(result.Handoff.Issues) != 0 {
		t.Fatalf("issues=%+v, want empty (the provider reported none)", result.Handoff.Issues)
	}
}

func TestParseWorkerHandoffReadsOptionalSubsteps(t *testing.T) {
	with := parseWorkerHandoff(`{"summary":"inspected","substeps":[{"id":"inspect_auth","title":"梳理认证流程","goal":"inspect the auth flow","role":"explorer","depends_on":["verify"]}]}`)
	if with.Summary != "inspected" || len(with.Substeps) != 1 {
		t.Fatalf("handoff=%+v", with)
	}
	substep := with.Substeps[0]
	if substep.ID != "inspect_auth" || substep.Role != team.RoleExplorer || len(substep.DependsOn) != 1 || substep.DependsOn[0] != "verify" {
		t.Fatalf("substep=%+v", substep)
	}
	without := parseWorkerHandoff(`{"summary":"inspected"}`)
	if without.Summary != "inspected" || len(without.Substeps) != 0 {
		t.Fatalf("handoff=%+v", without)
	}
}

func TestParseWorkerHandoffReadsOptionalFindings(t *testing.T) {
	with := parseWorkerHandoff(`{"summary":"reviewed","findings":[{"severity":"blocking","summary":"必须修复"},{"severity":"advisory","summary":"可选改进"}]}`)
	if with.Summary != "reviewed" || len(with.Findings) != 2 {
		t.Fatalf("handoff=%+v", with)
	}
	if with.Findings[0].Severity != team.SeverityBlocking || with.Findings[1].Severity != team.SeverityAdvisory {
		t.Fatalf("findings=%+v", with.Findings)
	}
	without := parseWorkerHandoff(`{"summary":"reviewed"}`)
	if without.Summary != "reviewed" || len(without.Findings) != 0 {
		t.Fatalf("handoff=%+v", without)
	}
}

func TestReviewerAssignmentPromptDocumentsFindingsSchema(t *testing.T) {
	reviewer, err := assignmentPrompt(team.Assignment{Goal: "goal", WorkItem: team.WorkItem{ID: "review", Role: team.RoleReviewer, Title: "Review", Goal: "review"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewer, "findings") || !strings.Contains(reviewer, "blocking") || !strings.Contains(reviewer, "advisory") {
		t.Fatalf("reviewer prompt lacks the findings severity schema: %q", reviewer)
	}
	builder, err := assignmentPrompt(team.Assignment{Goal: "goal", WorkItem: team.WorkItem{ID: "build", Role: team.RoleBuilder, Title: "Build", Goal: "implement"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(builder, "findings") {
		t.Fatalf("builder prompt must not report findings: %q", builder)
	}
}

func TestExplorerAssignmentPromptDocumentsSubstepSchema(t *testing.T) {
	explorer, err := assignmentPrompt(team.Assignment{Goal: "goal", WorkItem: team.WorkItem{ID: "explore", Role: team.RoleExplorer, Title: "Explore", Goal: "inspect"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(explorer, "substeps") || !strings.Contains(explorer, "read-only") {
		t.Fatalf("explorer prompt lacks the substep schema: %q", explorer)
	}
	builder, err := assignmentPrompt(team.Assignment{Goal: "goal", WorkItem: team.WorkItem{ID: "build", Role: team.RoleBuilder, Title: "Build", Goal: "implement"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(builder, "substeps") {
		t.Fatalf("builder prompt must not propose substeps: %q", builder)
	}
}

type failingTeamProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *failingTeamProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "noop", Arguments: json.RawMessage(`{}`)}}}, FinishReason: "tool_calls", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, Attempts: 1}, nil
	}
	return model.GenerateResponse{}, &model.ProviderError{Kind: model.ErrorUnavailable, Status: 500, Retryable: false, Attempts: 2, Message: "down"}
}

func (*failingTeamProvider) Capabilities() model.Capabilities { return model.Capabilities{} }

func TestTeamWorkerReportsPartialUsageOnChildRunFailure(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	provider := &failingTeamProvider{}
	build := func(context.Context, string, string, RuntimeOptions) (*Runtime, error) {
		manager, managerErr := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if managerErr != nil {
			return nil, managerErr
		}
		return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second}, Context: manager, Store: store, Config: agent.Config{MaxTurns: 3, Timeout: time.Minute, Model: "test"}}}, nil
	}
	worker := TeamWorker{Workspace: root, ParentSessionID: "parent", ParentRunID: "run", ParentStore: store, Build: build}
	assignment := team.Assignment{MissionID: "mission", Goal: "inspect", WorkItem: team.WorkItem{ID: "explore", Role: team.RoleExplorer, Title: "Explore", Goal: "inspect", ExecutionSessionID: "worker-mission-explore"}, MaxTokens: 1000, MaxDuration: time.Minute}
	result, err := worker.Execute(context.Background(), assignment)
	if err == nil {
		t.Fatal("expected child run failure")
	}
	if result.ModelCalls != 3 || result.Tokens != 15 {
		t.Fatalf("partial usage must report exactly what the failed run recorded: result=%+v", result)
	}
}

// TestTeamWorkerRoleOverrideSelectsTemplateRuntime: a role pinned by the team
// template runs on its own selection/credential/catalog, and its hidden
// execution session records the override — while a role without an override
// keeps the session-level inputs.
func TestTeamWorkerRoleOverrideSelectsTemplateRuntime(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	type built struct {
		selection config.RuntimeSelection
		apiKey    string
	}
	var mu sync.Mutex
	builds := map[string]built{}
	provider := &teamProvider{}
	build := func(_ context.Context, _, _ string, options RuntimeOptions) (*Runtime, error) {
		mu.Lock()
		builds[options.Selection.Agent] = built{selection: *options.Selection, apiKey: options.APIKey}
		mu.Unlock()
		manager, managerErr := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if managerErr != nil {
			return nil, managerErr
		}
		return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second}, Context: manager, Store: store, Config: agent.Config{MaxTurns: 2, Timeout: time.Minute, Model: "test"}}}, nil
	}
	worker := TeamWorker{
		Workspace: root, ParentSessionID: "parent", ParentRunID: "run", ParentStore: store, Build: build,
		Selection: config.RuntimeSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		APIKey:    "session-key",
		RoleSelections: map[string]RoleRuntime{
			"builder": {Selection: config.RuntimeSelection{Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus"}, APIKey: "builder-key"},
		},
	}
	builderAssignment := team.Assignment{MissionID: "mission", Goal: "build", WorkItem: team.WorkItem{ID: "build", Role: team.RoleBuilder, Title: "Build", Goal: "implement", ExecutionSessionID: "worker-mission-build"}, MaxTokens: 1000, MaxDuration: time.Minute}
	if _, err = worker.Execute(context.Background(), builderAssignment); err != nil {
		t.Fatal(err)
	}
	explorerAssignment := team.Assignment{MissionID: "mission", Goal: "inspect", WorkItem: team.WorkItem{ID: "explore", Role: team.RoleExplorer, Title: "Explore", Goal: "inspect", ExecutionSessionID: "worker-mission-explore"}, MaxTokens: 1000, MaxDuration: time.Minute}
	if _, err = worker.Execute(context.Background(), explorerAssignment); err != nil {
		t.Fatal(err)
	}
	if got := builds["coding"]; got.selection.Company != "alibaba" || got.selection.Model != "qwen3.7-plus" || got.apiKey != "builder-key" {
		t.Fatalf("builder runtime inputs = %+v, want the template override", got)
	}
	if got := builds["explorer"]; got.selection.Company != "deepseek" || got.selection.Model != "deepseek-chat" || got.apiKey != "session-key" {
		t.Fatalf("explorer runtime inputs = %+v, want the session-level inputs", got)
	}
	builderChild, err := store.Load(context.Background(), "worker-mission-build")
	if err != nil {
		t.Fatal(err)
	}
	if builderChild.Selection.Company != "alibaba" || builderChild.Selection.Access != "alibaba" || builderChild.Selection.Model != "qwen3.7-plus" || builderChild.Selection.Agent != "coding" {
		t.Fatalf("builder child selection = %+v, want the template override", builderChild.Selection)
	}
	explorerChild, err := store.Load(context.Background(), "worker-mission-explore")
	if err != nil {
		t.Fatal(err)
	}
	if explorerChild.Selection.Company != "deepseek" || explorerChild.Selection.Model != "deepseek-chat" || explorerChild.Selection.Agent != "explorer" {
		t.Fatalf("explorer child selection = %+v, want the session-level selection", explorerChild.Selection)
	}
}
