// Package session stores auditable, language-neutral checkpoints.
package session

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/storage"
)

const SchemaVersion = 1

type Status string

const (
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type ToolRecord struct {
	Time    time.Time `json:"time"`
	CallID  string    `json:"call_id"`
	Name    string    `json:"name"`
	Summary string    `json:"summary"`
	IsError bool      `json:"is_error"`
}
type TestResult struct {
	Command string    `json:"command"`
	Passed  bool      `json:"passed"`
	Summary string    `json:"summary"`
	Time    time.Time `json:"time"`
}
type Session struct {
	SchemaVersion  int               `json:"schema_version"`
	ID             string            `json:"id"`
	Goal           string            `json:"goal"`
	Status         Status            `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Turns          int               `json:"turns"`
	RecentMessages []model.Message   `json:"recent_messages"`
	Summary        string            `json:"summary"`
	ToolCalls      []ToolRecord      `json:"tool_calls"`
	ModifiedFiles  map[string]string `json:"modified_files"`
	CompletedSteps []string          `json:"completed_steps"`
	PendingSteps   []string          `json:"pending_steps"`
	TestResults    []TestResult      `json:"test_results"`
	LastError      string            `json:"last_error,omitempty"`
	Workspace      string            `json:"workspace"`
	GitState       string            `json:"git_state,omitempty"`
	ConfigDigest   string            `json:"config_digest"`
}

func New(goal, workspace, configDigest string) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Session{SchemaVersion: SchemaVersion, ID: id, Goal: goal, Status: Running, CreatedAt: now, UpdatedAt: now, Workspace: workspace, ConfigDigest: configDigest, ModifiedFiles: map[string]string{}}, nil
}
func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b), nil
}

type Store struct {
	mu              sync.Mutex
	workspace, root string
	pending         map[string][]event.Event
}

func NewStore(workspace, directory string) (*Store, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(abs, directory)
	rel, err := filepath.Rel(abs, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("session directory escapes workspace")
	}
	return &Store{workspace: abs, root: root, pending: map[string][]event.Event{}}, nil
}
func (s *Store) sessionDir(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return "", errors.New("invalid session ID")
	}
	return filepath.Join(s.root, "sessions", id), nil
}
func (s *Store) Save(ctx context.Context, session *Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session.UpdatedAt = time.Now().UTC()
	dir, err := s.sessionDir(session.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	if err = storage.AtomicWrite(filepath.Join(dir, "session.json"), append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err = storage.AtomicWrite(filepath.Join(dir, "summary.md"), []byte(session.Summary), 0600); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	if events := s.pending[session.ID]; len(events) > 0 {
		if err = appendEvents(filepath.Join(dir, "events.jsonl"), events); err != nil {
			return err
		}
		delete(s.pending, session.ID)
	}
	return nil
}
func appendEvents(path string, events []event.Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 32<<10)
	enc := json.NewEncoder(w)
	for _, e := range events {
		if err = enc.Encode(e); err != nil {
			f.Close()
			return err
		}
	}
	if err = w.Flush(); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}
func (s *Store) BufferEvent(id string, e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[id] = append(s.pending[id], e)
}
func (s *Store) Load(ctx context.Context, id string) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := s.sessionDir(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	var out Session
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&out); err != nil {
		return nil, fmt.Errorf("corrupt checkpoint: %w", err)
	}
	if out.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported session schema version %d", out.SchemaVersion)
	}
	current, _ := filepath.Abs(s.workspace)
	saved, _ := filepath.Abs(out.Workspace)
	if current != saved {
		return nil, fmt.Errorf("workspace mismatch: saved %s, current %s", saved, current)
	}
	if err := verifyFiles(current, out.ModifiedFiles); err != nil {
		return nil, err
	}
	if out.GitState != "" && GitState(ctx, current) != out.GitState {
		return nil, errors.New("workspace Git state changed since checkpoint")
	}
	return &out, nil
}
func verifyFiles(root string, files map[string]string) error {
	for path, want := range files {
		full := filepath.Join(root, path)
		b, err := os.ReadFile(full)
		if want == "deleted" && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("modified file changed externally: %s: %w", path, err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != want {
			return fmt.Errorf("modified file changed externally: %s", path)
		}
	}
	return nil
}
func (s *Store) SnapshotFile(session *Session, path string) error {
	b, err := os.ReadFile(filepath.Join(s.workspace, path))
	if os.IsNotExist(err) {
		session.ModifiedFiles[filepath.ToSlash(path)] = "deleted"
		return nil
	}
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	session.ModifiedFiles[filepath.ToSlash(path)] = hex.EncodeToString(sum[:])
	return nil
}
func GitState(ctx context.Context, workspace string) string {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	cmd.Dir = workspace
	b, err := cmd.Output()
	if err != nil {
		return "not-a-repository"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func ConfigDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Store) Clean(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		return 0, errors.New("older-than must be positive")
	}
	base := filepath.Join(s.root, "sessions")
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-olderThan)
	count := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			target := filepath.Join(base, entry.Name())
			rel, _ := filepath.Rel(base, target)
			if rel == entry.Name() {
				if err = os.RemoveAll(target); err != nil {
					return count, err
				}
				count++
			}
		}
	}
	return count, nil
}
func (s *Store) List() ([]string, error) {
	base := filepath.Join(s.root, "sessions")
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
