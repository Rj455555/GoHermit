package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/smtp"
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
	}
}

func (c Config) Configured() bool {
	return c.Host != "" && c.Username != "" && c.Password != "" && c.From != "" && c.To != ""
}

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
