package notify

import (
	"context"
	"testing"
)

func TestConfigFromEnvDefaultsRecipientWithoutExposingCredentials(t *testing.T) {
	t.Setenv("GOHERMIT_NOTIFY_EMAIL_TO", "")
	t.Setenv("GOHERMIT_SMTP_HOST", "")
	t.Setenv("GOHERMIT_SMTP_USERNAME", "")
	t.Setenv("GOHERMIT_SMTP_PASSWORD", "not-a-real-secret")
	config := ConfigFromEnv()
	if config.To != DefaultRecipient {
		t.Fatalf("recipient = %q, want %q", config.To, DefaultRecipient)
	}
	if config.Configured() {
		t.Fatal("empty SMTP host must not be considered configured")
	}
	public := config.PublicStatus()
	if public.Configured || public.Recipient != DefaultRecipient {
		t.Fatalf("unexpected public status: %+v", public)
	}
}

func TestSendRejectsUnsafeHeadersBeforeNetwork(t *testing.T) {
	config := Config{Host: "127.0.0.1", Port: 1, Username: "user", Password: "secret", From: "from@example.com", To: "to@example.com"}
	if err := config.Send(context.Background(), "bad\nsubject", "body"); err == nil {
		t.Fatal("newline in subject must be rejected")
	}
	if err := config.Send(context.Background(), "subject", "bad\x00body"); err == nil {
		t.Fatal("NUL in body must be rejected")
	}
}
