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

func TestEmployeeSkillAPIs(t *testing.T) {
	root := t.TempDir()
	writeWebAdapter(t, root, "review")
	t.Setenv("GOHERMIT_SKILL_CATALOG", root)
	server, workspace, _ := newEmployeeTestServer(t)
	handler := server.Handler()

	response := requestJSON(t, handler, http.MethodGet, "/api/skills", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("list Skills status %d: %s", response.Code, response.Body.String())
	}
	var catalog struct {
		Skills []controlplane.SkillCatalogItem `json:"skills"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil || len(catalog.Skills) != 1 {
		t.Fatalf("catalog = %#v, %v", catalog, err)
	}
	create := controlplane.EmployeeInput{
		Employee: webEmployeeDraft("employee-skills"),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-api", Label: "Current workspace", WorkspaceRealPath: workspace,
			ReadAllowed: true, MutationAllowed: true, AllowedToolCapabilities: []string{"read", "write"},
		}},
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees", create, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", response.Code, response.Body.String())
	}
	var record employeestore.Record
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	binding := employee.SkillBinding{
		SkillID: catalog.Skills[0].SkillID, Version: catalog.Skills[0].Version,
		Digest: catalog.Skills[0].Digest, Configuration: json.RawMessage(`{}`), Enabled: true,
	}
	update := controlplane.EmployeeSkillsUpdateInput{ExpectedRevision: record.Employee.Revision, Bindings: []employee.SkillBinding{binding}}
	response = requestJSON(t, handler, http.MethodPut, "/api/employees/employee-skills/skills", update, "")
	if response.Code != http.StatusOK {
		t.Fatalf("update Skills status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-skills/skills", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"current"`)) {
		t.Fatalf("get Skills status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPut, "/api/employees/employee-skills/skills", update, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("stale revision status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/missing/skills", nil, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing Employee status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/bad%25id/skills", nil, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid Employee ID status %d: %s", response.Code, response.Body.String())
	}
}

func TestEmployeeSkillAPIStrictBoundedAndSameOrigin(t *testing.T) {
	root := t.TempDir()
	writeWebAdapter(t, root, "review")
	t.Setenv("GOHERMIT_SKILL_CATALOG", root)
	server, _, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	response := requestJSON(t, handler, http.MethodPut, "/api/employees/missing/skills", map[string]any{"unknown": true}, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status %d: %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPut, "/api/employees/missing/skills",
		controlplane.EmployeeSkillsUpdateInput{ExpectedRevision: 1}, "https://evil.example")
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status %d: %s", response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPut, "/api/employees/missing/skills",
		bytes.NewReader(bytes.Repeat([]byte("x"), maxSkillBindingRequestBytes+1)))
	request.Host = "gohermit.test"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSkillCatalogCorruptionMapsToInternalServerError(t *testing.T) {
	root := t.TempDir()
	writeWebAdapter(t, root, "review")
	t.Setenv("GOHERMIT_SKILL_CATALOG", root)
	server, _, _ := newEmployeeTestServer(t)
	if err := os.WriteFile(filepath.Join(root, "review", "SKILL.md"), []byte("---\nname: Broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, server.Handler(), http.MethodGet, "/api/skills", nil, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt catalog status %d: %s", response.Code, response.Body.String())
	}
}

func writeWebAdapter(t *testing.T, root, id string) {
	t.Helper()
	directory := filepath.Join(root, id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"),
		[]byte("---\nname: Review\ndescription: Review carefully.\n---\n# Instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
