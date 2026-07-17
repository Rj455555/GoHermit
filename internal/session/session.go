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
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
)

const SchemaVersion = 4

const commitJournalVersion = 1

type Status string

const (
	Open     Status = "open"
	Archived Status = "archived"
	// Legacy task statuses remain accepted during schema migration and by the
	// v0.1 compatibility API. New sessions use Open/Archived and keep execution
	// state on Run.
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type RunStatus string

const (
	RunQueued      RunStatus = "queued"
	RunRunning     RunStatus = "running"
	RunVerifying   RunStatus = "verifying"
	RunCompleted   RunStatus = "completed"
	RunFailed      RunStatus = "failed"
	RunCancelled   RunStatus = "cancelled"
	RunInterrupted RunStatus = "interrupted"
)

type PlanMode string

const (
	PlanAuto   PlanMode = "auto"
	PlanReview PlanMode = "review"
)

func NormalizePlanMode(value string) (PlanMode, error) {
	switch PlanMode(strings.TrimSpace(value)) {
	case "", PlanAuto:
		return PlanAuto, nil
	case PlanReview:
		return PlanReview, nil
	default:
		return "", errors.New("plan mode must be auto or review")
	}
}

type Selection struct {
	Company string `json:"company,omitempty"`
	Access  string `json:"access,omitempty"`
	Model   string `json:"model,omitempty"`
	Agent   string `json:"agent,omitempty"`
}

type Run struct {
	ID                   string         `json:"id"`
	Message              string         `json:"message"`
	Status               RunStatus      `json:"status"`
	StartedAt            time.Time      `json:"started_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	CompletedAt          *time.Time     `json:"completed_at,omitempty"`
	StartTurn            int            `json:"start_turn"`
	EndTurn              int            `json:"end_turn,omitempty"`
	LastMutationTurn     int            `json:"last_mutation_turn,omitempty"`
	LastVerificationTurn int            `json:"last_verification_turn,omitempty"`
	VerificationAttempts int            `json:"verification_attempts,omitempty"`
	ModelCalls           int            `json:"model_calls,omitempty"`
	PromptTokens         int            `json:"prompt_tokens,omitempty"`
	CompletionTokens     int            `json:"completion_tokens,omitempty"`
	TotalTokens          int            `json:"total_tokens,omitempty"`
	Plan                 *taskplan.Plan `json:"plan,omitempty"`
	PlanMode             PlanMode       `json:"plan_mode,omitempty"`
	PlanApproved         bool           `json:"plan_approved,omitempty"`
	PlanApprovedAt       *time.Time     `json:"plan_approved_at,omitempty"`
	ModifiedFiles        []string       `json:"modified_files,omitempty"`
	FinalMessage         string         `json:"final_message,omitempty"`
	Error                string         `json:"error,omitempty"`
}

type MessageRecord struct {
	ID        string     `json:"id"`
	RunID     string     `json:"run_id"`
	Role      model.Role `json:"role"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
}

type SessionSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Status      Status    `json:"status"`
	UpdatedAt   time.Time `json:"updated_at"`
	ActiveRunID string    `json:"active_run_id,omitempty"`
	LastRun     RunStatus `json:"last_run_status,omitempty"`
	Selection   Selection `json:"selection"`
}

type ToolRecord struct {
	Time        time.Time  `json:"time"`
	RunID       string     `json:"run_id,omitempty"`
	CallID      string     `json:"call_id"`
	Name        string     `json:"name"`
	Summary     string     `json:"summary"`
	IsError     bool       `json:"is_error"`
	Status      string     `json:"status,omitempty"`
	StartedAt   time.Time  `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
type TestResult struct {
	Command string    `json:"command"`
	Passed  bool      `json:"passed"`
	Summary string    `json:"summary"`
	Time    time.Time `json:"time"`
	RunID   string    `json:"run_id,omitempty"`
	Turn    int       `json:"turn,omitempty"`
}
type Session struct {
	SchemaVersion     int               `json:"schema_version"`
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	Goal              string            `json:"goal"`
	Status            Status            `json:"status"`
	Selection         Selection         `json:"selection"`
	PlanMode          PlanMode          `json:"plan_mode,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	Turns             int               `json:"turns"`
	Runs              []Run             `json:"runs"`
	ActiveRunID       string            `json:"active_run_id,omitempty"`
	NextEventSequence uint64            `json:"next_event_sequence,omitempty"`
	RecentMessages    []model.Message   `json:"recent_messages"`
	Summary           string            `json:"summary"`
	ToolCalls         []ToolRecord      `json:"tool_calls"`
	ModifiedFiles     map[string]string `json:"modified_files"`
	CompletedSteps    []string          `json:"completed_steps"`
	PendingSteps      []string          `json:"pending_steps"`
	TestResults       []TestResult      `json:"test_results"`
	LastError         string            `json:"last_error,omitempty"`
	Workspace         string            `json:"workspace"`
	GitState          string            `json:"git_state,omitempty"`
	ConfigDigest      string            `json:"config_digest"`
	WorkspaceChanged  bool              `json:"workspace_changed,omitempty"`
	Mission           *team.Mission     `json:"mission,omitempty"`
	Hidden            bool              `json:"hidden,omitempty"`
	ParentSessionID   string            `json:"parent_session_id,omitempty"`
	ParentRunID       string            `json:"parent_run_id,omitempty"`
	WorkItemID        string            `json:"work_item_id,omitempty"`
}

func New(goal, workspace, configDigest string) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Session{SchemaVersion: SchemaVersion, ID: id, Title: clipTitle(goal), Goal: goal, Status: Open, CreatedAt: now, UpdatedAt: now, Workspace: workspace, ConfigDigest: configDigest, ModifiedFiles: map[string]string{}}, nil
}

func NewConversation(title, workspace, configDigest string, selection Selection) (*Session, error) {
	s, err := New(title, workspace, configDigest)
	if err != nil {
		return nil, err
	}
	s.Title = clipTitle(title)
	s.Goal = ""
	s.Selection = selection
	s.PlanMode = PlanAuto
	return s, nil
}

func clipTitle(goal string) string {
	goal = strings.TrimSpace(strings.ReplaceAll(goal, "\n", " "))
	if len(goal) > 80 {
		return goal[:80] + "…"
	}
	return goal
}

func (s *Session) NewRun(message string) (*Run, error) {
	if s.Status == Archived {
		return nil, errors.New("session is archived")
	}
	if s.ActiveRunID != "" {
		return nil, errors.New("session already has an active run")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	mode, err := NormalizePlanMode(string(s.PlanMode))
	if err != nil {
		return nil, err
	}
	s.PlanMode = mode
	run := Run{ID: id, Message: strings.TrimSpace(message), Status: RunQueued, StartedAt: now, UpdatedAt: now, StartTurn: s.Turns + 1, PlanMode: mode, PlanApproved: mode == PlanAuto}
	if run.Message == "" {
		return nil, errors.New("run message is required")
	}
	s.Runs = append(s.Runs, run)
	s.ActiveRunID = id
	s.Goal = run.Message
	s.UpdatedAt = now
	return &s.Runs[len(s.Runs)-1], nil
}

func (s *Session) ActiveRun() *Run {
	if s.ActiveRunID == "" {
		return nil
	}
	for i := range s.Runs {
		if s.Runs[i].ID == s.ActiveRunID {
			return &s.Runs[i]
		}
	}
	return nil
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
	sequences       map[string]uint64
	commitStageHook func(string) error
}

type commitJournal struct {
	Version int           `json:"version"`
	Session *Session      `json:"session"`
	Events  []event.Event `json:"events,omitempty"`
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
	return &Store{workspace: abs, root: root, pending: map[string][]event.Event{}, sequences: map[string]uint64{}}, nil
}
func (s *Store) sessionDir(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return "", errors.New("invalid session ID")
	}
	return filepath.Join(s.root, "sessions", id), nil
}

func (s *Store) Has(id string) bool {
	dir, err := s.sessionDir(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "session.json"))
	return err == nil
}
func (s *Store) Save(ctx context.Context, session *Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSessionPlans(session); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.commitLocked(ctx, session, append([]event.Event(nil), s.pending[session.ID]...)); err != nil {
		return err
	}
	delete(s.pending, session.ID)
	return nil
}

// CommitEvent durably checkpoints session state and a sequenced event before
// returning it to a presentation subscriber.
func (s *Store) CommitEvent(ctx context.Context, session *Session, runtimeEvent event.Event) (event.Event, error) {
	committed, err := s.CommitEvents(ctx, session, []event.Event{runtimeEvent})
	if err != nil {
		return event.Event{}, err
	}
	return committed[0], nil
}

// CommitEvents durably checkpoints session state and an ordered event batch.
func (s *Store) CommitEvents(ctx context.Context, session *Session, runtimeEvents []event.Event) ([]event.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if session == nil || len(runtimeEvents) == 0 {
		return nil, errors.New("session and at least one event are required")
	}
	if err := validateSessionPlans(session); err != nil {
		return nil, err
	}
	for _, runtimeEvent := range runtimeEvents {
		if runtimeEvent.SessionID != session.ID {
			return nil, errors.New("event session does not match checkpoint")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	committed := make([]event.Event, 0, len(runtimeEvents))
	for _, runtimeEvent := range runtimeEvents {
		runtimeEvent = s.sequenceEventLocked(session.ID, runtimeEvent)
		s.pending[session.ID] = append(s.pending[session.ID], runtimeEvent)
		committed = append(committed, runtimeEvent)
	}
	if err := s.commitLocked(ctx, session, append([]event.Event(nil), s.pending[session.ID]...)); err != nil {
		return nil, err
	}
	delete(s.pending, session.ID)
	return committed, nil
}

// CommitDetachedEvent durably relays activity from a child worker when the
// caller does not own the parent Session pointer. It checkpoints the latest
// persisted parent state and never publishes before the event is on disk.
func (s *Store) CommitDetachedEvent(ctx context.Context, sessionID string, runtimeEvent event.Event) (event.Event, error) {
	if err := ctx.Err(); err != nil {
		return event.Event{}, err
	}
	if runtimeEvent.SessionID != sessionID {
		return event.Event{}, errors.New("event session does not match checkpoint")
	}
	dir, err := s.sessionDir(sessionID)
	if err != nil {
		return event.Event{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.recoverJournalLocked(dir, sessionID); err != nil {
		return event.Event{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return event.Event{}, fmt.Errorf("read parent checkpoint: %w", err)
	}
	var checkpoint Session
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&checkpoint); err != nil || checkpoint.SchemaVersion != SchemaVersion || checkpoint.ID != sessionID {
		return event.Event{}, errors.New("parent checkpoint is corrupt or unsupported")
	}
	if err = validateSessionPlans(&checkpoint); err != nil {
		return event.Event{}, err
	}
	runtimeEvent = s.sequenceEventLocked(sessionID, runtimeEvent)
	events := append(append([]event.Event(nil), s.pending[sessionID]...), runtimeEvent)
	if err = s.commitLocked(ctx, &checkpoint, events); err != nil {
		return event.Event{}, err
	}
	delete(s.pending, sessionID)
	return runtimeEvent, nil
}

func validateSessionPlans(session *Session) error {
	if session == nil {
		return errors.New("session checkpoint is required")
	}
	if _, err := NormalizePlanMode(string(session.PlanMode)); err != nil {
		return err
	}
	for i := range session.Runs {
		if _, err := NormalizePlanMode(string(session.Runs[i].PlanMode)); err != nil {
			return fmt.Errorf("invalid run plan mode: %w", err)
		}
		if err := taskplan.Validate(session.Runs[i].Plan); err != nil {
			return fmt.Errorf("invalid run plan: %w", err)
		}
	}
	return nil
}

func (s *Store) commitLocked(ctx context.Context, session *Session, events []event.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if seq := s.sequences[session.ID]; seq > session.NextEventSequence {
		session.NextEventSequence = seq
	} else {
		s.sequences[session.ID] = session.NextEventSequence
	}
	session.UpdatedAt = time.Now().UTC()
	dir, err := s.sessionDir(session.ID)
	if err != nil {
		return err
	}
	journal := commitJournal{Version: commitJournalVersion, Session: session, Events: events}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	if err = storage.AtomicWrite(filepath.Join(dir, "commit.json"), append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write commit journal: %w", err)
	}
	if s.commitStageHook != nil {
		if err = s.commitStageHook("journal_written"); err != nil {
			return err
		}
	}
	return s.applyJournalLocked(dir, journal)
}

func (s *Store) applyJournalLocked(dir string, journal commitJournal) error {
	if journal.Version != commitJournalVersion || journal.Session == nil {
		return errors.New("corrupt commit journal")
	}
	data, err := json.MarshalIndent(journal.Session, "", "  ")
	if err != nil {
		return err
	}
	if err = storage.AtomicWrite(filepath.Join(dir, "session.json"), append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err = storage.AtomicWrite(filepath.Join(dir, "summary.md"), []byte(journal.Session.Summary), 0600); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	if s.commitStageHook != nil {
		if err = s.commitStageHook("checkpoint_written"); err != nil {
			return err
		}
	}
	if err = appendEventsIdempotent(filepath.Join(dir, "events.jsonl"), journal.Events); err != nil {
		return err
	}
	if s.commitStageHook != nil {
		if err = s.commitStageHook("events_written"); err != nil {
			return err
		}
	}
	if err = os.Remove(filepath.Join(dir, "commit.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove commit journal: %w", err)
	}
	return nil
}

func appendEventsIdempotent(path string, events []event.Event) error {
	if len(events) == 0 {
		return nil
	}
	last, err := lastEventSequence(path)
	if err != nil {
		return err
	}
	pending := make([]event.Event, 0, len(events))
	for _, runtimeEvent := range events {
		if runtimeEvent.Sequence == 0 {
			return errors.New("commit journal contains an unsequenced event")
		}
		if runtimeEvent.Sequence > last {
			pending = append(pending, runtimeEvent)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return appendEvents(path, pending)
}

func lastEventSequence(path string) (uint64, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var last uint64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var runtimeEvent event.Event
		if err = json.Unmarshal(scanner.Bytes(), &runtimeEvent); err != nil {
			return 0, fmt.Errorf("corrupt event history: %w", err)
		}
		if runtimeEvent.Sequence > last {
			last = runtimeEvent.Sequence
		}
	}
	return last, scanner.Err()
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
func (s *Store) BufferEvent(id string, e event.Event) event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	e = s.sequenceEventLocked(id, e)
	s.pending[id] = append(s.pending[id], e)
	return e
}

func (s *Store) sequenceEventLocked(id string, runtimeEvent event.Event) event.Event {
	if runtimeEvent.Sequence == 0 {
		s.sequences[id]++
		runtimeEvent.Sequence = s.sequences[id]
	} else if runtimeEvent.Sequence > s.sequences[id] {
		s.sequences[id] = runtimeEvent.Sequence
	}
	return runtimeEvent
}

func (s *Store) SeedEventSequence(id string, sequence uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sequence > s.sequences[id] {
		s.sequences[id] = sequence
	}
}
func (s *Store) Load(ctx context.Context, id string) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := s.sessionDir(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if err = s.recoverJournalLocked(dir, id); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err = json.Unmarshal(b, &header); err != nil {
		return nil, fmt.Errorf("corrupt checkpoint: %w", err)
	}
	if header.SchemaVersion != 1 && header.SchemaVersion != 2 && header.SchemaVersion != 3 && header.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported session schema version %d", header.SchemaVersion)
	}
	var out Session
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&out); err != nil {
		return nil, fmt.Errorf("corrupt checkpoint: %w", err)
	}
	if out.SchemaVersion == 1 {
		migrateV1(&out)
	} else if out.SchemaVersion == 2 {
		migrateV2(&out)
	} else if out.SchemaVersion == 3 {
		migrateV3(&out)
	}
	mode, modeErr := NormalizePlanMode(string(out.PlanMode))
	if modeErr != nil {
		return nil, fmt.Errorf("corrupt plan mode: %w", modeErr)
	}
	out.PlanMode = mode
	for i := range out.Runs {
		runMode, runModeErr := NormalizePlanMode(string(out.Runs[i].PlanMode))
		if runModeErr != nil {
			return nil, fmt.Errorf("corrupt run plan mode: %w", runModeErr)
		}
		out.Runs[i].PlanMode = runMode
		if runMode == PlanAuto {
			out.Runs[i].PlanApproved = true
		}
		if err = taskplan.Validate(out.Runs[i].Plan); err != nil {
			return nil, fmt.Errorf("corrupt run plan: %w", err)
		}
	}
	current, _ := filepath.Abs(s.workspace)
	saved, _ := filepath.Abs(out.Workspace)
	if current != saved {
		return nil, fmt.Errorf("workspace mismatch: saved %s, current %s", saved, current)
	}
	out.WorkspaceChanged = filesChanged(current, out.ModifiedFiles) || (out.GitState != "" && GitState(ctx, current) != out.GitState)
	s.mu.Lock()
	if out.NextEventSequence > s.sequences[out.ID] {
		s.sequences[out.ID] = out.NextEventSequence
	}
	s.mu.Unlock()
	return &out, nil
}

func (s *Store) recoverJournalLocked(dir, sessionID string) error {
	data, err := os.ReadFile(filepath.Join(dir, "commit.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read commit journal: %w", err)
	}
	if len(data) > 8<<20 {
		return errors.New("commit journal exceeds size limit")
	}
	var journal commitJournal
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&journal); err != nil {
		return fmt.Errorf("corrupt commit journal: %w", err)
	}
	if journal.Session == nil || journal.Session.ID != sessionID {
		return errors.New("commit journal session mismatch")
	}
	if err = validateSessionPlans(journal.Session); err != nil {
		return fmt.Errorf("corrupt commit journal: %w", err)
	}
	return s.applyJournalLocked(dir, journal)
}

func (s *Store) Recover(ctx context.Context, id string) (*Session, error) {
	out, err := s.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if active := out.ActiveRun(); active != nil && (active.Status == RunRunning || active.Status == RunVerifying || active.Status == RunQueued) {
		active.Status = RunInterrupted
		active.Error = "process stopped before the run reached a terminal state"
		for i := range out.ToolCalls {
			if out.ToolCalls[i].RunID == active.ID && out.ToolCalls[i].Status == "started" {
				out.ToolCalls[i].Status = "uncertain"
				out.ToolCalls[i].Summary = "execution outcome is unknown; inspect workspace state before replanning"
			}
		}
		if out.Mission != nil && (out.Mission.Status == team.Running || out.Mission.Status == team.Queued) {
			out.Mission.Interrupt("process stopped before the mission reached a terminal state")
		}
		if err := s.Save(ctx, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func filesChanged(root string, files map[string]string) bool {
	for path, want := range files {
		full := filepath.Join(root, path)
		b, err := os.ReadFile(full)
		if want == "deleted" && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return true
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != want {
			return true
		}
	}
	return false
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

func migrateV1(s *Session) {
	s.SchemaVersion = SchemaVersion
	if s.Title == "" {
		s.Title = clipTitle(s.Goal)
	}
	legacyStatus := RunInterrupted
	completedAt := (*time.Time)(nil)
	switch s.Status {
	case Completed:
		legacyStatus = RunCompleted
		t := s.UpdatedAt
		completedAt = &t
	case Failed:
		legacyStatus = RunFailed
		t := s.UpdatedAt
		completedAt = &t
	case Cancelled:
		legacyStatus = RunCancelled
		t := s.UpdatedAt
		completedAt = &t
	}
	if len(s.Runs) == 0 && s.Goal != "" {
		runID := "legacy-" + s.ID
		s.Runs = []Run{{ID: runID, Message: s.Goal, Status: legacyStatus, StartedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, CompletedAt: completedAt, StartTurn: 1, EndTurn: s.Turns, Error: s.LastError}}
		if legacyStatus == RunInterrupted {
			s.ActiveRunID = runID
		}
	}
	s.Status = Open
	if s.ModifiedFiles == nil {
		s.ModifiedFiles = map[string]string{}
	}
}

func migrateV2(s *Session) {
	s.SchemaVersion = SchemaVersion
	if s.ModifiedFiles == nil {
		s.ModifiedFiles = map[string]string{}
	}
}

func migrateV3(s *Session) {
	s.SchemaVersion = SchemaVersion
	if s.ModifiedFiles == nil {
		s.ModifiedFiles = map[string]string{}
	}
}

func (s *Store) AppendMessage(id string, message MessageRecord) error {
	if strings.TrimSpace(message.Content) == "" || (message.Role != model.RoleUser && message.Role != model.RoleAssistant) {
		return errors.New("only visible user or assistant messages may be persisted")
	}
	if message.ID == "" {
		var err error
		message.ID, err = newID()
		if err != nil {
			return err
		}
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	dir, err := s.sessionDir(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return appendJSONLines(filepath.Join(dir, "messages.jsonl"), []MessageRecord{message})
}

func appendJSONLines[T any](path string, records []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 32<<10)
	enc := json.NewEncoder(w)
	for _, record := range records {
		if err = enc.Encode(record); err != nil {
			_ = f.Close()
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

func (s *Store) Messages(id string) ([]MessageRecord, error) {
	dir, err := s.sessionDir(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "messages.jsonl"))
	if os.IsNotExist(err) {
		loaded, loadErr := s.Load(context.Background(), id)
		if loadErr != nil {
			return nil, loadErr
		}
		var fallback []MessageRecord
		for _, msg := range loaded.RecentMessages {
			if (msg.Role == model.RoleUser || msg.Role == model.RoleAssistant) && msg.Content != "" {
				fallback = append(fallback, MessageRecord{RunID: loaded.ActiveRunID, Role: msg.Role, Content: msg.Content, CreatedAt: loaded.UpdatedAt})
			}
		}
		return fallback, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []MessageRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var record MessageRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("corrupt message history: %w", err)
		}
		out = append(out, record)
	}
	return out, scanner.Err()
}

func (s *Store) Events(id string, after uint64) ([]event.Event, error) {
	dir, err := s.sessionDir(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []event.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var record event.Event
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("corrupt event history: %w", err)
		}
		if record.Sequence > after {
			out = append(out, record)
		}
	}
	return out, scanner.Err()
}

func (s *Store) ListSummaries(ctx context.Context, limit int) ([]SessionSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ids, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]SessionSummary, 0, len(ids))
	for _, id := range ids {
		loaded, loadErr := s.Load(ctx, id)
		if loadErr != nil {
			continue
		}
		if loaded.Hidden {
			continue
		}
		item := SessionSummary{ID: loaded.ID, Title: loaded.Title, Status: loaded.Status, UpdatedAt: loaded.UpdatedAt, ActiveRunID: loaded.ActiveRunID, Selection: loaded.Selection}
		if len(loaded.Runs) > 0 {
			item.LastRun = loaded.Runs[len(loaded.Runs)-1].Status
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
