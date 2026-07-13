package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadAndExternalChange(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New("goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "file.txt"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.SnapshotFile(s, "file.txt"); err != nil {
		t.Fatal(err)
	}
	s.Summary = "# summary"
	if err = store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Goal != "goal" || loaded.SchemaVersion != SchemaVersion {
		t.Fatalf("loaded=%+v", loaded)
	}
	if err = os.WriteFile(filepath.Join(root, "file.txt"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load(context.Background(), s.ID)
	if err == nil || !strings.Contains(err.Error(), "externally") {
		t.Fatalf("expected external-change error, got %v", err)
	}
}
func TestSchemaVersionAndCorruptCheckpoint(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("goal", root, "digest")
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gohermit", "sessions", s.ID, "session.json")
	b, _ := os.ReadFile(path)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	raw["schema_version"] = 999
	b, _ = json.Marshal(raw)
	_ = os.WriteFile(path, b, 0600)
	if _, err := store.Load(context.Background(), s.ID); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error=%v", err)
	}
	_ = os.WriteFile(path, []byte("{"), 0600)
	if _, err := store.Load(context.Background(), s.ID); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("error=%v", err)
	}
}
func TestClean(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("goal", root, "digest")
	_ = store.Save(context.Background(), s)
	dir := filepath.Join(root, ".gohermit", "sessions", s.ID)
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(dir, old, old)
	n, err := store.Clean(context.Background(), 24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("cleaned=%d err=%v", n, err)
	}
}

func TestDeletedFileSnapshot(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("goal", root, "digest")
	if err := store.SnapshotFile(s, "gone.txt"); err != nil {
		t.Fatal(err)
	}
	if s.ModifiedFiles["gone.txt"] != "deleted" {
		t.Fatalf("snapshot=%v", s.ModifiedFiles)
	}
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone.txt"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), s.ID); err == nil {
		t.Fatal("expected recreated-file conflict")
	}
}

func TestGitStateChangeRejected(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("goal", root, "digest")
	s.GitState = GitState(context.Background(), root)
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("change"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), s.ID); err == nil || !strings.Contains(err.Error(), "Git state") {
		t.Fatalf("error=%v", err)
	}
}
