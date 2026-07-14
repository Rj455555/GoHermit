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

func TestResolveSelectionUsesLiveCodexModels(t *testing.T) {
	selection := RuntimeSelection{Company: "openai", Access: "openai-codex", Model: "account-model", Agent: "coding"}
	preset, _, err := ResolveSelectionWithModels(selection, []ModelOption{{ID: "account-model", Label: "Account Model", Provider: "openai-codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if preset.Model != "account-model" || preset.Provider != "openai-codex" {
		t.Fatalf("preset=%+v", preset)
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

func TestHermesStyleProviderGroupingAndSelection(t *testing.T) {
	companies := CompanyPresets()
	if len(companies) < 3 || companies[0].ID != "openai" {
		t.Fatalf("companies=%+v", companies)
	}
	if companies[0].Access[0].ID != "openai-codex" || companies[0].Access[0].AuthType != "oauth_external" {
		t.Fatalf("OpenAI Codex descriptor=%+v", companies[0].Access[0])
	}
	preset, profile, err := ResolveSelection(RuntimeSelection{Company: "openai", Access: "openai-codex", Model: "gpt-5.3-codex", Agent: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if preset.Provider != "openai-codex" || preset.BaseURL != "https://chatgpt.com/backend-api/codex" || !profile.ReadOnly {
		t.Fatalf("preset=%+v profile=%+v", preset, profile)
	}
	if _, _, err = ResolveSelection(RuntimeSelection{Company: "openai", Access: "openai-codex", Model: "deepseek-chat", Agent: "coding"}); err == nil {
		t.Fatal("expected cross-provider model rejection")
	}
}
