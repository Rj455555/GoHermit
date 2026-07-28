package employeestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadRevisionValidatesRequestedIdentityAndContainment(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first := createRecord(t, store, "employee-a")
	second := createRecord(t, store, "employee-b")
	for _, test := range []struct {
		id       string
		revision int
	}{
		{"../employee-a", 1},
		{"..%2femployee-a", 1},
		{filepath.Join(root, "employee-a"), 1},
		{"employee-a", 0},
		{"employee-a", -1},
		{"missing", 1},
	} {
		if _, err := store.LoadRevision(test.id, test.revision); err == nil {
			t.Fatalf("LoadRevision(%q, %d) must fail", test.id, test.revision)
		}
	}

	firstPath := revisionPath(root, first.Employee.ID, 1)
	secondPath := revisionPath(root, second.Employee.ID, 1)
	secondRaw, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstPath, secondRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRevision(first.Employee.ID, 1); err == nil {
		t.Fatal("swapped snapshot identity must fail")
	}

	if err := os.WriteFile(firstPath, mustRead(t, secondPath), 0o600); err != nil {
		t.Fatal(err)
	}
	revisionTwoPath := revisionPath(root, second.Employee.ID, 2)
	if err := os.WriteFile(revisionTwoPath, secondRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRevision(second.Employee.ID, 2); err == nil {
		t.Fatal("snapshot revision must equal requested revision")
	}

	external := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(external, secondRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, secondPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRevision(second.Employee.ID, 1); err == nil {
		t.Fatal("snapshot symlink outside store must fail")
	}
}

func TestLoadRevisionRejectsUnknownFieldsDigestTamperingAndOverwrite(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	path := revisionPath(root, record.Employee.ID, 1)
	original := mustRead(t, path)

	var object map[string]any
	if err := json.Unmarshal(original, &object); err != nil {
		t.Fatal(err)
	}
	object["unknown"] = true
	writeJSONTest(t, path, object)
	if _, err := store.LoadRevision(record.Employee.ID, 1); err == nil {
		t.Fatal("unknown snapshot field must fail")
	}

	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(original, &object); err != nil {
		t.Fatal(err)
	}
	embedded := object["employee"].(map[string]any)
	embedded["name"] = "Tampered"
	writeJSONTest(t, path, object)
	if _, err := store.LoadRevision(record.Employee.ID, 1); err == nil {
		t.Fatal("digest tampering must fail")
	}

	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revisionPath(root, record.Employee.ID, 2), original, 0o600); err != nil {
		t.Fatal(err)
	}
	proposed := record.Employee
	proposed.Name = "Revised"
	if _, err := store.Update(record.Employee.ID, record.Employee.Revision, proposed, nil); err == nil {
		t.Fatal("pre-existing immutable snapshot must not be overwritten")
	}
}

func TestActivityIDsAndStablePagination(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	if err := store.RecordActivity(ActivityEvent{
		ID: "caller-controlled", EmployeeID: record.Employee.ID, Type: ActivitySkillBinding,
		SubjectID: "skill-a",
	}); err == nil {
		t.Fatal("RecordActivity must reject caller-provided ID")
	}
	for i := 0; i < 5; i++ {
		if err := store.RecordActivity(ActivityEvent{
			EmployeeID: record.Employee.ID, Type: ActivitySkillBinding,
			SubjectID: fmt.Sprintf("skill-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.Activity(record.Employee.ID, ListOptions{Limit: 2})
	if err != nil || len(first.Events) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	if err := store.RecordActivity(ActivityEvent{
		EmployeeID: record.Employee.ID, Type: ActivitySkillBinding, SubjectID: "skill-later",
	}); err != nil {
		t.Fatal(err)
	}
	reopened, _ := NewStore(root)
	second, err := reopened.Activity(record.Employee.ID, ListOptions{Limit: 10, Cursor: first.NextCursor})
	if err != nil || len(second.Events) != 5 {
		t.Fatalf("continued page = %#v, %v", second, err)
	}
	if _, err := reopened.Activity(record.Employee.ID, ListOptions{Cursor: "%%%"}); err == nil {
		t.Fatal("invalid activity cursor must fail")
	}
	if _, err := reopened.Activity(record.Employee.ID, ListOptions{Cursor: encodeCursor("event-x")}); err == nil {
		t.Fatal("well-encoded invalid activity cursor must fail")
	}
}

func TestLoadActivityRejectsDuplicateOutOfOrderAndIllegalIDs(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	page, err := store.Activity(record.Employee.ID, ListOptions{})
	if err != nil || len(page.Events) != 1 {
		t.Fatal(err)
	}
	event := page.Events[0]
	path := filepath.Join(root, record.Employee.ID, "activity", "events.jsonl")
	for name, events := range map[string][]ActivityEvent{
		"duplicate": {event, event},
		"out-of-order": {
			{SchemaVersion: ActivitySchemaVersion, ID: "99999999999999999999-ffffffffffffffff", EmployeeID: record.Employee.ID, Type: ActivitySkillBinding, Time: time.Now().UTC(), SubjectID: "a"},
			event,
		},
		"illegal": {
			{SchemaVersion: ActivitySchemaVersion, ID: "event-x", EmployeeID: record.Employee.ID, Type: ActivitySkillBinding, Time: time.Now().UTC(), SubjectID: "a"},
		},
		"unknown-schema": {
			{SchemaVersion: 99, ID: "00000000000000000001-0000000000000000", EmployeeID: record.Employee.ID, Type: ActivitySkillBinding, Time: time.Now().UTC(), SubjectID: "a"},
		},
		"identity-mismatch": {
			{SchemaVersion: ActivitySchemaVersion, ID: "00000000000000000001-0000000000000000", EmployeeID: "employee-b", Type: ActivitySkillBinding, Time: time.Now().UTC(), SubjectID: "a"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			writeActivityTest(t, path, events)
			if _, err := store.Activity(record.Employee.ID, ListOptions{}); err == nil {
				t.Fatal("invalid activity order or ID must fail")
			}
		})
	}
}

func TestActivityTruncationContinuationAndReopen(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	for i := 0; i < MaxActivityEvents+3; i++ {
		if err := store.RecordActivity(ActivityEvent{
			EmployeeID: record.Employee.ID, Type: ActivitySkillBinding,
			SubjectID: fmt.Sprintf("skill-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	reopened, _ := NewStore(root)
	page, err := reopened.Activity(record.Employee.ID, ListOptions{Limit: MaxPageSize})
	if err != nil || len(page.Events) != MaxPageSize || page.NextCursor == "" {
		t.Fatalf("truncated first page = %d, %q, %v", len(page.Events), page.NextCursor, err)
	}
	next, err := reopened.Activity(record.Employee.ID, ListOptions{Limit: MaxPageSize, Cursor: page.NextCursor})
	if err != nil || len(next.Events) == 0 || next.Events[0].ID <= page.Events[len(page.Events)-1].ID {
		t.Fatalf("truncated continuation = %#v, %v", next, err)
	}
}

func TestStoreFilesFailClosedMatrix(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   func([]byte) []byte
	}{
		{"index unknown field", "index", addJSONField("unknown", true)},
		{"index unknown schema", "index", addJSONField("schema_version", 99)},
		{"index damaged", "index", func([]byte) []byte { return []byte(`{"schema_version":`) }},
		{"index oversized", "index", oversized},
		{"employee unknown field", "employee", addJSONField("unknown", true)},
		{"employee damaged", "employee", func([]byte) []byte { return []byte(`{`) }},
		{"employee oversized", "employee", oversized},
		{"projects unknown field", "projects", addJSONField("unknown", true)},
		{"projects unknown schema", "projects", addJSONField("schema_version", 99)},
		{"projects damaged", "projects", func([]byte) []byte { return []byte(`{`) }},
		{"projects oversized", "projects", oversized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, _ := NewStore(root)
			record := createRecord(t, store, "employee-a")
			paths := map[string]string{
				"index":    filepath.Join(root, "index.json"),
				"employee": filepath.Join(root, record.Employee.ID, "employee.json"),
				"projects": filepath.Join(root, record.Employee.ID, "projects.json"),
			}
			path := paths[test.target]
			if err := os.WriteFile(path, test.body(mustRead(t, path)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Get(record.Employee.ID); err == nil {
				t.Fatal("Get must fail closed")
			}
			if _, err := store.List(ListOptions{}); err == nil {
				t.Fatal("List must fail closed")
			}
		})
	}
}

func TestIndexRecordMismatchFailsGetAndList(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	path := filepath.Join(root, "index.json")
	raw := mustRead(t, path)
	var index map[string]any
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	employees := index["employees"].([]any)
	employees[0].(map[string]any)["name"] = "Mismatch"
	writeJSONTest(t, path, index)
	if _, err := store.Get(record.Employee.ID); err == nil {
		t.Fatal("Get must reject index/record mismatch")
	}
	if _, err := store.List(ListOptions{}); err == nil {
		t.Fatal("List must reject index/record mismatch")
	}
}

func TestConcurrentUpdateConflictsAndEmployeeFilters(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	active := createRecord(t, store, "employee-active")
	disabled := createRecord(t, store, "employee-disabled")
	if _, err := store.Disable(disabled.Employee.ID, disabled.Employee.Revision); err != nil {
		t.Fatal(err)
	}
	filtered, err := store.List(ListOptions{State: employee.StateDisabled})
	if err != nil || len(filtered.Employees) != 1 || filtered.Employees[0].ID != disabled.Employee.ID {
		t.Fatalf("state filter = %#v, %v", filtered, err)
	}
	if _, err := store.List(ListOptions{Cursor: "%%%"}); err == nil {
		t.Fatal("invalid employee cursor must fail")
	}
	if _, err := store.List(ListOptions{Cursor: encodeCursor("../employee-active")}); err == nil {
		t.Fatal("well-encoded invalid employee cursor must fail")
	}
	proposed := active.Employee
	proposed.Name = "Concurrent"
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := store.Update(active.Employee.ID, active.Employee.Revision, proposed, nil)
			results <- err
		}()
	}
	var success, conflict int
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent update success=%d conflict=%d", success, conflict)
	}
}

func TestActivityValidationFilePermissionsAndSecrets(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	for _, path := range []string{
		filepath.Join(root, "index.json"),
		filepath.Join(root, record.Employee.ID, "employee.json"),
		filepath.Join(root, record.Employee.ID, "projects.json"),
		revisionPath(root, record.Employee.ID, 1),
		filepath.Join(root, record.Employee.ID, "activity", "events.jsonl"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", path, info.Mode().Perm())
		}
	}
	for _, event := range []ActivityEvent{
		{EmployeeID: "other", Type: ActivitySkillBinding, SubjectID: "skill-a"},
		{EmployeeID: record.Employee.ID, Type: ActivityType("tool_event"), SubjectID: "tool-a"},
		{EmployeeID: record.Employee.ID, Type: ActivitySkillBinding},
	} {
		if err := store.RecordActivity(event); err == nil {
			t.Fatalf("invalid activity accepted: %#v", event)
		}
	}
	path := filepath.Join(root, record.Employee.ID, "activity", "events.jsonl")
	event := ActivityEvent{SchemaVersion: 99, ID: "00000000000000000001-0000000000000000", EmployeeID: record.Employee.ID, Type: ActivitySkillBinding, Time: time.Now().UTC(), SubjectID: "skill"}
	writeActivityTest(t, path, []ActivityEvent{event})
	if _, err := store.Activity(record.Employee.ID, ListOptions{}); err == nil {
		t.Fatal("unknown activity schema must fail")
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), MaxActivityFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activity(record.Employee.ID, ListOptions{}); err == nil {
		t.Fatal("oversized activity must fail")
	}

	secretDraft := testDraft("secret-employee")
	secretDraft.Charter = "api_" + "key=credential"
	if _, err := store.Create(secretDraft, nil); err == nil {
		t.Fatal("secret-like employee input must fail")
	}
	bindingDraft := testDraft("secret-binding")
	if _, err := store.Create(bindingDraft, []employee.ProjectBinding{{
		ID: "project-secret", Label: "api_" + "key=credential",
		WorkspaceRealPath: filepath.Clean(root), ReadAllowed: true,
	}}); err == nil {
		t.Fatal("secret-like binding input must fail")
	}
}

func TestActivityRandomFailureFailsClosed(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	record := createRecord(t, store, "employee-a")
	original := activityRandomRead
	activityRandomRead = func([]byte) (int, error) { return 0, errors.New("random unavailable") }
	t.Cleanup(func() { activityRandomRead = original })
	if err := store.RecordActivity(ActivityEvent{
		EmployeeID: record.Employee.ID, Type: ActivitySkillBinding, SubjectID: "skill-a",
	}); err == nil || !strings.Contains(err.Error(), "random unavailable") {
		t.Fatalf("random failure = %v", err)
	}
}

func addJSONField(key string, value any) func([]byte) []byte {
	return func(raw []byte) []byte {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			panic(err)
		}
		object[key] = value
		updated, err := json.Marshal(object)
		if err != nil {
			panic(err)
		}
		return updated
	}
}

func oversized([]byte) []byte {
	return bytes.Repeat([]byte("x"), MaxStoreFileBytes+1)
}

func revisionPath(root, id string, revision int) string {
	return filepath.Join(root, id, "revisions", fmt.Sprintf("%d.json", revision))
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeJSONTest(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeActivityTest(t *testing.T, path string, events []ActivityEvent) {
	t.Helper()
	var lines []string
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
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
