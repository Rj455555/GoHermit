package employeestore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestStoreMissingIsEmptyAndRevisionSnapshotsAreImmutable(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.List(ListOptions{})
	if err != nil || len(page.Employees) != 0 {
		t.Fatalf("empty list = %#v, %v", page, err)
	}
	record := createRecord(t, store, "employee-a")
	first, err := store.LoadRevision(record.Employee.ID, 1)
	if err != nil || !first.VerifyDigest() {
		t.Fatalf("revision one invalid: %v", err)
	}
	proposed := record.Employee
	proposed.Name = "Revised Employee"
	updated, err := store.Update(record.Employee.ID, 1, proposed, record.ProjectBindings)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Employee.Revision != 2 {
		t.Fatalf("revision = %d", updated.Employee.Revision)
	}
	again, err := store.LoadRevision(record.Employee.ID, 1)
	if err != nil || again.Employee.Name != first.Employee.Name || again.Digest != first.Digest {
		t.Fatalf("revision one changed: %#v, %v", again, err)
	}
	if _, err := store.Update(record.Employee.ID, 1, proposed, record.ProjectBindings); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}
}

func TestStorePaginationConcurrencyAndFailClosed(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, id := range []string{"employee-c", "employee-a", "employee-b"} {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, createErr := store.Create(testDraft(id), nil); createErr != nil {
				t.Errorf("create %s: %v", id, createErr)
			}
		}()
	}
	wg.Wait()
	first, err := store.List(ListOptions{Limit: 2})
	if err != nil || len(first.Employees) != 2 || first.Employees[0].ID != "employee-a" || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	second, err := store.List(ListOptions{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Employees) != 1 || second.Employees[0].ID != "employee-c" {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	indexPath := filepath.Join(root, "index.json")
	if err := os.WriteFile(indexPath, []byte(`{"schema_version":99,"employees":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ListOptions{}); err == nil {
		t.Fatal("unknown index schema must fail closed")
	}
	if _, err := store.Get("employee-a"); err == nil {
		t.Fatal("get must fail closed when index is corrupt")
	}
}

func TestLifecycleActivityIsBoundedMetadataOnly(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record := createRecord(t, store, "employee-a")
	record, err = store.Disable(record.Employee.ID, record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.Enable(record.Employee.ID, record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.Archive(record.Employee.ID, record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := store.Activity(record.Employee.ID, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Events) != 4 {
		t.Fatalf("activity count = %d", len(activity.Events))
	}
	forbidden := ActivityEvent{
		SchemaVersion: ActivitySchemaVersion, ID: "event-x", EmployeeID: record.Employee.ID,
		Type: ActivityType("run_status"), Time: time.Now().UTC(),
	}
	if err := store.RecordActivity(forbidden); err == nil {
		t.Fatal("run state activity must be rejected")
	}
}

func TestStrictRecordAndSizeValidation(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record := createRecord(t, store, "employee-a")
	current := filepath.Join(root, record.Employee.ID, "employee.json")
	raw, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["unknown"] = true
	raw, _ = json.Marshal(value)
	if err := os.WriteFile(current, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(record.Employee.ID); err == nil {
		t.Fatal("unknown current field must fail closed")
	}
}

func createRecord(t *testing.T, store *Store, id string) Record {
	t.Helper()
	record, err := store.Create(testDraft(id), nil)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func testDraft(id string) employee.Employee {
	return employee.Employee{
		ID: id, Name: "Test Employee", Avatar: employee.Avatar{Kind: employee.AvatarInitials},
		JobTitle: "Engineer", Charter: "Build bounded systems.",
		DefaultSelection:  employee.ModelSelection{Company: "openai", Access: "openai-api", Model: "gpt-5.4-mini"},
		AgentProfile:      "coding",
		PermissionPolicy:  employee.PermissionPolicy{AllowedCapabilities: []string{"read"}},
		BudgetPolicy:      employee.BudgetPolicy{MaxModelCalls: 8, MaxTokens: 100000, TimeoutSeconds: 3600},
		ConcurrencyPolicy: employee.ConcurrencyPolicy{MaxRunningTasks: 1},
		MemoryPolicy:      employee.MemoryPolicy{Promotion: employee.MemoryPromotionOwnerConfirmation},
	}
}
