package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolRecordValidationRejectsUnsafeRecoveryEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Session, *ToolRecord)
	}{
		{
			name: "noncanonical digest",
			mutate: func(_ *Session, record *ToolRecord) {
				record.ArgsDigest = strings.Repeat("A", 64)
			},
		},
		{
			name: "invalid digest length",
			mutate: func(_ *Session, record *ToolRecord) {
				record.ArgsDigest = "abc"
			},
		},
		{
			name: "unknown status",
			mutate: func(_ *Session, record *ToolRecord) {
				record.Status = "replayed"
			},
		},
		{
			name: "missing run",
			mutate: func(_ *Session, record *ToolRecord) {
				record.RunID = "missing-run"
			},
		},
		{
			name: "zero turn",
			mutate: func(_ *Session, record *ToolRecord) {
				record.Turn = 0
			},
		},
		{
			name: "turn outside run",
			mutate: func(_ *Session, record *ToolRecord) {
				record.Turn = 4
			},
		},
		{
			name: "completion before start",
			mutate: func(_ *Session, record *ToolRecord) {
				before := record.StartedAt.Add(-time.Second)
				record.CompletedAt = &before
			},
		},
		{
			name: "started has completion",
			mutate: func(_ *Session, record *ToolRecord) {
				record.Status = "started"
			},
		},
		{
			name: "completed missing completion",
			mutate: func(_ *Session, record *ToolRecord) {
				record.CompletedAt = nil
			},
		},
		{
			name: "call ID too long",
			mutate: func(_ *Session, record *ToolRecord) {
				record.CallID = strings.Repeat("c", maxToolCallIDBytes+1)
			},
		},
		{
			name: "tool name too long",
			mutate: func(_ *Session, record *ToolRecord) {
				record.Name = strings.Repeat("n", maxToolNameBytes+1)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, value := validToolRecordSession(t, root)
			test.mutate(value, &value.ToolCalls[0])
			if err := store.Save(context.Background(), value); err == nil {
				t.Fatal("unsafe ToolRecord must be rejected before persistence")
			}
		})
	}
}

func TestToolRecordCorruptionMakesLoadAndRecoverFailClosed(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Store, string) error
	}{
		{
			name: "load",
			run: func(store *Store, id string) error {
				_, err := store.Load(context.Background(), id)
				return err
			},
		},
		{
			name: "recover",
			run: func(store *Store, id string) error {
				_, err := store.Recover(context.Background(), id)
				return err
			},
		},
	} {
		for _, test := range []struct {
			name   string
			mutate func(*ToolRecord)
		}{
			{name: "digest", mutate: func(record *ToolRecord) { record.ArgsDigest = strings.Repeat("A", 64) }},
			{name: "status", mutate: func(record *ToolRecord) { record.Status = "unknown" }},
			{name: "run", mutate: func(record *ToolRecord) { record.RunID = "wrong-run" }},
			{name: "turn", mutate: func(record *ToolRecord) { record.Turn = -1 }},
			{name: "time", mutate: func(record *ToolRecord) {
				before := record.StartedAt.Add(-time.Second)
				record.CompletedAt = &before
			}},
		} {
			t.Run(operation.name+"/"+test.name, func(t *testing.T) {
				root := t.TempDir()
				store, value := validToolRecordSession(t, root)
				if err := store.Save(context.Background(), value); err != nil {
					t.Fatal(err)
				}
				test.mutate(&value.ToolCalls[0])
				overwriteSessionCheckpoint(t, root, value)
				reopened, err := NewStore(root, ".gohermit")
				if err != nil {
					t.Fatal(err)
				}
				if err = operation.run(reopened, value.ID); err == nil {
					t.Fatal("corrupt ToolRecord must fail closed")
				}
			})
		}
	}
}

func TestCommitJournalApplyRejectsCorruptToolRecord(t *testing.T) {
	root := t.TempDir()
	store, value := validToolRecordSession(t, root)
	value.ToolCalls = nil
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	_, valid := validToolRecordSessionValue(t, root, value.ID)
	value.ToolCalls = valid.ToolCalls
	store.commitStageHook = func(stage string) error {
		if stage == "journal_written" {
			return errors.New("simulated crash")
		}
		return nil
	}
	if err := store.Save(context.Background(), value); err == nil {
		t.Fatal("expected simulated crash")
	}
	journalPath := filepath.Join(root, ".gohermit", "sessions", value.ID, "commit.json")
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var journal commitJournal
	if err = json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	journal.Session.ToolCalls[0].Status = "unknown"
	raw, err = json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(journalPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Load(context.Background(), value.ID); err == nil {
		t.Fatal("corrupt commit journal ToolRecord must not be applied")
	}
	checkpointRaw, err := os.ReadFile(filepath.Join(root, ".gohermit", "sessions", value.ID, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint Session
	if err = json.Unmarshal(checkpointRaw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.ToolCalls) != 0 {
		t.Fatal("corrupt journal modified the durable checkpoint")
	}
}

func TestApplyJournalValidatesToolRecordBeforeWritingCheckpoint(t *testing.T) {
	root := t.TempDir()
	store, value := validToolRecordSession(t, root)
	value.ToolCalls[0].Status = "unknown"
	err := store.applyJournalLocked(value.ID, commitJournal{
		Version: commitJournalVersion,
		Session: value,
	})
	if err == nil {
		t.Fatal("direct journal apply must reject corrupt ToolRecord")
	}
	checkpoint := filepath.Join(root, ".gohermit", "sessions", value.ID, "session.json")
	if _, statErr := os.Stat(checkpoint); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt journal wrote checkpoint: %v", statErr)
	}
}

func TestLegacyToolRecordWithoutDigestRoundTrips(t *testing.T) {
	root := t.TempDir()
	store, value := validToolRecordSession(t, root)
	record := &value.ToolCalls[0]
	record.ArgsDigest = ""
	record.Turn = 0
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Load(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ToolCalls) != 1 || loaded.ToolCalls[0].ArgsDigest != "" || loaded.ToolCalls[0].Turn != 0 {
		t.Fatalf("legacy record changed: %+v", loaded.ToolCalls)
	}
}

func validToolRecordSession(t *testing.T, root string) (*Store, *Session) {
	t.Helper()
	store, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	_, value := validToolRecordSessionValue(t, root, "")
	return store, value
}

func validToolRecordSessionValue(t *testing.T, root, stableID string) (*Store, *Session) {
	t.Helper()
	store, err := NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	value, err := New("recover tools", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if stableID != "" {
		value.ID = stableID
	}
	run, err := value.NewRunWithID("run-tools", "perform a bounded tool call")
	if err != nil {
		t.Fatal(err)
	}
	value.Turns = 3
	run.EndTurn = 3
	run.Status = RunInterrupted
	started := time.Now().UTC().Add(-time.Second)
	completed := started.Add(time.Millisecond)
	value.ToolCalls = []ToolRecord{{
		Time: started, StartedAt: started, CompletedAt: &completed,
		RunID: run.ID, Turn: 3, CallID: "call-1", Name: "noop",
		ArgsDigest: strings.Repeat("a", 64), Status: "completed",
	}}
	return store, value
}

func overwriteSessionCheckpoint(t *testing.T, root string, value *Session) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gohermit", "sessions", value.ID, "session.json")
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
