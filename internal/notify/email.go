package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultRecipient = "1143130628@qq.com"

// Config contains only delivery metadata. Password is intentionally kept
// private to the process and is never exposed through an API projection.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string
	OpenClaw OpenClawConfig
}

// OpenClawConfig describes the dedicated OpenClaw hooks/agent delivery path.
// The token is process-only and is never copied into a public projection.
type OpenClawConfig struct {
	URL     string
	Token   string
	Channel string
	Target  string
	AgentID string
}

func OpenClawConfigFromEnv() OpenClawConfig {
	baseURL := strings.TrimSpace(os.Getenv("GOHERMIT_OPENCLAW_URL"))
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Path == "" {
		parsed.Path = "/hooks/agent"
		baseURL = parsed.String()
	}
	return OpenClawConfig{
		URL:     baseURL,
		Token:   os.Getenv("GOHERMIT_OPENCLAW_HOOK_TOKEN"),
		Channel: strings.TrimSpace(os.Getenv("GOHERMIT_OPENCLAW_CHANNEL")),
		Target:  strings.TrimSpace(os.Getenv("GOHERMIT_OPENCLAW_TO")),
		AgentID: strings.TrimSpace(os.Getenv("GOHERMIT_OPENCLAW_AGENT_ID")),
	}
}

func (c OpenClawConfig) Configured() bool {
	return c.URL != "" && c.Token != "" && c.Channel != "" && c.Target != ""
}

func (c OpenClawConfig) PublicStatus() map[string]any {
	return map[string]any{
		"configured": c.Configured(),
		"channel":    c.Channel,
		"target":     c.Target,
		"agent_id":   c.AgentID,
	}
}

// Send posts a bounded isolated agent turn to OpenClaw's authenticated hooks
// endpoint. OpenClaw owns WeChat QR login, pairing and channel delivery; this
// service only supplies a report and an explicit channel target.
func (c OpenClawConfig) Send(ctx context.Context, reportID, name, message string) error {
	if !c.Configured() {
		return errors.New("OpenClaw 汇报通道未配置")
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("OpenClaw URL 无效")
	}
	if len(message) > 12<<10 {
		return errors.New("OpenClaw 汇报超过大小限制")
	}
	payload := struct {
		Message  string `json:"message"`
		Name     string `json:"name"`
		AgentID  string `json:"agentId,omitempty"`
		WakeMode string `json:"wakeMode"`
		Deliver  bool   `json:"deliver"`
		Channel  string `json:"channel"`
		To       string `json:"to"`
	}{Message: message, Name: name, AgentID: c.AgentID, WakeMode: "now", Deliver: true, Channel: c.Channel, To: c.Target}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GoHermit-Report-ID", reportID)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("OpenClaw 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OpenClaw 返回 HTTP %d: %s", resp.StatusCode, clipError(string(body)))
	}
	return nil
}

func clipError(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if len(value) > 256 {
		return value[:256] + "…"
	}
	return value
}

func ConfigFromEnv() Config {
	port := 587
	if raw := strings.TrimSpace(os.Getenv("GOHERMIT_SMTP_PORT")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed < 65536 {
			port = parsed
		}
	}
	to := strings.TrimSpace(os.Getenv("GOHERMIT_NOTIFY_EMAIL_TO"))
	if to == "" {
		to = DefaultRecipient
	}
	username := strings.TrimSpace(os.Getenv("GOHERMIT_SMTP_USERNAME"))
	from := strings.TrimSpace(os.Getenv("GOHERMIT_SMTP_FROM"))
	if from == "" {
		from = username
	}
	return Config{
		Host: strings.TrimSpace(os.Getenv("GOHERMIT_SMTP_HOST")),
		Port: port, Username: username,
		Password: os.Getenv("GOHERMIT_SMTP_PASSWORD"), From: from, To: to,
		OpenClaw: OpenClawConfigFromEnv(),
	}
}

func (c Config) Configured() bool {
	return c.Host != "" && c.Username != "" && c.Password != "" && c.From != "" && c.To != ""
}

func (c Config) AnyConfigured() bool { return c.Configured() || c.OpenClaw.Configured() }

// PublicStatus is safe to expose in Settings: it contains no credential or
// password material.
type PublicStatus struct {
	Configured bool   `json:"configured"`
	Recipient  string `json:"recipient"`
	From       string `json:"from,omitempty"`
	Host       string `json:"host,omitempty"`
}

func (c Config) PublicStatus() PublicStatus {
	return PublicStatus{Configured: c.Configured(), Recipient: c.To, From: c.From, Host: c.Host}
}

func (c Config) Send(ctx context.Context, subject, body string) error {
	if !c.Configured() {
		return errors.New("email notification SMTP is not configured")
	}
	for _, value := range []string{c.From, c.To, subject} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("email header contains a newline")
		}
	}
	if strings.ContainsAny(body, "\x00") {
		return errors.New("email body contains NUL")
	}

	payload := []byte("From: " + c.From + "\r\n" +
		"To: " + c.To + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body + "\r\n")
	address := fmt.Sprintf("%s:%d", c.Host, c.Port)
	auth := smtp.PlainAuth("", c.Username, c.Password, c.Host)
	result := make(chan error, 1)
	go func() { result <- smtp.SendMail(address, auth, c.From, []string{c.To}, payload) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return errors.New("email notification timed out")
	}
}

// SendTLS is available for providers that expose implicit TLS on port 465.
// It is not used by the default path, which uses STARTTLS-compatible SMTP
// delivery through net/smtp.
func (c Config) SendTLS(ctx context.Context, subject, body string) error {
	if c.Port != 465 {
		return c.Send(ctx, subject, body)
	}
	if !c.Configured() {
		return errors.New("email notification SMTP is not configured")
	}
	for _, value := range []string{c.From, c.To, subject} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("email header contains a newline")
		}
	}
	address := fmt.Sprintf("%s:%d", c.Host, c.Port)
	result := make(chan error, 1)
	go func() {
		conn, err := tls.Dial("tcp", address, &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			result <- err
			return
		}
		client, err := smtp.NewClient(conn, c.Host)
		if err == nil {
			err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host))
		}
		if err == nil {
			err = client.Mail(c.From)
		}
		if err == nil {
			err = client.Rcpt(c.To)
		}
		if err == nil {
			var writer smtpDataWriter
			writer, err = client.Data()
			if err == nil {
				_, err = writer.Write([]byte("From: " + c.From + "\r\nTo: " + c.To + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n"))
				if closeErr := writer.Close(); err == nil {
					err = closeErr
				}
			}
		}
		if quitErr := client.Quit(); err == nil {
			err = quitErr
		}
		result <- err
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return errors.New("email notification timed out")
	}
}

type smtpDataWriter interface {
	Write([]byte) (int, error)
	Close() error
}
