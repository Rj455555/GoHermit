package boardstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreDefaultRoundTripAndNormalization(t *testing.T) {
	workspace := t.TempDir()
	store, err := NewStore(filepath.Join(workspace, ".gohermit", "board"), "owner", workspace)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != SchemaVersion || len(document.Definition.Columns) != 6 {
		t.Fatalf("unexpected default document: %#v", document)
	}
	now := time.Now().UTC()
	document.Cards = append(document.Cards, CardMetadata{
		ID: "task-1", TaskID: "task-1", Kind: CardTask, ColumnID: "todo", Rank: 10,
		Labels: []string{"z", "a", "a"}, DependsOn: []string{"task-2", "task-2"}, UpdatedAt: now,
	})
	document.UpdatedAt = now
	if err := store.Save(document); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Cards) != 1 || len(reopened.Cards[0].Labels) != 2 || reopened.Cards[0].Labels[0] != "a" {
		t.Fatalf("normalization was not persisted: %#v", reopened.Cards)
	}
	if err := store.Save(reopened); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsPromptCopyAndUnknownData(t *testing.T) {
	workspace := t.TempDir()
	store, err := NewStore(filepath.Join(workspace, "board"), "owner", workspace)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	document.Cards = []CardMetadata{{ID: "task-1", TaskID: "task-1", Kind: CardTask, Title: "prompt must not be copied", ColumnID: "todo", UpdatedAt: now}}
	document.UpdatedAt = now
	if err := store.Save(document); err == nil {
		t.Fatal("expected prompt-copy rejection")
	}
	if err := os.MkdirAll(store.root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.filePath(), []byte(`{"schema_version":1,"owner_id":"owner","workspace_fingerprint":"bad","definition":{},"cards":[],"view":{},"filters":{},"updated_at":"2026-01-01T00:00:00Z","unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected corrupt unknown-field error, got %v", err)
	}
}

func TestStoreRejectsSymlinkRootAndFile(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := NewStore(link, "owner", workspace); err == nil {
		t.Fatal("expected symlink root rejection")
	}
	root := filepath.Join(workspace, "board")
	store, err := NewStore(root, "owner", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(root, "board.json")
	if err := os.Symlink(filepath.Join(workspace, "other.json"), fileLink); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	document := store.defaultDocument()
	if err := store.Save(document); err == nil {
		t.Fatal("expected symlink file rejection")
	}
}

func (s *Store) filePath() string { return filepath.Join(s.root, "board.json") }
