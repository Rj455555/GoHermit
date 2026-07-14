package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	codexDeviceURL   = "https://auth.openai.com/api/accounts/deviceauth"
	codexVerifyURL   = "https://auth.openai.com/codex/device"
	codexRedirectURL = "https://auth.openai.com/deviceauth/callback"
)

// LoginSession is the secret-free state returned to the browser during device login.
type LoginSession struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	UserCode        string    `json:"user_code,omitempty"`
	VerificationURL string    `json:"verification_url,omitempty"`
	Error           string    `json:"error,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// LoginManager owns short-lived Codex device-code login sessions.
type LoginManager struct {
	store     *Store
	client    *http.Client
	deviceURL string
	tokenURL  string
	mu        sync.Mutex
	sessions  map[string]LoginSession
}

func NewLoginManager(store *Store) *LoginManager {
	return &LoginManager{
		store: store, client: &http.Client{Timeout: 25 * time.Second},
		deviceURL: codexDeviceURL, tokenURL: codexTokenURL,
		sessions: make(map[string]LoginSession),
	}
}

// Start requests a device code and begins polling without exposing OAuth tokens to the browser.
func (m *LoginManager) Start(ctx context.Context) (LoginSession, error) {
	var issued struct {
		DeviceAuthID string      `json:"device_auth_id"`
		UserCode     string      `json:"user_code"`
		Interval     flexibleInt `json:"interval"`
	}
	if err := m.postJSON(ctx, m.deviceURL+"/usercode", map[string]string{"client_id": codexClientID}, &issued); err != nil {
		return LoginSession{}, fmt.Errorf("start Codex login: %w", err)
	}
	if issued.DeviceAuthID == "" || issued.UserCode == "" {
		return LoginSession{}, errors.New("start Codex login: authorization server returned an incomplete device code")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return LoginSession{}, fmt.Errorf("create login session: %w", err)
	}
	session := LoginSession{
		ID: hex.EncodeToString(idBytes), Status: "pending", UserCode: issued.UserCode,
		VerificationURL: codexVerifyURL, ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}
	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()
	interval := time.Duration(issued.Interval) * time.Second
	if interval < 2*time.Second {
		interval = 5 * time.Second
	}
	go m.poll(session.ID, issued.DeviceAuthID, issued.UserCode, interval)
	return session, nil
}

// flexibleInt accepts OAuth servers that encode interval as either 5 or "5".
type flexibleInt int

func (value *flexibleInt) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*value = flexibleInt(number)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return errors.New("device login interval must be a number or numeric string")
	}
	number, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return errors.New("device login interval must be a number or numeric string")
	}
	*value = flexibleInt(number)
	return nil
}

func (m *LoginManager) Status(id string) (LoginSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if ok && session.Status == "pending" && time.Now().After(session.ExpiresAt) {
		session.Status = "expired"
		session.Error = "登录已过期，请重新开始。"
		m.sessions[id] = session
	}
	return session, ok
}

func (m *LoginManager) poll(id, deviceAuthID, userCode string, interval time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.finish(id, "expired", "登录已过期，请重新开始。")
			return
		case <-ticker.C:
			var grant struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			status, err := m.postJSONStatus(ctx, m.deviceURL+"/token", map[string]string{
				"device_auth_id": deviceAuthID, "user_code": userCode,
			}, &grant)
			if status == http.StatusForbidden || status == http.StatusNotFound {
				continue
			}
			if err != nil {
				m.finish(id, "error", err.Error())
				return
			}
			if grant.AuthorizationCode == "" || grant.CodeVerifier == "" {
				m.finish(id, "error", "Codex 登录响应不完整。")
				return
			}
			tokens, err := m.exchange(ctx, grant.AuthorizationCode, grant.CodeVerifier)
			if err != nil {
				m.finish(id, "error", err.Error())
				return
			}
			if err = m.store.saveCodex(tokens); err != nil {
				m.finish(id, "error", "保存 Codex 登录失败："+err.Error())
				return
			}
			m.finish(id, "approved", "")
			return
		}
	}
}

func (m *LoginManager) exchange(ctx context.Context, code, verifier string) (codexTokens, error) {
	values := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {codexRedirectURL},
		"client_id": {codexClientID}, "code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return codexTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(req)
	if err != nil {
		return codexTokens{}, fmt.Errorf("exchange Codex login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return codexTokens{}, fmt.Errorf("Codex login exchange returned HTTP %d", resp.StatusCode)
	}
	var tokens codexTokens
	if err = json.NewDecoder(resp.Body).Decode(&tokens); err != nil || strings.TrimSpace(tokens.AccessToken) == "" {
		return codexTokens{}, errors.New("Codex login exchange did not return a valid access token")
	}
	return tokens, nil
}

func (m *LoginManager) postJSON(ctx context.Context, endpoint string, payload any, target any) error {
	_, err := m.postJSONStatus(ctx, endpoint, payload, target)
	return err
}

func (m *LoginManager) postJSONStatus(ctx context.Context, endpoint string, payload any, target any) (int, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gohermit/0.2")
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("authorization server returned HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(target); err != nil {
		return resp.StatusCode, fmt.Errorf("decode authorization response: %w", err)
	}
	return resp.StatusCode, nil
}

func (m *LoginManager) finish(id, status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return
	}
	session.Status, session.Error = status, message
	m.sessions[id] = session
}
