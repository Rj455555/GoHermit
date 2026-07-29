package web

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/approval"
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
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", filepath.Join(root, "team-template.json"))
	t.Setenv("GOHERMIT_LOOP_STORE", filepath.Join(root, "loops"))
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

func TestPhase4ReactAssetsAndDeclaredRoutesAreServed(t *testing.T) {
	handler := testServer(t).Handler()

	rootRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	rootResponse := httptest.NewRecorder()
	handler.ServeHTTP(rootResponse, rootRequest)
	if rootResponse.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET / status = %d, want 307", rootResponse.Code)
	}
	if location := rootResponse.Header().Get("Location"); location != "/dashboard" {
		t.Fatalf("GET / Location = %q, want /dashboard", location)
	}

	for _, path := range []string{
		"/dashboard",
		"/employees",
		"/employees/employee-ada",
		"/employees/employee.v2",
		"/tasks",
		"/tasks/task-1",
		"/agent",
		"/agent/sessions/session-1",
		"/loops",
		"/loops/loop-1",
		"/loops/release.v2",
		"/loops/loop-1/invocations/invocation-1",
		"/settings",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, body = %s", path, response.Code, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("GET %s Content-Type = %q, want text/html", path, contentType)
			}
			if !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatalf("GET %s did not return the embedded React index", path)
			}
		})
	}

	index, err := assets.ReadFile("assets/dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	reference := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindSubmatch(index)
	if len(reference) != 2 {
		t.Fatalf("React index has no hashed JS reference: %q", index)
	}
	request := httptest.NewRequest(http.MethodGet, string(reference[1]), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d", reference[1], response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("GET %s Content-Type = %q, want JavaScript", reference[1], contentType)
	}
}

func TestPhase4SPAFallbackFailsClosed(t *testing.T) {
	handler := testServer(t).Handler()
	for _, path := range []string{
		"/unknown",
		"/employees/employee-ada/extra",
		"/tasks/task-1/extra",
		"/loops/loop-1/invocations/invocation-1/extra",
		"/assets/missing.js",
		"/employees.js",
		"/dist",
		"/dist/index.html",
		"/api",
		"/api/",
		"/api/unknown",
		"/../dashboard",
		"/%2e%2e/dashboard",
		"/employees%2femployee-ada",
		"/employees%5cemployee-ada",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404; body = %s", path, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatalf("GET %s returned React HTML", path)
			}
		})
	}
}

func TestPhase1ReactDistIsEmbeddedWithHashedAssets(t *testing.T) {
	index, err := assets.ReadFile("assets/dist/index.html")
	if err != nil {
		t.Fatalf("read embedded React index: %v", err)
	}

	references := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+-[A-Za-z0-9_-]{8,}\.(?:js|css))"`).FindAllSubmatch(index, -1)
	if len(references) < 2 {
		t.Fatalf("React index must reference content-hashed JS and CSS assets, got %q", index)
	}

	foundJS, foundCSS := false, false
	generated := [][]byte{index}
	for _, match := range references {
		reference := string(match[1])
		switch {
		case strings.HasSuffix(reference, ".js"):
			foundJS = true
		case strings.HasSuffix(reference, ".css"):
			foundCSS = true
		}
		content, readErr := assets.ReadFile("assets/dist" + reference)
		if readErr != nil {
			t.Fatalf("read embedded React asset %q: %v", reference, readErr)
		}
		if len(content) == 0 {
			t.Fatalf("embedded React asset %q is empty", reference)
		}
		generated = append(generated, content)
	}
	if !foundJS || !foundCSS {
		t.Fatalf("React index hashed assets: found JS=%t CSS=%t", foundJS, foundCSS)
	}

	machinePath := regexp.MustCompile(`(?i)(?:/Users/[^/\s]+/|/home/[^/\s]+/|[a-z]:\\Users\\[^\\\s]+\\)`)
	for _, content := range generated {
		if match := machinePath.Find(content); match != nil {
			t.Fatalf("embedded React output contains a machine path: %q", match)
		}
	}

	if _, err = assets.ReadFile("assets/dist/assets/phase1-missing.js"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing embedded React asset error = %v, want fs.ErrNotExist", err)
	}
}

func TestPhase4LegacyAndDistAliasesAreNotHTTPAccessible(t *testing.T) {
	handler := testServer(t).Handler()
	for _, path := range []string{
		"/dist", "/dist/", "/dist/index.html",
		"/index.html", "/app.js", "/employees.js", "/tasks.js", "/loops.js", "/styles.css",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, response.Code)
		}
	}
}

// freshStore opens a second store over the server's workspace so HTTP-level
// tests can seed and inspect durable state without touching service
// internals — the store is file-backed, so both instances see the same data.
func freshStore(t *testing.T, server *Server) *session.Store {
	t.Helper()
	fresh, err := session.NewStore(server.Workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		result[relative] = string(raw)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHiddenTeamWorkerSessionIsAbsentFromEveryPublicSessionAPI(t *testing.T) {
	const sentinel = "PRIVATE_EMPLOYEE_MEMORY_PHASE9_SENTINEL"
	server := testServer(t)
	store := freshStore(t, server)
	parent, err := session.New("parent mission", server.Workspace, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent-private-boundary"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	hidden, err := session.New("hidden worker", server.Workspace, "digest")
	if err != nil {
		t.Fatal(err)
	}
	hidden.ID = "worker-private-boundary"
	hidden.Hidden = true
	hidden.ParentSessionID = parent.ID
	hidden.ParentRunID = "parent-run"
	hidden.WorkItemID = "explore"
	hidden.Selection = session.Selection{
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "explorer",
	}
	run, err := hidden.NewRun("inspect private context")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Status = session.RunCompleted
	run.FinalMessage = sentinel
	run.CompletedAt = &now
	run.UpdatedAt = now
	hidden.ActiveRunID = ""
	hidden.RecentMessages = []model.Message{
		{Role: model.RoleAssistant, Content: sentinel},
	}
	request, err := approval.Create(approval.CreateSpec{
		RequestID: "approval-private", SessionID: hidden.ID, RunID: run.ID,
		Tool: "write_file", ResourcePaths: []string{"private.txt"},
		ArgsSummary: sentinel, ArgsPayload: `{"path":"private.txt"}`,
		PolicyFingerprint: "policy", PlanRevision: 1,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	hidden.ApprovalRequests = append(hidden.ApprovalRequests, request)
	if err = store.Save(context.Background(), hidden); err != nil {
		t.Fatal(err)
	}
	if err = store.AppendMessage(hidden.ID, session.MessageRecord{
		RunID: run.ID, Role: model.RoleAssistant, Content: sentinel, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	completed := event.New(event.TaskCompleted, hidden.ID)
	completed.RunID = run.ID
	completed.Message = sentinel
	if _, err = store.CommitDetachedEvent(context.Background(), hidden.ID, completed); err != nil {
		t.Fatal(err)
	}

	sessionRoot := filepath.Join(server.Workspace, ".gohermit", "sessions", hidden.ID)
	beforeHidden := snapshotTree(t, sessionRoot)
	beforeParent := snapshotTree(t, filepath.Join(server.Workspace, ".gohermit", "sessions", parent.ID))
	markerPath := filepath.Join(server.Workspace, "workspace-marker.txt")
	if err = os.WriteFile(markerPath, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "get", method: http.MethodGet, path: "/api/sessions/" + hidden.ID},
		{name: "events", method: http.MethodGet, path: "/api/sessions/" + hidden.ID + "/events?after=0"},
		{name: "start", method: http.MethodPost, path: "/api/sessions/" + hidden.ID + "/runs", body: `{"message":"second run"}`},
		{name: "resume", method: http.MethodPost, path: "/api/sessions/" + hidden.ID + "/runs/" + run.ID + "/resume"},
		{name: "cancel", method: http.MethodPost, path: "/api/sessions/" + hidden.ID + "/runs/" + run.ID + "/cancel"},
		{name: "plan approve", method: http.MethodPost, path: "/api/sessions/" + hidden.ID + "/runs/" + run.ID + "/approve"},
		{name: "approval list", method: http.MethodGet, path: "/api/sessions/" + hidden.ID + "/approvals?status=pending"},
		{name: "approval decide", method: http.MethodPost, path: "/api/sessions/" + hidden.ID + "/approvals/" + request.RequestID + "/decide", body: `{"decision":"approve"}`},
	}
	handler := server.Handler()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			httpRequest := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			httpRequest.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httpRequest)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), sentinel) ||
				strings.Contains(strings.ToLower(response.Body.String()), "hidden") {
				t.Fatalf("response leaked hidden Session data: %s", response.Body.String())
			}
		})
	}
	server.subscribersMu.Lock()
	subscriberCount := len(server.subscribers[hidden.ID])
	server.subscribersMu.Unlock()
	if subscriberCount != 0 {
		t.Fatalf("hidden Session SSE registered %d subscribers", subscriberCount)
	}
	if after := snapshotTree(t, sessionRoot); !reflect.DeepEqual(after, beforeHidden) {
		t.Fatal("public operations changed the hidden Session")
	}
	if after := snapshotTree(t, filepath.Join(server.Workspace, ".gohermit", "sessions", parent.ID)); !reflect.DeepEqual(after, beforeParent) {
		t.Fatal("public operations changed the parent Session")
	}
	if marker, err := os.ReadFile(markerPath); err != nil || string(marker) != "unchanged" {
		t.Fatalf("workspace marker changed: %q err=%v", marker, err)
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

	if !server.svc.TryAcquireRun() {
		t.Fatal("run gate was not free")
	}
	defer server.svc.ReleaseRun()
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
	if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/dashboard" {
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
	if err := server.svc.SaveAPIKey("deepseek", "test-secret"); err != nil {
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

	fresh := freshStore(t, server)
	if err := fresh.AppendMessage(created.ID, session.MessageRecord{RunID: "r1", Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	e := fresh.BufferEvent(created.ID, event.New(event.TaskStarted, created.ID))
	if err := fresh.Save(context.Background(), &created); err != nil {
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

// injectTeamTemplate seeds the team-template file the server's service reads
// (GOHERMIT_TEAM_TEMPLATE_STORE points into the test workspace), so the HTTP
// template endpoints exercise the real store.
func injectTeamTemplate(t *testing.T, server *Server, template *teamtemplate.Template) {
	t.Helper()
	store, err := teamtemplate.NewStore(filepath.Join(server.Workspace, "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if template != nil {
		if err := store.Save(*template); err != nil {
			t.Fatal(err)
		}
	}
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
	stored, err := server.svc.TeamTemplate()
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
	stored, err := server.svc.TeamTemplate()
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
	stored, err := server.svc.TeamTemplate()
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

var approvalTestStart = time.Now().UTC().Add(-time.Minute)

func newApprovalRequest(t *testing.T, sessionID, requestID string, created time.Time) approval.Request {
	t.Helper()
	if sessionID == "" {
		// Create validates a non-empty session; seedApprovalSession rebinds the
		// request to the real session before persisting.
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

func seedApprovalSession(t *testing.T, server *Server, requests ...approval.Request) *session.Session {
	t.Helper()
	sess, err := session.NewConversation("Approvals", server.Workspace, "digest", session.Selection{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range requests {
		requests[i].SessionID = sess.ID
	}
	sess.ApprovalRequests = requests
	if err = freshStore(t, server).Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func decideApprovalRequest(server *Server, sessionID, requestID, origin, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/approvals/"+requestID+"/decide", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func loadApprovalStatus(t *testing.T, server *Server, sessionID, requestID string) approval.Status {
	t.Helper()
	loaded, err := freshStore(t, server).Load(context.Background(), sessionID)
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

func approvalEvents(t *testing.T, server *Server, sessionID string) []event.Event {
	t.Helper()
	events, err := freshStore(t, server).Events(sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestListApprovalsFiltersByStatusAndReportsLazyExpiryWithoutPersisting(t *testing.T) {
	server := testServer(t)
	live := newApprovalRequest(t, "", "apr-live", approvalTestStart)
	stale := newApprovalRequest(t, "", "apr-stale", time.Now().UTC().Add(-20*time.Minute))
	approved := newApprovalRequest(t, "", "apr-approved", approvalTestStart)
	approved.Status = approval.Approved
	sess := seedApprovalSession(t, server, live, stale, approved)

	request := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID+"/approvals", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Approvals []approval.Request `json:"approvals"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Approvals) != 1 || body.Approvals[0].RequestID != "apr-live" {
		t.Fatalf("default filter must list live pending only: %+v", body.Approvals)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID+"/approvals?status=expired", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Approvals) != 1 || body.Approvals[0].RequestID != "apr-stale" {
		t.Fatalf("expired filter must report the lazily expired request: %+v", body.Approvals)
	}
	// The lazy expiry is reported but never persisted by a read.
	if status := loadApprovalStatus(t, server, sess.ID, "apr-stale"); status != approval.Pending {
		t.Fatalf("read mutated durable state: %s", status)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID+"/approvals?status=bogus", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown status filter=%d", response.Code)
	}
}

func TestDecideApprovalPersistsApprovedBeforeAnySubscriber(t *testing.T) {
	server := testServer(t)
	sess := seedApprovalSession(t, server, newApprovalRequest(t, "", "apr-1", approvalTestStart))
	response := decideApprovalRequest(server, sess.ID, "apr-1", "", `{"decision":"approve"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("decide status=%d body=%s", response.Code, response.Body.String())
	}
	// Durable-before-visible: with no subscriber at all, a FRESH store already
	// reads the approved checkpoint and the committed event.
	if status := loadApprovalStatus(t, server, sess.ID, "apr-1"); status != approval.Approved {
		t.Fatalf("persisted status=%s", status)
	}
	events := approvalEvents(t, server, sess.ID)
	if len(events) != 1 || events[0].Type != event.ApprovalDecided || events[0].Sequence != 1 {
		t.Fatalf("events=%+v", events)
	}
}

func TestDecideApprovalDenyPersistsDenied(t *testing.T) {
	server := testServer(t)
	sess := seedApprovalSession(t, server, newApprovalRequest(t, "", "apr-1", approvalTestStart))
	response := decideApprovalRequest(server, sess.ID, "apr-1", "", `{"decision":"deny"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("decide status=%d body=%s", response.Code, response.Body.String())
	}
	if status := loadApprovalStatus(t, server, sess.ID, "apr-1"); status != approval.Denied {
		t.Fatalf("persisted status=%s", status)
	}
}

func TestDecideApprovalAlreadyDecidedConflictsWithoutChangingState(t *testing.T) {
	server := testServer(t)
	sess := seedApprovalSession(t, server, newApprovalRequest(t, "", "apr-1", approvalTestStart))
	if response := decideApprovalRequest(server, sess.ID, "apr-1", "", `{"decision":"approve"}`); response.Code != http.StatusOK {
		t.Fatalf("first decide status=%d", response.Code)
	}
	response := decideApprovalRequest(server, sess.ID, "apr-1", "", `{"decision":"deny"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("re-decide status=%d body=%s", response.Code, response.Body.String())
	}
	if status := loadApprovalStatus(t, server, sess.ID, "apr-1"); status != approval.Approved {
		t.Fatalf("failed re-decide changed state: %s", status)
	}
	if events := approvalEvents(t, server, sess.ID); len(events) != 1 {
		t.Fatalf("failed re-decide committed an event: %+v", events)
	}
}

func TestDecideApprovalExpiredPendingBecomesExpiredAndCannotBeApproved(t *testing.T) {
	server := testServer(t)
	stale := newApprovalRequest(t, "", "apr-stale", time.Now().UTC().Add(-20*time.Minute))
	sess := seedApprovalSession(t, server, stale)
	response := decideApprovalRequest(server, sess.ID, "apr-stale", "", `{"decision":"approve"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("expired decide status=%d body=%s", response.Code, response.Body.String())
	}
	if status := loadApprovalStatus(t, server, sess.ID, "apr-stale"); status != approval.Expired {
		t.Fatalf("persisted status=%s", status)
	}
	events := approvalEvents(t, server, sess.ID)
	if len(events) != 1 || events[0].Type != event.ApprovalExpired {
		t.Fatalf("events=%+v", events)
	}
	// The expired request stays terminal: a later decision still fails.
	if response = decideApprovalRequest(server, sess.ID, "apr-stale", "", `{"decision":"approve"}`); response.Code != http.StatusConflict {
		t.Fatalf("expired terminal decide status=%d", response.Code)
	}
}

func TestDecideApprovalRejectsUnknownAndCrossSessionIDs(t *testing.T) {
	server := testServer(t)
	sess := seedApprovalSession(t, server, newApprovalRequest(t, "", "apr-1", approvalTestStart))
	other := seedApprovalSession(t, server, newApprovalRequest(t, "", "apr-other", approvalTestStart))

	if response := decideApprovalRequest(server, sess.ID, "apr-missing", "", `{"decision":"approve"}`); response.Code != http.StatusNotFound {
		t.Fatalf("unknown id status=%d", response.Code)
	}
	// A request_id that exists only in another session must not be decidable here.
	if response := decideApprovalRequest(server, sess.ID, "apr-other", "", `{"decision":"approve"}`); response.Code != http.StatusNotFound {
		t.Fatalf("cross-session id status=%d", response.Code)
	}
	if status := loadApprovalStatus(t, server, other.ID, "apr-other"); status != approval.Pending {
		t.Fatalf("cross-session attempt mutated the other session: %s", status)
	}
	if events := approvalEvents(t, server, sess.ID); len(events) != 0 {
		t.Fatalf("failed decides committed events: %+v", events)
	}
}

func TestDecideApprovalRejectsCrossOriginBadDecisionAndUnknownFields(t *testing.T) {
	server := testServer(t)
	sess := seedApprovalSession(t, server, newApprovalRequest(t, "", "apr-1", approvalTestStart))
	if response := decideApprovalRequest(server, sess.ID, "apr-1", "https://attacker.example", `{"decision":"approve"}`); response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d", response.Code)
	}
	if response := decideApprovalRequest(server, sess.ID, "apr-1", "", `{"decision":"maybe"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("bad decision status=%d", response.Code)
	}
	if response := decideApprovalRequest(server, sess.ID, "apr-1", "", `{"decision":"approve","note":"x"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d", response.Code)
	}
	if status := loadApprovalStatus(t, server, sess.ID, "apr-1"); status != approval.Pending {
		t.Fatalf("rejected requests changed state: %s", status)
	}
}
