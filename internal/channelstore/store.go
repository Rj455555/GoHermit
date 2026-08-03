// Package channelstore persists bounded Weixin channel metadata. It never
// owns Session, Run, Plan, Tool, Approval, Verification, or recovery truth.
package channelstore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/storage"
)

const (
	SchemaVersion     = 1
	MaxFileBytes      = 256 << 10
	MaxMessageBytes   = 16 << 10
	MaxAccounts       = 32
	MaxBindings       = 512
	MaxInbox          = 4096
	MaxCursorBytes    = 64 << 10
	MaxContextToken   = 16 << 10
	MaxQRContentBytes = 256 << 10
)

var (
	ErrNotFound = errors.New("channel record not found")
	ErrCorrupt  = errors.New("channel store is corrupt")
	ErrConflict = errors.New("channel record conflict")
)

type AccountState string

const (
	StateDisconnected AccountState = "disconnected"
	StateQRPending    AccountState = "qr_pending"
	StateScanned      AccountState = "scanned"
	StateConfirmed    AccountState = "confirmed"
	StateConnected    AccountState = "connected"
	StateExpired      AccountState = "expired"
	StateReconnecting AccountState = "reconnecting"
	StateLoggedOut    AccountState = "logged_out"
	StateFailed       AccountState = "failed"
)

type Account struct {
	SchemaVersion int
	ID            string
	Label         string
	State         AccountState
	BaseURL       string
	WeixinUserID  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastError     string
}

type LoginAttempt struct {
	SchemaVersion int
	ID            string
	AccountID     string
	State         AccountState
	QRContent     string
	QRImage       string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type LoginSecret struct {
	SchemaVersion int
	QRContent     string
	QRImage       string
}

type Binding struct {
	SchemaVersion   int
	ID              string
	AccountID       string
	PeerID          string
	GroupID         string
	EmployeeID      string
	Enabled         bool
	MentionRequired bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type InboxMessage struct {
	SchemaVersion int
	ID            string
	AccountID     string
	PeerID        string
	GroupID       string
	MessageID     string
	Sequence      int64
	Text          string
	ContextToken  string
	State         string
	TaskID        string
	ReceivedAt    time.Time
}

type Inbound struct {
	AccountID    string
	PeerID       string
	GroupID      string
	MessageID    string
	Sequence     int64
	Text         string
	ContextToken string
}

type OutboxMessage struct {
	SchemaVersion int
	ID            string
	AccountID     string
	PeerID        string
	MessageID     string
	Kind          string
	Text          string
	State         string
	Attempts      int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("GOHERMIT_CHANNEL_STORE"))
	}
	if root == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve channel store: %w", err)
		}
		root = filepath.Join(config, "gohermit", "channels")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	if err := rejectSymlink(absolute); err != nil {
		return nil, err
	}
	return &Store{root: filepath.Clean(absolute)}, nil
}

func NewID(prefix string) string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(value[:])
}

func (s *Store) ListAccounts() ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.root, "accounts"))
	if errors.Is(err, os.ErrNotExist) {
		return []Account{}, nil
	}
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		value, readErr := s.readJSON(filepath.Join("accounts", entry.Name(), "account.json"), &Account{})
		if readErr != nil {
			return nil, readErr
		}
		account := *(value.(*Account))
		if err := validateAccount(account); err != nil {
			return nil, ErrCorrupt
		}
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	if len(accounts) > MaxAccounts {
		return nil, ErrCorrupt
	}
	return accounts, nil
}

func (s *Store) GetAccount(id string) (Account, error) {
	if !validID(id) {
		return Account{}, errors.New("invalid channel account id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.readJSON(filepath.Join("accounts", id, "account.json"), &Account{})
	if errors.Is(err, os.ErrNotExist) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	account := *(value.(*Account))
	if err := validateAccount(account); err != nil {
		return Account{}, ErrCorrupt
	}
	return account, nil
}

func (s *Store) SaveAccount(account Account) error {
	if err := validateAccount(account); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join("accounts", account.ID, "account.json"), account)
}

func (s *Store) DeleteAccount(id string) error {
	if !validID(id) {
		return errors.New("invalid channel account id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.RemoveAll(filepath.Join(s.root, "accounts", id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type Secret struct {
	SchemaVersion int
	Token         string
	ContextTokens map[string]string
}

func (s *Store) SaveSecret(accountID string, secret Secret) error {
	if !validID(accountID) || !validateSecret(secret) {
		return errors.New("invalid channel secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join("accounts", accountID, "credentials.json"), secret)
}

func (s *Store) LoadSecret(accountID string) (Secret, error) {
	if !validID(accountID) {
		return Secret{}, errors.New("invalid channel account id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.readJSON(filepath.Join("accounts", accountID, "credentials.json"), &Secret{})
	if errors.Is(err, os.ErrNotExist) {
		return Secret{}, ErrNotFound
	}
	if err != nil {
		return Secret{}, err
	}
	secret := *(value.(*Secret))
	if !validateSecret(secret) {
		return Secret{}, ErrCorrupt
	}
	return secret, nil
}

func (s *Store) SaveAttempt(attempt LoginAttempt) error {
	if !validID(attempt.ID) || !validID(attempt.AccountID) || attempt.SchemaVersion != SchemaVersion || attempt.ExpiresAt.IsZero() || attempt.CreatedAt.IsZero() || attempt.UpdatedAt.IsZero() || len(attempt.QRContent) > MaxQRContentBytes || len(attempt.QRImage) > MaxQRContentBytes || !utf8.ValidString(attempt.QRContent) || !utf8.ValidString(attempt.QRImage) {
		return errors.New("invalid login attempt")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join("login-attempts", attempt.ID+".json"), attempt)
}

func (s *Store) SaveLoginSecret(attemptID string, secret LoginSecret) error {
	if !validID(attemptID) || secret.SchemaVersion != SchemaVersion || len(secret.QRContent) > MaxQRContentBytes || len(secret.QRImage) > MaxQRContentBytes || !utf8.ValidString(secret.QRContent) || !utf8.ValidString(secret.QRImage) {
		return errors.New("invalid login secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join("login-attempts", attemptID+".credentials.json"), secret)
}

func (s *Store) LoadLoginSecret(attemptID string) (LoginSecret, error) {
	if !validID(attemptID) {
		return LoginSecret{}, errors.New("invalid login attempt id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.readJSON(filepath.Join("login-attempts", attemptID+".credentials.json"), &LoginSecret{})
	if errors.Is(err, os.ErrNotExist) {
		return LoginSecret{}, ErrNotFound
	}
	if err != nil {
		return LoginSecret{}, err
	}
	secret := *(value.(*LoginSecret))
	if secret.SchemaVersion != SchemaVersion || len(secret.QRContent) > MaxQRContentBytes || len(secret.QRImage) > MaxQRContentBytes || !utf8.ValidString(secret.QRContent) || !utf8.ValidString(secret.QRImage) {
		return LoginSecret{}, ErrCorrupt
	}
	return secret, nil
}

func (s *Store) DeleteLoginSecret(attemptID string) error {
	if !validID(attemptID) {
		return errors.New("invalid login attempt id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.root, "login-attempts", attemptID+".credentials.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) GetAttempt(id string) (LoginAttempt, error) {
	if !validID(id) {
		return LoginAttempt{}, errors.New("invalid login attempt id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.readJSON(filepath.Join("login-attempts", id+".json"), &LoginAttempt{})
	if errors.Is(err, os.ErrNotExist) {
		return LoginAttempt{}, ErrNotFound
	}
	if err != nil {
		return LoginAttempt{}, err
	}
	attempt := *(value.(*LoginAttempt))
	if attempt.SchemaVersion != SchemaVersion || !validID(attempt.AccountID) || len(attempt.QRContent) > MaxQRContentBytes || len(attempt.QRImage) > MaxQRContentBytes {
		return LoginAttempt{}, ErrCorrupt
	}
	return attempt, nil
}

func (s *Store) DeleteAttempt(id string) error {
	if !validID(id) {
		return errors.New("invalid login attempt id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.root, "login-attempts", id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	secretErr := os.Remove(filepath.Join(s.root, "login-attempts", id+".credentials.json"))
	if errors.Is(secretErr, os.ErrNotExist) {
		secretErr = nil
	}
	if err != nil {
		return err
	}
	return secretErr
}

// DeleteLoginAttemptsForAccount removes prior attempts for an account before
// a deliberate QR refresh. It keeps login credentials bounded without
// touching attempts belonging to another account.
func (s *Store) DeleteLoginAttemptsForAccount(accountID string) error {
	if !validID(accountID) {
		return errors.New("invalid channel account id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.root, "login-attempts"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".credentials.json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validID(id) {
			return ErrCorrupt
		}
		value, readErr := s.readJSON(filepath.Join("login-attempts", entry.Name()), &LoginAttempt{})
		if readErr != nil {
			return ErrCorrupt
		}
		attempt := *(value.(*LoginAttempt))
		if attempt.AccountID != accountID {
			continue
		}
		if err := os.Remove(filepath.Join(s.root, "login-attempts", entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(filepath.Join(s.root, "login-attempts", id+".credentials.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) LoadCursor(accountID string) (string, error) {
	if !validID(accountID) {
		return "", errors.New("invalid channel account id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var value struct {
		SchemaVersion int
		Cursor        string
	}
	_, err := s.readJSON(filepath.Join("accounts", accountID, "cursor.json"), &value)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil || value.SchemaVersion != SchemaVersion || !validText(value.Cursor, MaxCursorBytes) {
		return "", ErrCorrupt
	}
	return value.Cursor, nil
}

func (s *Store) SaveCursor(accountID, cursor string) error {
	if !validID(accountID) || !validText(cursor, MaxCursorBytes) {
		return errors.New("invalid cursor")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join("accounts", accountID, "cursor.json"), struct {
		SchemaVersion int
		Cursor        string
	}{SchemaVersion: SchemaVersion, Cursor: cursor})
}

func (s *Store) ListBindings() ([]Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value struct {
		SchemaVersion int
		Bindings      []Binding
	}
	_, err := s.readJSON("bindings.json", &value)
	if errors.Is(err, os.ErrNotExist) {
		return []Binding{}, nil
	}
	if err != nil || value.SchemaVersion != SchemaVersion || len(value.Bindings) > MaxBindings {
		return nil, ErrCorrupt
	}
	return append([]Binding(nil), value.Bindings...), nil
}

func (s *Store) UpsertBinding(binding Binding) error {
	if !validID(binding.ID) || !validID(binding.AccountID) || !validID(binding.EmployeeID) || len(binding.PeerID) > 512 || len(binding.GroupID) > 512 {
		return errors.New("invalid channel binding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var value struct {
		SchemaVersion int
		Bindings      []Binding
	}
	_, err := s.readJSON("bindings.json", &value)
	if errors.Is(err, os.ErrNotExist) {
		value.SchemaVersion = SchemaVersion
	} else if err != nil || value.SchemaVersion != SchemaVersion {
		return ErrCorrupt
	}
	found := false
	for i := range value.Bindings {
		if value.Bindings[i].ID == binding.ID {
			value.Bindings[i] = binding
			found = true
			break
		}
	}
	if !found {
		if len(value.Bindings) >= MaxBindings {
			return ErrConflict
		}
		value.Bindings = append(value.Bindings, binding)
	}
	sort.Slice(value.Bindings, func(i, j int) bool { return value.Bindings[i].ID < value.Bindings[j].ID })
	return s.writeJSON("bindings.json", value)
}

func (s *Store) DeleteBinding(id string) error {
	if !validID(id) {
		return errors.New("invalid channel binding id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var value struct {
		SchemaVersion int
		Bindings      []Binding
	}
	_, err := s.readJSON("bindings.json", &value)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil || value.SchemaVersion != SchemaVersion {
		return ErrCorrupt
	}
	kept := value.Bindings[:0]
	found := false
	for _, binding := range value.Bindings {
		if binding.ID == id {
			found = true
			continue
		}
		kept = append(kept, binding)
	}
	if !found {
		return ErrNotFound
	}
	value.Bindings = kept
	return s.writeJSON("bindings.json", value)
}

func (s *Store) ResolveBinding(accountID, peerID, groupID string) (Binding, bool, error) {
	bindings, err := s.ListBindings()
	if err != nil {
		return Binding{}, false, err
	}
	var groupFallback *Binding
	var accountFallback *Binding
	for _, binding := range bindings {
		if binding.AccountID != accountID || !binding.Enabled {
			continue
		}
		if groupID != "" && binding.GroupID == groupID && binding.PeerID == peerID {
			return binding, true, nil
		}
		if groupID != "" && binding.GroupID == groupID && binding.PeerID == "" {
			copy := binding
			groupFallback = &copy
		}
		if groupID == "" && binding.GroupID == "" && binding.PeerID == peerID {
			return binding, true, nil
		}
		if binding.GroupID == "" && binding.PeerID == "" {
			copy := binding
			accountFallback = &copy
		}
	}
	if groupFallback != nil {
		return *groupFallback, true, nil
	}
	if accountFallback != nil {
		return *accountFallback, true, nil
	}
	return Binding{}, false, nil
}

func (s *Store) Ingest(inbound Inbound) (InboxMessage, bool, error) {
	if !validID(inbound.AccountID) || inbound.PeerID == "" || inbound.MessageID == "" || len(inbound.PeerID) > 512 || len(inbound.GroupID) > 512 || len(inbound.MessageID) > 512 || !validText(inbound.Text, MaxMessageBytes) || !validText(inbound.ContextToken, MaxContextToken) {
		return InboxMessage{}, false, errors.New("invalid inbound message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := filepath.Join("accounts", inbound.AccountID, "inbox.json")
	var value struct {
		SchemaVersion int
		Messages      []InboxMessage
	}
	_, err := s.readJSON(file, &value)
	if errors.Is(err, os.ErrNotExist) {
		value.SchemaVersion = SchemaVersion
	} else if err != nil || value.SchemaVersion != SchemaVersion || len(value.Messages) > MaxInbox {
		return InboxMessage{}, false, ErrCorrupt
	}
	for _, existing := range value.Messages {
		if existing.AccountID == inbound.AccountID && existing.MessageID == inbound.MessageID && existing.PeerID == inbound.PeerID && existing.GroupID == inbound.GroupID {
			return existing, true, nil
		}
	}
	message := InboxMessage{SchemaVersion: SchemaVersion, ID: NewID("inbox"), AccountID: inbound.AccountID, PeerID: inbound.PeerID, GroupID: inbound.GroupID, MessageID: inbound.MessageID, Sequence: inbound.Sequence, Text: inbound.Text, State: "received", ReceivedAt: time.Now().UTC()}
	value.Messages = append(value.Messages, message)
	if len(value.Messages) > MaxInbox {
		value.Messages = value.Messages[len(value.Messages)-MaxInbox:]
	}
	if err := s.writeJSON(file, value); err != nil {
		return InboxMessage{}, false, err
	}
	return message, false, nil
}

func (s *Store) UpdateInbox(message InboxMessage) error {
	if !validID(message.ID) || !validID(message.AccountID) || message.MessageID == "" || !validText(message.Text, MaxMessageBytes) {
		return errors.New("invalid inbox message")
	}
	// Conversation context tokens are secrets and live only in Secret files.
	message.ContextToken = ""
	s.mu.Lock()
	defer s.mu.Unlock()
	file := filepath.Join("accounts", message.AccountID, "inbox.json")
	var value struct {
		SchemaVersion int
		Messages      []InboxMessage
	}
	_, err := s.readJSON(file, &value)
	if err != nil || value.SchemaVersion != SchemaVersion {
		return ErrCorrupt
	}
	for i := range value.Messages {
		if value.Messages[i].ID == message.ID {
			value.Messages[i] = message
			return s.writeJSON(file, value)
		}
	}
	return ErrNotFound
}

func (s *Store) ListInbox(accountID string, limit int) ([]InboxMessage, error) {
	if !validID(accountID) {
		return nil, errors.New("invalid channel account id")
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var value struct {
		SchemaVersion int
		Messages      []InboxMessage
	}
	_, err := s.readJSON(filepath.Join("accounts", accountID, "inbox.json"), &value)
	if errors.Is(err, os.ErrNotExist) {
		return []InboxMessage{}, nil
	}
	if err != nil || value.SchemaVersion != SchemaVersion || len(value.Messages) > MaxInbox {
		return nil, ErrCorrupt
	}
	if len(value.Messages) > limit {
		value.Messages = value.Messages[len(value.Messages)-limit:]
	}
	result := append([]InboxMessage(nil), value.Messages...)
	for i := range result {
		result[i].ContextToken = ""
	}
	return result, nil
}

func (s *Store) EnqueueOutbox(message OutboxMessage) (OutboxMessage, bool, error) {
	if !validID(message.ID) || !validID(message.AccountID) || message.PeerID == "" || message.MessageID == "" || message.Kind == "" || !validText(message.Text, MaxMessageBytes) || message.Attempts < 0 || message.Attempts > 8 {
		return OutboxMessage{}, false, errors.New("invalid outbox message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := filepath.Join("accounts", message.AccountID, "outbox.json")
	var value struct {
		SchemaVersion int
		Messages      []OutboxMessage
	}
	_, err := s.readJSON(file, &value)
	if errors.Is(err, os.ErrNotExist) {
		value.SchemaVersion = SchemaVersion
	} else if err != nil || value.SchemaVersion != SchemaVersion || len(value.Messages) > 1024 {
		return OutboxMessage{}, false, ErrCorrupt
	}
	for _, existing := range value.Messages {
		if existing.ID == message.ID {
			return existing, true, nil
		}
	}
	now := time.Now().UTC()
	message.SchemaVersion = SchemaVersion
	message.CreatedAt = now
	message.UpdatedAt = now
	value.Messages = append(value.Messages, message)
	if len(value.Messages) > 1024 {
		value.Messages = value.Messages[len(value.Messages)-1024:]
	}
	if err := s.writeJSON(file, value); err != nil {
		return OutboxMessage{}, false, err
	}
	return message, false, nil
}

func (s *Store) UpdateOutbox(message OutboxMessage) error {
	if !validID(message.ID) || !validID(message.AccountID) || message.PeerID == "" || !validText(message.Text, MaxMessageBytes) {
		return errors.New("invalid outbox message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := filepath.Join("accounts", message.AccountID, "outbox.json")
	var value struct {
		SchemaVersion int
		Messages      []OutboxMessage
	}
	_, err := s.readJSON(file, &value)
	if err != nil || value.SchemaVersion != SchemaVersion {
		return ErrCorrupt
	}
	for i := range value.Messages {
		if value.Messages[i].ID == message.ID {
			value.Messages[i] = message
			return s.writeJSON(file, value)
		}
	}
	return ErrNotFound
}

func (s *Store) writeJSON(relative string, value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > MaxFileBytes {
		return ErrCorrupt
	}
	data = normalizeJSONNames(data, false)
	file, err := s.safePath(relative)
	if err != nil {
		return err
	}
	return storage.AtomicWrite(file, append(data, '\n'), 0600)
}

func (s *Store) readJSON(relative string, target any) (any, error) {
	file, err := s.safePath(relative)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, ErrCorrupt
	}
	data, err := os.ReadFile(file)
	if err != nil || !utf8.Valid(data) {
		return nil, ErrCorrupt
	}
	data = normalizeJSONNames(data, true)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, ErrCorrupt
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, ErrCorrupt
	}
	return target, nil
}

func normalizeJSONNames(data []byte, toGo bool) []byte {
	names := map[string]string{
		"SchemaVersion":   "schema_version",
		"AccountID":       "account_id",
		"PeerID":          "peer_id",
		"GroupID":         "group_id",
		"MessageID":       "message_id",
		"ContextToken":    "context_token",
		"QRContent":       "qr_content",
		"QRImage":         "qr_image",
		"BaseURL":         "base_url",
		"WeixinUserID":    "weixin_user_id",
		"CreatedAt":       "created_at",
		"UpdatedAt":       "updated_at",
		"LastError":       "last_error",
		"ExpiresAt":       "expires_at",
		"TaskID":          "task_id",
		"ReceivedAt":      "received_at",
		"ID":              "id",
		"Label":           "label",
		"State":           "state",
		"Sequence":        "sequence",
		"Text":            "text",
		"Token":           "token",
		"ContextTokens":   "context_tokens",
		"Bindings":        "bindings",
		"Messages":        "messages",
		"EmployeeID":      "employee_id",
		"Enabled":         "enabled",
		"MentionRequired": "mention_required",
		"Kind":            "kind",
		"Attempts":        "attempts",
	}
	for goName, wireName := range names {
		from, to := goName, wireName
		if toGo {
			from, to = wireName, goName
		}
		data = bytes.ReplaceAll(data, []byte("\""+from+"\""), []byte("\""+to+"\""))
	}
	return data
}

func (s *Store) safePath(relative string) (string, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return "", ErrCorrupt
	}
	file := filepath.Join(s.root, relative)
	relativePath, err := filepath.Rel(s.root, file)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
		return "", ErrCorrupt
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return "", err
	}
	if err := rejectSymlink(filepath.Dir(file)); err != nil {
		return "", err
	}
	return file, nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrCorrupt
	}
	return nil
}

func validID(value string) bool {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validText(value string, max int) bool {
	return len(value) <= max && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00') && !strings.ContainsRune(value, '\uFFFD')
}

func validateAccount(value Account) error {
	if !validID(value.ID) || value.SchemaVersion != SchemaVersion || value.State == "" || value.BaseURL == "" || value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() {
		return errors.New("invalid channel account")
	}
	if len(value.BaseURL) > 2048 || len(value.Label) > 512 || len(value.LastError) > 4096 || !utf8.ValidString(value.BaseURL) {
		return errors.New("invalid channel account bounds")
	}
	return nil
}

func validateSecret(value Secret) bool {
	if value.SchemaVersion != SchemaVersion || !validText(value.Token, MaxContextToken) || len(value.ContextTokens) > MaxBindings {
		return false
	}
	for key, token := range value.ContextTokens {
		if !validText(key, 512) || !validText(token, MaxContextToken) {
			return false
		}
	}
	return true
}
