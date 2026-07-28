package employeestore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestTaskBindingIsAtomicIdempotentAndPreservesSnapshot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record := createTaskEmployee(t, store, "employee-a")
	created, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Execute once"))
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindTask(created.ID, "employee-session-a", "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if bound.SessionID != "employee-session-a" || bound.RunID != "run-a" ||
		bound.SnapshotDigest != created.SnapshotDigest || bound.State != employee.TaskQueued {
		t.Fatalf("bound Task = %#v", bound)
	}
	again, err := store.BindTask(created.ID, "employee-session-a", "run-a")
	if err != nil || !reflect.DeepEqual(again, bound) {
		t.Fatalf("idempotent binding = %#v, %v", again, err)
	}
	if _, err = store.BindTask(created.ID, "employee-session-b", "run-b"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting binding = %v", err)
	}
	if _, err = store.CancelTask(created.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("bound Task entered inbox cancellation state: %v", err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.GetTask(created.ID)
	if err != nil || !reflect.DeepEqual(loaded, bound) {
		t.Fatalf("reopened binding = %#v, %v", loaded, err)
	}
	activity, err := reopened.Activity("employee-a", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	references := 0
	for _, event := range activity.Events {
		if event.Type == ActivityExecutionRef {
			references++
			if event.TaskID != created.ID || event.SessionID != bound.SessionID ||
				event.RunID != bound.RunID || event.SubjectID != "" {
				t.Fatalf("execution Activity leaked or mismatched data: %#v", event)
			}
		}
	}
	if references != 1 {
		t.Fatalf("execution Activity references = %d", references)
	}
}

func TestVerifiedArtifactMetadataIsBoundedImmutableAndReopens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	record := createTaskEmployee(t, store, "employee-a")
	task, _ := store.CreateTask("employee-a", taskStoreDraft(t, record, "Produce metadata"))
	task, _ = store.BindTask(task.ID, "employee-session-a", "run-a")
	verifiedAt := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	first, err := NewArtifact("employee-a", task.ID, task.SessionID, task.RunID, "docs/result.md", strings.Repeat("a", 64), verifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewArtifact("employee-a", task.ID, task.SessionID, task.RunID, "old.txt", "deleted", verifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.PutVerifiedArtifacts(task.ID, []Artifact{second, first}); err != nil {
		t.Fatal(err)
	}
	if err = store.PutVerifiedArtifacts(task.ID, []Artifact{first, second}); err != nil {
		t.Fatalf("idempotent Artifact write: %v", err)
	}
	reopened, _ := NewStore(root)
	loaded, err := reopened.Artifacts(task.ID)
	if err != nil || len(loaded) != 2 || loaded[0].ID >= loaded[1].ID {
		t.Fatalf("reopened Artifacts = %#v, %v", loaded, err)
	}
	tampered := append([]Artifact{}, loaded...)
	tampered[0].Digest = strings.Repeat("b", 64)
	if err = reopened.PutVerifiedArtifacts(task.ID, tampered); err == nil {
		t.Fatal("Artifact overwrite succeeded")
	}
	oversized := make([]Artifact, MaxArtifactsPerTask+1)
	for index := range oversized {
		oversized[index], _ = NewArtifact(
			"employee-a", task.ID, task.SessionID, task.RunID,
			"generated/file-"+strings.Repeat("x", index%8)+"-"+string(rune('a'+index%26)),
			strings.Repeat("c", 64), verifiedAt,
		)
	}
	if err = reopened.PutVerifiedArtifacts(task.ID, oversized); err == nil {
		t.Fatal("Artifact count limit was not enforced")
	}
	path := filepath.Join(root, "employee-a", "tasks", artifactFileName(task.ID))
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() > MaxArtifactFileBytes {
		t.Fatalf("Artifact file mode/size = %v/%d, %v", info.Mode().Perm(), info.Size(), err)
	}
}

func TestArtifactSymlinkFailsClosedAndLeavesExternalFileUnchanged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	record := createTaskEmployee(t, store, "employee-a")
	task, _ := store.CreateTask("employee-a", taskStoreDraft(t, record, "Unsafe metadata"))
	task, _ = store.BindTask(task.ID, "employee-session-a", "run-a")
	external := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(external, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "employee-a", "tasks", artifactFileName(task.ID))
	if err := os.Symlink(external, target); err != nil {
		t.Fatalf("macOS acceptance requires symlink support: %v", err)
	}
	artifact, _ := NewArtifact("employee-a", task.ID, task.SessionID, task.RunID, "result.txt", strings.Repeat("a", 64), time.Now().UTC())
	if err := store.PutVerifiedArtifacts(task.ID, []Artifact{artifact}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Artifact symlink error = %v", err)
	}
	raw, err := os.ReadFile(external)
	if err != nil || string(raw) != "unchanged" {
		t.Fatalf("external target changed: %q, %v", raw, err)
	}
}
