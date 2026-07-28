package employeestore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

func TestKnowledgeAndMemoryAreEmployeeIsolatedAndSurviveReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	createRecord(t, store, "employee-a")
	createRecord(t, store, "employee-b")
	catalog, _ := knowledge.NewCatalog("")
	source, index, err := catalog.Index(knowledge.Source{
		ID: "handbook", EmployeeID: "employee-a", Kind: knowledge.KindManualText,
		Title: "Handbook", ManualText: "Employee A private Knowledge.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveKnowledge("employee-a", source, index); err != nil {
		t.Fatal(err)
	}
	candidate := phase4Candidate(t, "employee-a", "candidate-a")
	if err := store.AddMemoryCandidate("employee-a", candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptMemoryCandidate("employee-b", candidate.ID); !errors.Is(err, employeememory.ErrMissing) {
		t.Fatalf("cross-employee accept = %v", err)
	}
	fact, err := store.AcceptMemoryCandidate("employee-a", candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := store.Knowledge("employee-b"); err != nil || len(state.Sources) != 0 {
		t.Fatalf("Employee B Knowledge = %#v, %v", state, err)
	}
	if facts, err := store.Memory("employee-b"); err != nil || len(facts) != 0 {
		t.Fatalf("Employee B Memory = %#v, %v", facts, err)
	}
	reopened, _ := NewStore(root)
	state, err := reopened.Knowledge("employee-a")
	if err != nil || len(state.Sources) != 1 {
		t.Fatalf("reopened Knowledge = %#v, %v", state, err)
	}
	facts, err := reopened.Memory("employee-a")
	if err != nil || len(facts) != 1 || facts[0].ID != fact.ID {
		t.Fatalf("reopened Memory = %#v, %v", facts, err)
	}
	for _, relative := range []string{
		filepath.Join("employee-a", "knowledge", "sources.json"),
		filepath.Join("employee-a", "knowledge", "index.json"),
		filepath.Join("employee-a", "memory", "facts.json"),
		filepath.Join("employee-a", "memory", "candidates.json"),
	} {
		info, err := os.Stat(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("%s stat: %v", relative, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permission = %v", relative, info.Mode().Perm())
		}
	}
}

func TestCandidateAcceptRejectEditForgetAndActivityBoundaries(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "employees"))
	createRecord(t, store, "employee-a")
	accepted := phase4Candidate(t, "employee-a", "accept-me")
	rejected := phase4Candidate(t, "employee-a", "reject-me")
	if err := store.AddMemoryCandidate("employee-a", accepted); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMemoryCandidate("employee-a", rejected); err != nil {
		t.Fatal(err)
	}
	if err := store.RejectMemoryCandidate("employee-a", rejected.ID); err != nil {
		t.Fatal(err)
	}
	fact, err := store.AcceptMemoryCandidate("employee-a", accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.AcceptMemoryCandidate("employee-a", accepted.ID)
	if err != nil || again.ID != fact.ID {
		t.Fatalf("idempotent accept = %#v, %v", again, err)
	}
	edited, err := store.EditMemory("employee-a", fact.ID, "Owner edited bounded memory.")
	if err != nil || !edited.OwnerEdited || edited.Provenance[0] != fact.Provenance[0] {
		t.Fatalf("edit = %#v, %v", edited, err)
	}
	if err := store.ForgetMemory("employee-a", fact.ID); err != nil {
		t.Fatal(err)
	}
	if facts, err := store.Memory("employee-a"); err != nil || len(facts) != 0 {
		t.Fatalf("forgotten facts = %#v, %v", facts, err)
	}
	activity, err := store.Activity("employee-a", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var acceptedEvent, editedEvent, forgottenEvent bool
	for _, event := range activity.Events {
		acceptedEvent = acceptedEvent || event.Type == ActivityMemoryAccepted
		editedEvent = editedEvent || event.Type == ActivityMemoryEdited
		forgottenEvent = forgottenEvent || event.Type == ActivityMemoryForgotten
		if event.TaskID != "" || event.SessionID != "" || event.RunID != "" {
			t.Fatal("Memory activity copied execution state")
		}
	}
	if !acceptedEvent || !editedEvent || !forgottenEvent {
		t.Fatalf("missing bounded activity: %#v", activity.Events)
	}
}

func TestPhase4FilesFailClosedOnUnknownSchemaIdentityAndOversize(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	createRecord(t, store, "employee-a")
	path := filepath.Join(root, "employee-a", "memory", "facts.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"unknown":  `{"schema_version":1,"employee_id":"employee-a","facts":[],"unknown":true}`,
		"schema":   `{"schema_version":99,"employee_id":"employee-a","facts":[]}`,
		"identity": `{"schema_version":1,"employee_id":"employee-b","facts":[]}`,
		"damaged":  `{`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Memory("employee-a"); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", employeememory.MaxFactFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Memory("employee-a"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestPhase4StoreRejectsSymlinkEscapeForReadsAndWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	createRecord(t, store, "employee-a")
	outside := t.TempDir()
	marker := filepath.Join(outside, "facts.json")
	if err := os.WriteFile(marker, []byte("outside-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	memoryDirectory := filepath.Join(root, "employee-a", "memory")
	if err := os.Symlink(outside, memoryDirectory); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.Memory("employee-a"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("symlink read error = %v", err)
	}
	candidate := phase4Candidate(t, "employee-a", "candidate-a")
	if err := store.AddMemoryCandidate("employee-a", candidate); err == nil {
		t.Fatal("symlink write was allowed")
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "outside-marker" {
		t.Fatalf("outside file changed: %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "candidates.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("write created a file outside the Employee Store")
	}
}

func phase4Candidate(t *testing.T, employeeID, id string) employeememory.Candidate {
	t.Helper()
	now := time.Now().UTC()
	value, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: id, EmployeeID: employeeID, Category: "preference", Value: "Use bounded deterministic systems.",
		Provenance: []employeememory.Provenance{{SourceType: "owner", SourceID: "owner-note", VerifiedAt: now}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
