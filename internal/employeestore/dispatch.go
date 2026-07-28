package employeestore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
)

const (
	DispatchSchemaVersion = 1
	MaxDispatchBytes      = 16 << 10
)

type DispatchStage string

const (
	DispatchPrepared       DispatchStage = "prepared"
	DispatchSessionCreated DispatchStage = "session_created"
)

// DispatchRecord is a bounded cross-store idempotency journal. It records no
// Run state and never drives execution; Session/Run remain the only execution
// authority.
type DispatchRecord struct {
	SchemaVersion         int           `json:"schema_version"`
	EmployeeID            string        `json:"employee_id"`
	TaskID                string        `json:"task_id"`
	SessionID             string        `json:"session_id"`
	TaskSnapshotDigest    string        `json:"task_snapshot_digest"`
	CompactSnapshotDigest string        `json:"compact_snapshot_digest"`
	WorkspaceRealPath     string        `json:"workspace_real_path"`
	Stage                 DispatchStage `json:"stage"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
}

// PrepareDispatch exclusively creates or idempotently reloads a prepared
// journal after Control Plane has completed every readiness check.
func (s *Store) PrepareDispatch(expected DispatchRecord) (DispatchRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if expected.Stage == "" {
		expected.Stage = DispatchPrepared
	}
	if expected.Stage != DispatchPrepared || !expected.CreatedAt.IsZero() || !expected.UpdatedAt.IsZero() {
		return DispatchRecord{}, errors.New("new dispatch journal must be an un-timestamped prepared record")
	}
	task, err := s.getTaskLocked(expected.TaskID)
	if err != nil {
		return DispatchRecord{}, err
	}
	if task.EmployeeID != expected.EmployeeID || task.SnapshotDigest != expected.TaskSnapshotDigest {
		return DispatchRecord{}, fmt.Errorf("%w: dispatch Task identity mismatch", ErrConflict)
	}
	if task.State != employee.TaskQueued {
		return DispatchRecord{}, fmt.Errorf("%w: only a queued Employee Task can be prepared", ErrConflict)
	}
	if err := validateDispatchIdentity(expected, false); err != nil {
		return DispatchRecord{}, err
	}
	current, err := s.loadDispatchExpected(summarizeTask(task))
	if err == nil {
		if !sameDispatchIdentity(current, expected) {
			return DispatchRecord{}, fmt.Errorf("%w: dispatch journal does not match requested preparation", ErrConflict)
		}
		return current, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return DispatchRecord{}, err
	}
	now := time.Now().UTC()
	expected.CreatedAt, expected.UpdatedAt = now, now
	if err := validateDispatchIdentity(expected, true); err != nil {
		return DispatchRecord{}, err
	}
	if err := s.writeJSONExclusive(expected, MaxDispatchBytes, expected.EmployeeID, "tasks", dispatchFileName(expected.TaskID)); err != nil {
		return DispatchRecord{}, err
	}
	return expected, nil
}

func (s *Store) LoadDispatch(taskID string) (DispatchRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskLocked(taskID)
	if err != nil {
		return DispatchRecord{}, err
	}
	return s.loadDispatchExpected(summarizeTask(task))
}

func (s *Store) MarkDispatchSessionCreated(taskID string) (DispatchRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskLocked(taskID)
	if err != nil {
		return DispatchRecord{}, err
	}
	current, err := s.loadDispatchExpected(summarizeTask(task))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DispatchRecord{}, ErrNotFound
		}
		return DispatchRecord{}, err
	}
	if current.Stage == DispatchSessionCreated {
		return current, nil
	}
	if current.Stage != DispatchPrepared {
		return DispatchRecord{}, fmt.Errorf("%w: unsupported dispatch stage", ErrCorrupt)
	}
	current.Stage = DispatchSessionCreated
	current.UpdatedAt = time.Now().UTC()
	if err := validateDispatchIdentity(current, true); err != nil {
		return DispatchRecord{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if err := s.writeJSON(current, MaxDispatchBytes, current.EmployeeID, "tasks", dispatchFileName(current.TaskID)); err != nil {
		return DispatchRecord{}, err
	}
	return current, nil
}

func (s *Store) loadDispatchExpected(task TaskSummary) (DispatchRecord, error) {
	if err := validateStoreID(task.EmployeeID); err != nil {
		return DispatchRecord{}, fmt.Errorf("%w: invalid dispatch Employee identity: %v", ErrCorrupt, err)
	}
	if err := validateStoreID(task.ID); err != nil {
		return DispatchRecord{}, fmt.Errorf("%w: invalid dispatch Task identity: %v", ErrCorrupt, err)
	}
	var record DispatchRecord
	if err := s.decodeFileStrict(MaxDispatchBytes, &record, task.EmployeeID, "tasks", dispatchFileName(task.ID)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DispatchRecord{}, err
		}
		return DispatchRecord{}, fmt.Errorf("%w: load dispatch journal: %v", ErrCorrupt, err)
	}
	if err := validateDispatchIdentity(record, true); err != nil {
		return DispatchRecord{}, fmt.Errorf("%w: invalid dispatch journal: %v", ErrCorrupt, err)
	}
	if record.EmployeeID != task.EmployeeID || record.TaskID != task.ID ||
		record.TaskSnapshotDigest != task.SnapshotDigest {
		return DispatchRecord{}, fmt.Errorf("%w: dispatch journal and Task index disagree", ErrCorrupt)
	}
	return record, nil
}

func validateDispatchIdentity(record DispatchRecord, persisted bool) error {
	if record.SchemaVersion != DispatchSchemaVersion {
		return fmt.Errorf("unsupported dispatch schema version %d", record.SchemaVersion)
	}
	for name, id := range map[string]string{
		"Employee": record.EmployeeID, "Task": record.TaskID, "Session": record.SessionID,
	} {
		if err := validateStoreID(id); err != nil {
			return fmt.Errorf("invalid dispatch %s id: %w", name, err)
		}
	}
	if !validTaskDigest(record.TaskSnapshotDigest) || !validTaskDigest(record.CompactSnapshotDigest) {
		return errors.New("dispatch digests must be canonical lowercase SHA-256")
	}
	if record.WorkspaceRealPath == "" || !filepath.IsAbs(record.WorkspaceRealPath) ||
		filepath.Clean(record.WorkspaceRealPath) != record.WorkspaceRealPath ||
		strings.ContainsAny(record.WorkspaceRealPath, "\x00\r\n") {
		return errors.New("dispatch workspace real path is invalid")
	}
	real, err := filepath.EvalSymlinks(record.WorkspaceRealPath)
	if err != nil || filepath.Clean(real) != record.WorkspaceRealPath {
		return errors.New("dispatch workspace real path is not current")
	}
	if record.Stage != DispatchPrepared && record.Stage != DispatchSessionCreated {
		return errors.New("dispatch stage is invalid")
	}
	if persisted {
		if record.CreatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
			return errors.New("dispatch timestamps are invalid")
		}
	} else if !record.CreatedAt.IsZero() || !record.UpdatedAt.IsZero() {
		return errors.New("new dispatch timestamps must be empty")
	}
	return nil
}

func sameDispatchIdentity(left, right DispatchRecord) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.EmployeeID == right.EmployeeID && left.TaskID == right.TaskID &&
		left.SessionID == right.SessionID &&
		left.TaskSnapshotDigest == right.TaskSnapshotDigest &&
		left.CompactSnapshotDigest == right.CompactSnapshotDigest &&
		left.WorkspaceRealPath == right.WorkspaceRealPath
}

func dispatchFileName(taskID string) string { return taskID + ".dispatch.json" }
