package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

func TestEmployeeTaskInboxAPIQueuesListsGetsAndCancelsWithoutExecution(t *testing.T) {
	server, workspace, state := newEmployeeTestServer(t)
	handler := server.Handler()
	createEmployeeForTaskAPI(t, handler, workspace, "employee-a")
	createEmployeeForTaskAPI(t, handler, workspace, "employee-b")
	before := workspaceTree(t, workspace)
	input := taskAPIInput()

	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", input, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create Task status=%d body=%s", response.Code, response.Body.String())
	}
	var created controlplane.EmployeeTaskView
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.State != employee.TaskQueued || created.SessionID != "" || created.RunID != "" {
		t.Fatalf("created Task = %#v", created)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-a/tasks?state=queued&limit=1", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(created.ID)) {
		t.Fatalf("list Task status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-b/tasks", nil, "")
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte(created.ID)) {
		t.Fatalf("cross-Employee list status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employee-tasks/"+created.ID, nil, "")
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte(`"workspace_real_path"`)) {
		t.Fatalf("get Task status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employee-tasks/"+created.ID+"/cancel", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"state":"cancelled"`)) {
		t.Fatalf("cancel Task status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employee-tasks/"+created.ID+"/cancel", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent cancel status=%d body=%s", response.Code, response.Body.String())
	}
	after := workspaceTree(t, workspace)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("Task Inbox changed workspace tree:\nbefore=%v\nafter=%v", before, after)
	}
	store, _ := employeestore.NewStore(filepath.Join(state, "employees"))
	activity, err := store.Activity("employee-a", employeestore.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range activity.Events {
		if event.Type == employeestore.ActivityTaskCreated || event.Type == employeestore.ActivityTaskCancelled {
			if event.TaskID != created.ID || event.SubjectID != "" || event.SessionID != "" || event.RunID != "" {
				t.Fatalf("unbounded Task Activity = %#v", event)
			}
		}
	}
}

func TestEmployeeTaskAPIStrictBoundedUTF8SameOriginAndEmptyCancelBody(t *testing.T) {
	server, workspace, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	createEmployeeForTaskAPI(t, handler, workspace, "employee-a")

	for name, raw := range map[string][]byte{
		"unknown":       []byte(`{"prompt":"safe","project_binding_id":"project-employee-a","policy":{"allowed_capabilities":["read"],"network_allowed":false,"budget":{"max_model_calls":1,"max_tokens":1000,"timeout_seconds":60}},"unknown":true}`),
		"multiple":      []byte(`{} {}`),
		"invalid UTF-8": append([]byte(`{"prompt":"bad`), []byte{0xff, '"', '}'}...),
		"surrogate":     []byte(`{"prompt":"\ud800","project_binding_id":"project-employee-a","policy":{"allowed_capabilities":["read"],"network_allowed":false,"budget":{"max_model_calls":1,"max_tokens":1000,"timeout_seconds":60}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			response := rawTaskRequest(handler, http.MethodPost, "/api/employees/employee-a/tasks", raw, "")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	emptyPage := requestJSON(t, handler, http.MethodGet, "/api/employees/employee-a/tasks", nil, "")
	if emptyPage.Code != http.StatusOK || !bytes.Contains(emptyPage.Body.Bytes(), []byte(`"tasks":[]`)) {
		t.Fatalf("failed request persisted a Task: status=%d body=%s", emptyPage.Code, emptyPage.Body.String())
	}
	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", taskAPIInput(), "https://evil.example")
	if response.Code != http.StatusForbidden {
		t.Fatalf("same-origin status=%d", response.Code)
	}
	oversized := rawTaskRequest(handler, http.MethodPost, "/api/employees/employee-a/tasks",
		bytes.Repeat([]byte("x"), maxEmployeeTaskRequestBytes+1), "")
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("oversized status=%d", oversized.Code)
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", taskAPIInput(), "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var task controlplane.EmployeeTaskView
	_ = json.Unmarshal(response.Body.Bytes(), &task)

	chunked := rawTaskRequest(handler, http.MethodPost, "/api/employee-tasks/"+task.ID+"/cancel", []byte(`{}`), "")
	if chunked.Code != http.StatusBadRequest {
		t.Fatalf("chunked body status=%d body=%s", chunked.Code, chunked.Body.String())
	}
	largeCancel := rawTaskRequest(handler, http.MethodPost, "/api/employee-tasks/"+task.ID+"/cancel",
		bytes.Repeat([]byte("x"), maxPhase4EmptyBodyBytes+1), "")
	if largeCancel.Code != http.StatusBadRequest {
		t.Fatalf("oversized cancel status=%d", largeCancel.Code)
	}
	empty := rawTaskRequest(handler, http.MethodPost, "/api/employee-tasks/"+task.ID+"/cancel", nil, "")
	if empty.Code != http.StatusOK {
		t.Fatalf("empty cancel status=%d body=%s", empty.Code, empty.Body.String())
	}
}

func TestEmployeeTaskAPIErrorMapping(t *testing.T) {
	server, workspace, state := newEmployeeTestServer(t)
	handler := server.Handler()
	createEmployeeForTaskAPI(t, handler, workspace, "employee-a")
	if response := requestJSON(t, handler, http.MethodGet, "/api/employee-tasks/bad%25id", nil, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid Task id status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestJSON(t, handler, http.MethodGet, "/api/employee-tasks/task-missing", nil, ""); response.Code != http.StatusNotFound {
		t.Fatalf("missing Task status=%d body=%s", response.Code, response.Body.String())
	}
	store, err := employeestore.NewStore(filepath.Join(state, "employees"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Get("employee-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Disable("employee-a", record.Employee.Revision); err != nil {
		t.Fatal(err)
	}
	if response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", taskAPIInput(), ""); response.Code != http.StatusConflict {
		t.Fatalf("disabled Employee status=%d body=%s", response.Code, response.Body.String())
	}
	record, err = store.Get("employee-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enable("employee-a", record.Employee.Revision); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", taskAPIInput(), "")
	if response.Code != http.StatusCreated {
		t.Fatal(response.Body.String())
	}
	var task controlplane.EmployeeTaskView
	_ = json.Unmarshal(response.Body.Bytes(), &task)
	taskPath := filepath.Join(state, "employees", "employee-a", "tasks", task.ID+".json")
	raw, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"snapshot_digest": "`), []byte(`"snapshot_digest": "b`), 1)
	if err := os.WriteFile(taskPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if response := requestJSON(t, handler, http.MethodGet, "/api/employee-tasks/"+task.ID, nil, ""); response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt Task status=%d body=%s", response.Code, response.Body.String())
	}
}

func createEmployeeForTaskAPI(t *testing.T, handler http.Handler, workspace, id string) {
	t.Helper()
	input := controlplane.EmployeeInput{
		Employee: webEmployeeDraft(id),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-" + id, Label: "Current", WorkspaceRealPath: workspace,
			ReadAllowed: true, MutationAllowed: true, AllowedToolCapabilities: []string{"read", "write"},
		}},
	}
	if response := requestJSON(t, handler, http.MethodPost, "/api/employees", input, ""); response.Code != http.StatusCreated {
		t.Fatalf("create Employee %s status=%d body=%s", id, response.Code, response.Body.String())
	}
}

func taskAPIInput() controlplane.EmployeeTaskCreateInput {
	return controlplane.EmployeeTaskCreateInput{
		Prompt: "Inspect the current workspace.", ProjectBindingID: "project-employee-a",
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget: employee.BudgetPolicy{
				MaxModelCalls: 1, MaxTokens: 1000, TimeoutSeconds: 60,
			},
		},
	}
}

func rawTaskRequest(handler http.Handler, method, path string, raw []byte, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Host = "gohermit.test"
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func workspaceTree(t *testing.T, root string) []string {
	t.Helper()
	var items []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative != "." {
			items = append(items, relative)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return items
}
