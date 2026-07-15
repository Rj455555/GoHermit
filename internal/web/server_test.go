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

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
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
	messages, err := server.store.Messages(sess.ID)
	if err != nil || len(messages) != 1 || messages[0].Role != "assistant" {
		t.Fatalf("messages=%+v err=%v", messages, err)
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
		})
	}
}
