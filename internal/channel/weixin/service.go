package weixin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/channelstore"
)

const (
	FixedQueuedAcknowledgement      = "消息已收到，已进入任务收件箱，等待 Owner 显式开始。"
	FixedUnboundAcknowledgement     = "消息已收到，但当前会话尚未绑定 Employee；请先在设置中完成绑定。"
	FixedUnsupportedAcknowledgement = "消息已收到，但当前仅支持文本消息；该媒体类型已安全忽略。"
)

type TaskSink interface {
	CreateQueuedTask(ctx context.Context, employeeID, prompt string) (string, error)
}

type BindingValidator interface {
	ValidateChannelEmployee(employeeID string) error
}

type Service struct {
	store   *channelstore.Store
	backend Backend
	tasks   TaskSink
	mu      sync.Mutex
	pollers map[string]context.CancelFunc
	rootCtx context.Context
}

func NewService(store *channelstore.Store, backend Backend, tasks TaskSink) *Service {
	if backend == nil {
		backend = NewHTTPBackend(nil)
	}
	return &Service{store: store, backend: backend, tasks: tasks, pollers: make(map[string]context.CancelFunc)}
}

func NewServiceWithRoot(root string, backend Backend, tasks TaskSink) (*Service, error) {
	store, err := channelstore.New(root)
	if err != nil {
		return nil, err
	}
	return NewService(store, backend, tasks), nil
}

func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.rootCtx = ctx
	s.mu.Unlock()
	accounts, err := s.store.ListAccounts()
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if account.State == channelstore.StateConnected || account.State == channelstore.StateReconnecting {
			s.startPoller(account.ID)
		}
	}
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.pollers {
		cancel()
		delete(s.pollers, id)
	}
}

func (s *Service) ListAccounts() ([]channelstore.Account, error) {
	return s.store.ListAccounts()
}

func (s *Service) StartLogin(ctx context.Context, accountID, baseURL, label string) (channelstore.LoginAttempt, error) {
	if strings.TrimSpace(accountID) == "" {
		accountID = channelstore.NewID("wx")
	}
	if !validID(accountID) {
		return channelstore.LoginAttempt{}, errors.New("invalid Weixin account id")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if err := validateBaseURL(baseURL); err != nil {
		return channelstore.LoginAttempt{}, err
	}
	s.stopPoller(accountID)
	qr, err := s.backend.StartLogin(ctx, baseURL)
	if err != nil {
		return channelstore.LoginAttempt{}, err
	}
	if err := s.store.DeleteLoginAttemptsForAccount(accountID); err != nil {
		return channelstore.LoginAttempt{}, err
	}
	now := time.Now().UTC()
	attempt := channelstore.LoginAttempt{
		SchemaVersion: channelstore.SchemaVersion,
		ID:            channelstore.NewID("login"),
		AccountID:     accountID,
		State:         channelstore.StateQRPending,
		ExpiresAt:     now.Add(time.Duration(qr.ExpiresInSeconds) * time.Second),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.SaveAttempt(attempt); err != nil {
		return channelstore.LoginAttempt{}, err
	}
	if err := s.store.SaveLoginSecret(attempt.ID, channelstore.LoginSecret{SchemaVersion: channelstore.SchemaVersion, QRContent: qr.QRContent, QRImage: qr.ImageContent}); err != nil {
		return channelstore.LoginAttempt{}, err
	}
	account := channelstore.Account{SchemaVersion: channelstore.SchemaVersion, ID: accountID, Label: strings.TrimSpace(label), State: channelstore.StateQRPending, BaseURL: baseURL, CreatedAt: now, UpdatedAt: now}
	if existing, getErr := s.store.GetAccount(accountID); getErr == nil {
		account.CreatedAt = existing.CreatedAt
	}
	if err := s.store.SaveAccount(account); err != nil {
		return channelstore.LoginAttempt{}, err
	}
	attempt.QRImage = qr.ImageContent
	return attempt, nil
}

func (s *Service) LoginStatus(ctx context.Context, attemptID string) (channelstore.LoginAttempt, error) {
	attempt, err := s.store.GetAttempt(attemptID)
	if err != nil {
		return channelstore.LoginAttempt{}, err
	}
	if time.Now().After(attempt.ExpiresAt) {
		attempt.State = channelstore.StateExpired
		attempt.UpdatedAt = time.Now().UTC()
		_ = s.store.DeleteLoginSecret(attempt.ID)
		_ = s.store.SaveAttempt(attempt)
		return attempt, nil
	}
	account, err := s.store.GetAccount(attempt.AccountID)
	if err != nil {
		return channelstore.LoginAttempt{}, err
	}
	loginSecret, secretErr := s.store.LoadLoginSecret(attempt.ID)
	if secretErr != nil {
		return channelstore.LoginAttempt{}, secretErr
	}
	status, pollErr := s.backend.PollLogin(ctx, account.BaseURL, loginSecret.QRContent)
	if pollErr != nil {
		return attempt, pollErr
	}
	attempt.State = normalizeLoginState(status.Status)
	attempt.UpdatedAt = time.Now().UTC()
	if status.Token != "" && status.AccountID != "" {
		attempt.State = channelstore.StateConnected
		account.State = channelstore.StateConnected
		account.WeixinUserID = status.UserID
		account.UpdatedAt = time.Now().UTC()
		account.LastError = ""
		if err := s.store.SaveAccount(account); err != nil {
			return attempt, err
		}
		if err := s.store.SaveSecret(attempt.AccountID, channelstore.Secret{SchemaVersion: channelstore.SchemaVersion, Token: status.Token, ContextTokens: map[string]string{}}); err != nil {
			return attempt, err
		}
		_ = s.store.DeleteLoginSecret(attempt.ID)
		s.startPoller(attempt.AccountID)
	}
	if err := s.store.SaveAttempt(attempt); err != nil {
		return attempt, err
	}
	attempt.QRImage = loginSecret.QRImage
	return attempt, nil
}

func (s *Service) LoginQR(attemptID string) (string, error) {
	attempt, err := s.store.GetAttempt(attemptID)
	if err != nil {
		return "", err
	}
	if time.Now().After(attempt.ExpiresAt) {
		return "", errors.New("login QR expired")
	}
	secret, err := s.store.LoadLoginSecret(attempt.ID)
	if err != nil {
		return "", err
	}
	return secret.QRImage, nil
}

func (s *Service) CancelLogin(attemptID string) error {
	attempt, err := s.store.GetAttempt(attemptID)
	if err != nil {
		return err
	}
	attempt.State = channelstore.StateDisconnected
	attempt.UpdatedAt = time.Now().UTC()
	_ = s.store.DeleteLoginSecret(attempt.ID)
	return s.store.SaveAttempt(attempt)
}

func (s *Service) Logout(ctx context.Context, accountID string) error {
	account, err := s.store.GetAccount(accountID)
	if err != nil {
		return err
	}
	if secret, secretErr := s.store.LoadSecret(accountID); secretErr == nil && secret.Token != "" {
		_ = s.backend.Logout(ctx, account.BaseURL, secret.Token)
	}
	s.stopPoller(accountID)
	account.State = channelstore.StateLoggedOut
	account.LastError = ""
	account.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveAccount(account); err != nil {
		return err
	}
	return s.store.SaveSecret(accountID, channelstore.Secret{SchemaVersion: channelstore.SchemaVersion, ContextTokens: map[string]string{}})
}

func (s *Service) stopPoller(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.pollers[accountID]; cancel != nil {
		cancel()
		delete(s.pollers, accountID)
	}
}

func (s *Service) ListBindings() ([]channelstore.Binding, error) {
	return s.store.ListBindings()
}

func (s *Service) SaveBinding(binding channelstore.Binding) error {
	if binding.ID == "" {
		binding.ID = channelstore.NewID("binding")
	}
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = time.Now().UTC()
	}
	binding.UpdatedAt = time.Now().UTC()
	binding.SchemaVersion = channelstore.SchemaVersion
	if _, err := s.store.GetAccount(binding.AccountID); err != nil {
		return err
	}
	if validator, ok := s.tasks.(BindingValidator); ok {
		if err := validator.ValidateChannelEmployee(binding.EmployeeID); err != nil {
			return err
		}
	}
	return s.store.UpsertBinding(binding)
}

func (s *Service) DeleteBinding(id string) error {
	return s.store.DeleteBinding(id)
}

func (s *Service) ListInbox(accountID string, limit int) ([]channelstore.InboxMessage, error) {
	return s.store.ListInbox(accountID, limit)
}

func (s *Service) DeliverFinal(ctx context.Context, accountID, peerID, messageID, text string) error {
	if len(text) == 0 || len(text) > MaxTextBytes {
		return errors.New("final message is empty or too large")
	}
	account, err := s.store.GetAccount(accountID)
	if err != nil {
		return err
	}
	secret, err := s.store.LoadSecret(accountID)
	if err != nil || secret.Token == "" {
		return errors.New("Weixin account is not connected")
	}
	contextToken := secret.ContextTokens[peerID]
	if contextToken == "" {
		return errors.New("Weixin conversation context is unavailable")
	}
	return s.sendOutbox(ctx, account, secret.Token, peerID, contextToken, messageID, "final", text)
}

func (s *Service) startPoller(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pollers[accountID] != nil || s.rootCtx == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.rootCtx)
	s.pollers[accountID] = cancel
	go func() {
		s.poll(ctx, accountID)
		s.mu.Lock()
		delete(s.pollers, accountID)
		s.mu.Unlock()
	}()
}

func (s *Service) poll(ctx context.Context, accountID string) {
	backoff := time.Second
	for ctx.Err() == nil {
		account, err := s.store.GetAccount(accountID)
		if err != nil {
			return
		}
		secret, err := s.store.LoadSecret(accountID)
		if err != nil || secret.Token == "" {
			return
		}
		cursor, err := s.store.LoadCursor(accountID)
		if err != nil {
			return
		}
		updates, err := s.backend.GetUpdates(ctx, account.BaseURL, secret.Token, cursor)
		if err != nil {
			if errors.Is(err, ErrSessionExpired) {
				account.State = channelstore.StateExpired
				account.LastError = "Weixin session expired; reconnect is required"
				account.UpdatedAt = time.Now().UTC()
				_ = s.store.SaveAccount(account)
				return
			}
			account.State = channelstore.StateReconnecting
			account.LastError = "Weixin connection temporarily unavailable"
			account.UpdatedAt = time.Now().UTC()
			_ = s.store.SaveAccount(account)
			if !sleepContext(ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		account.State = channelstore.StateConnected
		account.LastError = ""
		account.UpdatedAt = time.Now().UTC()
		_ = s.store.SaveAccount(account)
		for _, message := range updates.Messages {
			s.handleMessage(ctx, account, secret, message)
		}
		if updates.Cursor != "" {
			if err := s.store.SaveCursor(accountID, updates.Cursor); err != nil {
				return
			}
		}
		if updates.LongPollTimeoutMS > 0 && updates.LongPollTimeoutMS < 120000 {
			// The upstream timeout is advisory; the next request remains bounded
			// by the transport client's context timeout.
		}
	}
}

func (s *Service) handleMessage(ctx context.Context, account channelstore.Account, secret channelstore.Secret, message Message) {
	if stored, err := s.store.LoadSecret(account.ID); err == nil && stored.Token != "" {
		secret = stored
	}
	inbox, duplicate, err := s.store.Ingest(channelstore.Inbound{AccountID: account.ID, PeerID: message.PeerID, GroupID: message.GroupID, MessageID: message.ID, Sequence: message.Sequence, Text: truncate(message.Text, MaxTextBytes), ContextToken: truncate(message.ContextToken, channelstore.MaxContextToken)})
	if err != nil || duplicate {
		return
	}
	if message.ContextToken != "" {
		if secret.ContextTokens == nil {
			secret.ContextTokens = map[string]string{}
		}
		secret.ContextTokens[message.PeerID] = truncate(message.ContextToken, channelstore.MaxContextToken)
		_ = s.store.SaveSecret(account.ID, secret)
	}
	if message.UnsupportedMedia {
		_ = s.sendOutbox(ctx, account, secret.Token, message.PeerID, message.ContextToken, message.ID, "ack", FixedUnsupportedAcknowledgement)
		return
	}
	binding, found, err := s.store.ResolveBinding(account.ID, message.PeerID, message.GroupID)
	if err != nil || !found || binding.EmployeeID == "" || (binding.MentionRequired && !message.Mentioned) {
		_ = s.sendOutbox(ctx, account, secret.Token, message.PeerID, message.ContextToken, message.ID, "ack", FixedUnboundAcknowledgement)
		return
	}
	if s.tasks == nil {
		return
	}
	taskID, err := s.tasks.CreateQueuedTask(ctx, binding.EmployeeID, message.Text)
	if err != nil {
		_ = s.sendOutbox(ctx, account, secret.Token, message.PeerID, message.ContextToken, message.ID, "error", "消息已收到，但任务未能进入收件箱；请在 GoHermit 中重试。")
		return
	}
	inbox.State = "queued"
	inbox.TaskID = taskID
	_ = s.store.UpdateInbox(inbox)
	_ = s.sendOutbox(ctx, account, secret.Token, message.PeerID, message.ContextToken, message.ID, "ack", FixedQueuedAcknowledgement)
}

func (s *Service) sendOutbox(ctx context.Context, account channelstore.Account, token, peerID, contextToken, messageID, kind, text string) error {
	if len(text) == 0 || len(text) > MaxTextBytes {
		return errors.New("outbox text is invalid")
	}
	outboxID := "out-" + stableID(account.ID, peerID, messageID, kind)
	record, duplicate, err := s.store.EnqueueOutbox(channelstore.OutboxMessage{ID: outboxID, AccountID: account.ID, PeerID: peerID, MessageID: messageID, Kind: kind, Text: text, State: "pending"})
	if err != nil {
		return err
	}
	if duplicate && record.State == "sent" {
		return nil
	}
	err = s.backend.SendMessage(ctx, account.BaseURL, token, peerID, contextToken, text)
	record.Attempts++
	record.UpdatedAt = time.Now().UTC()
	if err != nil {
		record.State = "unknown"
		_ = s.store.UpdateOutbox(record)
		return err
	}
	record.State = "sent"
	return s.store.UpdateOutbox(record)
}

func validateBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !allowedOrigin(parsed) || parsed.Path != "" && parsed.Path != "/" {
		return errors.New("Weixin endpoint must be an explicit HTTP(S) origin")
	}
	return nil
}

func normalizeLoginState(value string) channelstore.AccountState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "scaned", "scanned":
		return channelstore.StateScanned
	case "confirmed", "success", "connected", "ok":
		return channelstore.StateConfirmed
	case "expired", "timeout":
		return channelstore.StateExpired
	default:
		return channelstore.StateQRPending
	}
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

func stableID(parts ...string) string {
	value := strings.Join(parts, "\x00")
	hash := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", hash[:16])
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	value = value[:max]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
