package channelstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsSeparateCredentialsAndCursor(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := Account{SchemaVersion: SchemaVersion, ID: "account-1", State: StateConnected, BaseURL: "https://example.test", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveAccount(account); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSecret(account.ID, Secret{SchemaVersion: SchemaVersion, Token: "secret", ContextTokens: map[string]string{"peer": "context"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCursor(account.ID, "cursor-1"); err != nil {
		t.Fatal(err)
	}
	accounts, err := store.ListAccounts()
	if err != nil || len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("accounts = %#v err=%v", accounts, err)
	}
	cursor, err := store.LoadCursor(account.ID)
	if err != nil || cursor != "cursor-1" {
		t.Fatalf("cursor = %q err=%v", cursor, err)
	}
	raw, err := os.ReadFile(filepath.Join(t.TempDir(), "missing"))
	if err == nil || raw != nil {
		t.Fatal("unexpected read")
	}
}

func TestStoreRejectsSymlinkAccountDirectory(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "accounts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "accounts", "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = store.GetAccount("linked")
	if err == nil {
		t.Fatal("symlink account accepted")
	}
}

func TestStoreRejectsUnknownJSONFields(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	accountDir := filepath.Join(root, "accounts", "account-1")
	if err := os.MkdirAll(accountDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"schema_version":1,"id":"account-1","state":"connected","base_url":"https://example.test","created_at":"2026-08-04T00:00:00Z","updated_at":"2026-08-04T00:00:00Z","unexpected":true}`)
	if err := os.WriteFile(filepath.Join(accountDir, "account.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAccount("account-1"); err != ErrCorrupt {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestStoreResolvesExplicitAccountDefaultBinding(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertBinding(Binding{ID: "default-binding", AccountID: "account-1", EmployeeID: "employee-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	binding, found, err := store.ResolveBinding("account-1", "peer-1", "")
	if err != nil || !found || binding.EmployeeID != "employee-1" {
		t.Fatalf("binding = %#v found=%v err=%v", binding, found, err)
	}
}

func TestDeleteLoginAttemptsForAccountIsScoped(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, attempt := range []LoginAttempt{
		{SchemaVersion: SchemaVersion, ID: "attempt-a", AccountID: "account-a", State: StateQRPending, ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: SchemaVersion, ID: "attempt-b", AccountID: "account-b", State: StateQRPending, ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveLoginSecret("attempt-a", LoginSecret{SchemaVersion: SchemaVersion, QRContent: "qr-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteLoginAttemptsForAccount("account-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAttempt("attempt-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("account A attempt remains: %v", err)
	}
	if _, err := store.GetAttempt("attempt-b"); err != nil {
		t.Fatalf("account B attempt was removed: %v", err)
	}
}
