package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Keep the notification projection covered at the transport boundary. This
// test intentionally uses an unconfigured service; it proves that the
// endpoint is safe to expose without ever returning an SMTP secret.
func TestNotificationStatusProjection(t *testing.T) {
	server, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); body == "" || strings.Contains(body, "password") || strings.Contains(body, "secret") || strings.Contains(body, "api_key") {
		t.Fatalf("unsafe notification projection: %s", body)
	}
}
