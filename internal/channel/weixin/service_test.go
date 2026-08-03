package weixin

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/channelstore"
)

type fakeBackend struct {
	mu      sync.Mutex
	login   QRStart
	status  QRStatus
	updates []Updates
	sent    []string
}

func (f *fakeBackend) StartLogin(context.Context, string) (QRStart, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.login, nil
}
func (f *fakeBackend) PollLogin(context.Context, string, string) (QRStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}
func (f *fakeBackend) GetUpdates(context.Context, string, string, string) (Updates, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) == 0 {
		return Updates{}, nil
	}
	next := f.updates[0]
	f.updates = f.updates[1:]
	return next, nil
}
func (f *fakeBackend) SendMessage(_ context.Context, _, _, _, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	return nil
}
func (f *fakeBackend) GetConfig(context.Context, string, string, string, string) error { return nil }
func (f *fakeBackend) SendTyping(context.Context, string, string, string, string, bool) error {
	return nil
}
func (f *fakeBackend) Logout(context.Context, string, string) error { return nil }

type fakeTaskSink struct {
	mu      sync.Mutex
	prompts []string
}

func (f *fakeTaskSink) CreateQueuedTask(_ context.Context, _ string, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	return "task-queued", nil
}

func TestLoginStoresNoPublicTokenAndCreatesConnectedAccount(t *testing.T) {
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{
		login:  QRStart{QRContent: "opaque-qr", ImageContent: "data:image/png;base64,AA==", ExpiresInSeconds: 60},
		status: QRStatus{Status: "confirmed", AccountID: "bot-1", UserID: "user-1", Token: "secret-token"},
	}
	service := NewService(store, backend, nil)
	attempt, err := service.StartLogin(context.Background(), "account-1", "https://example.test", "primary")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.State != channelstore.StateQRPending {
		t.Fatalf("state = %s", attempt.State)
	}
	persisted, err := store.GetAttempt(attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.QRContent != "" || persisted.QRImage != "" {
		t.Fatal("QR authorization data persisted in public login attempt")
	}
	loginSecret, err := store.LoadLoginSecret(attempt.ID)
	if err != nil || loginSecret.QRContent != "opaque-qr" {
		t.Fatalf("login QR secret = %#v err=%v", loginSecret, err)
	}
	if _, err := service.LoginStatus(context.Background(), attempt.ID); err != nil {
		t.Fatal(err)
	}
	account, err := store.GetAccount("account-1")
	if err != nil {
		t.Fatal(err)
	}
	if account.State != channelstore.StateConnected {
		t.Fatalf("account state = %s", account.State)
	}
	secret, err := store.LoadSecret("account-1")
	if err != nil || secret.Token != "secret-token" {
		t.Fatalf("secret not stored separately: %#v %v", secret, err)
	}
}

func TestInboundBoundTaskWaitsForOwnerStartAndIsIdempotent(t *testing.T) {
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{}
	sink := &fakeTaskSink{}
	service := NewService(store, backend, sink)
	now := time.Now().UTC()
	if err := store.SaveAccount(channelstore.Account{SchemaVersion: channelstore.SchemaVersion, ID: "account-1", State: channelstore.StateConnected, BaseURL: "https://example.test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSecret("account-1", channelstore.Secret{SchemaVersion: channelstore.SchemaVersion, Token: "token", ContextTokens: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertBinding(channelstore.Binding{SchemaVersion: channelstore.SchemaVersion, ID: "binding-1", AccountID: "account-1", PeerID: "peer-1", EmployeeID: "employee-1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	message := Message{ID: "message-1", PeerID: "peer-1", Text: "请检查项目", ContextToken: "ctx-1"}
	service.handleMessage(context.Background(), channelstore.Account{ID: "account-1", BaseURL: "https://example.test"}, channelstore.Secret{SchemaVersion: channelstore.SchemaVersion, Token: "token", ContextTokens: map[string]string{}}, message)
	service.handleMessage(context.Background(), channelstore.Account{ID: "account-1", BaseURL: "https://example.test"}, channelstore.Secret{SchemaVersion: channelstore.SchemaVersion, Token: "token", ContextTokens: map[string]string{}}, message)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.prompts) != 1 || sink.prompts[0] != "请检查项目" {
		t.Fatalf("queued task calls = %#v", sink.prompts)
	}
	if len(backend.sent) != 1 || backend.sent[0] != FixedQueuedAcknowledgement {
		t.Fatalf("acknowledgements = %#v", backend.sent)
	}
}

func TestUnboundMessageNeverCreatesTask(t *testing.T) {
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{}
	sink := &fakeTaskSink{}
	service := NewService(store, backend, sink)
	now := time.Now().UTC()
	secret := channelstore.Secret{SchemaVersion: channelstore.SchemaVersion, Token: "token", ContextTokens: map[string]string{}}
	service.handleMessage(context.Background(), channelstore.Account{ID: "account-1", BaseURL: "https://example.test"}, secret, Message{ID: "m-1", PeerID: "peer-1", Text: "hello"})
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.prompts) != 0 {
		t.Fatal("unbound message created a task")
	}
	if len(backend.sent) != 1 || backend.sent[0] != FixedUnboundAcknowledgement {
		t.Fatalf("unbound response = %#v", backend.sent)
	}
	_ = now
}

func TestInvalidEndpointAndOversizedMessageFailClosed(t *testing.T) {
	store, err := channelstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeBackend{login: QRStart{QRContent: "q", ImageContent: "i", ExpiresInSeconds: 1}}, nil)
	if _, err := service.StartLogin(context.Background(), "account-1", "http://user:password@example.test", ""); err == nil {
		t.Fatal("credential-bearing endpoint accepted")
	}
	if _, _, err := store.Ingest(channelstore.Inbound{AccountID: "account-1", PeerID: "peer-1", MessageID: "m", Text: string(make([]byte, channelstore.MaxMessageBytes+1))}); err == nil {
		t.Fatal("oversized message accepted")
	}
	if !errors.Is(store.DeleteBinding("missing"), channelstore.ErrNotFound) {
		t.Fatal("missing binding should be not found")
	}
}

func TestStableIDHandlesEmptyParts(t *testing.T) {
	if stableID() == "" || stableID("account", "peer", "message", "ack") == stableID("account", "peer", "message", "final") {
		t.Fatal("stable outbox IDs are not bounded and distinct")
	}
}
