package boardstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/storage"
)

const (
	SchemaVersion     = 1
	MaxDocumentBytes  = 4 << 20
	MaxColumns        = 32
	MaxCards          = 10_000
	MaxLabelsPerCard  = 32
	MaxLabelBytes     = 128
	MaxNoteBodyBytes  = 16 << 10
	MaxDependencies   = 32
	MaxBoardNameBytes = 256
)

var (
	ErrCorrupt  = errors.New("task board store is corrupt")
	ErrConflict = errors.New("task board store conflict")
	ErrCapacity = errors.New("task board store capacity limit")
)

type CardKind string

const (
	CardTask CardKind = "task"
	CardNote CardKind = "note"
)

type Column struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Color    string `json:"color"`
	Hidden   bool   `json:"hidden"`
	WIPLimit int    `json:"wip_limit,omitempty"`
}

type Definition struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// CardMetadata is deliberately view metadata. Task execution state is never
// written here; TaskID is resolved against EmployeeTask and Session/Run truth
// when the Board projection is built.
type CardMetadata struct {
	ID            string     `json:"id"`
	TaskID        string     `json:"task_id,omitempty"`
	Kind          CardKind   `json:"kind"`
	Title         string     `json:"title,omitempty"`
	Body          string     `json:"body,omitempty"`
	ColumnID      string     `json:"column_id"`
	Rank          int64      `json:"rank"`
	Labels        []string   `json:"labels"`
	Priority      int        `json:"priority"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	Pinned        bool       `json:"pinned"`
	Blocked       bool       `json:"blocked"`
	BlockerReason string     `json:"blocker_reason,omitempty"`
	DependsOn     []string   `json:"depends_on"`
	SourceURL     string     `json:"source_url,omitempty"`
	LoopID        string     `json:"loop_id,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ViewPreferences struct {
	View        string `json:"view"`
	ColumnWidth int    `json:"column_width,omitempty"`
	WIPEnabled  bool   `json:"wip_enabled"`
}

type Filters struct {
	EmployeeID string   `json:"employee_id,omitempty"`
	States     []string `json:"states"`
	Labels     []string `json:"labels"`
	Priority   int      `json:"priority,omitempty"`
	Blocked    *bool    `json:"blocked,omitempty"`
	NeedsOwner *bool    `json:"needs_owner,omitempty"`
}

type Document struct {
	SchemaVersion        int             `json:"schema_version"`
	OwnerID              string          `json:"owner_id"`
	WorkspaceFingerprint string          `json:"workspace_fingerprint"`
	Definition           Definition      `json:"definition"`
	Cards                []CardMetadata  `json:"cards"`
	View                 ViewPreferences `json:"view"`
	Filters              Filters         `json:"filters"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type Store struct {
	root                 string
	ownerID              string
	workspaceFingerprint string
	mu                   sync.Mutex
}

func NewStore(root, ownerID, workspace string) (*Store, error) {
	ownerID = strings.TrimSpace(ownerID)
	if err := validateID("owner", ownerID); err != nil {
		return nil, err
	}
	fingerprint, err := WorkspaceFingerprint(workspace)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("resolve task board store: %w", err)
	}
	if err := rejectSymlinkPath(absolute); err != nil {
		return nil, fmt.Errorf("resolve task board store: %w", err)
	}
	return &Store{root: absolute, ownerID: ownerID, workspaceFingerprint: fingerprint}, nil
}

func WorkspaceFingerprint(workspace string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace real path: %w", err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(canonical)))
	return hex.EncodeToString(sum[:16]), nil
}

func (s *Store) Load() (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) Save(document Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(document)
}

func (s *Store) loadLocked() (Document, error) {
	path := filepath.Join(s.root, "board.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s.defaultDocument(), nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("stat task board: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Document{}, fmt.Errorf("%w: board.json must be a regular file", ErrCorrupt)
	}
	if info.Size() > MaxDocumentBytes {
		return Document{}, fmt.Errorf("%w: board.json exceeds size limit", ErrCapacity)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("read task board: %w", err)
	}
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("%w: decode board.json: %v", ErrCorrupt, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Document{}, fmt.Errorf("%w: trailing board data", ErrCorrupt)
	}
	if err := s.validateDocument(document); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return normalize(document), nil
}

func (s *Store) saveLocked(document Document) error {
	document = normalize(document)
	if err := s.validateDocument(document); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode task board: %w", err)
	}
	if len(raw) > MaxDocumentBytes {
		return fmt.Errorf("%w: encoded board is too large", ErrCapacity)
	}
	if err := rejectSymlinkPath(s.root); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return fmt.Errorf("create task board store: %w", err)
	}
	if err := rejectSymlinkPath(s.root); err != nil {
		return err
	}
	path := filepath.Join(s.root, "board.json")
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: board.json is not a regular file", ErrCorrupt)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := storage.AtomicWrite(path, raw, 0600); err != nil {
		return fmt.Errorf("write task board: %w", err)
	}
	return nil
}

func (s *Store) defaultDocument() Document {
	now := time.Now().UTC()
	return Document{
		SchemaVersion: SchemaVersion, OwnerID: s.ownerID,
		WorkspaceFingerprint: s.workspaceFingerprint,
		Definition:           DefaultDefinition(), Cards: []CardMetadata{},
		View:    ViewPreferences{View: "board", WIPEnabled: false},
		Filters: Filters{States: []string{}, Labels: []string{}}, UpdatedAt: now,
	}
}

func DefaultDefinition() Definition {
	return Definition{ID: "default", Name: "Task workspace", Columns: []Column{
		{ID: "backlog", Title: "Backlog", Color: "#64748b"},
		{ID: "todo", Title: "Todo", Color: "#2563eb"},
		{ID: "in_progress", Title: "In progress", Color: "#0891b2"},
		{ID: "review", Title: "Review", Color: "#d97706"},
		{ID: "done", Title: "Done", Color: "#16a34a"},
		{ID: "archived", Title: "Archived", Color: "#94a3b8", Hidden: true},
	}}
}

func (s *Store) validateDocument(document Document) error {
	if document.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported task board schema version %d", document.SchemaVersion)
	}
	if document.OwnerID != s.ownerID || document.WorkspaceFingerprint != s.workspaceFingerprint {
		return fmt.Errorf("task board owner or workspace does not match this service")
	}
	if err := validateID("board", document.Definition.ID); err != nil {
		return err
	}
	if err := validateText("board name", document.Definition.Name, MaxBoardNameBytes, true); err != nil {
		return err
	}
	if len(document.Definition.Columns) == 0 || len(document.Definition.Columns) > MaxColumns {
		return fmt.Errorf("task board columns must contain 1-%d items", MaxColumns)
	}
	columnIDs := make(map[string]struct{}, len(document.Definition.Columns))
	for _, column := range document.Definition.Columns {
		if err := validateID("column", column.ID); err != nil {
			return err
		}
		if _, exists := columnIDs[column.ID]; exists {
			return fmt.Errorf("duplicate task board column %q", column.ID)
		}
		columnIDs[column.ID] = struct{}{}
		if err := validateText("column title", column.Title, MaxBoardNameBytes, true); err != nil {
			return err
		}
		if len(column.Color) > 32 || strings.ContainsAny(column.Color, "\r\n") {
			return errors.New("task board column color is invalid")
		}
		if column.WIPLimit < 0 || column.WIPLimit > MaxCards {
			return errors.New("task board WIP limit is invalid")
		}
	}
	if len(document.Cards) > MaxCards {
		return fmt.Errorf("%w: too many task board cards", ErrCapacity)
	}
	cardIDs := make(map[string]struct{}, len(document.Cards))
	for _, card := range document.Cards {
		if err := validateID("card", card.ID); err != nil {
			return err
		}
		if _, exists := cardIDs[card.ID]; exists {
			return fmt.Errorf("duplicate task board card %q", card.ID)
		}
		cardIDs[card.ID] = struct{}{}
		if card.Kind != CardTask && card.Kind != CardNote {
			return fmt.Errorf("unsupported task board card kind %q", card.Kind)
		}
		if card.Kind == CardTask {
			if err := validateID("card task", card.TaskID); err != nil {
				return err
			}
			if card.Title != "" || card.Body != "" {
				return errors.New("task card metadata cannot persist a prompt")
			}
		} else {
			if err := validateText("note title", card.Title, MaxBoardNameBytes, true); err != nil {
				return err
			}
			if err := validateText("note body", card.Body, MaxNoteBodyBytes, false); err != nil {
				return err
			}
		}
		if _, exists := columnIDs[card.ColumnID]; !exists {
			return fmt.Errorf("card %q references unknown column %q", card.ID, card.ColumnID)
		}
		if len(card.Labels) > MaxLabelsPerCard || len(card.DependsOn) > MaxDependencies {
			return fmt.Errorf("card %q exceeds label or dependency limits", card.ID)
		}
		for _, label := range card.Labels {
			if err := validateText("card label", label, MaxLabelBytes, true); err != nil {
				return err
			}
		}
		for _, dependency := range card.DependsOn {
			if err := validateID("card dependency", dependency); err != nil {
				return err
			}
		}
		if card.Priority < 0 || card.Priority > 4 {
			return fmt.Errorf("card %q priority is outside 0-4", card.ID)
		}
		if card.SourceURL != "" && (len(card.SourceURL) > 2048 || strings.ContainsAny(card.SourceURL, "\r\n")) {
			return fmt.Errorf("card %q source URL is invalid", card.ID)
		}
		if card.LoopID != "" {
			if err := validateID("card loop", card.LoopID); err != nil {
				return err
			}
		}
		if card.UpdatedAt.IsZero() {
			return fmt.Errorf("card %q updated_at is required", card.ID)
		}
	}
	if document.UpdatedAt.IsZero() {
		return errors.New("task board updated_at is required")
	}
	return nil
}

func normalize(document Document) Document {
	if document.Cards == nil {
		document.Cards = []CardMetadata{}
	}
	if document.Filters.States == nil {
		document.Filters.States = []string{}
	}
	if document.Filters.Labels == nil {
		document.Filters.Labels = []string{}
	}
	for index := range document.Cards {
		document.Cards[index].Labels = uniqueSorted(document.Cards[index].Labels)
		document.Cards[index].DependsOn = uniqueSorted(document.Cards[index].DependsOn)
	}
	sort.Slice(document.Cards, func(left, right int) bool { return document.Cards[left].ID < document.Cards[right].ID })
	document.Filters.States = uniqueSorted(document.Filters.States)
	document.Filters.Labels = uniqueSorted(document.Filters.Labels)
	return document
}

func uniqueSorted(items []string) []string {
	result := append([]string{}, items...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	write := 1
	for _, item := range result[1:] {
		if item != result[write-1] {
			result[write] = item
			write++
		}
	}
	return result[:write]
}

func validateID(label, value string) error {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return fmt.Errorf("%s id is invalid", label)
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s id contains an unsafe character", label)
	}
	return nil
}

func validateText(label, value string, maximum int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(value) > maximum || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s is invalid or exceeds its size limit", label)
	}
	return nil
}

func rejectSymlinkPath(root string) error {
	path := filepath.Clean(root)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path %q is a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", path)
	}
	return nil
}
