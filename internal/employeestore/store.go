// Package employeestore persists owner-scoped Electronic Employees. It does
// not own Task, Session, Run, approval, verification, or recovery state.
package employeestore

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/storage"
)

const (
	StoreSchemaVersion    = 1
	ActivitySchemaVersion = 1
	MaxEmployees          = 256
	MaxPageSize           = 100
	MaxStoreFileBytes     = 256 << 10
	MaxActivityFileBytes  = 1 << 20
	MaxActivityEvents     = 1024
)

var (
	ErrNotFound = errors.New("employee not found")
	ErrConflict = errors.New("employee revision conflict")
	ErrCorrupt  = errors.New("employee store is corrupt")
)

type Store struct {
	root string
	mu   sync.Mutex
}

type Record struct {
	Employee        employee.Employee         `json:"employee"`
	ProjectBindings []employee.ProjectBinding `json:"project_bindings"`
}

type Summary struct {
	ID           string         `json:"id"`
	Revision     int            `json:"revision"`
	State        employee.State `json:"state"`
	Name         string         `json:"name"`
	JobTitle     string         `json:"job_title"`
	AgentProfile string         `json:"agent_profile"`
	ProjectCount int            `json:"project_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type ListOptions struct {
	Limit  int
	Cursor string
	State  employee.State
}

type Page struct {
	Employees  []Summary `json:"employees"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type ActivityType string

const (
	ActivityEmployeeCreated  ActivityType = "employee_created"
	ActivityEmployeeUpdated  ActivityType = "employee_updated"
	ActivityEmployeeDisabled ActivityType = "employee_disabled"
	ActivityEmployeeEnabled  ActivityType = "employee_enabled"
	ActivityEmployeeArchived ActivityType = "employee_archived"
	ActivitySkillBinding     ActivityType = "skill_binding_changed"
	ActivityKnowledgeBinding ActivityType = "knowledge_binding_changed"
	ActivityMemoryAccepted   ActivityType = "memory_accepted"
	ActivityMemoryEdited     ActivityType = "memory_edited"
	ActivityMemoryForgotten  ActivityType = "memory_forgotten"
	ActivityExecutionRef     ActivityType = "task_session_run_referenced"
	ActivityTaskCreated      ActivityType = "task_created"
	ActivityTaskCancelled    ActivityType = "task_cancelled"
)

// ActivityEvent is bounded audit/reference metadata, never execution truth.
type ActivityEvent struct {
	SchemaVersion    int          `json:"schema_version"`
	ID               string       `json:"id"`
	EmployeeID       string       `json:"employee_id"`
	Type             ActivityType `json:"type"`
	Time             time.Time    `json:"time"`
	EmployeeRevision int          `json:"employee_revision,omitempty"`
	SubjectID        string       `json:"subject_id,omitempty"`
	TaskID           string       `json:"task_id,omitempty"`
	SessionID        string       `json:"session_id,omitempty"`
	RunID            string       `json:"run_id,omitempty"`
}

type ActivityPage struct {
	Events     []ActivityEvent `json:"events"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type indexFile struct {
	SchemaVersion int       `json:"schema_version"`
	Employees     []Summary `json:"employees"`
}

type projectsFile struct {
	SchemaVersion int                       `json:"schema_version"`
	Bindings      []employee.ProjectBinding `json:"bindings"`
}

func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		root = strings.TrimSpace(os.Getenv("GOHERMIT_EMPLOYEE_STORE"))
	}
	if root == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve employee store: %w", err)
		}
		root = filepath.Join(config, "gohermit", "employees")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve employee store path: %w", err)
	}
	canonical, err := canonicalStoreRoot(filepath.Clean(absolute))
	if err != nil {
		return nil, fmt.Errorf("resolve employee store real path: %w", err)
	}
	return &Store{root: canonical}, nil
}

func (s *Store) Create(draft employee.Employee, bindingDrafts []employee.ProjectBinding) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndex()
	if err != nil {
		return Record{}, err
	}
	if len(index.Employees) >= MaxEmployees {
		return Record{}, errors.New("employee store is full")
	}
	now := time.Now().UTC()
	created, err := employee.Create(draft, now)
	if err != nil {
		return Record{}, err
	}
	if findSummary(index.Employees, created.ID) >= 0 {
		return Record{}, fmt.Errorf("%w: %s already exists", ErrConflict, created.ID)
	}
	bindings, err := prepareBindings(created.ID, bindingDrafts, now)
	if err != nil {
		return Record{}, err
	}
	created.ProjectBindingIDs = bindingIDs(bindings)
	if err := employee.Validate(created); err != nil {
		return Record{}, err
	}
	record := Record{Employee: created, ProjectBindings: bindings}
	if err := s.persistRecord(record, false); err != nil {
		return Record{}, err
	}
	event := lifecycleEvent(created, ActivityEmployeeCreated)
	if err := s.appendActivity(created.ID, event); err != nil {
		return Record{}, err
	}
	index.Employees = append(index.Employees, summarize(record))
	sortSummaries(index.Employees)
	if err := s.saveIndex(index); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Get(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return Record{}, err
	}
	return s.getLocked(id)
}

func (s *Store) getLocked(id string) (Record, error) {
	if err := validateStoreID(id); err != nil {
		return Record{}, err
	}
	index, err := s.loadIndex()
	if err != nil {
		return Record{}, err
	}
	position := findSummary(index.Employees, id)
	if position < 0 {
		return Record{}, ErrNotFound
	}
	return s.loadIndexedRecord(index.Employees[position])
}

func (s *Store) Update(id string, expectedRevision int, proposed employee.Employee, bindingDrafts []employee.ProjectBinding) (Record, error) {
	return s.change(id, expectedRevision, ActivityEmployeeUpdated, func(current Record, now time.Time) (Record, error) {
		bindings, err := prepareBindings(id, bindingDrafts, now)
		if err != nil {
			return Record{}, err
		}
		proposed.ProjectBindingIDs = bindingIDs(bindings)
		revised, err := employee.Revise(current.Employee, proposed, now)
		if err != nil {
			return Record{}, err
		}
		return Record{Employee: revised, ProjectBindings: bindings}, nil
	})
}

func (s *Store) Disable(id string, expectedRevision int) (Record, error) {
	return s.change(id, expectedRevision, ActivityEmployeeDisabled, func(current Record, now time.Time) (Record, error) {
		next, err := employee.Disable(current.Employee, now)
		current.Employee = next
		return current, err
	})
}

func (s *Store) Enable(id string, expectedRevision int) (Record, error) {
	return s.change(id, expectedRevision, ActivityEmployeeEnabled, func(current Record, now time.Time) (Record, error) {
		next, err := employee.Enable(current.Employee, now)
		current.Employee = next
		return current, err
	})
}

func (s *Store) Archive(id string, expectedRevision int) (Record, error) {
	return s.change(id, expectedRevision, ActivityEmployeeArchived, func(current Record, now time.Time) (Record, error) {
		next, err := employee.Archive(current.Employee, now)
		current.Employee = next
		return current, err
	})
}

func (s *Store) change(id string, expected int, activityType ActivityType, mutate func(Record, time.Time) (Record, error)) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return Record{}, err
	}
	current, err := s.getLockedWithoutMutex(id)
	if err != nil {
		return Record{}, err
	}
	if expected != current.Employee.Revision {
		return Record{}, fmt.Errorf("%w: expected %d, current %d", ErrConflict, expected, current.Employee.Revision)
	}
	next, err := mutate(current, time.Now().UTC())
	if err != nil {
		return Record{}, err
	}
	if err := validateRecord(next); err != nil {
		return Record{}, err
	}
	if err := s.persistRecord(next, true); err != nil {
		return Record{}, err
	}
	if err := s.appendActivity(id, lifecycleEvent(next.Employee, activityType)); err != nil {
		return Record{}, err
	}
	index, err := s.loadIndex()
	if err != nil {
		return Record{}, err
	}
	position := findSummary(index.Employees, id)
	if position < 0 {
		return Record{}, ErrNotFound
	}
	index.Employees[position] = summarize(next)
	if err := s.saveIndex(index); err != nil {
		return Record{}, err
	}
	return next, nil
}

func (s *Store) getLockedWithoutMutex(id string) (Record, error) {
	if err := validateStoreID(id); err != nil {
		return Record{}, err
	}
	index, err := s.loadIndex()
	if err != nil {
		return Record{}, err
	}
	position := findSummary(index.Employees, id)
	if position < 0 {
		return Record{}, ErrNotFound
	}
	return s.loadIndexedRecord(index.Employees[position])
}

func (s *Store) List(options ListOptions) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndex()
	if err != nil {
		return Page{}, err
	}
	if err := s.validateIndexedRecords(index); err != nil {
		return Page{}, err
	}
	limit, err := normalizedLimit(options.Limit)
	if err != nil {
		return Page{}, err
	}
	after, err := decodeCursor(options.Cursor)
	if err != nil {
		return Page{}, err
	}
	if after != "" {
		if err := validateStoreID(after); err != nil {
			return Page{}, errors.New("invalid employee pagination cursor")
		}
	}
	if options.State != "" && options.State != employee.StateActive && options.State != employee.StateDisabled && options.State != employee.StateArchived {
		return Page{}, errors.New("invalid employee state filter")
	}
	filtered := make([]Summary, 0, len(index.Employees))
	for _, summary := range index.Employees {
		if summary.ID <= after || options.State != "" && summary.State != options.State {
			continue
		}
		filtered = append(filtered, summary)
	}
	page := Page{Employees: []Summary{}}
	if len(filtered) > limit {
		page.Employees = append(page.Employees, filtered[:limit]...)
		page.NextCursor = encodeCursor(filtered[limit-1].ID)
	} else {
		page.Employees = append(page.Employees, filtered...)
	}
	return page, nil
}

func (s *Store) LoadRevision(id string, revision int) (employee.RevisionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return employee.RevisionSnapshot{}, err
	}
	if revision < 1 {
		return employee.RevisionSnapshot{}, errors.New("employee revision must be positive")
	}
	index, err := s.loadIndex()
	if err != nil {
		return employee.RevisionSnapshot{}, err
	}
	if findSummary(index.Employees, id) < 0 {
		return employee.RevisionSnapshot{}, ErrNotFound
	}
	var snapshot employee.RevisionSnapshot
	if err := s.decodeFileStrict(employee.MaxSnapshotBytes, &snapshot, id, "revisions", strconv.Itoa(revision)+".json"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return employee.RevisionSnapshot{}, ErrNotFound
		}
		return employee.RevisionSnapshot{}, fmt.Errorf("%w: load employee revision: %v", ErrCorrupt, err)
	}
	if err := employee.ValidateRevisionSnapshot(snapshot); err != nil {
		return employee.RevisionSnapshot{}, err
	}
	if snapshot.EmployeeID != id || snapshot.Revision != revision {
		return employee.RevisionSnapshot{}, errors.New("employee snapshot does not match requested identity")
	}
	return snapshot, nil
}

func (s *Store) loadIndexedRecord(expected Summary) (Record, error) {
	if err := validateStoreID(expected.ID); err != nil {
		return Record{}, fmt.Errorf("%w: employee index contains invalid id %q: %v", ErrCorrupt, expected.ID, err)
	}
	if err := s.validateEmployeeLayout(expected.ID); err != nil {
		return Record{}, fmt.Errorf("%w: employee %q layout: %v", ErrCorrupt, expected.ID, err)
	}
	var current employee.Employee
	if err := s.decodeFileStrict(MaxStoreFileBytes, &current, expected.ID, "employee.json"); err != nil {
		return Record{}, fmt.Errorf("%w: load employee %q: %v", ErrCorrupt, expected.ID, err)
	}
	var projects projectsFile
	if err := s.decodeFileStrict(MaxStoreFileBytes, &projects, expected.ID, "projects.json"); err != nil {
		return Record{}, fmt.Errorf("%w: load employee projects %q: %v", ErrCorrupt, expected.ID, err)
	}
	if projects.SchemaVersion != StoreSchemaVersion {
		return Record{}, fmt.Errorf("%w: unsupported employee projects schema version %d", ErrCorrupt, projects.SchemaVersion)
	}
	record := Record{Employee: current, ProjectBindings: projects.Bindings}
	if err := validateRecord(record); err != nil {
		return Record{}, fmt.Errorf("%w: employee %q: %v", ErrCorrupt, expected.ID, err)
	}
	if summarize(record) != expected {
		return Record{}, fmt.Errorf("%w: employee index and record disagree", ErrCorrupt)
	}
	return record, nil
}

func (s *Store) validateEmployeeLayout(id string) error {
	if err := validateStoreID(id); err != nil {
		return err
	}
	if err := s.requireDirectory(id); err != nil {
		return err
	}
	if _, err := s.safeFileInfo(id, "employee.json"); err != nil {
		return err
	}
	if _, err := s.safeFileInfo(id, "projects.json"); err != nil {
		return err
	}
	if err := s.requireDirectory(id, "activity"); err != nil {
		return err
	}
	if _, err := s.safeFileInfo(id, "activity", "events.jsonl"); err != nil {
		return err
	}
	if err := s.requireDirectory(id, "revisions"); err != nil {
		return err
	}
	entries, err := s.readDirectory(id, "revisions")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("employee revision entry is not a regular file")
		}
		if _, err := s.safeFileInfo(id, "revisions", entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Activity(id string, options ListOptions) (ActivityPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(id); err != nil {
		return ActivityPage{}, err
	}
	if _, err := s.getLockedWithoutMutex(id); err != nil {
		return ActivityPage{}, err
	}
	events, err := s.loadActivity(id)
	if err != nil {
		return ActivityPage{}, err
	}
	limit, err := normalizedLimit(options.Limit)
	if err != nil {
		return ActivityPage{}, err
	}
	after, err := decodeCursor(options.Cursor)
	if err != nil {
		return ActivityPage{}, err
	}
	if after != "" && !validActivityID(after) {
		return ActivityPage{}, errors.New("invalid employee activity cursor")
	}
	filtered := make([]ActivityEvent, 0, len(events))
	for _, event := range events {
		if event.ID > after {
			filtered = append(filtered, event)
		}
	}
	page := ActivityPage{Events: []ActivityEvent{}}
	if len(filtered) > limit {
		page.Events = append(page.Events, filtered[:limit]...)
		page.NextCursor = encodeCursor(filtered[limit-1].ID)
	} else {
		page.Events = append(page.Events, filtered...)
	}
	return page, nil
}

func (s *Store) RecordActivity(event ActivityEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStoreID(event.EmployeeID); err != nil {
		return err
	}
	if event.ID != "" {
		return errors.New("employee activity id is store-assigned")
	}
	if _, err := s.getLockedWithoutMutex(event.EmployeeID); err != nil {
		return err
	}
	return s.appendActivity(event.EmployeeID, event)
}

func (s *Store) persistRecord(record Record, updating bool) error {
	if err := validateRecord(record); err != nil {
		return err
	}
	if err := validateStoreID(record.Employee.ID); err != nil {
		return err
	}
	snapshot, err := employee.NewRevisionSnapshot(record.Employee, record.ProjectBindings)
	if err != nil {
		return err
	}
	revisionName := strconv.Itoa(record.Employee.Revision) + ".json"
	if err := s.writeJSONExclusive(snapshot, employee.MaxSnapshotBytes, record.Employee.ID, "revisions", revisionName); err != nil {
		return err
	}
	if err := s.writeJSON(projectsFile{StoreSchemaVersion, record.ProjectBindings}, MaxStoreFileBytes, record.Employee.ID, "projects.json"); err != nil {
		return err
	}
	if err := s.writeJSON(record.Employee, MaxStoreFileBytes, record.Employee.ID, "employee.json"); err != nil {
		return err
	}
	_ = updating
	return nil
}

func (s *Store) loadIndex() (indexFile, error) {
	var index indexFile
	err := s.decodeFileStrict(MaxStoreFileBytes, &index, "index.json")
	if errors.Is(err, os.ErrNotExist) {
		return indexFile{SchemaVersion: StoreSchemaVersion, Employees: []Summary{}}, nil
	}
	if err != nil {
		return indexFile{}, fmt.Errorf("%w: load employee index: %v", ErrCorrupt, err)
	}
	if index.SchemaVersion != StoreSchemaVersion {
		return indexFile{}, fmt.Errorf("%w: unsupported employee index schema version %d", ErrCorrupt, index.SchemaVersion)
	}
	if len(index.Employees) > MaxEmployees {
		return indexFile{}, fmt.Errorf("%w: employee index exceeds record limit", ErrCorrupt)
	}
	for i, summary := range index.Employees {
		if err := validateStoreID(summary.ID); err != nil {
			return indexFile{}, fmt.Errorf("%w: employee index contains invalid id %q: %v", ErrCorrupt, summary.ID, err)
		}
		if summary.ID == "" || summary.Revision < 1 || summary.Name == "" || summary.UpdatedAt.IsZero() {
			return indexFile{}, fmt.Errorf("%w: employee index contains invalid summary", ErrCorrupt)
		}
		if i > 0 && index.Employees[i-1].ID >= summary.ID {
			return indexFile{}, fmt.Errorf("%w: employee index is not strictly sorted", ErrCorrupt)
		}
	}
	if index.Employees == nil {
		index.Employees = []Summary{}
	}
	return index, nil
}

func (s *Store) validateIndexedRecords(index indexFile) error {
	for _, expected := range index.Employees {
		if _, err := s.loadIndexedRecord(expected); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) saveIndex(index indexFile) error {
	index.SchemaVersion = StoreSchemaVersion
	sortSummaries(index.Employees)
	return s.writeJSON(index, MaxStoreFileBytes, "index.json")
}

func (s *Store) loadActivity(id string) ([]ActivityEvent, error) {
	if err := validateStoreID(id); err != nil {
		return nil, err
	}
	raw, err := s.readBounded(MaxActivityFileBytes, id, "activity", "events.jsonl")
	if errors.Is(err, os.ErrNotExist) {
		return []ActivityEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte{'\n'})
	if len(lines) == 1 && len(lines[0]) == 0 {
		return []ActivityEvent{}, nil
	}
	if len(lines) > MaxActivityEvents {
		return nil, errors.New("employee activity exceeds event limit")
	}
	events := make([]ActivityEvent, 0, len(lines))
	lastID := ""
	for _, line := range lines {
		var event ActivityEvent
		if err := decodeStrict(line, &event); err != nil {
			return nil, fmt.Errorf("decode employee activity: %w", err)
		}
		if err := validateActivity(event); err != nil {
			return nil, err
		}
		if event.EmployeeID != id {
			return nil, errors.New("employee activity identity mismatch")
		}
		if !validActivityID(event.ID) {
			return nil, errors.New("employee activity id is invalid")
		}
		if lastID != "" && event.ID <= lastID {
			return nil, errors.New("employee activity ids are not strictly increasing")
		}
		events = append(events, event)
		lastID = event.ID
	}
	return events, nil
}

func (s *Store) appendActivity(id string, event ActivityEvent) error {
	if event.SchemaVersion == 0 {
		event.SchemaVersion = ActivitySchemaVersion
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	events, err := s.loadActivity(id)
	if err != nil {
		return err
	}
	lastID := ""
	if len(events) > 0 {
		lastID = events[len(events)-1].ID
	}
	if event.ID != "" {
		return errors.New("employee activity id is store-assigned")
	}
	event.ID, err = newActivityID(lastID)
	if err != nil {
		return err
	}
	if err := validateActivity(event); err != nil {
		return err
	}
	if event.EmployeeID != id {
		return errors.New("employee activity identity mismatch")
	}
	if lastID != "" && event.ID <= lastID {
		return errors.New("employee activity id is not strictly increasing")
	}
	events = append(events, event)
	if len(events) > MaxActivityEvents {
		events = events[len(events)-MaxActivityEvents:]
	}
	var buffer bytes.Buffer
	for _, item := range events {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		buffer.Write(raw)
		buffer.WriteByte('\n')
	}
	if buffer.Len() > MaxActivityFileBytes {
		return errors.New("employee activity exceeds file size limit")
	}
	return s.atomicWrite(buffer.Bytes(), id, "activity", "events.jsonl")
}

func validateRecord(record Record) error {
	if err := employee.Validate(record.Employee); err != nil {
		return err
	}
	snapshot, err := employee.NewRevisionSnapshot(record.Employee, record.ProjectBindings)
	if err != nil {
		return err
	}
	return employee.ValidateRevisionSnapshot(snapshot)
}

func validateActivity(event ActivityEvent) error {
	if event.SchemaVersion != ActivitySchemaVersion || event.ID == "" || event.EmployeeID == "" || event.Time.IsZero() {
		return errors.New("invalid employee activity identity")
	}
	if len(event.ID) > employee.MaxIDBytes || len(event.EmployeeID) > employee.MaxIDBytes ||
		len(event.SubjectID) > employee.MaxIDBytes || len(event.TaskID) > employee.MaxIDBytes ||
		len(event.SessionID) > employee.MaxIDBytes || len(event.RunID) > employee.MaxIDBytes {
		return errors.New("employee activity identifier exceeds size limit")
	}
	switch event.Type {
	case ActivityEmployeeCreated, ActivityEmployeeUpdated, ActivityEmployeeDisabled, ActivityEmployeeEnabled, ActivityEmployeeArchived:
		if event.EmployeeRevision < 1 || event.SubjectID != "" || event.TaskID != "" || event.SessionID != "" || event.RunID != "" {
			return errors.New("invalid employee lifecycle activity")
		}
	case ActivitySkillBinding, ActivityKnowledgeBinding, ActivityMemoryAccepted, ActivityMemoryEdited, ActivityMemoryForgotten:
		if event.SubjectID == "" || event.TaskID != "" || event.SessionID != "" || event.RunID != "" {
			return errors.New("invalid employee binding or memory activity")
		}
	case ActivityExecutionRef:
		if event.TaskID == "" {
			return errors.New("employee execution reference requires task_id")
		}
	case ActivityTaskCreated, ActivityTaskCancelled:
		if event.EmployeeRevision < 1 || event.TaskID == "" || event.SubjectID != "" || event.SessionID != "" || event.RunID != "" {
			return errors.New("invalid Employee Task reference activity")
		}
		if err := validateStoreID(event.TaskID); err != nil {
			return errors.New("invalid Employee Task activity Task id")
		}
	default:
		return fmt.Errorf("unsupported employee activity type %q", event.Type)
	}
	raw, _ := json.Marshal(event)
	if len(raw) > 4<<10 {
		return errors.New("employee activity record exceeds size limit")
	}
	return nil
}

func prepareBindings(employeeID string, drafts []employee.ProjectBinding, now time.Time) ([]employee.ProjectBinding, error) {
	if len(drafts) > employee.MaxProjectBindings {
		return nil, errors.New("too many employee project bindings")
	}
	bindings := make([]employee.ProjectBinding, len(drafts))
	seen := map[string]struct{}{}
	for i, draft := range drafts {
		draft.EmployeeID = employeeID
		binding, err := employee.CreateProjectBinding(draft, now)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[binding.ID]; exists {
			return nil, errors.New("duplicate employee project binding")
		}
		seen[binding.ID] = struct{}{}
		bindings[i] = binding
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ID < bindings[j].ID })
	return bindings, nil
}

func bindingIDs(bindings []employee.ProjectBinding) []string {
	ids := make([]string, len(bindings))
	for i := range bindings {
		ids[i] = bindings[i].ID
	}
	return ids
}

func lifecycleEvent(value employee.Employee, kind ActivityType) ActivityEvent {
	return ActivityEvent{SchemaVersion: ActivitySchemaVersion, EmployeeID: value.ID, Type: kind, Time: value.UpdatedAt, EmployeeRevision: value.Revision}
}

func summarize(record Record) Summary {
	value := record.Employee
	return Summary{ID: value.ID, Revision: value.Revision, State: value.State, Name: value.Name, JobTitle: value.JobTitle, AgentProfile: value.AgentProfile, ProjectCount: len(record.ProjectBindings), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func sortSummaries(items []Summary) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}

func findSummary(items []Summary, id string) int {
	position := sort.Search(len(items), func(i int) bool { return items[i].ID >= id })
	if position < len(items) && items[position].ID == id {
		return position
	}
	return -1
}

func normalizedLimit(limit int) (int, error) {
	if limit == 0 {
		return 50, nil
	}
	if limit < 1 || limit > MaxPageSize {
		return 0, fmt.Errorf("limit must be between 1 and %d", MaxPageSize)
	}
	return limit, nil
}

func encodeCursor(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) == 0 || len(raw) > employee.MaxIDBytes {
		return "", errors.New("invalid pagination cursor")
	}
	return string(raw), nil
}

func (s *Store) writeJSON(value any, maximum int64, parts ...string) error {
	raw, err := marshalBoundedJSON(value, maximum)
	if err != nil {
		return err
	}
	return s.atomicWrite(raw, parts...)
}

func (s *Store) writeJSONExclusive(value any, maximum int64, parts ...string) error {
	raw, err := marshalBoundedJSON(value, maximum)
	if err != nil {
		return err
	}
	return s.atomicWriteExclusive(raw, parts...)
}

func marshalBoundedJSON(value any, maximum int64) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if int64(len(raw)) > maximum {
		return nil, errors.New("employee store record exceeds size limit")
	}
	return raw, nil
}

func (s *Store) decodeFileStrict(maximum int64, target any, parts ...string) error {
	raw, err := s.readBounded(maximum, parts...)
	if err != nil {
		return err
	}
	return decodeStrict(raw, target)
}

func (s *Store) readBounded(maximum int64, parts ...string) ([]byte, error) {
	path, info, err := s.safeFileInfoPath(parts...)
	if err != nil {
		return nil, err
	}
	if info.Size() > maximum {
		return nil, errors.New("employee store file exceeds size limit")
	}
	return os.ReadFile(path)
}

func (s *Store) safeFileInfo(parts ...string) (os.FileInfo, error) {
	_, info, err := s.safeFileInfoPath(parts...)
	return info, err
}

func (s *Store) safeFileInfoPath(parts ...string) (string, os.FileInfo, error) {
	path, err := s.safeStorePath(parts...)
	if err != nil {
		return "", nil, err
	}
	if err := walkSafeDirectories(filepath.Dir(path), false); err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("employee store file must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("employee store file is not regular")
	}
	return path, info, nil
}

func (s *Store) requireDirectory(parts ...string) error {
	path, err := s.safeStorePath(parts...)
	if err != nil {
		return err
	}
	if err := walkSafeDirectories(filepath.Dir(path), false); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("employee store directory must not be a symlink")
	}
	if !info.IsDir() {
		return errors.New("employee store path is not a directory")
	}
	return nil
}

func (s *Store) readDirectory(parts ...string) ([]os.DirEntry, error) {
	if err := s.requireDirectory(parts...); err != nil {
		return nil, err
	}
	path, err := s.safeStorePath(parts...)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}

func (s *Store) atomicWrite(data []byte, parts ...string) error {
	return s.atomicWriteMode(data, false, parts...)
}

func (s *Store) atomicWriteExclusive(data []byte, parts ...string) error {
	return s.atomicWriteMode(data, true, parts...)
}

func (s *Store) atomicWriteMode(data []byte, exclusive bool, parts ...string) error {
	path, err := s.safeStorePath(parts...)
	if err != nil {
		return err
	}
	if err := walkSafeDirectories(filepath.Dir(path), true); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return errors.New("employee store write target must not be a symlink")
	case err == nil && !info.Mode().IsRegular():
		return errors.New("employee store write target is not regular")
	case err == nil && exclusive:
		return errors.New("immutable employee revision already exists")
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return err
	}
	if err := storage.AtomicWrite(path, data, 0o600); err != nil {
		return err
	}
	written, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if written.Mode()&os.ModeSymlink != 0 || !written.Mode().IsRegular() {
		return errors.New("employee store atomic write produced an unsafe file")
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return errors.New("employee store file is invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("employee store file contains multiple values")
		}
		return err
	}
	return nil
}

var activityRandomRead = rand.Read

func newActivityID(after string) (string, error) {
	nanos := time.Now().UTC().UnixNano()
	if after != "" {
		if !validActivityID(after) {
			return "", errors.New("previous employee activity id is invalid")
		}
		previous, err := strconv.ParseInt(after[:20], 10, 64)
		if err != nil {
			return "", errors.New("previous employee activity timestamp is invalid")
		}
		if nanos <= previous {
			if previous == int64(^uint64(0)>>1) {
				return "", errors.New("employee activity id space exhausted")
			}
			nanos = previous + 1
		}
	}
	var random [8]byte
	if _, err := activityRandomRead(random[:]); err != nil {
		return "", fmt.Errorf("generate employee activity id: %w", err)
	}
	id := fmt.Sprintf("%020d-%s", nanos, hex.EncodeToString(random[:]))
	if after != "" && id <= after {
		return "", errors.New("generated employee activity id is not strictly increasing")
	}
	return id, nil
}

func validActivityID(id string) bool {
	if len(id) != 37 || id[20] != '-' {
		return false
	}
	for _, char := range id[:20] {
		if char < '0' || char > '9' {
			return false
		}
	}
	_, err := hex.DecodeString(id[21:])
	return err == nil
}

func validateStoreID(id string) error {
	if id == "" || len(id) > employee.MaxIDBytes || filepath.IsAbs(id) ||
		filepath.Clean(id) != id || filepath.Base(id) != id || id == "." || id == ".." {
		return errors.New("employee id is invalid")
	}
	for _, char := range id {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || strings.ContainsRune("_-.", char) {
			continue
		}
		return errors.New("employee id contains an unsupported character")
	}
	return nil
}

func (s *Store) safeStorePath(parts ...string) (string, error) {
	for _, part := range parts {
		if part == "" || filepath.IsAbs(part) || filepath.Clean(part) != part ||
			filepath.Base(part) != part || part == "." || part == ".." {
			return "", errors.New("employee store path component is invalid")
		}
	}
	path := filepath.Join(append([]string{s.root}, parts...)...)
	if err := ensureContained(s.root, path); err != nil {
		return "", err
	}
	return path, nil
}

func ensureContained(root, path string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("employee store path escapes root")
	}
	return nil
}

func canonicalStoreRoot(root string) (string, error) {
	current := filepath.Clean(root)
	var missing []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			resolvedInfo, err := os.Stat(resolved)
			if err != nil {
				return "", err
			}
			if !resolvedInfo.IsDir() {
				return "", errors.New("employee store root or existing parent is not a directory")
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("employee store has no existing directory ancestor")
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func walkSafeDirectories(target string, create bool) error {
	target = filepath.Clean(target)
	var chain []string
	for current := target; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		path := chain[i]
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(path)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("employee store parent directory must not be a symlink")
		}
		if !info.IsDir() {
			return errors.New("employee store parent path is not a directory")
		}
	}
	return nil
}
