package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenClawSendUsesBoundedAuthenticatedAgentHook(t *testing.T) {
	var got struct {
		Message string `json:"message"`
		Deliver bool   `json:"deliver"`
		Channel string `json:"channel"`
		To      string `json:"to"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hook-secret" {
			t.Error("missing hook auth")
		}
		if r.Header.Get("X-GoHermit-Report-ID") != "report-1" {
			t.Error("missing report id")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	config := OpenClawConfig{URL: server.URL, Token: "hook-secret", Channel: "openclaw-weixin", Target: "wx-user"}
	if err := config.Send(context.Background(), "report-1", "Daily report", "hello"); err != nil {
		t.Fatal(err)
	}
	if got.Message != "hello" || !got.Deliver || got.Channel != "openclaw-weixin" || got.To != "wx-user" {
		t.Fatalf("payload = %+v", got)
	}
}

func TestOpenClawSendRejectsInvalidURLAndOversizedMessage(t *testing.T) {
	config := OpenClawConfig{URL: "file:///etc/passwd", Token: "x", Channel: "openclaw-weixin", Target: "wx"}
	if err := config.Send(context.Background(), "id", "name", "body"); err == nil {
		t.Fatal("file URL accepted")
	}
	config.URL = "http://127.0.0.1:1"
	if err := config.Send(context.Background(), "id", "name", string(make([]byte, 12<<10+1))); err == nil {
		t.Fatal("oversized report accepted")
	}
}
