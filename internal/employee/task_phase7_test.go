package employee

import (
	"testing"
	"time"
)

func TestTaskExecutionBindingDoesNotRewriteImmutableSnapshotDigest(t *testing.T) {
	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindTask(task, "employee-session-a", "run-a", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if bound.SnapshotDigest != task.SnapshotDigest || bound.SessionID == "" || bound.RunID == "" {
		t.Fatalf("binding rewrote immutable snapshot: %#v", bound)
	}
	again, err := BindTask(bound, bound.SessionID, bound.RunID, now.Add(2*time.Second))
	if err != nil || again.SnapshotDigest != task.SnapshotDigest || !again.UpdatedAt.Equal(bound.UpdatedAt) {
		t.Fatalf("idempotent binding = %#v, %v", again, err)
	}
	if _, err = BindTask(bound, "employee-session-b", "run-b", now.Add(2*time.Second)); err == nil {
		t.Fatal("conflicting binding succeeded")
	}
	cancelled, err := CancelTask(task, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = BindTask(cancelled, "employee-session-a", "run-a", now.Add(2*time.Second)); err == nil {
		t.Fatal("cancelled Task was bound")
	}
}
