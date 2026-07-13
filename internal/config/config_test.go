package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndRejectUnknown(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.toml"), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Agent.MaxTurns != 50 {
		t.Fatalf("default max turns=%d", c.Agent.MaxTurns)
	}
	path := filepath.Join(t.TempDir(), "bad.toml")
	if err = os.WriteFile(path, []byte("[agent]\nunknown = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(path, false)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}
func TestUnsafeStorageOptionsRejected(t *testing.T) {
	c := Default()
	c.Storage.SaveFullPrompts = true
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation failure")
	}
}
