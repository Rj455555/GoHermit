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
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type teamProvider struct {
	mu       sync.Mutex
	calls    int
	requests []model.GenerateRequest
	response string
}

func (p *teamProvider) Generate(_ context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	p.mu.Lock()
	p.calls++
	p.requests = append(p.requests, model.GenerateRequest{Model: request.Model, Messages: append([]model.Message{}, request.Messages...)})
	p.mu.Unlock()
	response := p.response
	if response == "" {
		response = `{"summary":"inspected","evidence":["workspace"]}`
	}
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: response}, FinishReason: "stop", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
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

func TestTeamWorkerEmployeeContextsAreIsolatedAndHandoffStaysPublic(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	run := func(id, memoryValue string) (string, team.Result) {
		t.Helper()
		provider := &teamProvider{}
		build := func(context.Context, string, string, RuntimeOptions) (*Runtime, error) {
			manager, managerErr := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
			if managerErr != nil {
				return nil, managerErr
			}
			return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{
				Provider: provider, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second},
				Context: manager, Store: store, Config: agent.Config{MaxTurns: 2, Timeout: time.Minute, Model: "test"},
			}}, nil
		}
		workID := "explore-" + id
		compact := employee.CompactSnapshot{
			SchemaVersion: employee.CompactSnapshotSchemaVersion,
			EmployeeID:    id, EmployeeRevision: 1, TaskID: workID,
			TaskSnapshotDigest: strings.Repeat("a", 64),
			Identity:           employee.CompactIdentity{Name: id, JobTitle: "Explorer", Charter: "Inspect bounded evidence."},
			EffectivePolicy:    employee.EffectivePolicy{AllowedCapabilities: []string{"read"}},
			Budget:             employee.BudgetPolicy{MaxModelCalls: 2, MaxTokens: 1000, TimeoutSeconds: 60},
			Project: employee.CompactProject{
				BindingID: "project-" + id, WorkspaceFingerprint: strings.Repeat("b", 64),
				ReadAllowed: true, WorkspaceSummary: "bounded workspace",
			},
			Skills: []employee.CompactSkill{}, Knowledge: []employee.CompactKnowledge{},
			Memory: []employee.CompactMemory{{
				FactID: "fact-" + id, Digest: strings.Repeat("c", 64),
				Category: "preference", Value: memoryValue, Provenance: `{"source":"owner"}`,
			}},
		}
		if err := employee.SealCompactSnapshot(&compact); err != nil {
			t.Fatal(err)
		}
		assignment := team.TeamEmployeeAssignment{
			WorkItemID: workID, Role: team.RoleExplorer, EmployeeID: id, EmployeeRevision: 1,
			EmployeeSnapshotDigest: strings.Repeat("a", 64), ProjectBindingID: "project-" + id,
			WorkspaceFingerprint: strings.Repeat("b", 64), Company: "deepseek", Access: "deepseek",
			Model: "deepseek-chat", AgentProfile: "explorer", EffectivePolicyDigest: strings.Repeat("d", 64),
			ContextDigest: compact.Digest,
		}
		if err := team.SealTeamEmployeeAssignment(&assignment); err != nil {
			t.Fatal(err)
		}
		assignmentCopy, compactCopy := assignment, compact
		worker := TeamWorker{
			Workspace: root, ParentSessionID: "parent", ParentRunID: "run", ParentStore: store, Build: build,
			WorkItemRuntimes: map[string]RoleRuntime{workID: {
				Selection:          config.RuntimeSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "explorer"},
				EmployeeAssignment: &assignmentCopy, EmployeeContext: &compactCopy,
			}},
		}
		result, err := worker.Execute(context.Background(), team.Assignment{
			MissionID: "mission", Goal: "inspect",
			WorkItem:  team.WorkItem{ID: workID, Role: team.RoleExplorer, Title: "Explore", Goal: "inspect", ExecutionSessionID: "worker-" + workID},
			MaxTokens: 1000, MaxDuration: time.Minute, Employee: &assignmentCopy,
		})
		if err != nil {
			t.Fatal(err)
		}
		var transcript strings.Builder
		for _, request := range provider.requests {
			for _, message := range request.Messages {
				transcript.WriteString(message.Content)
			}
		}
		return transcript.String(), result
	}

	aText, aResult := run("employee-a", "Employee A prefers alpha.")
	bText, bResult := run("employee-b", "Employee B prefers beta.")
	if !strings.Contains(aText, "prefers alpha") || strings.Contains(aText, "prefers beta") ||
		!strings.Contains(bText, "prefers beta") || strings.Contains(bText, "prefers alpha") {
		t.Fatalf("Employee contexts crossed: A=%q B=%q", aText, bText)
	}
	handoffs, _ := json.Marshal([]team.Handoff{aResult.Handoff, bResult.Handoff})
	if strings.Contains(string(handoffs), "prefers alpha") || strings.Contains(string(handoffs), "prefers beta") {
		t.Fatal("private Employee Memory leaked into public Handoff")
	}
}

func TestTeamWorkerRejectsProviderEchoOfPrivateMemory(t *testing.T) {
	memory := []employee.CompactMemory{{Value: "owner-private-preference"}}
	cases := []team.Handoff{
		{Summary: "echo owner-private-preference"},
		{Evidence: []string{"owner-private-preference"}},
		{Checks: []team.Check{{Summary: "owner-private-preference"}}},
		{Substeps: []team.SubstepSpec{{Goal: "owner-private-preference"}}},
		{Findings: []team.Finding{{Summary: "owner-private-preference"}}},
	}
	for index, handoff := range cases {
		if !handoffContainsPrivateMemory(handoff, memory) {
			t.Fatalf("case %d did not detect private Employee Memory", index)
		}
	}
	if handoffContainsPrivateMemory(team.Handoff{Summary: "public bounded result"}, memory) {
		t.Fatal("public handoff was incorrectly rejected")
	}
}

func TestTeamWorkerPrivateMemoryEchoPersistsOnlyInHiddenSession(t *testing.T) {
	const sentinel = "PRIVATE_EMPLOYEE_MEMORY_PHASE9_SENTINEL"
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent-private-echo"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	provider := &teamProvider{
		response: `{"summary":"PRIVATE_EMPLOYEE_MEMORY_PHASE9_SENTINEL","evidence":[]}`,
	}
	build := func(context.Context, string, string, RuntimeOptions) (*Runtime, error) {
		manager, managerErr := contextmgr.New(contextmgr.Config{
			MaxTokens: 4096, CompressionThreshold: .8,
			HardLimitThreshold: .92, ReserveOutputTokens: 512,
		})
		if managerErr != nil {
			return nil, managerErr
		}
		return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{
			Provider: provider,
			Executor: tool.Executor{
				Registry: tool.NewRegistry(), DefaultTimeout: time.Second,
			},
			Context: manager, Store: store,
			Config: agent.Config{MaxTurns: 2, Timeout: time.Minute, Model: "test"},
		}}, nil
	}
	compact := employee.CompactSnapshot{
		SchemaVersion: employee.CompactSnapshotSchemaVersion,
		EmployeeID:    "employee-private", EmployeeRevision: 1, TaskID: "explore-private",
		TaskSnapshotDigest: strings.Repeat("a", 64),
		Identity: employee.CompactIdentity{
			Name: "Private Employee", JobTitle: "Explorer", Charter: "Inspect bounded evidence.",
		},
		EffectivePolicy: employee.EffectivePolicy{AllowedCapabilities: []string{"read"}},
		Budget: employee.BudgetPolicy{
			MaxModelCalls: 2, MaxTokens: 1000, TimeoutSeconds: 60,
		},
		Project: employee.CompactProject{
			BindingID: "project-private", WorkspaceFingerprint: strings.Repeat("b", 64),
			ReadAllowed: true, WorkspaceSummary: "bounded workspace",
		},
		Skills: []employee.CompactSkill{}, Knowledge: []employee.CompactKnowledge{},
		Memory: []employee.CompactMemory{{
			FactID: "fact-private", Digest: strings.Repeat("c", 64),
			Category: "preference", Value: sentinel, Provenance: `{"source":"owner"}`,
		}},
	}
	if err = employee.SealCompactSnapshot(&compact); err != nil {
		t.Fatal(err)
	}
	employeeAssignment := team.TeamEmployeeAssignment{
		WorkItemID: "explore-private", Role: team.RoleExplorer,
		EmployeeID: "employee-private", EmployeeRevision: 1,
		EmployeeSnapshotDigest: strings.Repeat("a", 64),
		ProjectBindingID:       "project-private", WorkspaceFingerprint: strings.Repeat("b", 64),
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat",
		AgentProfile: "explorer", EffectivePolicyDigest: strings.Repeat("d", 64),
		ContextDigest: compact.Digest,
	}
	if err = team.SealTeamEmployeeAssignment(&employeeAssignment); err != nil {
		t.Fatal(err)
	}
	assignmentCopy, compactCopy := employeeAssignment, compact
	childID := "worker-private-echo"
	worker := TeamWorker{
		Workspace: root, ParentSessionID: parent.ID, ParentRunID: "parent-run",
		ParentStore: store, Build: build,
		WorkItemRuntimes: map[string]RoleRuntime{"explore-private": {
			Selection: config.RuntimeSelection{
				Company: "deepseek", Access: "deepseek",
				Model: "deepseek-chat", Agent: "explorer",
			},
			EmployeeAssignment: &assignmentCopy, EmployeeContext: &compactCopy,
		}},
	}
	result, err := worker.Execute(context.Background(), team.Assignment{
		MissionID: "mission-private", Goal: "inspect",
		WorkItem: team.WorkItem{
			ID: "explore-private", Role: team.RoleExplorer, Title: "Explore",
			Goal: "inspect", ExecutionSessionID: childID,
		},
		MaxTokens: 1000, MaxDuration: time.Minute, Employee: &assignmentCopy,
	})
	if err == nil || !strings.Contains(err.Error(), "private Employee Memory") {
		t.Fatalf("result=%+v err=%v, want private-memory fail closed", result, err)
	}
	if strings.Contains(string(mustJSON(t, result.Handoff)), sentinel) {
		t.Fatal("private Memory entered a public Handoff")
	}
	child, err := store.Load(context.Background(), childID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages(childID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(childID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{
		"checkpoint": child, "messages": messages, "events": events,
	} {
		if !strings.Contains(string(mustJSON(t, value)), sentinel) {
			t.Fatalf("%s did not prove the provider echo was persisted", name)
		}
	}
	if !child.Hidden || provider.calls == 0 {
		t.Fatalf("hidden=%t provider_calls=%d", child.Hidden, provider.calls)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
