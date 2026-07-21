package evals

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/model"
)

// liveSmokeEnv gates the paid live Codex smoke test; only "1" opts in.
const liveSmokeEnv = "GOHERMIT_LIVE_CODEX_SMOKE"

// liveSmokeEnabled reports whether the opt-in live smoke may run. Default and
// CI push/PR runs leave the variable unset, so no paid call can trigger.
func liveSmokeEnabled() bool {
	return os.Getenv(liveSmokeEnv) == "1"
}

// TestLiveCodexSmoke makes one bounded non-streaming call against the real
// Codex backend. It is strictly opt-in and skips when no credentials resolve;
// missing credentials are an environment fact, not a test failure.
func TestLiveCodexSmoke(t *testing.T) {
	if !liveSmokeEnabled() {
		t.Skip("live Codex smoke is opt-in; set GOHERMIT_LIVE_CODEX_SMOKE=1 with valid credentials")
	}
	resolveCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	credentials, err := auth.ResolveCodex(resolveCtx)
	cancel()
	if err != nil {
		t.Skipf("no Codex credentials configured: %v", err)
	}
	preset, ok := codexPreset()
	if !ok {
		t.Fatal("openai-codex preset missing from config.ModelPresets()")
	}
	// Same constructor path as internal/app.NewProvider for the codex slug.
	provider, err := model.NewResponsesProvider(model.ResponsesConfig{BaseURL: preset.BaseURL, APIKey: credentials.Token, Headers: credentials.Headers, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := provider.Generate(ctx, model.GenerateRequest{
		Model:    preset.Model,
		Messages: []model.Message{{Role: model.RoleUser, Content: "Reply with the word: ok"}},
	})
	if err != nil {
		t.Fatalf("live Codex generate: %v", err)
	}
	if strings.TrimSpace(response.Message.Content) == "" {
		t.Fatal("live Codex reply is empty")
	}
	if response.Attempts < 1 {
		t.Fatalf("attempts=%d, want >= 1", response.Attempts)
	}
	usage := response.Usage
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 {
		t.Fatalf("negative usage: %+v", usage)
	}
	// Numbers only: never log the prompt, the reply, or auth headers.
	t.Logf("attempts=%d prompt_tokens=%d completion_tokens=%d total_tokens=%d", response.Attempts, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
}

// TestLiveSmokeRequiresExplicitOptIn runs by default and verifies the paid
// smoke gate stays closed unless the exact opt-in value is set.
func TestLiveSmokeRequiresExplicitOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "true", "yes"} {
		t.Setenv(liveSmokeEnv, value)
		if liveSmokeEnabled() {
			t.Fatalf("live smoke enabled for %s=%q", liveSmokeEnv, value)
		}
	}
	t.Setenv(liveSmokeEnv, "1")
	if !liveSmokeEnabled() {
		t.Fatalf("live smoke disabled for %s=1", liveSmokeEnv)
	}
}

// codexPreset mirrors the production openai-codex defaults from config.
func codexPreset() (config.ModelPreset, bool) {
	for _, preset := range config.ModelPresets() {
		if preset.Provider == "openai-codex" {
			return preset, true
		}
	}
	return config.ModelPreset{}, false
}
