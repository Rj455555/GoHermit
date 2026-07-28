package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestEmployeeAPIAndSingleWorkspaceProjects(t *testing.T) {
	server, workspace := newEmployeeTestServer(t)
	handler := server.Handler()
	input := controlplane.EmployeeInput{
		Employee: webEmployeeDraft("employee-api"),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-api", Label: "Current workspace", WorkspaceRealPath: workspace,
			ReadAllowed: true, MutationAllowed: true, AllowedToolCapabilities: []string{"read", "write"},
		}},
	}
	response := requestJSON(t, handler, http.MethodPost, "/api/employees", input, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees?limit=1", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"employee-api"`)) {
		t.Fatalf("list status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-api/dry-run", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"ready":true`)) {
		t.Fatalf("dry-run status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-api/activity", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"employee_created"`)) {
		t.Fatalf("activity status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/projects", nil, "")
	if response.Code != http.StatusOK || bytes.Count(response.Body.Bytes(), []byte(`"workspace_real_path"`)) != 1 {
		t.Fatalf("projects status %d: %s", response.Code, response.Body.String())
	}
}

func TestEmployeeAPIStrictJSONAndSameOrigin(t *testing.T) {
	server, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	response := requestJSON(t, handler, http.MethodPost, "/api/employees", map[string]any{"unknown": true}, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", response.Code)
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees", map[string]any{}, "https://evil.example")
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", response.Code)
	}
}

func newEmployeeTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("GOHERMIT_EMPLOYEE_STORE", filepath.Join(state, "employees"))
	t.Setenv("GOHERMIT_AUTH_STORE", filepath.Join(state, "auth.json"))
	t.Setenv("GOHERMIT_OWNER_STORE", filepath.Join(state, "owner.json"))
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", filepath.Join(state, "teams"))
	t.Setenv("GOHERMIT_LOOP_STORE", filepath.Join(state, "loops"))
	t.Setenv("DEEPSEEK_API_KEY", "test-only-not-persisted")
	configPath, err := filepath.Abs("../../configs/deepseek.toml")
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	return server, workspace
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, body any, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Host = "gohermit.test"
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func webEmployeeDraft(id string) employee.Employee {
	return employee.Employee{
		ID: id, Name: "API Employee", Avatar: employee.Avatar{Kind: employee.AvatarInitials},
		JobTitle: "Engineer", Charter: "Build bounded systems.",
		DefaultSelection:  employee.ModelSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		AgentProfile:      "coding",
		PermissionPolicy:  employee.PermissionPolicy{AllowedCapabilities: []string{"read", "write"}},
		BudgetPolicy:      employee.BudgetPolicy{MaxModelCalls: 8, MaxTokens: 100000, TimeoutSeconds: 3600},
		ConcurrencyPolicy: employee.ConcurrencyPolicy{MaxRunningTasks: 1},
		MemoryPolicy:      employee.MemoryPolicy{Promotion: employee.MemoryPromotionOwnerConfirmation},
	}
}
