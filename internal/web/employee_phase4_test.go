package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

func TestEmployeeKnowledgeAndMemoryAPI(t *testing.T) {
	server, _, state := newEmployeeTestServer(t)
	handler := server.Handler()
	create := controlplane.EmployeeInput{Employee: webEmployeeDraft("employee-phase4")}
	if response := requestJSON(t, handler, http.MethodPost, "/api/employees", create, ""); response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	source := knowledge.Source{ID: "guide", Kind: knowledge.KindManualText, Title: "Guide", ManualText: "Stable local citations."}
	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-phase4/knowledge", source, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("add Knowledge status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-phase4/knowledge?query=stable", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"citation"`)) {
		t.Fatalf("get Knowledge status=%d body=%s", response.Code, response.Body.String())
	}
	store, err := employeestore.NewStore(filepath.Join(state, "employees"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: "candidate-api", EmployeeID: "employee-phase4", Category: "preference", Value: "Prefer stable ordering.",
		Provenance: []employeememory.Provenance{{SourceType: "owner", SourceID: "owner-note", VerifiedAt: now}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddMemoryCandidate("employee-phase4", candidate); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-phase4/memory", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"facts":[]`)) {
		t.Fatalf("Candidate auto-promoted: %s", response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-phase4/memory-candidates/candidate-api/accept", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", response.Code, response.Body.String())
	}
	var fact employeememory.Fact
	if err := json.Unmarshal(response.Body.Bytes(), &fact); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, handler, http.MethodPut, "/api/employees/employee-phase4/memory/"+fact.ID,
		controlplane.EmployeeMemoryEditInput{Value: "Owner edited stable ordering."}, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"owner_edited":true`)) {
		t.Fatalf("edit status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodDelete, "/api/employees/employee-phase4/memory/"+fact.ID, nil, "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("forget status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/employee-phase4/memory", nil, "")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"facts":[]`)) {
		t.Fatalf("forgotten fact remains: %s", response.Body.String())
	}
}

func TestPhase4APIStrictBoundedSameOriginAndErrorMapping(t *testing.T) {
	server, _, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	create := controlplane.EmployeeInput{Employee: webEmployeeDraft("employee-phase4")}
	_ = requestJSON(t, handler, http.MethodPost, "/api/employees", create, "")
	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-phase4/knowledge",
		map[string]any{"unknown": true}, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("strict JSON status=%d", response.Code)
	}
	response = requestJSON(t, handler, http.MethodPost, "/api/employees/employee-phase4/knowledge",
		knowledge.Source{ID: "guide", Kind: knowledge.KindManualText, Title: "Guide", ManualText: "bounded"}, "https://evil.example")
	if response.Code != http.StatusForbidden {
		t.Fatalf("same-origin status=%d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/employees/employee-phase4/knowledge",
		bytes.NewReader(bytes.Repeat([]byte("x"), maxKnowledgeRequestBytes+1)))
	request.Host = "gohermit.test"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("body limit status=%d", recorder.Code)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/employees/missing/memory", nil, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found status=%d body=%s", response.Code, response.Body.String())
	}
}
