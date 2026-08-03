package weixin

import (
	"context"
	"errors"
)

const (
	DefaultBaseURL   = "https://ilinkai.weixin.qq.com"
	BotAgent         = "GoHermit/0.8-dev"
	MaxResponseBytes = 1 << 20
	MaxUpdates       = 64
	MaxTextBytes     = 16 << 10
)

var ErrSessionExpired = errors.New("weixin session expired")

type QRStart struct {
	QRContent        string
	ImageContent     string
	ExpiresInSeconds int
}

type QRStatus struct {
	Status    string
	AccountID string
	UserID    string
	Token     string
}

type Message struct {
	ID               string
	Sequence         int64
	PeerID           string
	GroupID          string
	Text             string
	ContextToken     string
	Mentioned        bool
	UnsupportedMedia bool
}

type Updates struct {
	Messages          []Message
	Cursor            string
	LongPollTimeoutMS int
}

type Backend interface {
	StartLogin(ctx context.Context, baseURL string) (QRStart, error)
	PollLogin(ctx context.Context, baseURL, qrContent string) (QRStatus, error)
	GetUpdates(ctx context.Context, baseURL, token, cursor string) (Updates, error)
	SendMessage(ctx context.Context, baseURL, token, peerID, contextToken, text string) error
	GetConfig(ctx context.Context, baseURL, token, peerID, contextToken string) error
	SendTyping(ctx context.Context, baseURL, token, peerID, contextToken string, active bool) error
	Logout(ctx context.Context, baseURL, token string) error
}
