package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/channel/weixin"
	"github.com/Rj455555/GoHermit/internal/channelstore"
)

type channelTestBackend struct{}

func (channelTestBackend) StartLogin(context.Context, string) (weixin.QRStart, error) {
	return weixin.QRStart{QRContent: "opaque", ImageContent: "data:image/png;base64,AA==", ExpiresInSeconds: 60}, nil
}
func (channelTestBackend) PollLogin(context.Context, string, string) (weixin.QRStatus, error) {
	return weixin.QRStatus{Status: "pending"}, nil
}
func (channelTestBackend) GetUpdates(context.Context, string, string, string) (weixin.Updates, error) {
	return weixin.Updates{}, nil
}
func (channelTestBackend) SendMessage(context.Context, string, string, string, string, string) error {
	return nil
}
func (channelTestBackend) GetConfig(context.Context, string, string, string, string) error {
	return nil
}
func (channelTestBackend) SendTyping(context.Context, string, string, string, string, bool) error {
	return nil
}
func (channelTestBackend) Logout(context.Context, string, string) error {
	return nil
}

func TestWeixinChannelAPIDoesNotExposeSecrets(t *testing.T) {
	server := testServer(t)
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.channels = weixin.NewService(store, channelTestBackend{}, server.svc)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/channels/weixin/login", strings.NewReader("{\"account_id\":\"account-1\",\"base_url\":\"https://example.test\",\"label\":\"primary\"}"))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["token"]; exists || strings.Contains(response.Body.String(), "opaque") {
		t.Fatalf("login response leaked secret: %s", response.Body.String())
	}
	attemptID, ok := body["id"].(string)
	if !ok || attemptID == "" {
		t.Fatalf("attempt id missing: %#v", body)
	}
	qr := httptest.NewRecorder()
	server.Handler().ServeHTTP(qr, httptest.NewRequest(http.MethodGet, "/api/channels/weixin/login/"+attemptID+"/qr", nil))
	if qr.Code != http.StatusOK || qr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("qr response = %d headers=%v", qr.Code, qr.Header())
	}
	if strings.Contains(qr.Body.String(), "opaque") {
		t.Fatal("QR bearer reflected in fallback response")
	}
}

func TestWeixinAccountBindingRoutesAreAccountScoped(t *testing.T) {
	server := testServer(t)
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.channels = weixin.NewService(store, channelTestBackend{}, server.svc)
	if err := store.UpsertBinding(channelstore.Binding{ID: "binding-1", AccountID: "account-1", EmployeeID: "employee-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/channels/weixin/accounts/account-1/bindings", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "binding-1") {
		t.Fatalf("scoped bindings response = %d body=%s", response.Code, response.Body.String())
	}
	other := httptest.NewRecorder()
	server.Handler().ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/api/channels/weixin/accounts/account-2/bindings", nil))
	if other.Code != http.StatusOK || strings.Contains(other.Body.String(), "binding-1") {
		t.Fatalf("cross-account binding response = %d body=%s", other.Code, other.Body.String())
	}
}

func TestWeixinConversationRouteReturnsInboundAndOutboundProjection(t *testing.T) {
	server := testServer(t)
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server.channels = weixin.NewService(store, channelTestBackend{}, server.svc)
	now := time.Now().UTC()
	if err = store.SaveAccount(channelstore.Account{SchemaVersion: channelstore.SchemaVersion, ID: "account-1", State: channelstore.StateConnected, BaseURL: "https://example.test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Ingest(channelstore.Inbound{AccountID: "account-1", PeerID: "peer-1", GroupID: "group-1", MessageID: "message-1", Sequence: 1, Text: "inbound", ContextToken: "secret-context"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.EnqueueOutbox(channelstore.OutboxMessage{ID: "out-1", AccountID: "account-1", PeerID: "peer-1", MessageID: "message-1", Kind: "ack", Text: "outbound", State: "sent"}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/channels/weixin/conversations?account_id=account-1&limit=20", nil)
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"direction":"inbound"`) || !strings.Contains(response.Body.String(), `"direction":"outbound"`) {
		t.Fatalf("conversation response = %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-context") {
		t.Fatalf("conversation leaked context token: %s", response.Body.String())
	}
}
