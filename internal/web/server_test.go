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
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GOHERMIT_AUTH_STORE", filepath.Join(root, "credentials.json"))
	t.Setenv("GOHERMIT_OWNER_STORE", filepath.Join(root, "owner.json"))
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
