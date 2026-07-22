package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
	"github.com/Rj455555/GoHermit/internal/tool"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GOHERMIT_AUTH_STORE", filepath.Join(root, "credentials.json"))
	t.Setenv("GOHERMIT_OWNER_STORE", filepath.Join(root, "owner.json"))
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", filepath.Join(root, "team-template.json"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "missing-codex"))
	if err := os.WriteFile(filepath.Join(root, "hermit.toml"), []byte("[model]\nprovider = \"codex\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := New(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return server
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
	server := testServer(t)
	sess, _ := session.New("goal", server.Workspace, "digest")
	if err := server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	subscriber := server.subscribe(sess.ID)
	defer server.unsubscribe(sess.ID, subscriber)
	committed, err := server.commitAndPublish(sess, event.New(event.PlanUpdated, sess.ID))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case delivered := <-subscriber:
		if delivered.Sequence != committed.Sequence {
			t.Fatalf("delivered=%+v committed=%+v", delivered, committed)
		}
	case <-time.After(time.Second):
		t.Fatal("event was not published")
	}
	fresh, _ := session.NewStore(server.Workspace, ".gohermit")
	events, err := fresh.Events(sess.ID, 0)
	if err != nil || len(events) != 1 || events[0].Sequence != committed.Sequence {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestTeamRunCompletesParentSessionWithVisibleLeadHandoff(t *testing.T) {
	server := testServer(t)
	server.teamWorker = webTeamWorker{}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team task", server.Workspace, "digest", selection)
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
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	server.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)
	loaded, err := server.store.Load(context.Background(), sess.ID)
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
	messages, err := server.store.Messages(sess.ID)
	if err != nil || len(messages) != 1 || messages[0].Role != "assistant" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestLaunchTeamRunCreatesAndStreamsDurablePlan(t *testing.T) {
	server := testServer(t)
	server.teamWorker = webTeamWorker{}
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team plan", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := server.launchSessionRun(sess, "build it")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for server.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	loaded, err := server.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := loaded.Runs[len(loaded.Runs)-1]
	if run.ID != runID || run.Plan == nil || run.Plan.Status != taskplan.Completed {
		t.Fatalf("run=%+v", run)
	}
	events, err := server.store.Events(sess.ID, 0)
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
	server := testServer(t)
	server.teamWorker = webTeamWorker{}
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Review plan", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	sess.PlanMode = session.PlanReview
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	runID, err := server.launchSessionRun(sess, "修复 Codex 登录流式输出")
	if err != nil {
		t.Fatal(err)
	}
	if server.active.Load() {
		t.Fatal("review-first run occupied the workspace before approval")
	}
	loaded, err := server.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := loaded.ActiveRun()
	if run == nil || run.ID != runID || run.Status != session.RunQueued || run.PlanApproved || run.Plan == nil || !strings.Contains(run.Plan.Steps[0].Title, "Codex 登录") {
		t.Fatalf("run=%+v", run)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID+"/runs/"+runID+"/approve", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("approve status=%d body=%s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for server.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	loaded, err = server.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runs[0].Status != session.RunCompleted || !loaded.Runs[0].PlanApproved || loaded.Runs[0].PlanApprovedAt == nil {
		t.Fatalf("approved run=%+v", loaded.Runs[0])
	}
}

func TestVerifierFailureMarksLivePlanFailed(t *testing.T) {
	plan, _ := taskplan.DefaultTeam("run")
	for _, id := range []string{"explore", "build", "review", "repair"} {
		_, _ = plan.Start(id, "started")
		_, _ = plan.Complete(id, "done")
	}
	mission, _ := team.DefaultMission("mission", "run", "goal", team.DefaultBudget())
	mission.Handoffs = append(mission.Handoffs, team.Handoff{ID: "handoff-verify", WorkItemID: "verify", Role: team.RoleVerifier, Summary: "tests failed", Checks: []team.Check{{Command: "go test ./...", Passed: false, Summary: "failed"}}})
	started := team.TeamEvent{Type: team.WorkItemStarted, WorkItemID: "verify", Role: team.RoleVerifier, Message: "verify"}
	if transition, _ := runcontrol.ApplyTeamEvent(plan, started, mission); !transition.Changed {
		t.Fatal("verifier plan step did not start")
	}
	done := team.TeamEvent{Type: team.WorkItemDone, WorkItemID: "verify", Role: team.RoleVerifier, Message: "tests failed"}
	if transition, _ := runcontrol.ApplyTeamEvent(plan, done, mission); !transition.Changed || plan.Status != taskplan.Failed || plan.Current() != nil {
		t.Fatalf("plan=%+v changed=%v", plan, transition.Changed)
	}
}

func TestOwnerProfileAPI(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	request := httptest.NewRequest(http.MethodPut, "/api/owner", strings.NewReader(`{"identity":{"display_name":"Yuanxin","language":"Chinese"},"preferences":{"verification":"run tests"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Yuanxin") {
		t.Fatalf("save status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/owner/facts/macmini", strings.NewReader(`{"category":"environment","value":"macmini is the development host","confirmed":true}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "macmini") {
		t.Fatalf("fact status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/info", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"owner":{"configured":true,"display_name":"Yuanxin"}`) {
		t.Fatalf("info status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/owner/facts/macmini", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "development host") {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAPIKeySettingsControlAvailableCatalogWithoutLeakingSecret(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), `"available_companies":[{"id":"deepseek"`) {
		t.Fatal("unconfigured provider appeared in runnable catalog")
	}

	request = httptest.NewRequest(http.MethodPut, "/api/settings/providers/deepseek/api-key", strings.NewReader(`{"api_key":"secret-key"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret-key") {
		t.Fatalf("save status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/info", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), `"id":"deepseek"`) || !strings.Contains(response.Body.String(), `"configured":true`) || strings.Contains(response.Body.String(), "secret-key") {
		t.Fatalf("info did not reflect safe configured status: %s", response.Body.String())
	}
}

func TestHealthAndInfoDoNotExposeKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "top-secret")
	server := testServer(t)
	for _, path := range []string{"/api/health", "/api/info"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "top-secret") {
			t.Fatalf("%s leaked API key", path)
		}
		if path == "/api/info" && (!strings.Contains(response.Body.String(), `"openai-codex"`) || !strings.Contains(response.Body.String(), `"auth_type":"oauth_external"`)) {
			t.Fatalf("%s missing grouped provider catalog: %s", path, response.Body.String())
		}
	}
}

func TestRunRejectsCrossOriginAndConcurrentTask(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"task":"test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d", response.Code)
	}

	server.active.Store(true)
	request = httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"task":"test"}`))
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("concurrent status=%d", response.Code)
	}
}

func TestStaticIndexHasSecurityHeaders(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "GoHermit") || !strings.Contains(response.Body.String(), "nav-settings") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
}

func TestNormalizeSelectionPrefersVerifiedCodexMini(t *testing.T) {
	companies := []config.CompanyPreset{{ID: "openai", Access: []config.AccessPreset{{ID: "openai-codex", Models: []config.ModelOption{
		{ID: "gpt-5.6-sol"}, {ID: "gpt-5.4-mini"},
	}}}}}
	selection := normalizeSelection(config.RuntimeSelection{Agent: "coding"}, companies)
	if selection.Company != "openai" || selection.Access != "openai-codex" || selection.Model != "gpt-5.4-mini" {
		t.Fatalf("selection=%+v", selection)
	}
}

func TestPersistentSessionAPIAndEventReplay(t *testing.T) {
	server := testServer(t)
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	body := `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"coding"}`
	request := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "test-secret") {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created session.Session
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != session.Open || created.Selection.Model != "deepseek-chat" {
		t.Fatalf("created=%+v", created)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), created.ID) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}

	if err := server.store.AppendMessage(created.ID, session.MessageRecord{RunID: "r1", Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	e := server.store.BufferEvent(created.ID, event.New(event.TaskStarted, created.ID))
	if err := server.store.Save(context.Background(), &created); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello") {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	request = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID+"/events?after=0", nil).WithContext(ctx)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "id: "+strconv.FormatUint(e.Sequence, 10)) || !strings.Contains(response.Body.String(), "task_started") {
		t.Fatalf("events status=%d body=%s", response.Code, response.Body.String())
	}

	ctx, cancel = context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	request = httptest.NewRequest(http.MethodGet, "/api/sessions/"+created.ID+"/events?after=0", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", strconv.FormatUint(e.Sequence, 10))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "task_started") {
		t.Fatalf("Last-Event-ID replayed an acknowledged event: status=%d body=%s", response.Code, response.Body.String())
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
			server := testServer(t)
			sess, err := session.NewConversation("team", server.Workspace, "config", session.Selection{Agent: "team"})
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
			if err = server.store.Save(context.Background(), sess); err != nil {
				t.Fatal(err)
			}
			server.finishTeamCancelled(sess, run, tc.cause)
			loaded, err := server.store.Load(context.Background(), sess.ID)
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

func injectTeamTemplate(t *testing.T, server *Server, template *teamtemplate.Template) {
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
	server.teamTemplates = store
}

func createSessionRequest(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertNoSessionPersisted(t *testing.T, server *Server) {
	t.Helper()
	ids, err := server.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("rejected creation left sessions behind: %v", ids)
	}
}

func TestCreateTeamSessionRejectsRoleWithoutCredential(t *testing.T) {
	server := testServer(t)
	t.Setenv("DASHSCOPE_API_KEY", "")
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "mixed",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus"},
		},
	})
	response := createSessionRequest(t, server.Handler(), `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"team"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "builder") {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	assertNoSessionPersisted(t, server)
}

func TestCreateTeamSessionRejectsUnknownRoleModel(t *testing.T) {
	server := testServer(t)
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "unknown-model",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"reviewer": {Company: "deepseek", Access: "deepseek", Model: "no-such-model"},
		},
	})
	response := createSessionRequest(t, server.Handler(), `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"team"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "reviewer") || !strings.Contains(response.Body.String(), "no-such-model") {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	assertNoSessionPersisted(t, server)
}

func TestCreateTeamSessionWithValidTemplate(t *testing.T) {
	server := testServer(t)
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "valid",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"verifier": {Company: "deepseek", Access: "deepseek", Model: "deepseek-reasoner"},
		},
	})
	response := createSessionRequest(t, server.Handler(), `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"team"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created session.Session
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Selection.Agent != "team" {
		t.Fatalf("created=%+v", created)
	}
}

func TestCreateTeamSessionWithoutTemplateStaysBackwardCompatible(t *testing.T) {
	server := testServer(t)
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, nil)
	response := createSessionRequest(t, server.Handler(), `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"team"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateNonTeamSessionIgnoresInvalidTemplate(t *testing.T) {
	server := testServer(t)
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	store, err := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"schema_version": 99}`), 0600); err != nil {
		t.Fatal(err)
	}
	server.teamTemplates = store
	response := createSessionRequest(t, server.Handler(), `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"coding"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateTeamSessionRejectsProviderWithoutToolCalls(t *testing.T) {
	server := testServer(t)
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "no-tools",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
	})
	server.build = func(_ context.Context, workspace, _ string, _ config.RuntimeSelection, _ string, _ []config.ModelOption) (*app.Runtime, error) {
		return &app.Runtime{Workspace: workspace, Runner: &agent.Runner{Provider: noToolCallsProvider{}}}, nil
	}
	response := createSessionRequest(t, server.Handler(), `{"company":"deepseek","access":"deepseek","model":"deepseek-chat","agent":"team"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "tool calls") {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	assertNoSessionPersisted(t, server)
}

func TestTeamTemplateExportEndpoint(t *testing.T) {
	server := testServer(t)
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "exportable",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"verifier": {Company: "deepseek", Access: "deepseek", Model: "deepseek-reasoner"},
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/team-template/export", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="team-template.json"` {
		t.Fatalf("content disposition = %q", got)
	}
	stored, err := server.teamTemplates.Load()
	if err != nil {
		t.Fatal(err)
	}
	want, err := teamtemplate.Export(stored)
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.String() != string(want) {
		t.Fatalf("export body = %s, want %s", response.Body.String(), want)
	}
}

func TestTeamTemplateImportRejectsPoisonedBody(t *testing.T) {
	server := testServer(t)
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "previous",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
	})
	poisoned := `{"schema_version": 1, "name": "core api_key=abc123", "default": {"company": "deepseek", "access": "deepseek", "model": "deepseek-chat"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/team-template/import", strings.NewReader(poisoned))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "api_key=abc123") {
		t.Fatalf("error response echoed the secret: %s", response.Body.String())
	}
	stored, err := server.teamTemplates.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "previous" {
		t.Fatalf("poisoned import overwrote the store: %+v", stored)
	}
}

func TestTeamTemplateImportExportRoundTrip(t *testing.T) {
	server := testServer(t)
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "round-trip",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"verifier": {Company: "deepseek", Access: "deepseek", Model: "deepseek-reasoner"},
		},
	})
	handler := server.Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/team-template/export", nil)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, request)
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportResponse.Code, exportResponse.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/team-template/import", strings.NewReader(exportResponse.Body.String()))
	request.Header.Set("Content-Type", "application/json")
	importResponse := httptest.NewRecorder()
	handler.ServeHTTP(importResponse, request)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importResponse.Code, importResponse.Body.String())
	}
	if !strings.Contains(importResponse.Body.String(), `"name":"round-trip"`) || !strings.Contains(importResponse.Body.String(), "verifier") {
		t.Fatalf("import summary = %s", importResponse.Body.String())
	}
	stored, err := server.teamTemplates.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "round-trip" || stored.Roles["verifier"].Model != "deepseek-reasoner" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestTeamTemplateImportRejectsCrossOriginAndWrongMethod(t *testing.T) {
	server := testServer(t)
	injectTeamTemplate(t, server, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/team-template/import", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d", response.Code)
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		request = httptest.NewRequest(method, "/api/team-template/import", nil)
		response = httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s import status=%d", method, response.Code)
		}
	}
	request = httptest.NewRequest(http.MethodPost, "/api/team-template/export", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST export status=%d", response.Code)
	}
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

func waitForRun(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for server.active.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if server.active.Load() {
		t.Fatal("run did not finish in time")
	}
}

// TestTeamTemplateOverrideReachesBuilderExecutionSession proves acceptance 1:
// with a template pinning the builder to a different provider/model, the real
// TeamWorker builds the builder's runtime from the template override and the
// builder's hidden execution session records the override, while a role
// without an override keeps the session-level selection.
func TestTeamTemplateOverrideReachesBuilderExecutionSession(t *testing.T) {
	server := testServer(t)
	t.Setenv("DASHSCOPE_API_KEY", "")
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := server.credentials.SetAPIKey("alibaba", "test-secret-2"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "override",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus"},
		},
	})
	var mu sync.Mutex
	built := map[string]config.RuntimeSelection{}
	server.build = func(_ context.Context, workspace, _ string, selection config.RuntimeSelection, _ string, _ []config.ModelOption) (*app.Runtime, error) {
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
		return &app.Runtime{Workspace: workspace, Store: server.store, Runner: &agent.Runner{Provider: templateRoleProvider{}, Executor: tool.Executor{Registry: registry, DefaultTimeout: time.Second}, Context: manager, Store: server.store, Config: agent.Config{MaxTurns: 4, Timeout: time.Minute, Model: selection.Model}}}, nil
	}
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team override", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := server.launchSessionRun(sess, "build the requested change")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, server)
	loaded, err := server.store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := loaded.Runs[len(loaded.Runs)-1]
	if run.Status != session.RunCompleted {
		t.Fatalf("run=%s error=%q", run.Status, run.Error)
	}
	builderChild, err := server.store.Load(context.Background(), "worker-mission-"+runID+"-build")
	if err != nil {
		t.Fatal(err)
	}
	if !builderChild.Hidden || builderChild.Selection.Company != "alibaba" || builderChild.Selection.Access != "alibaba" || builderChild.Selection.Model != "qwen3.7-plus" || builderChild.Selection.Agent != "coding" {
		t.Fatalf("builder child selection = %+v hidden=%v, want the template override", builderChild.Selection, builderChild.Hidden)
	}
	explorerChild, err := server.store.Load(context.Background(), "worker-mission-"+runID+"-explore")
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
	server := testServer(t)
	server.teamWorker = webTeamWorker{}
	t.Setenv("DASHSCOPE_API_KEY", "")
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := server.credentials.SetAPIKey("alibaba", "test-secret-2"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "limits",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		Roles: map[string]teamtemplate.RoleSelection{
			"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus", MaxModelCalls: 5, MaxTokens: 50_000},
		},
	})
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team limits", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	runID, err := server.launchSessionRun(sess, "build it")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, server)
	loaded, err := server.store.Load(context.Background(), sess.ID)
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
	server := testServer(t)
	server.teamWorker = webTeamWorker{}
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	injectTeamTemplate(t, server, &teamtemplate.Template{
		Name:    "no-limits",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
	})
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team no limits", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err = server.launchSessionRun(sess, "build it"); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, server)
	loaded, err := server.store.Load(context.Background(), sess.ID)
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
	server := testServer(t)
	server.teamWorker = webTeamWorker{}
	if err := server.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	store, err := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"schema_version": 99}`), 0600); err != nil {
		t.Fatal(err)
	}
	server.teamTemplates = store
	selection := session.Selection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team"}
	sess, err := session.NewConversation("Team broken template", server.Workspace, "digest", selection)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if _, err = server.launchSessionRun(sess, "build it"); err == nil || !strings.Contains(err.Error(), "team template") {
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
	if err = server.store.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	server.runTeam(context.Background(), sess, run.ID, config.RuntimeSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}, "test-key", nil)
	loaded, err := server.store.Load(context.Background(), sess.ID)
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
