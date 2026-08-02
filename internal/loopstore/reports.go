package loopstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/storage"
)

const (
	reportSchemaVersion = 1
	maxReports          = 512
	maxReportText       = 12 << 10
)

// ReportRecord is the bounded, user-visible projection of one terminal Loop
// or Employee Task outcome. It deliberately excludes prompts, tool payloads,
// credentials and private model reasoning.
type ReportRecord struct {
	SchemaVersion   int         `json:"schema_version"`
	ID              string      `json:"id"`
	SourceType      string      `json:"source_type"`
	SourceID        string      `json:"source_id"`
	Title           string      `json:"title"`
	Status          loop.Status `json:"status"`
	FailureCode     string      `json:"failure_code,omitempty"`
	Summary         string      `json:"summary,omitempty"`
	FinishedAt      *time.Time  `json:"finished_at,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	DeliveryStatus  string      `json:"delivery_status"`
	DeliveryChannel string      `json:"delivery_channel,omitempty"`
	DeliveredAt     *time.Time  `json:"delivered_at,omitempty"`
	LastError       string      `json:"last_error,omitempty"`
}

func (s *Store) reportDir() string { return filepath.Join(s.dir, "reports") }

func (s *Store) ReportDir() string { return s.reportDir() }

func reportIDPath(dir, id string) (string, error) {
	if err := validateLoopPathID(id); err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

func validateReport(r ReportRecord) error {
	if r.SchemaVersion != reportSchemaVersion || strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.SourceID) == "" || strings.TrimSpace(r.Title) == "" || r.Status == "" || !r.Status.Terminal() || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return errors.New("invalid report record")
	}
	if err := validateLoopPathID(r.ID); err != nil {
		return err
	}
	if r.SourceType != "loop" && r.SourceType != "employee_task" {
		return errors.New("invalid report source type")
	}
	if r.DeliveryStatus != "pending" && r.DeliveryStatus != "sent" && r.DeliveryStatus != "failed" {
		return errors.New("invalid report delivery status")
	}
	if len([]byte(r.Title)) > 512 || len([]byte(r.Summary)) > maxReportText || len([]byte(r.LastError)) > 512 || len([]byte(r.FailureCode)) > 256 {
		return errors.New("report text exceeds limit")
	}
	if r.DeliveryStatus == "sent" && (r.DeliveryChannel == "" || r.DeliveredAt == nil || r.DeliveredAt.IsZero()) {
		return errors.New("sent report requires delivery evidence")
	}
	return nil
}

func decodeReport(raw []byte) (ReportRecord, error) {
	if len(raw) > MaxStoreBytes {
		return ReportRecord{}, errors.New("report exceeds size limit")
	}
	var report ReportRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return ReportRecord{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ReportRecord{}, errors.New("report contains trailing data")
	}
	if err := validateReport(report); err != nil {
		return ReportRecord{}, err
	}
	return report, nil
}

// SaveReport creates or updates a report while preserving its immutable
// identity and creation time. Delivery metadata is intentionally separate
// from Loop/Task execution state.
func (s *Store) SaveReport(report ReportRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if report.SchemaVersion == 0 {
		report.SchemaVersion = reportSchemaVersion
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	if report.UpdatedAt.IsZero() {
		report.UpdatedAt = time.Now().UTC()
	}
	if err := validateReport(report); err != nil {
		return err
	}
	path, err := reportIDPath(s.reportDir(), report.ID)
	if err != nil {
		return err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		prior, decodeErr := decodeReport(existing)
		if decodeErr != nil {
			return fmt.Errorf("existing report is corrupt: %w", decodeErr)
		}
		if prior.ID != report.ID || prior.SourceID != report.SourceID || prior.SourceType != report.SourceType || prior.Status != report.Status {
			return errors.New("report identity is immutable")
		}
		if !report.CreatedAt.Equal(prior.CreatedAt) {
			report.CreatedAt = prior.CreatedAt
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	report.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return storage.AtomicWrite(path, append(raw, '\n'), 0600)
}

func (s *Store) GetReport(id string) (ReportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := reportIDPath(s.reportDir(), id)
	if err != nil {
		return ReportRecord{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReportRecord{}, err
	}
	return decodeReport(raw)
}

func (s *Store) ListReports(limit int) ([]ReportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > maxReports {
		limit = maxReports
	}
	entries, err := os.ReadDir(s.reportDir())
	if errors.Is(err, os.ErrNotExist) {
		return []ReportRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]ReportRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if len(items) >= maxReports {
			return nil, errors.New("report store exceeds limit")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("report symlink is not allowed")
		}
		raw, readErr := os.ReadFile(filepath.Join(s.reportDir(), entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		report, decodeErr := decodeReport(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		items = append(items, report)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
