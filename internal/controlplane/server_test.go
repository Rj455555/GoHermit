package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
	"github.com/Rj455555/GoHermit/internal/tool"
)

// newTestService builds a service over a temp workspace with all credential
// stores redirected into it, exactly like the pre-refactor web testServer.
func newTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GOHERMIT_AUTH_STORE", filepath.Join(root, "credentials.json"))
	t.Setenv("GOHERMIT_OWNER_STORE", filepath.Join(root, "owner.json"))
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", filepath.Join(root, "team-template.json"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "missing-codex"))
	if err := os.WriteFile(filepath.Join(root, "hermit.toml"), []byte("[model]\nprovider = \"codex\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

type webTeamWorker struct{}

func (webTeamWorker) Execute(_ context.Context, assignment team.Assignment) (team.Result, error) {
	handoff := team.Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "completed " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == team.RoleVerifier {
		handoff.Checks = []team.Check{{Command: "go test ./...", Passed: true, Summary: "ok"}}
	}
	return team.Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

func TestCommitAndPublishMakesEventDurableBeforeSubscriberDelivery(t *testing.T) {
	svc := newTestService(t)
	sess, _ := session.New("goal", svc.Workspace, "digest")
	if err := svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	subscriber := make(chan event.Event, 1)
	svc.publish = func(e event.Event) { subscriber <- e }
	committed, err := svc.commitAndPublish(sess, event.New(event.PlanUpdated, sess.ID))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case delivered := <-subscriber:
		if delivered.Sequence != committed.Sequence {
			t.Fatalf("delivered=%+v committed=%+v", delivered, committed)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("event was not published")
	}
	fresh, _ := session.NewStore(svc.Workspace, ".gohermit")
	events, err := fresh.Events(sess.ID, 0)
	if err != nil || len(events) != 1 || events[0].Sequence != committed.Sequence {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestTeamRunCompletesParentSessionWithVisibleLeadHandoff(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team task", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.NewRun("build the requested change")
	if err != nil {
		t.Fatal(err)
	}
	sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	run.Plan, err = taskplan.DefaultTeam(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	svc.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveRunID != "" || loaded.Runs[0].Status != session.RunCompleted || loaded.Mission == nil || loaded.Mission.Status != team.Completed || len(loaded.TestResults) != 1 {
		t.Fatalf("loaded=%+v", loaded)
	}
	done, total := loaded.Runs[0].Plan.Progress()
	if loaded.Runs[0].Plan.Status != taskplan.Completed || done != total || total != 6 {
		t.Fatalf("plan=%+v", loaded.Runs[0].Plan)
	}
	messages, err := svc.store.Messages(sess.ID)
	if err != nil || len(messages) != 1 || messages[0].Role != "assistant" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestLaunchTeamRunCreatesAndStreamsDurablePlan(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team plan", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := svc.launchSessionRun(sess, "build it")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for svc.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := loaded.Runs[len(loaded.Runs)-1]
	if run.ID != runID || run.Plan == nil || run.Plan.Status != taskplan.Completed {
		t.Fatalf("run=%+v", run)
	}
	events, err := svc.store.Events(sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	createdSequence, updates := uint64(0), 0
	for _, runtimeEvent := range events {
		if runtimeEvent.Type == event.PlanCreated {
			createdSequence = runtimeEvent.Sequence
		}
		if runtimeEvent.Type == event.PlanUpdated {
			updates++
			if createdSequence == 0 || runtimeEvent.Sequence <= createdSequence {
				t.Fatalf("plan update preceded creation: %+v", runtimeEvent)
			}
		}
	}
	if createdSequence == 0 || updates < 6 {
		t.Fatalf("created=%d updates=%d", createdSequence, updates)
	}
}

func TestReviewPlanWaitsForApprovalBeforeTeamExecution(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Review plan", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	sess.PlanMode = session.PlanReview
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	runID, err := svc.launchSessionRun(sess, "修复 Codex 登录流式输出")
	if err != nil {
		t.Fatal(err)
	}
	if svc.active.Load() {
		t.Fatal("review-first run occupied the workspace before approval")
	}
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := loaded.ActiveRun()
	if run == nil || run.ID != runID || run.Status != session.RunQueued || run.PlanApproved || run.Plan == nil || !strings.Contains(run.Plan.Steps[0].Title, "Codex 登录") {
		t.Fatalf("run=%+v", run)
	}

	if _, err = svc.ApprovePlan(context.Background(), sess.ID, runID); err != nil {
		t.Fatalf("approve err = %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for svc.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	loaded, err = svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runs[0].Status != session.RunCompleted || !loaded.Runs[0].PlanApproved || loaded.Runs[0].PlanApprovedAt == nil {
		t.Fatalf("approved run=%+v", loaded.Runs[0])
	}
}

func TestTeamCancellationAndTimeoutHaveDistinctRecoverySemantics(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cause         error
		wantRun       session.RunStatus
		wantMission   team.Status
		wantActiveRun bool
		wantCompleted bool
	}{
		{name: "owner cancellation is terminal", cause: context.Canceled, wantRun: session.RunCancelled, wantMission: team.Cancelled, wantCompleted: true},
		{name: "timeout is resumable", cause: context.DeadlineExceeded, wantRun: session.RunInterrupted, wantMission: team.Interrupted, wantActiveRun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			sess, err := session.NewConversation("team", svc.Workspace, "config", session.Selection{Agent: "team"})
			if err != nil {
				t.Fatal(err)
			}
			run, err := sess.NewRun("build it")
			if err != nil {
				t.Fatal(err)
			}
			sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			run.Plan, err = taskplan.DefaultTeam(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = run.Plan.Start("explore", "started")
			sess.Mission.Status = team.Running
			if err = svc.store.Save(context.Background(), sess); err != nil {
				t.Fatal(err)
			}
			svc.finishTeamCancelled(sess, run, tc.cause)
			loaded, err := svc.store.Load(context.Background(), sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			gotRun := loaded.Runs[len(loaded.Runs)-1]
			if gotRun.Status != tc.wantRun || loaded.Mission.Status != tc.wantMission {
				t.Fatalf("run=%s mission=%s", gotRun.Status, loaded.Mission.Status)
			}
			if (loaded.ActiveRunID != "") != tc.wantActiveRun || (gotRun.CompletedAt != nil) != tc.wantCompleted {
				t.Fatalf("active=%q completed=%v", loaded.ActiveRunID, gotRun.CompletedAt)
			}
			if tc.wantActiveRun && gotRun.Plan.Status != taskplan.Active {
				t.Fatalf("interrupted plan=%+v", gotRun.Plan)
			}
			if !tc.wantActiveRun && gotRun.Plan.Status != taskplan.Cancelled {
				t.Fatalf("cancelled plan=%+v", gotRun.Plan)
			}
		})
	}
}

type noToolCallsProvider struct{}

func (noToolCallsProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	return model.GenerateResponse{}, nil
}

func (noToolCallsProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true}
}

func injectTeamTemplate(t *testing.T, svc *Service, template *teamtemplate.Template) {
	t.Helper()
	store, err := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if template != nil {
		if err := store.Save(*template); err != nil {
			t.Fatal(err)
		}
	}
	svc.teamTemplates = store
}

func createSession(t *testing.T, svc *Service, agent string) (*session.Session, error) {
	t.Helper()
	return svc.CreateSession(context.Background(), CreateSessionInput{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: agent})
}

func assertNoSessionPersisted(t *testing.T, svc *Service) {
	t.Helper()
	ids, err := svc.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("rejected creation left sessions behind: %v", ids)
	}
}

func TestCreateTeamSessionRejectsRoleWithoutCredential(t *testing.T) {
	svc := newTestService(t)
	t.Setenv("DASHSCOPE_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "mixed",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus"},
		},
	})
	if _, err := createSession(t, svc, "team"); err == nil || !strings.Contains(err.Error(), "builder") {
		t.Fatalf("create err = %v, want a builder failure", err)
	}
	assertNoSessionPersisted(t, svc)
}

func TestCreateTeamSessionRejectsUnknownRoleModel(t *testing.T) {
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "unknown-model",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"reviewer": {Company: "deepseek", Access: "deepseek", Model: "no-such-model"},
		},
	})
	if _, err := createSession(t, svc, "team"); err == nil || !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "no-such-model") {
		t.Fatalf("create err = %v, want a reviewer model failure", err)
	}
	assertNoSessionPersisted(t, svc)
}

func TestCreateTeamSessionWithValidTemplate(t *testing.T) {
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "valid",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"verifier": {Company: "deepseek", Access: "deepseek", Model: "deepseek-reasoner"},
		},
	})
	created, err := createSession(t, svc, "team")
	if err != nil {
		t.Fatalf("create err = %v", err)
	}
	if created.Selection.Agent != "team" {
		t.Fatalf("created=%+v", created)
	}
}

func TestCreateTeamSessionWithoutTemplateStaysBackwardCompatible(t *testing.T) {
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, nil)
	if _, err := createSession(t, svc, "team"); err != nil {
		t.Fatalf("create err = %v", err)
	}
}

func TestCreateNonTeamSessionIgnoresInvalidTemplate(t *testing.T) {
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	store, err := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"schema_version": 99}`), 0600); err != nil {
		t.Fatal(err)
	}
	svc.teamTemplates = store
	if _, err := createSession(t, svc, "coding"); err != nil {
		t.Fatalf("create err = %v", err)
	}
}

func TestCreateTeamSessionRejectsProviderWithoutToolCalls(t *testing.T) {
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "no-tools",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
	})
	svc.build = func(_ context.Context, workspace, _ string, _ config.RuntimeSelection, _ string, _ []config.ModelOption) (*app.Runtime, error) {
		return &app.Runtime{Workspace: workspace, Runner: &agent.Runner{Provider: noToolCallsProvider{}}}, nil
	}
	if _, err := createSession(t, svc, "team"); err == nil || !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("create err = %v, want a tool-calls failure", err)
	}
	assertNoSessionPersisted(t, svc)
}

// templateRoleProvider answers every role with a bounded JSON handoff; the
// verifier first runs the registered test.run tool so its handoff carries a
// passing deterministic check, like the real verification stage.
type templateRoleProvider struct{}

func (templateRoleProvider) Generate(_ context.Context, request model.GenerateRequest) (model.GenerateResponse, error) {
	verifier, answeredTool := false, false
	for _, message := range request.Messages {
		if strings.Contains(message.Content, "Your assigned role: verifier") {
			verifier = true
		}
		if message.Role == model.RoleTool {
			answeredTool = true
		}
	}
	if verifier && !answeredTool {
		return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "test.run", Arguments: json.RawMessage(`{}`)}}}, FinishReason: "tool_calls", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, Attempts: 1}, nil
	}
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{"summary":"done","evidence":["workspace"]}`}, FinishReason: "stop", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, Attempts: 1}, nil
}

func (templateRoleProvider) Capabilities() model.Capabilities {
	return model.Capabilities{ToolCalls: true}
}

type fakeTestRunTool struct{}

func (fakeTestRunTool) Definition() tool.Definition {
	return tool.Definition{Name: "test.run", Description: "fake deterministic tests", Permission: tool.PermissionExecute}
}

func (fakeTestRunTool) Execute(_ context.Context, call tool.Call) (tool.Result, error) {
	return tool.Result{CallID: call.ID, Name: call.Name, Output: "ok"}, nil
}

func waitForRun(t *testing.T, svc *Service) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for svc.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.active.Load() {
		t.Fatal("run did not finish in time")
	}
}

// TestTeamTemplateOverrideReachesBuilderExecutionSession proves acceptance 1:
// with a template pinning the builder to a different provider/model, the real
// TeamWorker builds the builder's runtime from the template override and the
// builder's hidden execution session records the override, while a role
// without an override keeps the session-level selection.
func TestTeamTemplateOverrideReachesBuilderExecutionSession(t *testing.T) {
	svc := newTestService(t)
	t.Setenv("DASHSCOPE_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := svc.credentials.SetAPIKey("alibaba", "test-secret-2"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "override",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus"},
		},
	})
	var mu sync.Mutex
	built := map[string]config.RuntimeSelection{}
	svc.build = func(_ context.Context, workspace, _ string, selection config.RuntimeSelection, _ string, _ []config.ModelOption) (*app.Runtime, error) {
		mu.Lock()
		built[selection.Agent] = selection
		mu.Unlock()
		manager, err := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if err != nil {
			return nil, err
		}
		registry := tool.NewRegistry()
		if err := registry.Register(fakeTestRunTool{}); err != nil {
			return nil, err
		}
		return &app.Runtime{Workspace: workspace, Store: svc.store, Runner: &agent.Runner{Provider: templateRoleProvider{}, Executor: tool.Executor{Registry: registry, DefaultTimeout: time.Second}, Context: manager, Store: svc.store, Config: agent.Config{MaxTurns: 4, Timeout: time.Minute, Model: selection.Model}}}, nil
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team override", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := svc.launchSessionRun(sess, "build the requested change")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc)
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := loaded.Runs[len(loaded.Runs)-1]
	if run.Status != session.RunCompleted {
		t.Fatalf("run=%s error=%q", run.Status, run.Error)
	}
	builderChild, err := svc.store.Load(context.Background(), "worker-mission-"+runID+"-build")
	if err != nil {
		t.Fatal(err)
	}
	if !builderChild.Hidden || builderChild.Selection.Company != "alibaba" || builderChild.Selection.Access != "alibaba" || builderChild.Selection.Model != "qwen3.7-plus" || builderChild.Selection.Agent != "coding" {
		t.Fatalf("builder child selection = %+v hidden=%v, want the template override", builderChild.Selection, builderChild.Hidden)
	}
	explorerChild, err := svc.store.Load(context.Background(), "worker-mission-"+runID+"-explore")
	if err != nil {
		t.Fatal(err)
	}
	if explorerChild.Selection.Company != "deepseek" || explorerChild.Selection.Access != "deepseek" || explorerChild.Selection.Model != "deepseek-chat" || explorerChild.Selection.Agent != "explorer" {
		t.Fatalf("explorer child selection = %+v, want the session-level selection", explorerChild.Selection)
	}
	if got := built["coding"]; got.Company != "alibaba" || got.Model != "qwen3.7-plus" {
		t.Fatalf("builder runtime built with %+v, want the template override", got)
	}
}

// TestTeamTemplateLimitsReachMissionBudget proves acceptance 2: per-role
// limits from the template land on the launched mission's Budget.RoleLimits.
func TestTeamTemplateLimitsReachMissionBudget(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	t.Setenv("DASHSCOPE_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := svc.credentials.SetAPIKey("alibaba", "test-secret-2"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "limits",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus", MaxModelCalls: 5, MaxTokens: 50_000},
		},
	})
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team limits", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := svc.launchSessionRun(sess, "build it")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc)
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mission == nil || loaded.Mission.RunID != runID {
		t.Fatalf("mission=%+v", loaded.Mission)
	}
	limits := loaded.Mission.Budget.RoleLimits
	if len(limits) != 1 || limits[team.RoleBuilder] != (team.Usage{ModelCalls: 5, Tokens: 50_000}) {
		t.Fatalf("role_limits=%+v, want only the builder limit from the template", limits)
	}
	if run := loaded.Runs[len(loaded.Runs)-1]; run.Status != session.RunCompleted {
		t.Fatalf("run=%s error=%q", run.Status, run.Error)
	}
}

// TestTeamTemplateWithoutLimitsKeepsDefaultBudget: a non-empty template
// without limits leaves Budget.RoleLimits nil, exactly like DefaultBudget.
func TestTeamTemplateWithoutLimitsKeepsDefaultBudget(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "no-limits",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
	})
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team no limits", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.launchSessionRun(sess, "build it"); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc)
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mission == nil || loaded.Mission.Budget.RoleLimits != nil {
		t.Fatalf("template without limits must keep RoleLimits nil: %+v", loaded.Mission.Budget)
	}
	if run := loaded.Runs[len(loaded.Runs)-1]; run.Status != session.RunCompleted {
		t.Fatalf("run=%s error=%q", run.Status, run.Error)
	}
}

// TestTeamRunFailsClosedWhenTemplateUnloadable: a template that cannot be
// loaded fails the launch and, at run time, fails the launched run through
// the bounded failLaunchedRun path instead of running without the template.
func TestTeamRunFailsClosedWhenTemplateUnloadable(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	store, err := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"schema_version": 99}`), 0600); err != nil {
		t.Fatal(err)
	}
	svc.teamTemplates = store
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team broken template", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.launchSessionRun(sess, "build it"); err == nil || !strings.Contains(err.Error(), "team template") {
		t.Fatalf("launch err = %v, want a team template failure", err)
	}

	// A launched run (e.g. approved review plan resumed later) fails closed too.
	run := sess.ActiveRun()
	if run == nil {
		t.Fatal("launch left no active run behind")
	}
	sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	run.Plan, err = taskplan.DefaultTeam(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	svc.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)
	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed := loaded.Runs[len(loaded.Runs)-1]
	if failed.Status != session.RunFailed || !strings.Contains(failed.Error, "team template") {
		t.Fatalf("run=%s error=%q, want a bounded team template failure", failed.Status, failed.Error)
	}
	if loaded.Mission.Status == team.Running || loaded.Mission.Status == team.Completed {
		t.Fatalf("mission must not have run: %s", loaded.Mission.Status)
	}
}

var approvalTestStart = time.Now().UTC().Add(-time.Minute)

func newApprovalRequest(t *testing.T, sessionID, requestID string, created time.Time) approval.Request {
	t.Helper()
	if sessionID == "" {
		// Create validates a non-empty session; the caller rebinds the request
		// to the real session before persisting.
		sessionID = "session-unassigned"
	}
	req, err := approval.Create(approval.CreateSpec{
		RequestID: requestID, SessionID: sessionID, RunID: "run-1", Tool: "shell",
		ResourcePaths: []string{"src/main.go"}, ArgsSummary: "go build ./...",
		ArgsPayload: `{"command":"go build ./..."}`, PolicyFingerprint: "fp-1", PlanRevision: 1,
	}, created)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func loadApprovalStatus(t *testing.T, svc *Service, sessionID, requestID string) approval.Status {
	t.Helper()
	fresh, err := session.NewStore(svc.Workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fresh.Load(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range loaded.ApprovalRequests {
		if req.RequestID == requestID {
			return req.Status
		}
	}
	t.Fatalf("request %q missing from fresh store", requestID)
	return ""
}

func approvalEvents(t *testing.T, svc *Service, sessionID string) []event.Event {
	t.Helper()
	fresh, err := session.NewStore(svc.Workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	events, err := fresh.Events(sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// newRunApproval builds a live pending request bound to a specific run and
// plan revision.
func newRunApproval(t *testing.T, runID, requestID string, revision int) approval.Request {
	t.Helper()
	req := newApprovalRequest(t, "", requestID, approvalTestStart)
	req.RunID, req.PlanRevision = runID, revision
	return req
}

func terminalApproval(req approval.Request, status approval.Status) approval.Request {
	req.Status = status
	return req
}

func approvalExpiredEvents(t *testing.T, svc *Service, sessionID string) []event.Event {
	t.Helper()
	var expired []event.Event
	for _, runtimeEvent := range approvalEvents(t, svc, sessionID) {
		if runtimeEvent.Type == event.ApprovalExpired {
			expired = append(expired, runtimeEvent)
		}
	}
	return expired
}

// assertApprovalEventPayload proves the durable event carries exactly the
// bounded C2 payload: request id, tool, status — never arguments.
func assertApprovalEventPayload(t *testing.T, runtimeEvent event.Event, requestID string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(runtimeEvent.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 3 || payload["request_id"] != requestID || payload["tool"] != "shell" || payload["status"] != string(approval.Expired) {
		t.Fatalf("payload=%v", payload)
	}
}

func assertApprovalStatuses(t *testing.T, svc *Service, sessionID string, want map[string]approval.Status) {
	t.Helper()
	for id, status := range want {
		if got := loadApprovalStatus(t, svc, sessionID, id); got != status {
			t.Fatalf("%s status=%s want %s", id, got, status)
		}
	}
}

// TestCancelQueuedReviewRunExpiresItsPendingApprovals drives the real cancel
// operation against a queued review-plan run: every pending request of that
// run expires at the transition (well before its TTL), the approval_expired
// events are durable from a fresh store, and terminal or other-run requests
// are untouched.
func TestCancelQueuedReviewRunExpiresItsPendingApprovals(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Review plan approvals", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	sess.PlanMode = session.PlanReview
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := svc.launchSessionRun(sess, "build it")
	if err != nil {
		t.Fatal(err)
	}
	if svc.active.Load() {
		t.Fatal("review-first run occupied the workspace before approval")
	}
	sess.ApprovalRequests = []approval.Request{
		newRunApproval(t, runID, "apr-cancel-1", 1),
		newRunApproval(t, runID, "apr-cancel-2", 1),
		terminalApproval(newRunApproval(t, runID, "apr-cancel-approved", 1), approval.Approved),
		terminalApproval(newRunApproval(t, runID, "apr-cancel-denied", 1), approval.Denied),
		terminalApproval(newRunApproval(t, runID, "apr-cancel-consumed", 1), approval.Consumed),
		newRunApproval(t, "run-other", "apr-cancel-other", 1),
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	activeCancelled, err := svc.CancelRun(context.Background(), sess.ID, runID)
	if err != nil || activeCancelled {
		t.Fatalf("cancel active=%v err=%v", activeCancelled, err)
	}

	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-cancel-1": approval.Expired, "apr-cancel-2": approval.Expired,
		"apr-cancel-approved": approval.Approved, "apr-cancel-denied": approval.Denied,
		"apr-cancel-consumed": approval.Consumed, "apr-cancel-other": approval.Pending,
	})
	expired := approvalExpiredEvents(t, svc, sess.ID)
	if len(expired) != 2 {
		t.Fatalf("expired events=%+v", expired)
	}
	assertApprovalEventPayload(t, expired[0], "apr-cancel-1")
	assertApprovalEventPayload(t, expired[1], "apr-cancel-2")
}

// gateTeamWorker blocks the explorer work item so the test can inspect the
// durable state right after the team sink applied the first plan transition.
type gateTeamWorker struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *gateTeamWorker) Execute(_ context.Context, assignment team.Assignment) (team.Result, error) {
	if assignment.WorkItem.Role == team.RoleExplorer {
		w.once.Do(func() { close(w.started) })
		<-w.release
	}
	handoff := team.Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "completed " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == team.RoleVerifier {
		handoff.Checks = []team.Check{{Command: "go test ./...", Passed: true, Summary: "ok"}}
	}
	return team.Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

// TestPlanRevisionBumpExpiresStalePendingApprovals drives a real plan-revision
// bump through the team sink: starting the explorer moves the plan from
// revision 1 to 2, expiring the pending request recorded against revision 1
// while the revision-2 request survives.
func TestPlanRevisionBumpExpiresStalePendingApprovals(t *testing.T) {
	svc := newTestService(t)
	worker := &gateTeamWorker{started: make(chan struct{}), release: make(chan struct{})}
	svc.teamWorker = worker
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Revision approvals", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.NewRun("build it")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget()); err != nil {
		t.Fatal(err)
	}
	if run.Plan, err = taskplan.DefaultTeam(run.ID); err != nil {
		t.Fatal(err)
	}
	sess.ApprovalRequests = []approval.Request{
		newRunApproval(t, run.ID, "apr-rev-stale", 1),
		newRunApproval(t, run.ID, "apr-rev-live", 2),
		terminalApproval(newRunApproval(t, run.ID, "apr-rev-approved", 1), approval.Approved),
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)
	}()
	select {
	case <-worker.started:
	case <-time.After(30 * time.Second):
		t.Fatal("explorer never started")
	}

	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-rev-stale": approval.Expired, "apr-rev-live": approval.Pending, "apr-rev-approved": approval.Approved,
	})
	expired := approvalExpiredEvents(t, svc, sess.ID)
	if len(expired) != 1 {
		t.Fatalf("expired events=%+v", expired)
	}
	assertApprovalEventPayload(t, expired[0], "apr-rev-stale")

	close(worker.release)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("team run never finished")
	}
	// Run completion terminates the surviving pending request as well.
	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-rev-live": approval.Expired, "apr-rev-approved": approval.Approved,
	})
}

// TestLaunchExpiresApprovalsWhenPolicyFingerprintChanges changes the effective
// config between session creation and launch: the recomputed config digest no
// longer matches the session's stored digest, so pending approvals recorded
// under the old fingerprint expire before the run executes.
func TestLaunchExpiresApprovalsWhenPolicyFingerprintChanges(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	digestFor := func() string {
		runtimeSelection := config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}
		runtime, err := svc.build(context.Background(), svc.Workspace, svc.ConfigPath, runtimeSelection, "test-secret", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Close()
		return session.ConfigDigest(runtime.Config)
	}
	digestBefore := digestFor()
	sess, err := session.NewConversation("Policy approvals", svc.Workspace, digestBefore, selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(svc.Workspace, "hermit.toml"), []byte("[model]\nprovider = \"codex\"\n\n[agent]\nmax_turns = 51\n"), 0600); err != nil {
		t.Fatal(err)
	}
	digestAfter := digestFor()
	if digestAfter == digestBefore {
		t.Fatal("config change did not move the config digest")
	}
	stale := newApprovalRequest(t, "", "apr-policy-stale", approvalTestStart)
	stale.PolicyFingerprint = digestBefore
	live := newApprovalRequest(t, "", "apr-policy-live", approvalTestStart)
	live.PolicyFingerprint = digestAfter
	approved := newApprovalRequest(t, "", "apr-policy-approved", approvalTestStart)
	approved.PolicyFingerprint = digestBefore
	approved.Status = approval.Approved
	sess.ApprovalRequests = []approval.Request{stale, live, approved}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	if _, err = svc.launchSessionRun(sess, "build it"); err != nil {
		t.Fatal(err)
	}
	// The policy trigger commits synchronously before the run starts executing.
	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-policy-stale": approval.Expired, "apr-policy-live": approval.Pending, "apr-policy-approved": approval.Approved,
	})
	expired := approvalExpiredEvents(t, svc, sess.ID)
	if len(expired) != 1 {
		t.Fatalf("expired events=%+v", expired)
	}
	assertApprovalEventPayload(t, expired[0], "apr-policy-stale")

	deadline := time.Now().Add(30 * time.Second)
	for svc.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.active.Load() {
		t.Fatal("team run never finished")
	}
}

// TestTeamTerminationExpiresPendingApprovals covers the deadline/interruption
// and user-cancellation transitions: ADR 0011 treats both as termination for
// approval purposes.
func TestTeamTerminationExpiresPendingApprovals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		wantRun session.RunStatus
	}{
		{name: "user cancellation", cause: context.Canceled, wantRun: session.RunCancelled},
		{name: "deadline interruption", cause: context.DeadlineExceeded, wantRun: session.RunInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			sess, err := session.NewConversation("Termination approvals", svc.Workspace, "digest", session.Selection{Agent: "team"})
			if err != nil {
				t.Fatal(err)
			}
			run, err := sess.NewRun("build it")
			if err != nil {
				t.Fatal(err)
			}
			if sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget()); err != nil {
				t.Fatal(err)
			}
			if run.Plan, err = taskplan.DefaultTeam(run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = run.Plan.Start("explore", "started"); err != nil {
				t.Fatal(err)
			}
			sess.Mission.Status = team.Running
			sess.ApprovalRequests = []approval.Request{
				newRunApproval(t, run.ID, "apr-term-pending", 2),
				terminalApproval(newRunApproval(t, run.ID, "apr-term-approved", 2), approval.Approved),
				terminalApproval(newRunApproval(t, run.ID, "apr-term-denied", 2), approval.Denied),
				terminalApproval(newRunApproval(t, run.ID, "apr-term-consumed", 2), approval.Consumed),
				newRunApproval(t, "run-other", "apr-term-other", 2),
			}
			if err = svc.store.Save(context.Background(), sess); err != nil {
				t.Fatal(err)
			}

			svc.finishTeamCancelled(sess, run, tc.cause)

			loaded, err := svc.store.Load(context.Background(), sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := loaded.Runs[len(loaded.Runs)-1].Status; got != tc.wantRun {
				t.Fatalf("run status=%s want %s", got, tc.wantRun)
			}
			assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
				"apr-term-pending": approval.Expired, "apr-term-approved": approval.Approved,
				"apr-term-denied": approval.Denied, "apr-term-consumed": approval.Consumed,
				"apr-term-other": approval.Pending,
			})
			expired := approvalExpiredEvents(t, svc, sess.ID)
			if len(expired) != 1 {
				t.Fatalf("expired events=%+v", expired)
			}
			assertApprovalEventPayload(t, expired[0], "apr-term-pending")
		})
	}
}

// TestTeamRunCompletionExpiresPendingApprovals covers normal completion: a
// run that finishes successfully still terminates its pending approvals.
func TestTeamRunCompletionExpiresPendingApprovals(t *testing.T) {
	svc := newTestService(t)
	svc.teamWorker = webTeamWorker{}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Completion approvals", svc.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.NewRun("build it")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Mission, err = team.DefaultMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget()); err != nil {
		t.Fatal(err)
	}
	if run.Plan, err = taskplan.DefaultTeam(run.ID); err != nil {
		t.Fatal(err)
	}
	sess.ApprovalRequests = []approval.Request{
		newRunApproval(t, run.ID, "apr-done-pending", 1),
		terminalApproval(newRunApproval(t, run.ID, "apr-done-approved", 1), approval.Approved),
		newRunApproval(t, "run-other", "apr-done-other", 1),
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	svc.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)

	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runs[len(loaded.Runs)-1].Status != session.RunCompleted {
		t.Fatalf("run status=%s", loaded.Runs[len(loaded.Runs)-1].Status)
	}
	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-done-pending": approval.Expired, "apr-done-approved": approval.Approved, "apr-done-other": approval.Pending,
	})
	expired := approvalExpiredEvents(t, svc, sess.ID)
	if len(expired) != 1 {
		t.Fatalf("expired events=%+v", expired)
	}
	assertApprovalEventPayload(t, expired[0], "apr-done-pending")
}

// TestFailLaunchedRunExpiresPendingApprovals covers the failure path: a run
// that fails terminates its pending approvals through the same commit path.
func TestFailLaunchedRunExpiresPendingApprovals(t *testing.T) {
	svc := newTestService(t)
	sess, err := session.NewConversation("Failure approvals", svc.Workspace, "digest", session.Selection{Agent: "team"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.NewRun("build it")
	if err != nil {
		t.Fatal(err)
	}
	if run.Plan, err = taskplan.DefaultTeam(run.ID); err != nil {
		t.Fatal(err)
	}
	sess.ApprovalRequests = []approval.Request{
		newRunApproval(t, run.ID, "apr-fail-pending", 1),
		terminalApproval(newRunApproval(t, run.ID, "apr-fail-approved", 1), approval.Approved),
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	svc.failLaunchedRun(sess, run.ID, errors.New("boom"))

	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Runs[len(loaded.Runs)-1].Status; got != session.RunFailed {
		t.Fatalf("run status=%s", got)
	}
	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-fail-pending": approval.Expired, "apr-fail-approved": approval.Approved,
	})
	expired := approvalExpiredEvents(t, svc, sess.ID)
	if len(expired) != 1 {
		t.Fatalf("expired events=%+v", expired)
	}
	assertApprovalEventPayload(t, expired[0], "apr-fail-pending")
}

// finalAnswerProvider answers every Generate call with an immediate final
// answer, so the single-agent runner completes without any tool call.
type finalAnswerProvider struct{}

func (finalAnswerProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func (finalAnswerProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true, ToolCalls: true}
}

// TestSingleAgentRunCompletionExpiresPendingApprovals drives a real
// single-agent launch with a stubbed runtime: when the runner completes the
// run, the launch goroutine terminates the run's pending approvals.
func TestSingleAgentRunCompletionExpiresPendingApprovals(t *testing.T) {
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	conf, err := app.LoadConfig(svc.Workspace, svc.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .9, ReserveOutputTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	runner := &agent.Runner{Provider: finalAnswerProvider{}, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second}, Context: manager, Store: svc.store, Config: agent.Config{MaxTurns: 3, Timeout: 30 * time.Second, Model: "test", CheckpointEveryTurns: 5}}
	svc.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
		return &app.Runtime{Workspace: svc.Workspace, Config: conf, Store: svc.store, Runner: runner}, nil
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "coding"}
	sess, err := session.NewConversation("Single agent approvals", svc.Workspace, session.ConfigDigest(conf), selection)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.NewRun("build it")
	if err != nil {
		t.Fatal(err)
	}
	sess.ApprovalRequests = []approval.Request{
		newRunApproval(t, run.ID, "apr-single-pending", 1),
		terminalApproval(newRunApproval(t, run.ID, "apr-single-approved", 1), approval.Approved),
		terminalApproval(newRunApproval(t, run.ID, "apr-single-consumed", 1), approval.Consumed),
		newRunApproval(t, "run-other", "apr-single-other", 1),
	}
	if err = svc.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	if _, err = svc.launchSessionRun(sess, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for svc.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.active.Load() {
		t.Fatal("single-agent run never finished")
	}

	loaded, err := svc.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Runs[len(loaded.Runs)-1].Status; got != session.RunCompleted {
		t.Fatalf("run status=%s error=%q", got, loaded.Runs[len(loaded.Runs)-1].Error)
	}
	assertApprovalStatuses(t, svc, sess.ID, map[string]approval.Status{
		"apr-single-pending": approval.Expired, "apr-single-approved": approval.Approved,
		"apr-single-consumed": approval.Consumed, "apr-single-other": approval.Pending,
	})
	expired := approvalExpiredEvents(t, svc, sess.ID)
	if len(expired) != 1 {
		t.Fatalf("expired events=%+v", expired)
	}
	assertApprovalEventPayload(t, expired[0], "apr-single-pending")
}
