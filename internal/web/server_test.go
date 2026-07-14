package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GOHERMIT_AUTH_STORE", filepath.Join(root, "credentials.json"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "missing-codex"))
	if err := os.WriteFile(filepath.Join(root, "hermit.toml"), []byte("[model]\nprovider = \"codex\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := New(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestAPIKeySettingsControlAvailableCatalogWithoutLeakingSecret(t *testing.T) {
	server := testServer(t)
	handler := server.Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), `"available_companies":[{"id":"deepseek"`) {
		t.Fatal("unconfigured provider appeared in runnable catalog")
	}

	request = httptest.NewRequest(http.MethodPut, "/api/settings/providers/deepseek/api-key", strings.NewReader(`{"api_key":"secret-key"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret-key") {
		t.Fatalf("save status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/info", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), `"id":"deepseek"`) || !strings.Contains(response.Body.String(), `"configured":true`) || strings.Contains(response.Body.String(), "secret-key") {
		t.Fatalf("info did not reflect safe configured status: %s", response.Body.String())
	}
}

func TestHealthAndInfoDoNotExposeKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "top-secret")
	server := testServer(t)
	for _, path := range []string{"/api/health", "/api/info"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "top-secret") {
			t.Fatalf("%s leaked API key", path)
		}
		if path == "/api/info" && (!strings.Contains(response.Body.String(), `"openai-codex"`) || !strings.Contains(response.Body.String(), `"auth_type":"oauth_external"`)) {
			t.Fatalf("%s missing grouped provider catalog: %s", path, response.Body.String())
		}
	}
}

func TestRunRejectsCrossOriginAndConcurrentTask(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"task":"test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d", response.Code)
	}

	server.active.Store(true)
	request = httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"task":"test"}`))
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("concurrent status=%d", response.Code)
	}
}

func TestStaticIndexHasSecurityHeaders(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "GoHermit") || !strings.Contains(response.Body.String(), "nav-settings") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
}
