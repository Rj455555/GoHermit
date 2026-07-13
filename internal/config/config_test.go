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

func TestProviderPresetsFillOnlyMissingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex.toml")
	if err := os.WriteFile(path, []byte("[model]\nprovider = \"codex\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model.Protocol() != "responses" || c.Model.Name != "gpt-5.3-codex" || c.Model.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("codex preset=%+v", c.Model)
	}

	path = filepath.Join(t.TempDir(), "deepseek.toml")
	data := "[model]\nprovider = \"deepseek\"\nmodel = \"deepseek-v4-flash\"\napi_key_env = \"CUSTOM_KEY\"\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model.BaseURL != "https://api.deepseek.com" || c.Model.Name != "deepseek-v4-flash" || c.Model.APIKeyEnv != "CUSTOM_KEY" {
		t.Fatalf("deepseek preset=%+v", c.Model)
	}
}
