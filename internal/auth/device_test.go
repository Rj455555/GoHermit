package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestLoginManagerCompletesDeviceFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /device/usercode", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_auth_id": "device", "user_code": "TEST-CODE", "interval": "2"})
	})
	mux.HandleFunc("POST /device/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization_code": "code", "code_verifier": "verifier"})
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh"})
	})
	issuer := httptest.NewServer(mux)
	defer issuer.Close()
	store, _ := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	manager := NewLoginManager(store)
	manager.client = issuer.Client()
	manager.deviceURL = issuer.URL + "/device"
	manager.tokenURL = issuer.URL + "/oauth/token"
	session, err := manager.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if session.UserCode != "TEST-CODE" || session.Status != "pending" {
		t.Fatalf("session=%+v", session)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := manager.Status(session.ID)
		if status.Status == "approved" {
			if tokens, ok := store.codexTokens(); !ok || tokens.AccessToken != "access" {
				t.Fatalf("tokens=%+v ok=%v", tokens, ok)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("device login did not complete")
}
