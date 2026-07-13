// Package auth resolves account-based model credentials without exposing them to callers.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexTokenURL = "https://auth.openai.com/oauth/token"
)

// CodexCredentials contains the transient bearer and required backend headers.
type CodexCredentials struct {
	Token   string
	Headers map[string]string
	Source  string
}

type codexTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// CodexStatus reports whether a usable Codex account login can be found.
func CodexStatus() (bool, string) {
	if strings.TrimSpace(os.Getenv("GOHERMIT_CODEX_ACCESS_TOKEN")) != "" {
		return true, "environment"
	}
	tokens, path, err := readCodexCLIAuth()
	if err != nil {
		return false, "Run `codex login` on the host and mount CODEX_HOME read-only."
	}
	if tokenExpiring(tokens.AccessToken, 0) && tokens.RefreshToken == "" {
		return false, "Codex login is expired; run `codex login` again."
	}
	return true, path
}

// ResolveCodex imports Codex CLI auth and refreshes an expiring token in memory.
func ResolveCodex(ctx context.Context) (CodexCredentials, error) {
	accessToken := strings.TrimSpace(os.Getenv("GOHERMIT_CODEX_ACCESS_TOKEN"))
	source := "environment"
	var refreshToken string
	if accessToken == "" {
		tokens, path, err := readCodexCLIAuth()
		if err != nil {
			return CodexCredentials{}, err
		}
		accessToken, refreshToken, source = tokens.AccessToken, tokens.RefreshToken, path
	}
	if tokenExpiring(accessToken, 120*time.Second) {
		if refreshToken == "" {
			return CodexCredentials{}, errors.New("Codex access token is expiring and no refresh token is available; run `codex login`")
		}
		refreshed, err := refreshCodex(ctx, refreshToken)
		if err != nil {
			return CodexCredentials{}, err
		}
		accessToken = refreshed.AccessToken
	}
	headers := map[string]string{
		"User-Agent": "codex_cli_rs/0.0.0 (GoHermit)",
		"originator": "codex_cli_rs",
	}
	if accountID := codexAccountID(accessToken); accountID != "" {
		headers["ChatGPT-Account-ID"] = accountID
	}
	return CodexCredentials{Token: accessToken, Headers: headers, Source: source}, nil
}

func readCodexCLIAuth() (codexTokens, string, error) {
	home := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return codexTokens{}, "", errors.New("CODEX_HOME is not configured")
		}
		home = filepath.Join(userHome, ".codex")
	}
	path := filepath.Join(home, "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return codexTokens{}, "", fmt.Errorf("Codex CLI login not found at %s: %w", path, err)
	}
	var payload struct {
		Tokens codexTokens `json:"tokens"`
	}
	if err = json.Unmarshal(data, &payload); err != nil {
		return codexTokens{}, "", fmt.Errorf("decode Codex CLI auth: %w", err)
	}
	if strings.TrimSpace(payload.Tokens.AccessToken) == "" {
		return codexTokens{}, "", errors.New("Codex CLI auth is missing access_token")
	}
	payload.Tokens.AccessToken = strings.TrimSpace(payload.Tokens.AccessToken)
	payload.Tokens.RefreshToken = strings.TrimSpace(payload.Tokens.RefreshToken)
	return payload.Tokens, path, nil
}

func refreshCodex(ctx context.Context, refreshToken string) (codexTokens, error) {
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {codexClientID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return codexTokens{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "gohermit/0.2")
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return codexTokens{}, fmt.Errorf("refresh Codex token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return codexTokens{}, fmt.Errorf("Codex token refresh returned HTTP %d; run `codex login` if the token was revoked", response.StatusCode)
	}
	var tokens codexTokens
	if err = json.NewDecoder(response.Body).Decode(&tokens); err != nil {
		return codexTokens{}, fmt.Errorf("decode Codex token refresh: %w", err)
	}
	if tokens.AccessToken == "" {
		return codexTokens{}, errors.New("Codex token refresh did not return access_token")
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refreshToken
	}
	return tokens, nil
}

func tokenExpiring(token string, skew time.Duration) bool {
	claims := jwtClaims(token)
	exp, ok := claims["exp"].(float64)
	if !ok {
		return false
	}
	return time.Now().Add(skew).Unix() >= int64(exp)
}

func codexAccountID(token string) string {
	claims := jwtClaims(token)
	authClaim, _ := claims["https://api.openai.com/auth"].(map[string]any)
	accountID, _ := authClaim["chatgpt_account_id"].(string)
	return accountID
}

func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(data, &claims) != nil {
		return nil
	}
	return claims
}
