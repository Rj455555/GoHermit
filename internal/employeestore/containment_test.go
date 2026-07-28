package employeestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexRejectsIllegalSummaryIDsAsCorrupt(t *testing.T) {
	for _, id := range []string{"../outside", filepath.Join(t.TempDir(), "outside"), "..%2foutside"} {
		t.Run(id, func(t *testing.T) {
			root := t.TempDir()
			store, _ := NewStore(root)
			record := createRecord(t, store, "employee-a")
			indexPath := filepath.Join(root, "index.json")
			var index map[string]any
			if err := json.Unmarshal(mustRead(t, indexPath), &index); err != nil {
				t.Fatal(err)
			}
			index["employees"].([]any)[0].(map[string]any)["id"] = id
			writeJSONTest(t, indexPath, index)
			if _, err := store.List(ListOptions{}); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("List error = %v", err)
			}
			if _, err := store.Get(record.Employee.ID); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Get error = %v", err)
			}
		})
	}
}

func TestStoreOperationsRejectIllegalCallerIDsAsInput(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	for _, id := range []string{"../outside", filepath.Join(t.TempDir(), "outside"), "..%2foutside"} {
		t.Run(id, func(t *testing.T) {
			assertInputError := func(name string, err error) {
				t.Helper()
				if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrCorrupt) {
					t.Fatalf("%s error = %v", name, err)
				}
			}
			_, err := store.Get(id)
			assertInputError("Get", err)
			_, err = store.Update(id, 1, testDraft(id), nil)
			assertInputError("Update", err)
			_, err = store.Disable(id, 1)
			assertInputError("Disable", err)
			_, err = store.Enable(id, 1)
			assertInputError("Enable", err)
			_, err = store.Archive(id, 1)
			assertInputError("Archive", err)
			_, err = store.Activity(id, ListOptions{})
			assertInputError("Activity", err)
			err = store.RecordActivity(ActivityEvent{EmployeeID: id, Type: ActivitySkillBinding, SubjectID: "skill"})
			assertInputError("RecordActivity", err)
			_, err = store.LoadRevision(id, 1)
			assertInputError("LoadRevision", err)
		})
	}
}

func TestStoreReadsAndLifecycleRejectSymlinkEscapes(t *testing.T) {
	attacks := []struct {
		name  string
		apply func(*testing.T, string, Record) (string, []byte)
	}{
		{"index file", symlinkIndexOutside},
		{"employee directory", symlinkEmployeeDirectoryOutside},
		{"employee file", symlinkEmployeeFileOutside},
		{"projects file", symlinkProjectsFileOutside},
		{"activity directory", symlinkActivityDirectoryOutside},
		{"activity file", symlinkActivityFileOutside},
		{"revisions directory", symlinkRevisionsDirectoryOutside},
		{"revision file", symlinkRevisionFileOutside},
	}
	for _, attack := range attacks {
		t.Run(attack.name, func(t *testing.T) {
			root := t.TempDir()
			store, _ := NewStore(root)
			record := createRecord(t, store, "employee-a")
			tracked, before := attack.apply(t, root, record)

			if _, err := store.Get(record.Employee.ID); err == nil {
				t.Fatal("Get must fail closed")
			}
			if _, err := store.List(ListOptions{}); err == nil {
				t.Fatal("List must fail closed")
			}
			if _, err := store.Activity(record.Employee.ID, ListOptions{}); err == nil {
				t.Fatal("Activity must fail closed")
			}
			if _, err := store.Disable(record.Employee.ID, record.Employee.Revision); err == nil {
				t.Fatal("lifecycle mutation must fail closed")
			}
			if after := mustRead(t, tracked); !bytes.Equal(after, before) {
				t.Fatal("outside target was modified")
			}
		})
	}
}

func TestStoreRejectsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	record := createRecord(t, store, "employee-a")
	path := filepath.Join(root, record.Employee.ID, "employee.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(record.Employee.ID); err == nil {
		t.Fatal("directory in place of employee.json must fail")
	}
	if _, err := store.List(ListOptions{}); err == nil {
		t.Fatal("List must reject non-regular employee.json")
	}
}

func TestCreateDoesNotWriteThroughSymlinkEmployeeDirectory(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(root)
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	requireSymlink(t, outside, filepath.Join(root, "employee-new"))
	if _, err := store.Create(testDraft("employee-new"), nil); err == nil {
		t.Fatal("Create must reject symlink employee directory")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || string(mustRead(t, sentinel)) != "unchanged" {
		t.Fatal("Create wrote outside the Store")
	}
}

func symlinkIndexOutside(t *testing.T, root string, _ Record) (string, []byte) {
	t.Helper()
	path := filepath.Join(root, "index.json")
	return replaceFileWithOutsideSymlink(t, path)
}

func symlinkEmployeeDirectoryOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	path := filepath.Join(root, record.Employee.ID)
	outside := filepath.Join(t.TempDir(), "employee")
	if err := os.Rename(path, outside); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(outside, "employee.json")
	before := mustRead(t, tracked)
	requireSymlink(t, outside, path)
	return tracked, before
}

func symlinkEmployeeFileOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	return replaceFileWithOutsideSymlink(t, filepath.Join(root, record.Employee.ID, "employee.json"))
}

func symlinkProjectsFileOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	return replaceFileWithOutsideSymlink(t, filepath.Join(root, record.Employee.ID, "projects.json"))
}

func symlinkActivityDirectoryOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	path := filepath.Join(root, record.Employee.ID, "activity")
	outside := filepath.Join(t.TempDir(), "activity")
	if err := os.Rename(path, outside); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(outside, "events.jsonl")
	before := mustRead(t, tracked)
	requireSymlink(t, outside, path)
	return tracked, before
}

func symlinkActivityFileOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	return replaceFileWithOutsideSymlink(t, filepath.Join(root, record.Employee.ID, "activity", "events.jsonl"))
}

func symlinkRevisionsDirectoryOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	path := filepath.Join(root, record.Employee.ID, "revisions")
	outside := filepath.Join(t.TempDir(), "revisions")
	if err := os.Rename(path, outside); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(outside, "1.json")
	before := mustRead(t, tracked)
	requireSymlink(t, outside, path)
	return tracked, before
}

func symlinkRevisionFileOutside(t *testing.T, root string, record Record) (string, []byte) {
	t.Helper()
	return replaceFileWithOutsideSymlink(t, revisionPath(root, record.Employee.ID, 1))
}

func replaceFileWithOutsideSymlink(t *testing.T, path string) (string, []byte) {
	t.Helper()
	before := mustRead(t, path)
	outside := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.WriteFile(outside, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	requireSymlink(t, outside, path)
	return outside, before
}

func requireSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(newname), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
}
