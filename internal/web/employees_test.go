package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

func TestEmployeeAPIAndSingleWorkspaceProjects(t *testing.T) {
	server, workspace, _ := newEmployeeTestServer(t)
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
	var record employeestore.Record
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees?limit=1", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"employee-api"`)) {
		t.Fatalf("list status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-api", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("get status %d: %s", response.Code, response.Body.String())
	}
	proposed := record.Employee
	proposed.Name = "Updated API Employee"
	update := controlplane.EmployeeUpdateInput{ExpectedRevision: record.Employee.Revision, Employee: proposed, ProjectBindings: record.ProjectBindings}
	response = requestJSON(t, handler, http.MethodPut, "/api/employees/employee-api", update, "")
	if response.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, handler, http.MethodPut, "/api/employees/employee-api", update, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("stale update status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-api/dry-run", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"ready":true`)) {
		t.Fatalf("dry-run status %d: %s", response.Code, response.Body.String())
	}
	for _, transition := range []struct {
		path  string
		state employee.State
	}{
		{"disable", employee.StateDisabled},
		{"enable", employee.StateActive},
		{"archive", employee.StateArchived},
	} {
		response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-api/"+transition.path,
			controlplane.EmployeeTransitionInput{ExpectedRevision: record.Employee.Revision}, "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", transition.path, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record.Employee.State != transition.state {
			t.Fatalf("%s state = %s", transition.path, record.Employee.State)
		}
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-api/enable",
		controlplane.EmployeeTransitionInput{ExpectedRevision: record.Employee.Revision}, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("archived transition status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-api/activity", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"employee_created"`)) {
		t.Fatalf("activity status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/projects", nil, "")
	if response.Code != http.StatusOK || bytes.Count(response.Body.Bytes(), []byte(`"workspace_real_path"`)) != 1 {
		t.Fatalf("projects status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/missing", nil, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d", response.Code)
	}
}

func TestEmployeeRecordResponseKeepsCompatibilityFields(t *testing.T) {
	server, workspace, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	input := controlplane.EmployeeInput{
		Employee: webEmployeeDraft("employee-wire"),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-wire", Label: "Current workspace", WorkspaceRealPath: workspace,
			ReadAllowed: true,
		}},
	}
	created := requestJSON(t, handler, http.MethodPost, "/api/employees", input, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", created.Code, created.Body.String())
	}
	assertEmployeeRecordCompatibilityFields(t, created.Body.Bytes())

	loaded := requestJSON(t, handler, http.MethodGet, "/api/employees/employee-wire", nil, "")
	if loaded.Code != http.StatusOK {
		t.Fatalf("get status %d: %s", loaded.Code, loaded.Body.String())
	}
	assertEmployeeRecordCompatibilityFields(t, loaded.Body.Bytes())
}

func assertEmployeeRecordCompatibilityFields(t *testing.T, raw []byte) {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	rawEmployee, ok := record["employee"].(map[string]any)
	if !ok {
		t.Fatalf("employee response missing employee object: %s", raw)
	}
	if got := rawEmployee["project_count"]; got != float64(1) {
		t.Fatalf("project_count = %#v, want 1: %s", got, raw)
	}
	for _, field := range []string{"responsibilities", "behavior_boundaries", "skill_bindings", "project_binding_ids"} {
		if _, exists := rawEmployee[field]; !exists {
			t.Fatalf("employee response omitted %s: %s", field, raw)
		}
		if _, ok := rawEmployee[field].([]any); !ok {
			t.Fatalf("employee field %s = %#v, want array", field, rawEmployee[field])
		}
	}
	rawBindings, ok := record["project_bindings"].([]any)
	if !ok || len(rawBindings) != 1 {
		t.Fatalf("project_bindings = %#v, want one binding", record["project_bindings"])
	}
	binding, ok := rawBindings[0].(map[string]any)
	if !ok {
		t.Fatalf("project binding = %#v, want object", rawBindings[0])
	}
	if capabilities, ok := binding["allowed_tool_capabilities"].([]any); !ok || len(capabilities) != 0 {
		t.Fatalf("allowed_tool_capabilities = %#v, want empty array", binding["allowed_tool_capabilities"])
	}
}

func TestEmployeeAPIStrictJSONAndSameOrigin(t *testing.T) {
	server, workspace, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	response := requestJSON(t, handler, http.MethodPost, "/api/employees", map[string]any{"unknown": true}, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", response.Code)
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees", map[string]any{}, "https://evil.example")
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", response.Code)
	}
	input := controlplane.EmployeeInput{
		Employee:        webEmployeeDraft("employee-other"),
		ProjectBindings: []employee.ProjectBinding{{ID: "project-other", Label: "Other", WorkspaceRealPath: filepath.Dir(workspace), ReadAllowed: true}},
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees", input, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace status = %d: %s", response.Code, response.Body.String())
	}
	large := httptest.NewRequest(http.MethodPost, "/api/employees", bytes.NewReader(bytes.Repeat([]byte("x"), maxEmployeeRequestBytes+1)))
	large.Host = "gohermit.test"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, large)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized request status = %d", recorder.Code)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/bad%25id", nil, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid employee id status = %d: %s", response.Code, response.Body.String())
	}
}

func TestEmployeeAPICorruptStoreMapsInternal(t *testing.T) {
	server, workspace, state := newEmployeeTestServer(t)
	handler := server.Handler()
	input := controlplane.EmployeeInput{
		Employee:        webEmployeeDraft("employee-corrupt"),
		ProjectBindings: []employee.ProjectBinding{{ID: "project-corrupt", Label: "Current", WorkspaceRealPath: workspace, ReadAllowed: true}},
	}
	if response := requestJSON(t, handler, http.MethodPost, "/api/employees", input, ""); response.Code != http.StatusCreated {
		t.Fatalf("create status = %d", response.Code)
	}
	indexPath := filepath.Join(state, "employees", "index.json")
	var index map[string]any
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	index["employees"].([]any)[0].(map[string]any)["id"] = "../outside"
	raw, err = json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, handler, http.MethodGet, "/api/employees/employee-corrupt", nil, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt store status %d: %s", response.Code, response.Body.String())
	}
}

func newEmployeeTestServer(t *testing.T) (*Server, string, string) {
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
	return server, workspace, state
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
