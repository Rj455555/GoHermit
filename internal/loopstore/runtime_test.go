package loopstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
)

func employeeDefinition(id string) loop.Definition {
	value := validDefinition(id)
	value.EmployeeID = "employee-knowledge"
	value.Contract = loop.Contract{
		Goal:       "Archive new knowledge.",
		Boundaries: []string{"Keep provenance.", "Never store credentials."},
		SOP:        []string{"Inspect.", "Deduplicate.", "Report."},
	}
	value.Schedule = loop.Schedule{
		Kind: loop.ScheduleDaily, LocalTime: "02:00", Timezone: "Asia/Shanghai",
	}
	return value
}

func TestSaveEmployeeLoopWritesImmutableContractAndSeparateRuntimeState(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveDefinition(employeeDefinition("knowledge-daily")); err != nil {
		t.Fatal(err)
	}

	contractPath := filepath.Join(store.Dir(), "contracts", "knowledge-daily", "LOOP.md")
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "## Goal") || strings.Contains(string(raw), "## Logs") {
		t.Fatalf("unexpected LOOP.md:\n%s", raw)
	}

	saved, err := store.GetDefinition("knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	state := loop.NewRuntimeState(saved, time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC))
	if err := store.SaveRuntimeState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetRuntimeState("knowledge-daily")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LoopID != saved.ID || loaded.DefinitionRevision != saved.Revision || loaded.NextRunAt == nil {
		t.Fatalf("runtime state = %+v", loaded)
	}

	stateInfo, err := os.Stat(filepath.Join(store.Dir(), "states", "knowledge-daily.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stateInfo.Mode().Perm() != 0600 {
		t.Fatalf("state permissions = %o, want 600", stateInfo.Mode().Perm())
	}
}

func TestRuntimeStateRejectsTraversalAndCorruption(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetRuntimeState("../outside"); err == nil {
		t.Fatal("traversal state id accepted")
	}
	path := filepath.Join(store.Dir(), "states", "broken.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRuntimeState("broken"); err == nil {
		t.Fatal("corrupt runtime state accepted")
	}
	valid := loop.NewRuntimeState(employeeDefinition("trailing"), time.Now().UTC())
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	trailingPath := filepath.Join(store.Dir(), "states", "trailing.json")
	if err = os.WriteFile(trailingPath, append(raw, []byte(` {}`)...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetRuntimeState("trailing"); err == nil {
		t.Fatal("runtime state with a second JSON value accepted")
	}
}
