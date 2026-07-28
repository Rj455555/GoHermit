package employeestore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatchJournalIsBoundedStableAndReopenSafe(t *testing.T) {
	store, root, task := seedTaskStore(t)
	expected := DispatchRecord{
		SchemaVersion:         DispatchSchemaVersion,
		EmployeeID:            task.EmployeeID,
		TaskID:                task.ID,
		SessionID:             "employee-session-stable",
		TaskSnapshotDigest:    task.SnapshotDigest,
		CompactSnapshotDigest: strings.Repeat("b", 64),
		WorkspaceRealPath:     task.ProjectBinding.WorkspaceRealPath,
		Stage:                 DispatchPrepared,
	}
	first, err := store.PrepareDispatch(expected)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PrepareDispatch(expected)
	if err != nil || second.SessionID != first.SessionID || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("idempotent prepare = %#v, %v", second, err)
	}
	path := filepath.Join(root, task.EmployeeID, "tasks", task.ID+".dispatch.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() > MaxDispatchBytes {
		t.Fatalf("dispatch mode/size = %v/%d, %v", info.Mode().Perm(), info.Size(), err)
	}
	reopened, _ := NewStore(root)
	loaded, err := reopened.LoadDispatch(task.ID)
	if err != nil || loaded != first {
		t.Fatalf("reopened dispatch = %#v, %v", loaded, err)
	}
	created, err := reopened.MarkDispatchSessionCreated(task.ID)
	if err != nil || created.Stage != DispatchSessionCreated ||
		created.SessionID != first.SessionID || created.TaskSnapshotDigest != task.SnapshotDigest {
		t.Fatalf("session-created dispatch = %#v, %v", created, err)
	}
	again, err := reopened.MarkDispatchSessionCreated(task.ID)
	if err != nil || again != created {
		t.Fatalf("idempotent stage advance = %#v, %v", again, err)
	}
	if _, err := reopened.GetTask(task.ID); err != nil {
		t.Fatalf("dispatch sidecar broke Task lookup: %v", err)
	}
}

func TestDispatchJournalMismatchAndCorruptionFailClosed(t *testing.T) {
	store, root, task := seedTaskStore(t)
	expected := DispatchRecord{
		SchemaVersion:         DispatchSchemaVersion,
		EmployeeID:            task.EmployeeID,
		TaskID:                task.ID,
		SessionID:             "employee-session-stable",
		TaskSnapshotDigest:    task.SnapshotDigest,
		CompactSnapshotDigest: strings.Repeat("b", 64),
		WorkspaceRealPath:     task.ProjectBinding.WorkspaceRealPath,
		Stage:                 DispatchPrepared,
	}
	if _, err := store.PrepareDispatch(expected); err != nil {
		t.Fatal(err)
	}
	mismatch := expected
	mismatch.SessionID = "employee-session-other"
	if _, err := store.PrepareDispatch(mismatch); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatch error = %v", err)
	}
	path := filepath.Join(root, task.EmployeeID, "tasks", task.ID+".dispatch.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadDispatch(task.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt dispatch error = %v", err)
	}
	if _, err := store.GetTask(task.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Task lookup did not fail closed on corrupt sidecar: %v", err)
	}
}

func TestDispatchJournalRejectsUnsafeIdentityAndSymlink(t *testing.T) {
	store, root, task := seedTaskStore(t)
	value := DispatchRecord{
		SchemaVersion:         DispatchSchemaVersion,
		EmployeeID:            task.EmployeeID,
		TaskID:                "../outside",
		SessionID:             "employee-session-stable",
		TaskSnapshotDigest:    task.SnapshotDigest,
		CompactSnapshotDigest: strings.Repeat("b", 64),
		WorkspaceRealPath:     task.ProjectBinding.WorkspaceRealPath,
		Stage:                 DispatchPrepared,
	}
	if _, err := store.PrepareDispatch(value); err == nil {
		t.Fatal("expected unsafe Task identity rejection")
	}
	value.TaskID = task.ID
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, task.EmployeeID, "tasks", task.ID+".dispatch.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.PrepareDispatch(value); err == nil {
		t.Fatal("expected dispatch symlink rejection")
	}
	raw, _ := os.ReadFile(outside)
	if string(raw) != "{}" {
		t.Fatalf("outside file changed: %q", raw)
	}
}
