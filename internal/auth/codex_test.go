package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveCodexImportsCLIAuthAndBuildsHeaders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("GOHERMIT_CODEX_ACCESS_TOKEN", "")
	claims, _ := json.Marshal(map[string]any{
		"exp":                         float64(time.Now().Add(time.Hour).Unix()),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct_test"},
	})
	token := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	payload, _ := json.Marshal(map[string]any{"tokens": map[string]any{"access_token": token, "refresh_token": "refresh"}})
	if err := os.WriteFile(filepath.Join(home, "auth.json"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	credentials, err := ResolveCodex(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token != token || credentials.Headers["ChatGPT-Account-ID"] != "acct_test" || credentials.Headers["originator"] != "codex_cli_rs" {
		t.Fatalf("credentials=%+v", credentials)
	}
	configured, _ := CodexStatus(t.Context(), nil)
	if !configured {
		t.Fatal("Codex CLI auth should be configured")
	}
}
