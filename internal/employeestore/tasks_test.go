package employeestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestTaskStoreMultipleQueuedStablePaginationFilterCancelAndReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	record := createTaskEmployee(t, store, "employee-a")
	var created []employee.EmployeeTask
	for index := 0; index < 4; index++ {
		draft := taskStoreDraft(t, record, fmt.Sprintf("Goal %d", index))
		task, err := store.CreateTask("employee-a", draft)
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, task)
	}
	first, err := store.ListTasks("employee-a", TaskListOptions{Limit: 2})
	if err != nil || len(first.Tasks) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Appended later")); err != nil {
		t.Fatal(err)
	}
	second, err := store.ListTasks("employee-a", TaskListOptions{Limit: 100, Cursor: first.NextCursor})
	if err != nil || len(second.Tasks) != 2 {
		t.Fatalf("stable second page = %#v, %v", second, err)
	}
	seen := map[string]struct{}{}
	for _, summary := range append(first.Tasks, second.Tasks...) {
		if _, duplicate := seen[summary.ID]; duplicate {
			t.Fatalf("duplicate Task across pages: %s", summary.ID)
		}
		seen[summary.ID] = struct{}{}
	}
	cancelled, err := store.CancelTask(created[1].ID)
	if err != nil || cancelled.State != employee.TaskCancelled {
		t.Fatalf("cancel = %#v, %v", cancelled, err)
	}
	if cancelled.SnapshotDigest != created[1].SnapshotDigest {
		t.Fatal("cancel rewrote immutable Task Snapshot Digest")
	}
	again, err := store.CancelTask(created[1].ID)
	if err != nil || !reflect.DeepEqual(again, cancelled) {
		t.Fatalf("idempotent cancel = %#v, %v", again, err)
	}
	cancelledPage, err := store.ListTasks("employee-a", TaskListOptions{State: employee.TaskCancelled})
	if err != nil || len(cancelledPage.Tasks) != 1 || cancelledPage.Tasks[0].ID != cancelled.ID {
		t.Fatalf("cancelled filter = %#v, %v", cancelledPage, err)
	}
	queuedPage, err := store.ListTasks("employee-a", TaskListOptions{State: employee.TaskQueued})
	if err != nil || len(queuedPage.Tasks) != 4 {
		t.Fatalf("queued filter = %#v, %v", queuedPage, err)
	}
	reopened, _ := NewStore(root)
	loaded, err := reopened.GetTask(cancelled.ID)
	if err != nil || !reflect.DeepEqual(loaded, cancelled) {
		t.Fatalf("reopened Task = %#v, %v", loaded, err)
	}
	if loaded.SnapshotDigest != created[1].SnapshotDigest {
		t.Fatal("Store reopen changed immutable Task Snapshot Digest")
	}
	reopenedPage, err := reopened.ListTasks("employee-a", TaskListOptions{State: employee.TaskCancelled})
	if err != nil || len(reopenedPage.Tasks) != 1 {
		t.Fatalf("reopened page = %#v, %v", reopenedPage, err)
	}
	for _, path := range []string{
		filepath.Join(root, "employee-a", "tasks", "index.json"),
		filepath.Join(root, "employee-a", "tasks", cancelled.ID+".json"),
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v", path, info.Mode().Perm(), err)
		}
	}
	activity, err := reopened.Activity("employee-a", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	createdEvents, cancelledEvents := 0, 0
	for _, event := range activity.Events {
		switch event.Type {
		case ActivityTaskCreated:
			createdEvents++
		case ActivityTaskCancelled:
			cancelledEvents++
		}
		if event.Type == ActivityTaskCreated || event.Type == ActivityTaskCancelled {
			if event.TaskID == "" || event.SubjectID != "" || event.SessionID != "" || event.RunID != "" {
				t.Fatalf("Task Activity leaked non-reference data: %#v", event)
			}
		}
	}
	if createdEvents != 5 || cancelledEvents != 1 {
		t.Fatalf("Task Activity counts created=%d cancelled=%d", createdEvents, cancelledEvents)
	}
}

func TestTaskStoreEmployeeLifecycleCreationGateAndHistoricalCancel(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "employees"))
	record := createTaskEmployee(t, store, "employee-a")
	task, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Historical Task"))
	if err != nil {
		t.Fatal(err)
	}
	record, err = store.Disable("employee-a", record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Disabled")); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled create error = %v", err)
	}
	if _, err := store.CancelTask(task.ID); err != nil {
		t.Fatalf("disabled historical cancel = %v", err)
	}
	record, err = store.Enable("employee-a", record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Enabled")); err != nil {
		t.Fatal(err)
	}
	record, err = store.Archive("employee-a", record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Archived")); !errors.Is(err, ErrConflict) {
		t.Fatalf("archived create error = %v", err)
	}
	if _, err := store.ListTasks("employee-a", TaskListOptions{}); err != nil {
		t.Fatalf("archived history list = %v", err)
	}
}

func TestTaskStoreConcurrentCreateHasNoDuplicateOrLostIndex(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "employees"))
	record := createTaskEmployee(t, store, "employee-a")
	const count = 48
	var wait sync.WaitGroup
	errorsByCall := make(chan error, count)
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, err := store.CreateTask("employee-a", taskStoreDraft(t, record, fmt.Sprintf("Concurrent %d", index)))
			errorsByCall <- err
		}(index)
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListTasks("employee-a", TaskListOptions{Limit: MaxTaskPageSize})
	if err != nil || len(page.Tasks) != count {
		t.Fatalf("concurrent page count=%d err=%v", len(page.Tasks), err)
	}
	seen := make(map[string]struct{}, count)
	for _, item := range page.Tasks {
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate generated Task ID %s", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
}

func TestTaskStoreIDCollisionFailsClosed(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "employees"))
	record := createTaskEmployee(t, store, "employee-a")
	original := taskRandomRead
	taskRandomRead = func(buffer []byte) (int, error) {
		for index := range buffer {
			buffer[index] = 0
		}
		return len(buffer), nil
	}
	t.Cleanup(func() { taskRandomRead = original })
	if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "First")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Collision")); !errors.Is(err, ErrConflict) {
		t.Fatalf("collision error = %v", err)
	}
}

func TestTaskStoreRandomIDFailureDoesNotPersist(t *testing.T) {
	for name, read := range map[string]func([]byte) (int, error){
		"error": func([]byte) (int, error) { return 0, errors.New("entropy unavailable") },
		"short": func(buffer []byte) (int, error) {
			buffer[0] = 1
			return 1, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := NewStore(filepath.Join(t.TempDir(), "employees"))
			record := createTaskEmployee(t, store, "employee-a")
			original := taskRandomRead
			taskRandomRead = read
			t.Cleanup(func() { taskRandomRead = original })
			if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "No entropy")); err == nil {
				t.Fatal("Task id generation failure was ignored")
			}
			page, err := store.ListTasks("employee-a", TaskListOptions{})
			if err != nil || len(page.Tasks) != 0 {
				t.Fatalf("failed id generation persisted Task: %#v, %v", page, err)
			}
		})
	}
}

func TestTaskStoreRejectsCorruptTaskAndIndex(t *testing.T) {
	taskMutations := map[string]func([]byte) []byte{
		"unknown field": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"run_id": ""`), []byte(`"run_id": "", "unknown": true`), 1)
		},
		"unknown schema": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"schema_version": 1`), []byte(`"schema_version": 99`), 1)
		},
		"damaged JSON": func([]byte) []byte { return []byte(`{`) },
		"invalid UTF-8": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte("Historical"), []byte{'H', 0xff}, 1)
		},
		"snapshot digest": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"snapshot_digest": "`), []byte(`"snapshot_digest": "b`), 1)
		},
		"identity": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"employee_id": "employee-a"`), []byte(`"employee_id": "employee-b"`), 1)
		},
		"phase6 session": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"session_id": ""`), []byte(`"session_id": "session-a"`), 1)
		},
	}
	for name, mutate := range taskMutations {
		t.Run("Task "+name, func(t *testing.T) {
			store, root, task := seedTaskStore(t)
			path := filepath.Join(root, "employee-a", "tasks", task.ID+".json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, mutate(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.GetTask(task.ID); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("corrupt Task error = %v", err)
			}
			if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("corrupt Task list error = %v", err)
			}
		})
	}

	indexMutations := map[string]func([]byte) []byte{
		"unknown field": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"tasks": [`), []byte(`"unknown": true, "tasks": [`), 1)
		},
		"unknown schema": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"schema_version": 1`), []byte(`"schema_version": 99`), 1)
		},
		"damaged JSON": func([]byte) []byte { return []byte(`{`) },
		"invalid UTF-8": func(raw []byte) []byte {
			return append(raw[:len(raw)-2], []byte{0xff, '\n'}...)
		},
		"identity": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"employee_id": "employee-a"`), []byte(`"employee_id": "employee-b"`), 1)
		},
	}
	for name, mutate := range indexMutations {
		t.Run("Index "+name, func(t *testing.T) {
			store, root, _ := seedTaskStore(t)
			path := filepath.Join(root, "employee-a", "tasks", "index.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, mutate(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("corrupt index error = %v", err)
			}
		})
	}
}

func TestTaskStoreRejectsUnsafePathsSymlinksAndNonRegularFiles(t *testing.T) {
	store, root, task := seedTaskStore(t)
	for _, id := range []string{"../outside", "/tmp/outside", "bad%2fid"} {
		if _, err := store.GetTask(id); err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("unsafe Task ID %q error = %v", id, err)
		}
	}
	if _, err := store.ListTasks("../outside", TaskListOptions{}); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("unsafe Employee ID error = %v", err)
	}

	outside := t.TempDir()
	marker := filepath.Join(outside, "marker")
	if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(root, "employee-a", "tasks", task.ID+".json")
	if err := os.Remove(taskPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(marker, taskPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.GetTask(task.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Task symlink error = %v", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "outside" {
		t.Fatalf("outside marker changed: %q, %v", raw, err)
	}

	store, root, _ = seedTaskStore(t)
	indexPath := filepath.Join(root, "employee-a", "tasks", "index.json")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("non-regular index error = %v", err)
	}
}

func TestTaskStoreRejectsTaskDirectoryAndIndexSymlinkWithoutOutsideWrites(t *testing.T) {
	t.Run("tasks directory", func(t *testing.T) {
		store, root, _ := seedTaskStore(t)
		record, err := store.Get("employee-a")
		if err != nil {
			t.Fatal(err)
		}
		tasksPath := filepath.Join(root, "employee-a", "tasks")
		if err := os.RemoveAll(tasksPath); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, tasksPath); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("tasks directory symlink list error = %v", err)
		}
		if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Must not escape")); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("tasks directory symlink create error = %v", err)
		}
		entries, err := os.ReadDir(outside)
		if err != nil || len(entries) != 0 {
			t.Fatalf("outside Task directory changed: %v, %v", entries, err)
		}
	})

	t.Run("index file", func(t *testing.T) {
		store, root, _ := seedTaskStore(t)
		indexPath := filepath.Join(root, "employee-a", "tasks", "index.json")
		raw, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside-index.json")
		if err := os.WriteFile(outside, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(indexPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, indexPath); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Task index symlink error = %v", err)
		}
		before, _ := os.ReadFile(outside)
		record, _ := store.Get("employee-a")
		if _, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Must not overwrite")); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Task index symlink create error = %v", err)
		}
		after, _ := os.ReadFile(outside)
		if !bytes.Equal(before, after) {
			t.Fatal("outside Task index was modified")
		}
	})

	t.Run("employee directory", func(t *testing.T) {
		store, root, task := seedTaskStore(t)
		employeePath := filepath.Join(root, "employee-a")
		outside := filepath.Join(t.TempDir(), "outside-employee")
		if err := os.Rename(employeePath, outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, employeePath); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := store.GetTask(task.ID); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Employee directory symlink get error = %v", err)
		}
		if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Employee directory symlink list error = %v", err)
		}
	})
}

func TestTaskStoreRejectsOversizedTaskFile(t *testing.T) {
	store, root, task := seedTaskStore(t)
	path := filepath.Join(root, "employee-a", "tasks", task.ID+".json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), employee.MaxTaskFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTask(task.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("oversized Task error = %v", err)
	}
	if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("oversized Task list error = %v", err)
	}
}

func TestTaskStoreRejectsUnindexedFilesAndRecordLimit(t *testing.T) {
	store, root, task := seedTaskStore(t)
	indexPath := filepath.Join(root, "employee-a", "tasks", "index.json")
	var index taskIndexFile
	readJSONFile(t, indexPath, &index)
	index.Tasks = nil
	writeJSONFile(t, indexPath, index)
	if _, err := store.ListTasks("employee-a", TaskListOptions{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unindexed Task %s error = %v", task.ID, err)
	}
	items := make([]TaskSummary, MaxTasksPerEmployee+1)
	now := time.Now().UTC()
	for index := range items {
		items[index] = TaskSummary{
			ID:         fmt.Sprintf("task-%032x", MaxTasksPerEmployee-index),
			EmployeeID: "employee-a", EmployeeRevision: 1, State: employee.TaskQueued,
			CreatedAt:      now.Add(-time.Duration(index) * time.Second),
			UpdatedAt:      now.Add(-time.Duration(index) * time.Second),
			SnapshotDigest: strings.Repeat("a", 64),
		}
	}
	if err := validateTaskIndex(taskIndexFile{
		SchemaVersion: TaskIndexSchemaVersion, EmployeeID: "employee-a", Tasks: items,
	}, "employee-a"); err == nil {
		t.Fatal("Task index over 10,000 records was accepted")
	}
}

func TestTaskStorePaginationRejectsInvalidLimitFilterAndCursor(t *testing.T) {
	store, _, _ := seedTaskStore(t)
	for name, options := range map[string]TaskListOptions{
		"limit":  {Limit: MaxTaskPageSize + 1},
		"state":  {State: employee.TaskState("running")},
		"cursor": {Cursor: "%%%"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.ListTasks("employee-a", options); err == nil {
				t.Fatal("invalid pagination input was accepted")
			}
		})
	}
}

func seedTaskStore(t *testing.T) (*Store, string, employee.EmployeeTask) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := NewStore(root)
	record := createTaskEmployee(t, store, "employee-a")
	task, err := store.CreateTask("employee-a", taskStoreDraft(t, record, "Historical Task"))
	if err != nil {
		t.Fatal(err)
	}
	return store, root, task
}

func createTaskEmployee(t *testing.T, store *Store, id string) Record {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Create(testDraft(id), []employee.ProjectBinding{{
		ID: "project-" + id, Label: "Workspace", WorkspaceRealPath: workspace,
		ReadAllowed: true, MutationAllowed: true, AllowedToolCapabilities: []string{"read"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func taskStoreDraft(t *testing.T, record Record, prompt string) employee.EmployeeTask {
	t.Helper()
	snapshot, err := employee.NewRevisionSnapshot(record.Employee, record.ProjectBindings)
	if err != nil {
		t.Fatal(err)
	}
	return employee.EmployeeTask{
		EmployeeID: record.Employee.ID, EmployeeRevision: record.Employee.Revision,
		Prompt: prompt, EmployeeSnapshot: snapshot,
		ProjectBinding: record.ProjectBindings[0],
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget: employee.BudgetPolicy{
				MaxModelCalls: 2, MaxTokens: 20000, TimeoutSeconds: 900,
			},
		},
	}
}

func TestTaskFileLimitIsEnforcedByStore(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "employees"))
	record := createTaskEmployee(t, store, "employee-a")
	draft := taskStoreDraft(t, record, "bounded")
	draft.Prompt = strings.Repeat("x", employee.MaxTaskPromptBytes)
	created, err := store.CreateTask("employee-a", draft)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(created, "", "  ")
	if err != nil || len(raw) > employee.MaxTaskFileBytes {
		t.Fatalf("Task file size = %d, %v", len(raw), err)
	}
}
