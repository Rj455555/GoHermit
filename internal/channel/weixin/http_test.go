package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBackendCursorContextAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/getupdates" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("AuthorizationType") != "ilink_bot_token" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("User-Agent") != BotAgent || r.Header.Get("X-WECHAT-UIN") == "" {
			t.Fatalf("headers = %#v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["get_updates_buf"] != "cursor-1" {
			t.Fatalf("cursor = %#v", body["get_updates_buf"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "get_updates_buf": "cursor-2", "msgs": []any{map[string]any{"message_id": "m-1", "from_user_id": "peer-1", "context_token": "ctx-1", "item_list": []any{map[string]any{"type": 1, "text_item": map[string]any{"text": "你好"}}}}}})
	}))
	defer server.Close()
	updates, err := NewHTTPBackend(server.Client()).GetUpdates(context.Background(), server.URL, "token", "cursor-1")
	if err != nil {
		t.Fatal(err)
	}
	if updates.Cursor != "cursor-2" || len(updates.Messages) != 1 || updates.Messages[0].Text != "你好" || updates.Messages[0].ContextToken != "ctx-1" {
		t.Fatalf("updates = %#v", updates)
	}
}

func TestHTTPBackendPollLoginPreservesQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/get_qrcode_status" || r.URL.Query().Get("qrcode") != "opaque-qr" {
			t.Fatalf("QR status request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "scanned"})
	}))
	defer server.Close()
	backend := NewHTTPBackend(server.Client())
	status, err := backend.PollLogin(context.Background(), server.URL, "opaque-qr")
	if err != nil || status.Status != "scanned" {
		t.Fatalf("status = %#v err=%v", status, err)
	}
}

func TestHTTPBackendRejectsRedirectAndInvalidEndpoint(t *testing.T) {
	backend := NewHTTPBackend(nil)
	if _, err := backend.GetUpdates(context.Background(), "file:///tmp", "token", ""); err == nil {
		t.Fatal("file endpoint accepted")
	}
	redirect := httptest.NewServer(http.RedirectHandler("http://example.invalid", http.StatusFound))
	defer redirect.Close()
	if _, err := backend.GetUpdates(context.Background(), redirect.URL, "token", ""); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestHTTPBackendRejectsUnallowlistedOrigin(t *testing.T) {
	backend := NewHTTPBackend(nil)
	if _, err := backend.GetUpdates(context.Background(), "https://example.com", "token", ""); err == nil {
		t.Fatal("unallowlisted HTTPS origin accepted")
	}
}

func TestHTTPBackendRejectsOversizedMessageProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "msgs": []any{map[string]any{
			"message_id": "message-1", "from_user_id": "peer-1", "item_list": []any{map[string]any{
				"type": 1, "text_item": map[string]any{"text": strings.Repeat("x", MaxTextBytes+1)},
			}},
		}}})
	}))
	defer server.Close()
	if _, err := NewHTTPBackend(server.Client()).GetUpdates(context.Background(), server.URL, "token", ""); err == nil {
		t.Fatal("oversized message accepted")
	}
}
