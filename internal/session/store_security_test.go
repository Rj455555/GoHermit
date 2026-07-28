package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionStoreRejectsStoreRootAndSessionsSymlinks(t *testing.T) {
	for _, test := range []struct {
		name string
		link func(workspace, outside string) error
	}{
		{
			name: "store root",
			link: func(workspace, outside string) error {
				return os.Symlink(outside, filepath.Join(workspace, ".gohermit"))
			},
		},
		{
			name: "sessions directory",
			link: func(workspace, outside string) error {
				if err := os.Mkdir(filepath.Join(workspace, ".gohermit"), 0o755); err != nil {
					return err
				}
				return os.Symlink(outside, filepath.Join(workspace, ".gohermit", "sessions"))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			outside := t.TempDir()
			marker := filepath.Join(outside, "marker.txt")
			if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := test.link(workspace, outside); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(workspace, ".gohermit"); err == nil {
				t.Fatal("unsafe Session Store root must fail closed")
			}
			assertFileContent(t, marker, "unchanged")
		})
	}
}

func TestSessionStoreRejectsUnsafeSessionTargets(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, sessionDir, outside string)
	}{
		{
			name: "session directory symlink",
			setup: func(t *testing.T, sessionDir, outside string) {
				t.Helper()
				if err := os.Symlink(outside, sessionDir); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "session checkpoint symlink",
			setup: func(t *testing.T, sessionDir, outside string) {
				t.Helper()
				if err := os.Mkdir(sessionDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "marker.txt"), filepath.Join(sessionDir, "session.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "commit journal symlink",
			setup: func(t *testing.T, sessionDir, outside string) {
				t.Helper()
				if err := os.Mkdir(sessionDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "marker.txt"), filepath.Join(sessionDir, "commit.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "non regular checkpoint",
			setup: func(t *testing.T, sessionDir, _ string) {
				t.Helper()
				if err := os.Mkdir(sessionDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(sessionDir, "session.json"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			outside := t.TempDir()
			marker := filepath.Join(outside, "marker.txt")
			if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(workspace, ".gohermit")
			if err != nil {
				t.Fatal(err)
			}
			sessionsDir := filepath.Join(workspace, ".gohermit", "sessions")
			if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			sessionID := "stable-session-id"
			test.setup(t, filepath.Join(sessionsDir, sessionID), outside)

			if _, err = store.CheckTarget(sessionID); err == nil {
				t.Fatal("unsafe Session target readiness must fail closed")
			}
			value, newErr := NewPrepared(
				sessionID, "Review.", workspace, "config-digest",
				"employee-a", "task-a", 7, strings.Repeat("a", 64), preparedCompactSnapshot(),
			)
			if newErr != nil {
				t.Fatal(newErr)
			}
			if err = store.Save(context.Background(), value); err == nil {
				t.Fatal("unsafe Session target write must fail closed")
			}
			assertFileContent(t, marker, "unchanged")
		})
	}
}

func TestSessionStoreCheckTargetDistinguishesMissingAndUnsafe(t *testing.T) {
	workspace := t.TempDir()
	store, err := NewStore(workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	exists, err := store.CheckTarget("stable-session-id")
	if err != nil || exists {
		t.Fatalf("missing safe target = %t, %v", exists, err)
	}
	value, err := NewPrepared(
		"stable-session-id", "Review.", workspace, "config-digest",
		"employee-a", "task-a", 7, strings.Repeat("a", 64), preparedCompactSnapshot(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	exists, err = store.CheckTarget(value.ID)
	if err != nil || !exists {
		t.Fatalf("existing safe target = %t, %v", exists, err)
	}
	reopened, err := NewStore(workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Load(context.Background(), value.ID); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != expected {
		t.Fatalf("%s changed: %q", path, raw)
	}
}
