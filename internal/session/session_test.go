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

	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
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
	loaded, err = store.Load(context.Background(), s.ID)
	if err != nil || !loaded.WorkspaceChanged {
		t.Fatalf("expected reconciled external change, loaded=%+v err=%v", loaded, err)
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
	loaded, err := store.Load(context.Background(), s.ID)
	if err != nil || !loaded.WorkspaceChanged {
		t.Fatalf("expected recreated file to require reconciliation: loaded=%+v err=%v", loaded, err)
	}
}

func TestGitStateChangeRequiresReconciliation(t *testing.T) {
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
	loaded, err := store.Load(context.Background(), s.ID)
	if err != nil || !loaded.WorkspaceChanged {
		t.Fatalf("loaded=%+v error=%v", loaded, err)
	}
}

func TestSchemaV1MigrationAndVisibleHistory(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("legacy goal", root, "digest")
	s.SchemaVersion = 1
	s.Status = Completed
	s.RecentMessages = []model.Message{{Role: model.RoleUser, Content: "legacy goal"}, {Role: model.RoleAssistant, Content: "done"}}
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gohermit", "sessions", s.ID, "session.json")
	b, _ := os.ReadFile(path)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	for _, key := range []string{"title", "selection", "runs", "active_run_id", "next_event_sequence", "workspace_changed"} {
		delete(raw, key)
	}
	raw["schema_version"] = float64(1)
	b, _ = json.Marshal(raw)
	_ = os.WriteFile(path, b, 0600)
	loaded, err := store.Load(context.Background(), s.ID)
	if err != nil || loaded.SchemaVersion != SchemaVersion || loaded.Status != Open || len(loaded.Runs) != 1 || loaded.Runs[0].Status != RunCompleted {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	messages, err := store.Messages(s.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestSchemaV2MigrationKeepsSingleAgentSession(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("v2 goal", root, "digest")
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gohermit", "sessions", s.ID, "session.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["schema_version"] = float64(2)
	delete(document, "mission")
	raw, _ = json.Marshal(document)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), s.ID)
	if err != nil || loaded.SchemaVersion != SchemaVersion || loaded.Mission != nil {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestEventSequenceAndMessageHistory(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root, ".gohermit")
	s, _ := New("goal", root, "digest")
	first := store.BufferEvent(s.ID, event.New(event.TaskStarted, s.ID))
	second := store.BufferEvent(s.ID, event.New(event.TaskCompleted, s.ID))
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences=%d,%d", first.Sequence, second.Sequence)
	}
	if err := store.AppendMessage(s.ID, MessageRecord{RunID: "r1", Role: model.RoleUser, Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(s.ID, 1)
	if err != nil || len(events) != 1 || events[0].Sequence != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	messages, err := store.Messages(s.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestEventSequenceContinuesAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	firstStore, _ := NewStore(root, ".gohermit")
	s, _ := New("goal", root, "digest")
	firstStore.SeedEventSequence(s.ID, 9)
	e := firstStore.BufferEvent(s.ID, event.New(event.TaskStarted, s.ID))
	if err := firstStore.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if e.Sequence != 10 || s.NextEventSequence != 10 {
		t.Fatalf("first sequence=%d session=%d", e.Sequence, s.NextEventSequence)
	}
	secondStore, _ := NewStore(root, ".gohermit")
	loaded, err := secondStore.Load(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondStore.SeedEventSequence(loaded.ID, loaded.NextEventSequence)
	next := secondStore.BufferEvent(loaded.ID, event.New(event.TaskCompleted, loaded.ID))
	if next.Sequence != 11 {
		t.Fatalf("next sequence=%d", next.Sequence)
	}
}
