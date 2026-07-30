package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/loop"
)

func webLoopDefinition(workspace, id string) loop.Definition {
	return loop.Definition{
		ID:                id,
		SchemaVersion:     loop.SchemaVersion,
		Name:              "Documentation maintenance",
		Description:       "Check canonical documentation for drift.",
		WorkspaceIdentity: workspace,
		Enabled:           true,
		TaskSource:        loop.TaskSource{Type: loop.TaskSourceFixedPrompt, Prompt: "Inspect canonical documentation and report drift."},
		AgentSelection:    loop.AgentSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "coding"},
		PlanMode:          loop.PlanAuto,
		VerificationRecipe: loop.VerificationRecipe{
			Checks: []loop.RecipeCheck{{ID: "docs", Command: []string{"git", "diff", "--check"}, Required: true, TimeoutSeconds: 60}},
		},
		Budget:          loop.Budget{MaxModelCalls: 8, MaxTokens: 80_000, TimeoutSeconds: 900},
		ApprovalPolicy:  loop.ApprovalPolicy{RequireForMutation: false},
		WorkspacePolicy: loop.WorkspacePolicy{ReadOnly: true, RequireCleanGit: false},
		OutputPolicy:    loop.OutputPolicy{IncludeDiff: false, MaxReportBytes: 64 << 10},
	}
}

func serveLoopJSON(t *testing.T, handler http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestLoopDefinitionResourceAPI(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	definition := webLoopDefinition(server.Workspace, "docs-loop")

	response := serveLoopJSON(t, handler, http.MethodPost, "/api/loops", definition)
	if response.Code != http.StatusCreated || response.Header().Get("Location") != "/api/loops/docs-loop" {
		t.Fatalf("create status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	var created loop.Definition
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.Name != definition.Name {
		t.Fatalf("created=%+v", created)
	}

	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/docs-loop", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"revision":1`) {
		t.Fatalf("get status=%d body=%s", response.Code, response.Body.String())
	}

	definition.Name = "Updated documentation maintenance"
	definition.Revision = 99
	response = serveLoopJSON(t, handler, http.MethodPut, "/api/loops/docs-loop", definition)
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	var updated loop.Definition
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Name != definition.Name {
		t.Fatalf("updated=%+v", updated)
	}

	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"docs-loop"`) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/missing", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEmployeeLoopExposesContractAndBoundedRuntimeProjection(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	definition := webLoopDefinition(server.Workspace, "knowledge-archive")
	definition.EmployeeID = "employee-knowledge"
	definition.Contract = loop.Contract{
		Goal:             "Archive verified knowledge.",
		Boundaries:       []string{"Keep provenance."},
		SOP:              []string{"Collect.", "Deduplicate.", "Report."},
		DefinitionOfDone: []string{"A reviewable archive exists."},
	}
	definition.Schedule = loop.Schedule{Kind: loop.ScheduleDaily, LocalTime: "02:00", Timezone: "Asia/Shanghai"}
	if response := serveLoopJSON(t, handler, http.MethodPost, "/api/loops", definition); response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}

	response := serveLoopJSON(t, handler, http.MethodGet, "/api/loops/knowledge-archive/contract.md", nil)
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "text/markdown; charset=utf-8" ||
		!strings.Contains(response.Body.String(), "## Goal") ||
		strings.Contains(response.Body.String(), "## Logs") {
		t.Fatalf("contract status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/knowledge-archive/runtime", nil)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"loop_id":"knowledge-archive"`) ||
		!strings.Contains(response.Body.String(), `"next_run_at"`) {
		t.Fatalf("runtime status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLoopAPIRejectsInvalidUnknownOversizedAndCrossOriginBodies(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	definition := webLoopDefinition(server.Workspace, "invalid-loop")
	definition.Name = ""
	response := serveLoopJSON(t, handler, http.MethodPost, "/api/loops", definition)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", response.Code, response.Body.String())
	}

	raw := `{"id":"unknown","schema_version":1,"unknown":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/loops", strings.NewReader(raw))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "unknown field") {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/loops/import", strings.NewReader(strings.Repeat("x", maxLoopBodyBytes+1)))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized status=%d body=%s", response.Code, response.Body.String())
	}

	definition = webLoopDefinition(server.Workspace, "cross-origin")
	rawBytes, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/loops", bytes.NewReader(rawBytes))
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d body=%s", response.Code, response.Body.String())
	}

	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"loops":[]`) {
		t.Fatalf("rejected requests persisted data: %s", response.Body.String())
	}
}

func TestLoopImportRejectsSecretsWithoutEchoOrOverwrite(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	definition := webLoopDefinition(server.Workspace, "imported-loop")
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/loops/import", bytes.NewReader(raw))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}

	const planted = "api_key=deadbeef00000000000000000000"
	poisoned := webLoopDefinition(server.Workspace, "poisoned-loop")
	poisoned.Description = planted
	raw, err = json.Marshal(poisoned)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/loops/import", bytes.NewReader(raw))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), planted) {
		t.Fatalf("poisoned import status=%d body=%s", response.Code, response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/poisoned-loop", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("poisoned import was persisted: %s", response.Body.String())
	}
}

func TestLoopDryRunAndUnconfiguredInvocationUseControlPlane(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	definition := webLoopDefinition(server.Workspace, "dry-loop")
	if response := serveLoopJSON(t, handler, http.MethodPost, "/api/loops", definition); response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}

	response := serveLoopJSON(t, handler, http.MethodPost, "/api/loops/dry-loop/dry-run", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ready":false`) || !strings.Contains(response.Body.String(), "credential missing") {
		t.Fatalf("dry run status=%d body=%s", response.Code, response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/sessions", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sessions":[]`) {
		t.Fatalf("dry run created a session: %s", response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/dry-loop/invocations?limit=10", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"invocations":[]`) {
		t.Fatalf("dry run created an invocation: %s", response.Body.String())
	}

	response = serveLoopJSON(t, handler, http.MethodPost, "/api/loops/dry-loop/invocations", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unconfigured start status=%d body=%s", response.Code, response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/dry-loop/invocations?limit=1", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"skipped"`) {
		t.Fatalf("failed start history status=%d body=%s", response.Code, response.Body.String())
	}
	response = serveLoopJSON(t, handler, http.MethodGet, "/api/loops/dry-loop/invocations?limit=0", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d body=%s", response.Code, response.Body.String())
	}
}
