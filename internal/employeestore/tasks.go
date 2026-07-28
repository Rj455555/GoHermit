package employeestore

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
)

const (
	TaskIndexSchemaVersion = 1
	MaxTasksPerEmployee    = 10_000
	MaxTaskPageSize        = 100
	MaxTaskIndexBytes      = 8 << 20
)

type TaskListOptions struct {
	Limit  int
	Cursor string
	State  employee.TaskState
}

type TaskSummary struct {
	ID               string             `json:"id"`
	EmployeeID       string             `json:"employee_id"`
	EmployeeRevision int                `json:"employee_revision"`
	State            employee.TaskState `json:"state"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	CancelledAt      *time.Time         `json:"cancelled_at,omitempty"`
	SnapshotDigest   string             `json:"snapshot_digest"`
}

type TaskPage struct {
	Tasks      []TaskSummary `json:"tasks"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type taskIndexFile struct {
	SchemaVersion int           `json:"schema_version"`
	EmployeeID    string        `json:"employee_id"`
	Tasks         []TaskSummary `json:"tasks"`
}

// CreateTask durably queues a Task. It never prepares or starts execution.
func (s *Store) CreateTask(employeeID string, draft employee.EmployeeTask) (employee.EmployeeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateStoreID(employeeID); err != nil {
		return employee.EmployeeTask{}, err
	}
	record, err := s.getLockedWithoutMutex(employeeID)
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if record.Employee.State != employee.StateActive {
		return employee.EmployeeTask{}, fmt.Errorf("%w: only an active Employee can create a Task", ErrConflict)
	}
	if draft.ID != "" {
		return employee.EmployeeTask{}, errors.New("Employee Task id is store-assigned")
	}
	if draft.EmployeeID != employeeID || draft.EmployeeRevision != record.Employee.Revision {
		return employee.EmployeeTask{}, fmt.Errorf("%w: Employee Task revision is stale or mismatched", ErrConflict)
	}
	currentSnapshot, err := employee.NewRevisionSnapshot(record.Employee, record.ProjectBindings)
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if !reflect.DeepEqual(draft.EmployeeSnapshot, currentSnapshot) {
		return employee.EmployeeTask{}, fmt.Errorf("%w: Employee Task snapshot is not the current Employee revision", ErrConflict)
	}

	index, err := s.loadTaskIndex(employeeID)
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if len(index.Tasks) >= MaxTasksPerEmployee {
		return employee.EmployeeTask{}, fmt.Errorf("%w: Employee Task inbox is full", ErrConflict)
	}
	taskID, err := newTaskID()
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	exists, err := s.taskIDExists(taskID)
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if exists {
		return employee.EmployeeTask{}, fmt.Errorf("%w: generated Employee Task id already exists", ErrConflict)
	}

	draft.ID = taskID
	created, err := employee.NewTask(draft, time.Now().UTC())
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if err := s.writeJSONExclusive(created, employee.MaxTaskFileBytes, employeeID, "tasks", taskID+".json"); err != nil {
		return employee.EmployeeTask{}, err
	}
	index.Tasks = append(index.Tasks, summarizeTask(created))
	sortTaskSummaries(index.Tasks)
	if err := s.saveTaskIndex(index); err != nil {
		return employee.EmployeeTask{}, err
	}
	if err := s.appendActivity(employeeID, taskActivity(created, ActivityTaskCreated)); err != nil {
		return employee.EmployeeTask{}, err
	}
	return created.Clone(), nil
}

// ListTasks returns stable newest-first pages from one Employee's isolated inbox.
func (s *Store) ListTasks(employeeID string, options TaskListOptions) (TaskPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateStoreID(employeeID); err != nil {
		return TaskPage{}, err
	}
	if _, err := s.getLockedWithoutMutex(employeeID); err != nil {
		return TaskPage{}, err
	}
	limit, err := normalizedTaskLimit(options.Limit)
	if err != nil {
		return TaskPage{}, err
	}
	if options.State != "" && options.State != employee.TaskQueued && options.State != employee.TaskCancelled {
		return TaskPage{}, errors.New("invalid Employee Task state filter")
	}
	index, err := s.loadTaskIndex(employeeID)
	if err != nil {
		return TaskPage{}, err
	}
	filtered := make([]TaskSummary, 0, len(index.Tasks))
	for _, item := range index.Tasks {
		if options.State == "" || item.State == options.State {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if options.Cursor != "" {
		cursor, err := decodeTaskCursor(options.Cursor)
		if err != nil {
			return TaskPage{}, err
		}
		found := false
		for index, item := range filtered {
			if taskCursorKey(item) == cursor {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			return TaskPage{}, errors.New("invalid or stale Employee Task cursor")
		}
	}
	page := TaskPage{Tasks: []TaskSummary{}}
	end := min(start+limit, len(filtered))
	page.Tasks = append(page.Tasks, filtered[start:end]...)
	if end < len(filtered) && len(page.Tasks) > 0 {
		page.NextCursor = encodeTaskCursor(taskCursorKey(page.Tasks[len(page.Tasks)-1]))
	}
	return page, nil
}

// GetTask performs an unambiguous owner-scoped lookup across indexed Employees.
func (s *Store) GetTask(taskID string) (employee.EmployeeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getTaskLocked(taskID)
}

// CancelTask is idempotent. Cancellation never changes immutable snapshot data.
func (s *Store) CancelTask(taskID string) (employee.EmployeeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.getTaskLocked(taskID)
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if current.State == employee.TaskCancelled {
		return current.Clone(), nil
	}
	next, err := employee.CancelTask(current, time.Now().UTC())
	if err != nil {
		return employee.EmployeeTask{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	index, err := s.loadTaskIndex(next.EmployeeID)
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	position := findTaskSummary(index.Tasks, next.ID)
	if position < 0 {
		return employee.EmployeeTask{}, fmt.Errorf("%w: cancelled Employee Task is absent from index", ErrCorrupt)
	}
	if err := s.writeJSON(next, employee.MaxTaskFileBytes, next.EmployeeID, "tasks", next.ID+".json"); err != nil {
		return employee.EmployeeTask{}, err
	}
	index.Tasks[position] = summarizeTask(next)
	sortTaskSummaries(index.Tasks)
	if err := s.saveTaskIndex(index); err != nil {
		return employee.EmployeeTask{}, err
	}
	if err := s.appendActivity(next.EmployeeID, taskActivity(next, ActivityTaskCancelled)); err != nil {
		return employee.EmployeeTask{}, err
	}
	return next.Clone(), nil
}

func (s *Store) getTaskLocked(taskID string) (employee.EmployeeTask, error) {
	if err := validateStoreID(taskID); err != nil {
		return employee.EmployeeTask{}, err
	}
	index, err := s.loadIndex()
	if err != nil {
		return employee.EmployeeTask{}, err
	}
	if err := s.validateIndexedRecords(index); err != nil {
		return employee.EmployeeTask{}, err
	}
	var found *TaskSummary
	for _, employeeSummary := range index.Employees {
		taskIndex, err := s.loadTaskIndex(employeeSummary.ID)
		if err != nil {
			return employee.EmployeeTask{}, err
		}
		for itemIndex := range taskIndex.Tasks {
			if taskIndex.Tasks[itemIndex].ID != taskID {
				continue
			}
			if found != nil {
				return employee.EmployeeTask{}, fmt.Errorf("%w: Employee Task id is ambiguous", ErrCorrupt)
			}
			copy := taskIndex.Tasks[itemIndex]
			found = &copy
		}
	}
	if found == nil {
		return employee.EmployeeTask{}, ErrNotFound
	}
	return s.loadTask(*found)
}

func (s *Store) taskIDExists(taskID string) (bool, error) {
	index, err := s.loadIndex()
	if err != nil {
		return false, err
	}
	if err := s.validateIndexedRecords(index); err != nil {
		return false, err
	}
	found := false
	for _, employeeSummary := range index.Employees {
		taskIndex, err := s.loadTaskIndex(employeeSummary.ID)
		if err != nil {
			return false, err
		}
		for _, item := range taskIndex.Tasks {
			if item.ID == taskID {
				if found {
					return false, fmt.Errorf("%w: duplicate Employee Task id", ErrCorrupt)
				}
				found = true
			}
		}
	}
	return found, nil
}

func (s *Store) loadTaskIndex(employeeID string) (taskIndexFile, error) {
	if err := validateStoreID(employeeID); err != nil {
		return taskIndexFile{}, err
	}
	err := s.requireDirectory(employeeID, "tasks")
	if errors.Is(err, os.ErrNotExist) {
		return taskIndexFile{
			SchemaVersion: TaskIndexSchemaVersion,
			EmployeeID:    employeeID,
			Tasks:         []TaskSummary{},
		}, nil
	}
	if err != nil {
		return taskIndexFile{}, fmt.Errorf("%w: unsafe Employee Task directory: %v", ErrCorrupt, err)
	}
	entries, err := s.readDirectory(employeeID, "tasks")
	if err != nil {
		return taskIndexFile{}, fmt.Errorf("%w: read Employee Task directory: %v", ErrCorrupt, err)
	}
	var index taskIndexFile
	err = s.decodeFileStrict(MaxTaskIndexBytes, &index, employeeID, "tasks", "index.json")
	if errors.Is(err, os.ErrNotExist) {
		if len(entries) == 0 {
			return taskIndexFile{
				SchemaVersion: TaskIndexSchemaVersion,
				EmployeeID:    employeeID,
				Tasks:         []TaskSummary{},
			}, nil
		}
		return taskIndexFile{}, fmt.Errorf("%w: Employee Task directory is missing index.json", ErrCorrupt)
	}
	if err != nil {
		return taskIndexFile{}, fmt.Errorf("%w: load Employee Task index: %v", ErrCorrupt, err)
	}
	if err := validateTaskIndex(index, employeeID); err != nil {
		return taskIndexFile{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	expectedFiles := make(map[string]TaskSummary, len(index.Tasks))
	for _, summary := range index.Tasks {
		expectedFiles[summary.ID+".json"] = summary
	}
	seenDispatch := make(map[string]struct{})
	for _, entry := range entries {
		if entry.Name() == "index.json" {
			if _, err := s.safeFileInfo(employeeID, "tasks", entry.Name()); err != nil {
				return taskIndexFile{}, fmt.Errorf("%w: unsafe Employee Task index: %v", ErrCorrupt, err)
			}
			continue
		}
		if strings.HasSuffix(entry.Name(), ".dispatch.json") {
			taskID := strings.TrimSuffix(entry.Name(), ".dispatch.json")
			summary, exists := expectedFiles[taskID+".json"]
			if !exists {
				return taskIndexFile{}, fmt.Errorf("%w: dispatch journal has no indexed Employee Task", ErrCorrupt)
			}
			if _, duplicate := seenDispatch[taskID]; duplicate {
				return taskIndexFile{}, fmt.Errorf("%w: duplicate Employee Task dispatch journal", ErrCorrupt)
			}
			seenDispatch[taskID] = struct{}{}
			if _, err := s.loadDispatchExpected(summary); err != nil {
				return taskIndexFile{}, err
			}
			continue
		}
		summary, exists := expectedFiles[entry.Name()]
		if !exists {
			return taskIndexFile{}, fmt.Errorf("%w: unindexed Employee Task entry %q", ErrCorrupt, entry.Name())
		}
		if _, err := s.loadTask(summary); err != nil {
			return taskIndexFile{}, err
		}
		delete(expectedFiles, entry.Name())
	}
	if len(expectedFiles) != 0 {
		return taskIndexFile{}, fmt.Errorf("%w: Employee Task index references missing files", ErrCorrupt)
	}
	return index, nil
}

func (s *Store) loadTask(expected TaskSummary) (employee.EmployeeTask, error) {
	if err := validateStoreID(expected.EmployeeID); err != nil {
		return employee.EmployeeTask{}, fmt.Errorf("%w: invalid indexed Employee id: %v", ErrCorrupt, err)
	}
	if err := validateStoreID(expected.ID); err != nil {
		return employee.EmployeeTask{}, fmt.Errorf("%w: invalid indexed Task id: %v", ErrCorrupt, err)
	}
	var task employee.EmployeeTask
	if err := s.decodeFileStrict(employee.MaxTaskFileBytes, &task, expected.EmployeeID, "tasks", expected.ID+".json"); err != nil {
		return employee.EmployeeTask{}, fmt.Errorf("%w: load Employee Task: %v", ErrCorrupt, err)
	}
	if err := employee.ValidateTask(task); err != nil {
		return employee.EmployeeTask{}, fmt.Errorf("%w: invalid Employee Task: %v", ErrCorrupt, err)
	}
	if task.ID != expected.ID || task.EmployeeID != expected.EmployeeID ||
		!reflect.DeepEqual(summarizeTask(task), expected) {
		return employee.EmployeeTask{}, fmt.Errorf("%w: Employee Task index and record disagree", ErrCorrupt)
	}
	return task.Clone(), nil
}

func (s *Store) saveTaskIndex(index taskIndexFile) error {
	if err := validateTaskIndex(index, index.EmployeeID); err != nil {
		return err
	}
	return s.writeJSON(index, MaxTaskIndexBytes, index.EmployeeID, "tasks", "index.json")
}

func validateTaskIndex(index taskIndexFile, employeeID string) error {
	if err := validateStoreID(employeeID); err != nil {
		return err
	}
	if index.SchemaVersion != TaskIndexSchemaVersion {
		return fmt.Errorf("unsupported Employee Task index schema version %d", index.SchemaVersion)
	}
	if index.EmployeeID != employeeID {
		return errors.New("Employee Task index identity mismatch")
	}
	if len(index.Tasks) > MaxTasksPerEmployee {
		return errors.New("Employee Task index exceeds 10,000 records")
	}
	if index.Tasks == nil {
		return errors.New("Employee Task index tasks must be an array")
	}
	seen := make(map[string]struct{}, len(index.Tasks))
	for itemIndex, item := range index.Tasks {
		if err := validateStoreID(item.ID); err != nil {
			return fmt.Errorf("Employee Task index contains invalid id: %w", err)
		}
		if item.EmployeeID != employeeID || item.EmployeeRevision < 1 ||
			item.CreatedAt.IsZero() || item.UpdatedAt.Before(item.CreatedAt) ||
			!validTaskDigest(item.SnapshotDigest) {
			return errors.New("Employee Task index contains invalid summary")
		}
		switch item.State {
		case employee.TaskQueued:
			if item.CancelledAt != nil || !item.UpdatedAt.Equal(item.CreatedAt) {
				return errors.New("Employee Task index contains invalid queued lifecycle")
			}
		case employee.TaskCancelled:
			if item.CancelledAt == nil || !item.CancelledAt.Equal(item.UpdatedAt) {
				return errors.New("Employee Task index contains invalid cancelled lifecycle")
			}
		default:
			return errors.New("Employee Task index contains unsupported state")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return errors.New("Employee Task index contains duplicate id")
		}
		seen[item.ID] = struct{}{}
		if itemIndex > 0 && !taskSummaryBefore(index.Tasks[itemIndex-1], item) {
			return errors.New("Employee Task index is not strictly sorted")
		}
	}
	return nil
}

func summarizeTask(task employee.EmployeeTask) TaskSummary {
	return TaskSummary{
		ID:               task.ID,
		EmployeeID:       task.EmployeeID,
		EmployeeRevision: task.EmployeeRevision,
		State:            task.State,
		CreatedAt:        task.CreatedAt,
		UpdatedAt:        task.UpdatedAt,
		CancelledAt:      cloneTaskTime(task.CancelledAt),
		SnapshotDigest:   task.SnapshotDigest,
	}
}

func sortTaskSummaries(items []TaskSummary) {
	sort.Slice(items, func(left, right int) bool {
		return taskSummaryBefore(items[left], items[right])
	})
}

func taskSummaryBefore(left, right TaskSummary) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		return left.ID > right.ID
	}
	return left.CreatedAt.After(right.CreatedAt)
}

func findTaskSummary(items []TaskSummary, taskID string) int {
	for index := range items {
		if items[index].ID == taskID {
			return index
		}
	}
	return -1
}

func taskActivity(task employee.EmployeeTask, kind ActivityType) ActivityEvent {
	return ActivityEvent{
		SchemaVersion:    ActivitySchemaVersion,
		EmployeeID:       task.EmployeeID,
		Type:             kind,
		Time:             task.UpdatedAt,
		EmployeeRevision: task.EmployeeRevision,
		TaskID:           task.ID,
	}
}

func normalizedTaskLimit(limit int) (int, error) {
	if limit == 0 {
		return 50, nil
	}
	if limit < 1 || limit > MaxTaskPageSize {
		return 0, fmt.Errorf("limit must be between 1 and %d", MaxTaskPageSize)
	}
	return limit, nil
}

func taskCursorKey(item TaskSummary) string {
	return item.CreatedAt.UTC().Format(time.RFC3339Nano) + "\x00" + item.ID
}

func encodeTaskCursor(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeTaskCursor(value string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 || !strings.ContainsRune(string(raw), '\x00') {
		return "", errors.New("invalid Employee Task cursor")
	}
	return string(raw), nil
}

func validTaskDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func cloneTaskTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var taskRandomRead = rand.Read

func newTaskID() (string, error) {
	var random [16]byte
	count, err := taskRandomRead(random[:])
	if err != nil {
		return "", fmt.Errorf("generate Employee Task id: %w", err)
	}
	if count != len(random) {
		return "", errors.New("generate Employee Task id: short random read")
	}
	return "task-" + hex.EncodeToString(random[:]), nil
}
