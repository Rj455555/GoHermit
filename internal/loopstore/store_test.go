package loopstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
)

func validDefinition(id string) loop.Definition {
	return loop.Definition{
		ID:                id,
		SchemaVersion:     loop.SchemaVersion,
		Name:              "loop " + id,
		Description:       "test loop",
		WorkspaceIdentity: "github.com/acme/widget",
		Enabled:           true,
		TaskSource:        loop.TaskSource{Type: loop.TaskSourceFixedPrompt, Prompt: "review the latest changes"},
		AgentSelection:    loop.AgentSelection{Company: "acme", Access: "api", Model: "model-x", Agent: "hermit"},
		PlanMode:          loop.PlanAuto,
		VerificationRecipe: loop.VerificationRecipe{
			Checks: []loop.RecipeCheck{
				{ID: "vet", Command: []string{"go", "vet", "./..."}, Required: true, TimeoutSeconds: 120},
			},
			MaxRepairAttempts: 1,
		},
		Budget:          loop.Budget{MaxModelCalls: 10, MaxTokens: 100_000, TimeoutSeconds: 900},
		ApprovalPolicy:  loop.ApprovalPolicy{RequireForMutation: true},
		WorkspacePolicy: loop.WorkspacePolicy{RequireCleanGit: true},
		OutputPolicy:    loop.OutputPolicy{IncludeDiff: true, MaxReportBytes: 64 << 10},
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "loops"))
	if err != nil {
		t.Fatalf("NewStore = %v", err)
	}
	return store
}

func TestPathResolution(t *testing.T) {
	t.Run("explicit wins over env", func(t *testing.T) {
		explicit := filepath.Join(t.TempDir(), "explicit")
		t.Setenv("GOHERMIT_LOOP_STORE", filepath.Join(t.TempDir(), "env"))
		store, err := NewStore(explicit)
		if err != nil {
			t.Fatalf("NewStore = %v", err)
		}
		if store.Dir() != explicit {
			t.Fatalf("dir = %s, want %s", store.Dir(), explicit)
		}
	})
	t.Run("env wins over default", func(t *testing.T) {
		env := filepath.Join(t.TempDir(), "env")
		t.Setenv("GOHERMIT_LOOP_STORE", env)
		store, err := NewStore("")
		if err != nil {
			t.Fatalf("NewStore = %v", err)
		}
		if store.Dir() != env {
			t.Fatalf("dir = %s, want %s", store.Dir(), env)
		}
	})
	t.Run("default under user config dir, never the workspace", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GOHERMIT_LOOP_STORE", "")
		workspace := t.TempDir()
		t.Chdir(workspace)
		store, err := NewStore(" ")
		if err != nil {
			t.Fatalf("NewStore = %v", err)
		}
		if strings.HasPrefix(store.Dir(), workspace) {
			t.Fatalf("store dir %s resolved inside the workspace", store.Dir())
		}
		config, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("UserConfigDir = %v", err)
		}
		want := filepath.Join(config, "gohermit", "loops")
		if store.Dir() != want {
			t.Fatalf("dir = %s, want %s", store.Dir(), want)
		}
	})
}

func TestDefinitionRoundTrip(t *testing.T) {
	store := newTestStore(t)
	d := validDefinition("loop-a")

	if err := store.SaveDefinition(d); err != nil {
		t.Fatalf("SaveDefinition = %v", err)
	}
	got, err := store.GetDefinition("loop-a")
	if err != nil {
		t.Fatalf("GetDefinition = %v", err)
	}
	if got.Revision != 1 {
		t.Fatalf("revision = %d, want 1 on insert", got.Revision)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("timestamps not stamped")
	}
	// Every caller-supplied field survives the round trip losslessly.
	want := d
	want.Revision, want.CreatedAt, want.UpdatedAt = got.Revision, got.CreatedAt, got.UpdatedAt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}

	// An update bumps the revision and keeps CreatedAt.
	d.Name = "renamed loop"
	d.CreatedAt = time.Time{} // caller cannot forge timestamps
	if err := store.SaveDefinition(d); err != nil {
		t.Fatalf("SaveDefinition(update) = %v", err)
	}
	updated, err := store.GetDefinition("loop-a")
	if err != nil {
		t.Fatalf("GetDefinition(update) = %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("revision = %d, want 2 on update", updated.Revision)
	}
	if !updated.CreatedAt.Equal(got.CreatedAt) {
		t.Fatal("CreatedAt changed on update")
	}
	if updated.Name != "renamed loop" {
		t.Fatal("update not persisted")
	}

	// List is sorted by id.
	if err = store.SaveDefinition(validDefinition("loop-c")); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveDefinition(validDefinition("loop-b")); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListDefinitions()
	if err != nil {
		t.Fatalf("ListDefinitions = %v", err)
	}
	if len(list) != 3 || list[0].ID != "loop-a" || list[1].ID != "loop-b" || list[2].ID != "loop-c" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Delete, then misses fail loudly.
	if err = store.DeleteDefinition("loop-b"); err != nil {
		t.Fatalf("DeleteDefinition = %v", err)
	}
	if _, err = store.GetDefinition("loop-b"); err == nil {
		t.Fatal("GetDefinition after delete succeeded, want error")
	}
	if err = store.DeleteDefinition("loop-b"); err == nil {
		t.Fatal("DeleteDefinition of missing id succeeded, want error")
	}
}

func TestSaveDefinitionValidates(t *testing.T) {
	store := newTestStore(t)
	d := validDefinition("loop-a")
	d.TaskSource.Type = "webhook"
	if err := store.SaveDefinition(d); err == nil {
		t.Fatal("SaveDefinition of invalid definition succeeded, want error")
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), "definitions.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid save wrote the definitions file")
	}
}

func TestDefinitionCountLimit(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < MaxDefinitions; i++ {
		if err := store.SaveDefinition(validDefinition(strings.Repeat("x", 3) + string(rune('a'+i%26)) + strings.Repeat("0", i/26))); err != nil {
			t.Fatalf("SaveDefinition %d = %v", i, err)
		}
	}
	if err := store.SaveDefinition(validDefinition("one-too-many")); err == nil {
		t.Fatal("SaveDefinition beyond the count limit succeeded, want error")
	}
}

func TestAtomicWriteMode(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveDefinition(validDefinition("loop-a")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(store.Dir(), "definitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("definitions.json mode = %o, want 600", perm)
	}
	inv := mustInvocation(t, "loop-a", time.Now().UTC())
	if err = store.SaveInvocation(inv); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Join(store.Dir(), "invocations", inv.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("invocation file mode = %o, want 600", perm)
	}
}

func writeDefinitions(t *testing.T, store *Store, content string) {
	t.Helper()
	if err := os.MkdirAll(store.Dir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "definitions.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptDefinitionsFile(t *testing.T) {
	store := newTestStore(t)
	corrupt := "{ not json"
	writeDefinitions(t, store, corrupt)

	if _, err := store.GetDefinition("loop-a"); err == nil {
		t.Fatal("GetDefinition on corrupt file succeeded, want error")
	}
	if _, err := store.ListDefinitions(); err == nil {
		t.Fatal("ListDefinitions on corrupt file succeeded, want error")
	}
	if err := store.SaveDefinition(validDefinition("loop-a")); err == nil {
		t.Fatal("SaveDefinition on corrupt file succeeded, want error")
	}
	// The corrupt file is never silently wiped.
	raw, err := os.ReadFile(filepath.Join(store.Dir(), "definitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != corrupt {
		t.Fatal("corrupt definitions file was modified")
	}
}

func TestConcurrentAccess(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveDefinition(validDefinition("loop-a")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 8)
	for i := 0; i < 4; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				d := validDefinition("loop-a")
				d.Name = "concurrent rename"
				if err := store.SaveDefinition(d); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()
		go func() {
			for j := 0; j < 10; j++ {
				if _, err := store.GetDefinition("loop-a"); err != nil {
					done <- err
					return
				}
				if _, err := store.ListDefinitions(); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent access = %v", err)
		}
	}
	// Concurrent updates serialize on the mutex, so every bump lands.
	got, err := store.GetDefinition("loop-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 41 {
		t.Fatalf("revision = %d, want 41 after 40 serialized updates", got.Revision)
	}
}

func TestDefinitionsDecodeDiscipline(t *testing.T) {
	t.Run("unknown schema version", func(t *testing.T) {
		store := newTestStore(t)
		writeDefinitions(t, store, `{"schema_version": 99, "definitions": []}`)
		if _, err := store.ListDefinitions(); err == nil {
			t.Fatal("unknown schema version accepted, want error")
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		store := newTestStore(t)
		writeDefinitions(t, store, `{"schema_version": 1, "definitions": [], "surprise": true}`)
		if _, err := store.ListDefinitions(); err == nil {
			t.Fatal("unknown JSON field accepted, want error")
		}
	})
	t.Run("oversize file", func(t *testing.T) {
		store := newTestStore(t)
		writeDefinitions(t, store, strings.Repeat(" ", MaxStoreBytes+1))
		if _, err := store.ListDefinitions(); err == nil {
			t.Fatal("oversize file accepted, want error")
		}
	})
}

func mustInvocation(t *testing.T, loopID string, createdAt time.Time) loop.Invocation {
	t.Helper()
	def := validDefinition(loopID)
	def.Revision = 1
	def.CreatedAt, def.UpdatedAt = createdAt, createdAt
	inv, err := loop.NewInvocation(def, loop.TriggerManual, def.TaskSource.Prompt, createdAt)
	if err != nil {
		t.Fatalf("NewInvocation = %v", err)
	}
	return inv
}

func TestInvocationSaveGetList(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	first := mustInvocation(t, "loop-a", base)
	second := mustInvocation(t, "loop-a", base.Add(2*time.Minute))
	other := mustInvocation(t, "loop-b", base.Add(time.Minute))

	for _, inv := range []loop.Invocation{second, other, first} { // saved out of order
		if err := store.SaveInvocation(inv); err != nil {
			t.Fatalf("SaveInvocation = %v", err)
		}
	}

	got, err := store.GetInvocation(first.ID)
	if err != nil {
		t.Fatalf("GetInvocation = %v", err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("invocation round trip mismatch:\n got %+v\nwant %+v", got, first)
	}

	// List filters by loop id and sorts by created_at.
	list, err := store.ListInvocations("loop-a")
	if err != nil {
		t.Fatalf("ListInvocations = %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("unexpected invocation list: %+v", list)
	}
	all, err := store.ListInvocations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != first.ID || all[1].ID != other.ID || all[2].ID != second.ID {
		t.Fatalf("unexpected unfiltered list: %+v", all)
	}

	// A transition applied by the caller is persisted on re-save.
	if err = first.Dispatch(); err != nil {
		t.Fatal(err)
	}
	if err = first.Attach("session-1", "run-1", base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveInvocation(first); err != nil {
		t.Fatalf("SaveInvocation(transition) = %v", err)
	}
	reloaded, err := store.GetInvocation(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != loop.Attached || reloaded.SessionID != "session-1" || reloaded.RunID != "run-1" {
		t.Fatalf("transition not persisted: %+v", reloaded)
	}

	// Invalid invocations and ids are refused.
	bad := mustInvocation(t, "loop-a", base)
	bad.Status = "running"
	if err = store.SaveInvocation(bad); err == nil {
		t.Fatal("SaveInvocation of invalid invocation succeeded, want error")
	}
	if _, err = store.GetInvocation("../escape"); err == nil {
		t.Fatal("GetInvocation with traversal id succeeded, want error")
	}
	if _, err = store.GetInvocation("missing"); err == nil {
		t.Fatal("GetInvocation of missing id succeeded, want error")
	}
}

func TestExportDefinition(t *testing.T) {
	store := newTestStore(t)
	d := validDefinition("loop-a")
	if err := store.SaveDefinition(d); err != nil {
		t.Fatal(err)
	}
	raw, err := store.ExportDefinition("loop-a")
	if err != nil {
		t.Fatalf("ExportDefinition = %v", err)
	}
	// A clean definition exports with its content intact and imports back.
	if !strings.Contains(string(raw), "review the latest changes") {
		t.Fatal("export dropped the task prompt")
	}
	imported, err := ImportDefinition(raw)
	if err != nil {
		t.Fatalf("ImportDefinition(export) = %v", err)
	}
	if imported.ID != d.ID || imported.Name != d.Name || imported.TaskSource.Prompt != d.TaskSource.Prompt {
		t.Fatal("export/import round trip lost content")
	}
	if _, err = store.ExportDefinition("missing"); err == nil {
		t.Fatal("ExportDefinition of missing id succeeded, want error")
	}

	// Redaction: the export path blanks any secret-looking field. Stored
	// definitions can never carry secrets (validation screens them), so the
	// redaction pass itself is exercised directly with planted markers.
	planted := validDefinition("loop-b")
	planted.Name = "token ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	planted.TaskSource.Prompt = "sync with api_key=deadbeef now"
	redacted := loop.RedactDefinition(planted)
	out, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ghp_") || strings.Contains(string(out), "api_key=") {
		t.Fatal("redacted export still carries secret markers")
	}
}

func TestImportDefinition(t *testing.T) {
	marshal := func(d loop.Definition) []byte {
		t.Helper()
		raw, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	t.Run("valid", func(t *testing.T) {
		imported, err := ImportDefinition(marshal(validDefinition("loop-a")))
		if err != nil {
			t.Fatalf("ImportDefinition = %v", err)
		}
		if imported.ID != "loop-a" {
			t.Fatalf("imported id = %s", imported.ID)
		}
	})

	// Every screened field location rejects a planted secret with
	// ErrImportSecret, distinct from generic validation failures.
	secretLocations := map[string]func(*loop.Definition){
		"name":         func(d *loop.Definition) { d.Name = "x ghp_abcdefghijklmnopqrstuvwxyz0123456789" },
		"description":  func(d *loop.Definition) { d.Description = "api_key=deadbeef" },
		"workspace":    func(d *loop.Definition) { d.WorkspaceIdentity = "github_pat_123456" },
		"template ref": func(d *loop.Definition) { d.TeamTemplateRef = "password=hunter2" },
		"prompt":       func(d *loop.Definition) { d.TaskSource.Prompt = "use sk-proj-abcdef" },
		"selection":    func(d *loop.Definition) { d.AgentSelection.Agent = "access_token=xyz" },
		"check id": func(d *loop.Definition) {
			d.VerificationRecipe.Checks[0].ID = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
		},
		"check command": func(d *loop.Definition) {
			d.VerificationRecipe.Checks[0].Command[1] = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
		},
	}
	for name, plant := range secretLocations {
		t.Run("secret in "+name, func(t *testing.T) {
			d := validDefinition("loop-a")
			plant(&d)
			_, err := ImportDefinition(marshal(d))
			if !errors.Is(err, ErrImportSecret) {
				t.Fatalf("ImportDefinition(secret in %s) = %v, want ErrImportSecret", name, err)
			}
		})
	}

	t.Run("unknown field", func(t *testing.T) {
		d := validDefinition("loop-a")
		withExtra := strings.Replace(string(marshal(d)), `"schema_version": 1,`, `"schema_version": 1, "surprise": true,`, 1)
		if _, err := ImportDefinition([]byte(withExtra)); err == nil {
			t.Fatal("unknown JSON field accepted, want error")
		}
	})
	t.Run("unknown schema version", func(t *testing.T) {
		d := validDefinition("loop-a")
		d.SchemaVersion = 99
		if _, err := ImportDefinition(marshal(d)); err == nil {
			t.Fatal("unknown schema version accepted, want error")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		if _, err := ImportDefinition(make([]byte, MaxStoreBytes+1)); err == nil {
			t.Fatal("oversize import accepted, want error")
		}
	})
	t.Run("invalid definition", func(t *testing.T) {
		d := validDefinition("loop-a")
		d.Name = ""
		_, err := ImportDefinition(marshal(d))
		if err == nil {
			t.Fatal("invalid definition imported, want error")
		}
		if errors.Is(err, ErrImportSecret) {
			t.Fatal("generic validation failure reported as ErrImportSecret")
		}
	})
}
