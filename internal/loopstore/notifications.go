package loopstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/storage"
)

const notificationSchemaVersion = 1

type NotificationDelivery struct {
	SchemaVersion int         `json:"schema_version"`
	InvocationID  string      `json:"invocation_id"`
	Status        loop.Status `json:"status"`
	SentAt        time.Time   `json:"sent_at"`
}

func (s *Store) notificationPath(invocationID string) (string, error) {
	if err := validateLoopPathID(invocationID); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, "notifications", invocationID+".json"), nil
}

func validateNotification(value NotificationDelivery) error {
	if value.SchemaVersion != notificationSchemaVersion || value.InvocationID == "" ||
		value.Status == "" || !value.Status.Terminal() || value.SentAt.IsZero() {
		return errors.New("invalid loop notification delivery")
	}
	if err := validateLoopPathID(value.InvocationID); err != nil {
		return err
	}
	return nil
}

// NotificationSent returns whether the terminal status has already been
// delivered. A missing marker is normal; malformed markers fail closed.
func (s *Store) NotificationSent(invocationID string, status loop.Status) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.notificationPath(invocationID)
	if err != nil {
		return false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var delivery NotificationDelivery
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&delivery); err != nil {
		return false, fmt.Errorf("decode notification delivery: %w", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false, errors.New("notification delivery contains trailing data")
	}
	if err = validateNotification(delivery); err != nil {
		return false, err
	}
	if delivery.InvocationID != invocationID || delivery.Status != status {
		return false, errors.New("notification delivery identity mismatch")
	}
	return true, nil
}

// MarkNotificationSent persists an immutable terminal delivery marker. It is
// deliberately separate from the invocation file so a mail retry can never
// rewrite invocation truth.
func (s *Store) MarkNotificationSent(invocationID string, status loop.Status, sentAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.notificationPath(invocationID)
	if err != nil {
		return err
	}
	delivery := NotificationDelivery{
		SchemaVersion: notificationSchemaVersion,
		InvocationID:  invocationID,
		Status:        status,
		SentAt:        sentAt.UTC(),
	}
	if err = validateNotification(delivery); err != nil {
		return err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		var prior NotificationDelivery
		decoder := json.NewDecoder(bytes.NewReader(existing))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&prior); decodeErr != nil || validateNotification(prior) != nil {
			return errors.New("existing notification delivery is corrupt")
		}
		if prior.InvocationID == invocationID && prior.Status == status {
			return nil
		}
		return errors.New("notification delivery already exists with a different status")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	raw, err := json.MarshalIndent(delivery, "", "  ")
	if err != nil {
		return err
	}
	if err = storage.AtomicWrite(path, append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("save notification delivery: %w", err)
	}
	return nil
}

func (s *Store) NotificationDir() string {
	return filepath.Join(strings.TrimSpace(s.dir), "notifications")
}
