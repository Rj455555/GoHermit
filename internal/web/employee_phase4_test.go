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

func TestPhase4APIRejectsNULManualTextAndOversizedQuery(t *testing.T) {
	server, _, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	create := controlplane.EmployeeInput{Employee: webEmployeeDraft("employee-phase4")}
	_ = requestJSON(t, handler, http.MethodPost, "/api/employees", create, "")
	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-phase4/knowledge",
		knowledge.Source{ID: "nul", Kind: knowledge.KindManualText, Title: "NUL", ManualText: "bad\x00text"}, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("NUL ManualText status=%d body=%s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet,
		"/api/employees/employee-phase4/knowledge?query="+strings.Repeat("x", maxKnowledgeQueryBytes+1), nil, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized query status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPhase4EmptyBodyMutationsRejectChunkedPayloads(t *testing.T) {
	server, _, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	create := controlplane.EmployeeInput{Employee: webEmployeeDraft("employee-phase4")}
	_ = requestJSON(t, handler, http.MethodPost, "/api/employees", create, "")
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/employees/employee-phase4/knowledge/missing/refresh"},
		{http.MethodDelete, "/api/employees/employee-phase4/knowledge/missing"},
		{http.MethodPost, "/api/employees/employee-phase4/memory-candidates/missing/accept"},
		{http.MethodDelete, "/api/employees/employee-phase4/memory-candidates/missing"},
		{http.MethodDelete, "/api/employees/employee-phase4/memory/missing"},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			request := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewBufferString(`{}`))
			request.Host = "gohermit.test"
			request.ContentLength = -1
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("chunked payload status=%d body=%s", response.Code, response.Body.String())
			}

			request = httptest.NewRequest(endpoint.method, endpoint.path,
				bytes.NewReader(bytes.Repeat([]byte("x"), maxPhase4EmptyBodyBytes+1)))
			request.Host = "gohermit.test"
			request.ContentLength = -1
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("oversized chunked payload status=%d body=%s", response.Code, response.Body.String())
			}

			request = httptest.NewRequest(endpoint.method, endpoint.path, http.NoBody)
			request.Host = "gohermit.test"
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusBadRequest {
				t.Fatalf("truly empty body rejected: %s", response.Body.String())
			}
		})
	}
}

func TestEmployeeKnowledgeAPICorruptPersistedSourceReturnsInternalError(t *testing.T) {
	server, _, state := newEmployeeTestServer(t)
	handler := server.Handler()
	create := controlplane.EmployeeInput{Employee: webEmployeeDraft("employee-phase4")}
	_ = requestJSON(t, handler, http.MethodPost, "/api/employees", create, "")
	source := knowledge.Source{ID: "guide", Kind: knowledge.KindManualText, Title: "Guide", ManualText: "bounded"}
	if response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-phase4/knowledge", source, ""); response.Code != http.StatusCreated {
		t.Fatalf("add status=%d body=%s", response.Code, response.Body.String())
	}
	path := filepath.Join(state, "employees", "employee-phase4", "knowledge", "sources.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	sources := file["sources"].([]any)
	stored := sources[0].(map[string]any)
	stored["kind"] = string(knowledge.KindFile)
	stored["relative_path"] = "../outside.md"
	delete(stored, "manual_text")
	raw, _ = json.Marshal(file)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, handler, http.MethodGet, "/api/employees/employee-phase4/knowledge", nil, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt Store status=%d body=%s", response.Code, response.Body.String())
	}
}
