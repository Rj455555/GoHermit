package loopstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
)

func TestNotificationDeliveryIsIdempotentAndFailClosed(t *testing.T) {
	store := newTestStore(t)
	invocation, err := loop.NewInvocation(validDefinition("notify-loop"), loop.TriggerManual, "notify me", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = invocation.Skip("done", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	sent, err := store.NotificationSent(invocation.ID, invocation.Status)
	if err != nil || sent {
		t.Fatalf("missing marker = %v, %v", sent, err)
	}
	when := time.Now().UTC()
	if err = store.MarkNotificationSent(invocation.ID, invocation.Status, when); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkNotificationSent(invocation.ID, invocation.Status, when.Add(time.Minute)); err != nil {
		t.Fatalf("idempotent mark = %v", err)
	}
	sent, err = store.NotificationSent(invocation.ID, invocation.Status)
	if err != nil || !sent {
		t.Fatalf("persisted marker = %v, %v", sent, err)
	}
	path := filepath.Join(store.NotificationDir(), invocation.ID+".json")
	if err = os.WriteFile(path, []byte(`{"schema_version":1,"invocation_id":"bad","status":"skipped","sent_at":"2026-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.NotificationSent(invocation.ID, invocation.Status); err == nil {
		t.Fatal("corrupt marker was accepted")
	}
}
