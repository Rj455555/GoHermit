package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Rj455555/GoHermit/internal/controlplane"
)

func TestEmployeeTaskExecutionMutationsRequireSameOriginAndTrueEmptyBody(t *testing.T) {
	server, workspace, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	createEmployeeForTaskAPI(t, handler, workspace, "employee-a")
	created := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", taskAPIInput(), "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var task controlplane.EmployeeTaskView
	if err := json.Unmarshal(created.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"start", "resume"} {
		t.Run(operation, func(t *testing.T) {
			path := "/api/employee-tasks/" + task.ID + "/" + operation
			if response := rawTaskRequest(handler, http.MethodPost, path, []byte(`{}`), ""); response.Code != http.StatusBadRequest {
				t.Fatalf("chunked body status=%d body=%s", response.Code, response.Body.String())
			}
			if response := rawTaskRequest(handler, http.MethodPost, path, bytes.Repeat([]byte("x"), maxPhase4EmptyBodyBytes+1), ""); response.Code != http.StatusBadRequest {
				t.Fatalf("oversized chunked body status=%d body=%s", response.Code, response.Body.String())
			}
			if response := rawTaskRequest(handler, http.MethodPost, path, nil, "https://evil.example"); response.Code != http.StatusForbidden {
				t.Fatalf("same-origin status=%d body=%s", response.Code, response.Body.String())
			}
			response := rawTaskRequest(handler, http.MethodPost, "/api/employee-tasks/task-missing/"+operation, nil, "")
			if response.Code != http.StatusNotFound {
				t.Fatalf("true empty body rejected by transport: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
