package employeestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	ArtifactSchemaVersion = 1
	MaxArtifactsPerTask   = 128
	MaxArtifactFileBytes  = 256 << 10
)

type Artifact struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	EmployeeID    string    `json:"employee_id"`
	TaskID        string    `json:"task_id"`
	SessionID     string    `json:"session_id"`
	RunID         string    `json:"run_id"`
	Path          string    `json:"path"`
	Digest        string    `json:"digest"`
	VerifiedAt    time.Time `json:"verified_at"`
}

type artifactFile struct {
	SchemaVersion      int        `json:"schema_version"`
	EmployeeID         string     `json:"employee_id"`
	TaskID             string     `json:"task_id"`
	SessionID          string     `json:"session_id"`
	RunID              string     `json:"run_id"`
	TaskSnapshotDigest string     `json:"task_snapshot_digest"`
	Artifacts          []Artifact `json:"artifacts"`
}

// PutVerifiedArtifacts writes bounded metadata only. Exact retries are
// idempotent; metadata for one immutable Run can never be overwritten.
func (s *Store) PutVerifiedArtifacts(taskID string, artifacts []Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskLocked(taskID)
	if err != nil {
		return err
	}
	if task.SessionID == "" || task.RunID == "" {
		return fmt.Errorf("%w: unbound Task cannot store artifacts", ErrConflict)
	}
	file := artifactFile{
		SchemaVersion: ArtifactSchemaVersion, EmployeeID: task.EmployeeID,
		TaskID: task.ID, SessionID: task.SessionID, RunID: task.RunID,
		TaskSnapshotDigest: task.SnapshotDigest, Artifacts: append([]Artifact{}, artifacts...),
	}
	sort.Slice(file.Artifacts, func(i, j int) bool { return file.Artifacts[i].ID < file.Artifacts[j].ID })
	if err := validateArtifactFile(file, task.EmployeeID, task.ID, task.SessionID, task.RunID, task.SnapshotDigest); err != nil {
		return err
	}
	current, err := s.loadArtifactFile(summarizeTask(task))
	if err == nil {
		if reflect.DeepEqual(current, file) {
			return nil
		}
		return fmt.Errorf("%w: verified Artifact metadata is immutable", ErrConflict)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.writeJSONExclusive(file, MaxArtifactFileBytes, task.EmployeeID, "tasks", artifactFileName(task.ID))
}

func (s *Store) Artifacts(taskID string) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskLocked(taskID)
	if err != nil {
		return nil, err
	}
	file, err := s.loadArtifactFile(summarizeTask(task))
	if errors.Is(err, os.ErrNotExist) {
		return []Artifact{}, nil
	}
	if err != nil {
		return nil, err
	}
	return append([]Artifact{}, file.Artifacts...), nil
}

func (s *Store) loadArtifactFile(task TaskSummary) (artifactFile, error) {
	var file artifactFile
	if err := s.decodeFileStrict(MaxArtifactFileBytes, &file, task.EmployeeID, "tasks", artifactFileName(task.ID)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifactFile{}, err
		}
		return artifactFile{}, fmt.Errorf("%w: load Artifact metadata: %v", ErrCorrupt, err)
	}
	loaded, err := s.loadTask(task)
	if err != nil {
		return artifactFile{}, err
	}
	if err := validateArtifactFile(file, loaded.EmployeeID, loaded.ID, loaded.SessionID, loaded.RunID, loaded.SnapshotDigest); err != nil {
		return artifactFile{}, fmt.Errorf("%w: invalid Artifact metadata: %v", ErrCorrupt, err)
	}
	return file, nil
}

func validateArtifactFile(file artifactFile, employeeID, taskID, sessionID, runID, taskDigest string) error {
	if file.SchemaVersion != ArtifactSchemaVersion || file.EmployeeID != employeeID ||
		file.TaskID != taskID || file.SessionID != sessionID || file.RunID != runID ||
		file.TaskSnapshotDigest != taskDigest || file.Artifacts == nil ||
		len(file.Artifacts) > MaxArtifactsPerTask {
		return errors.New("Artifact file identity, schema, or size is invalid")
	}
	seen := make(map[string]struct{}, len(file.Artifacts))
	for index, artifact := range file.Artifacts {
		if artifact.SchemaVersion != ArtifactSchemaVersion || artifact.EmployeeID != employeeID ||
			artifact.TaskID != taskID || artifact.SessionID != sessionID || artifact.RunID != runID ||
			artifact.VerifiedAt.IsZero() || !validArtifactPath(artifact.Path) ||
			(artifact.Digest != "deleted" && !validTaskDigest(artifact.Digest)) ||
			artifact.ID != artifactID(employeeID, taskID, sessionID, runID, artifact.Path, artifact.Digest) {
			return errors.New("Artifact metadata is invalid")
		}
		if _, duplicate := seen[artifact.ID]; duplicate {
			return errors.New("Artifact metadata contains duplicate ID")
		}
		seen[artifact.ID] = struct{}{}
		if index > 0 && file.Artifacts[index-1].ID >= artifact.ID {
			return errors.New("Artifact metadata is not strictly sorted")
		}
	}
	return nil
}

func NewArtifact(employeeID, taskID, sessionID, runID, relative, digest string, verifiedAt time.Time) (Artifact, error) {
	value := Artifact{
		SchemaVersion: ArtifactSchemaVersion, EmployeeID: employeeID, TaskID: taskID,
		SessionID: sessionID, RunID: runID, Path: relative, Digest: digest,
		VerifiedAt: verifiedAt.UTC(),
	}
	value.ID = artifactID(employeeID, taskID, sessionID, runID, relative, digest)
	if err := validateArtifactFile(artifactFile{
		SchemaVersion: ArtifactSchemaVersion, EmployeeID: employeeID, TaskID: taskID,
		SessionID: sessionID, RunID: runID, TaskSnapshotDigest: strings.Repeat("0", 64),
		Artifacts: []Artifact{value},
	}, employeeID, taskID, sessionID, runID, strings.Repeat("0", 64)); err != nil {
		return Artifact{}, err
	}
	return value, nil
}

func artifactID(employeeID, taskID, sessionID, runID, relative, digest string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{employeeID, taskID, sessionID, runID, relative, digest}, "\x00")))
	return "artifact-" + hex.EncodeToString(sum[:16])
}

func validArtifactPath(value string) bool {
	return value != "" && len(value) <= 1024 && value == path.Clean(value) &&
		!path.IsAbs(value) && value != "." && value != ".." &&
		!strings.HasPrefix(value, "../") && !strings.Contains(value, "%") &&
		!strings.ContainsAny(value, "\\\x00\r\n")
}

func artifactFileName(taskID string) string { return taskID + ".artifacts.json" }
