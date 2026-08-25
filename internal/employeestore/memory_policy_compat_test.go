package employeestore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const compatEmployeeID = "employee-a"

// These fixtures are byte-for-byte snapshots produced from a5723eef's P0.1
// domain/store contract. The tests intentionally read fixed documents rather
// than regenerating expected JSON or digests with the code under test.
const legacyEmployeeJSON = `{
  "id": "employee-a",
  "schema_version": 1,
  "revision": 1,
  "state": "active",
  "name": "Test Employee",
  "avatar": {
    "kind": "initials",
    "value": "TE"
  },
  "job_title": "Engineer",
  "charter": "Build bounded systems.",
  "default_selection": {
    "company": "openai",
    "access": "openai-api",
    "model": "gpt-5.4-mini"
  },
  "agent_profile": "coding",
  "permission_policy": {
    "allowed_capabilities": [
      "read"
    ],
    "network_allowed": false
  },
  "budget_policy": {
    "max_model_calls": 8,
    "max_tokens": 100000,
    "timeout_seconds": 3600
  },
  "concurrency_policy": {
    "max_running_tasks": 1
  },
  "memory_policy": {
    "candidate_generation": false,
    "promotion": "owner_confirmation",
    "max_context_facts": 0,
    "max_context_bytes": 0
  },
  "created_at": "2026-07-28T00:00:00Z",
  "updated_at": "2026-07-28T00:00:00Z"
}`

const legacyRevisionJSON = `{
  "schema_version": 1,
  "employee_id": "employee-a",
  "revision": 1,
  "captured_at": "2026-07-28T00:00:00Z",
  "employee": {
    "id": "employee-a",
    "schema_version": 1,
    "revision": 1,
    "state": "active",
    "name": "Test Employee",
    "avatar": {
      "kind": "initials",
      "value": "TE"
    },
    "job_title": "Engineer",
    "charter": "Build bounded systems.",
    "default_selection": {
      "company": "openai",
      "access": "openai-api",
      "model": "gpt-5.4-mini"
    },
    "agent_profile": "coding",
    "permission_policy": {
      "allowed_capabilities": [
        "read"
      ],
      "network_allowed": false
    },
    "budget_policy": {
      "max_model_calls": 8,
      "max_tokens": 100000,
      "timeout_seconds": 3600
    },
    "concurrency_policy": {
      "max_running_tasks": 1
    },
    "memory_policy": {
      "candidate_generation": false,
      "promotion": "owner_confirmation",
      "max_context_facts": 0,
      "max_context_bytes": 0
    },
    "created_at": "2026-07-28T00:00:00Z",
    "updated_at": "2026-07-28T00:00:00Z"
  },
  "digest": "3c3b120d2dca624339d2a95c01e9f5c42f7ea22b3ba6703b00919edf1def130b"
}`

const legacyRevisionDigest = "3c3b120d2dca624339d2a95c01e9f5c42f7ea22b3ba6703b00919edf1def130b"

const trueEmployeeJSON = `{
  "id": "employee-a",
  "schema_version": 1,
  "revision": 1,
  "state": "active",
  "name": "Test Employee",
  "avatar": {
    "kind": "initials",
    "value": "TE"
  },
  "job_title": "Engineer",
  "charter": "Build bounded systems.",
  "default_selection": {
    "company": "openai",
    "access": "openai-api",
    "model": "gpt-5.4-mini"
  },
  "agent_profile": "coding",
  "permission_policy": {
    "allowed_capabilities": [
      "read"
    ],
    "network_allowed": false
  },
  "budget_policy": {
    "max_model_calls": 8,
    "max_tokens": 100000,
    "timeout_seconds": 3600
  },
  "concurrency_policy": {
    "max_running_tasks": 1
  },
  "memory_policy": {
    "candidate_generation": false,
    "promotion": "owner_confirmation",
    "automatic_recall": true,
    "max_context_facts": 0,
    "max_context_bytes": 0
  },
  "created_at": "2026-07-28T00:00:00Z",
  "updated_at": "2026-07-28T00:00:00Z"
}`

const trueRevisionJSON = `{
  "schema_version": 1,
  "employee_id": "employee-a",
  "revision": 1,
  "captured_at": "2026-07-28T00:00:00Z",
  "employee": {
    "id": "employee-a",
    "schema_version": 1,
    "revision": 1,
    "state": "active",
    "name": "Test Employee",
    "avatar": {
      "kind": "initials",
      "value": "TE"
    },
    "job_title": "Engineer",
    "charter": "Build bounded systems.",
    "default_selection": {
      "company": "openai",
      "access": "openai-api",
      "model": "gpt-5.4-mini"
    },
    "agent_profile": "coding",
    "permission_policy": {
      "allowed_capabilities": [
        "read"
      ],
      "network_allowed": false
    },
    "budget_policy": {
      "max_model_calls": 8,
      "max_tokens": 100000,
      "timeout_seconds": 3600
    },
    "concurrency_policy": {
      "max_running_tasks": 1
    },
    "memory_policy": {
      "candidate_generation": false,
      "promotion": "owner_confirmation",
      "automatic_recall": true,
      "max_context_facts": 0,
      "max_context_bytes": 0
    },
    "created_at": "2026-07-28T00:00:00Z",
    "updated_at": "2026-07-28T00:00:00Z"
  },
  "digest": "65b9920118574d6738bb7ad1d57604a49def7ee77e31349630e6688404d48e9a"
}`

const trueRevisionDigest = "65b9920118574d6738bb7ad1d57604a49def7ee77e31349630e6688404d48e9a"

func TestCompatibilityFixtureReadsLegacyEmployeeAndRevisionWithoutDigestChange(t *testing.T) {
	store := writeCompatibilityFixture(t, legacyEmployeeJSON, legacyRevisionJSON)
	record, err := store.Get(compatEmployeeID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Employee.MemoryPolicy.AutomaticRecall {
		t.Fatal("legacy Employee unexpectedly enabled automatic recall")
	}
	snapshot, err := store.LoadRevision(compatEmployeeID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Digest != legacyRevisionDigest || !snapshot.VerifyDigest() {
		t.Fatalf("legacy digest = %q, valid=%t", snapshot.Digest, snapshot.VerifyDigest())
	}
	assertCanonicalJSON(t, legacyEmployeeJSON, record.Employee)
	assertCanonicalJSON(t, legacyRevisionJSON, snapshot)
}

func TestCompatibilityFixtureReadsTrueAndRevisePreservesAutomaticRecall(t *testing.T) {
	store := writeCompatibilityFixture(t, trueEmployeeJSON, trueRevisionJSON)
	record, err := store.Get(compatEmployeeID)
	if err != nil {
		t.Fatal(err)
	}
	if !record.Employee.MemoryPolicy.AutomaticRecall {
		t.Fatal("automatic_recall=true was not read from Employee JSON")
	}
	snapshot, err := store.LoadRevision(compatEmployeeID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Employee.MemoryPolicy.AutomaticRecall || snapshot.Digest != trueRevisionDigest || !snapshot.VerifyDigest() {
		t.Fatalf("true compatibility snapshot = %#v", snapshot)
	}

	proposed := record.Employee
	proposed.Name = "Revised Employee"
	updated, err := store.Update(compatEmployeeID, record.Employee.Revision, proposed, record.ProjectBindings)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Employee.MemoryPolicy.AutomaticRecall {
		t.Fatal("Revise dropped automatic_recall=true")
	}
	revisedSnapshot, err := store.LoadRevision(compatEmployeeID, updated.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !revisedSnapshot.Employee.MemoryPolicy.AutomaticRecall || !revisedSnapshot.VerifyDigest() {
		t.Fatalf("revised compatibility snapshot = %#v", revisedSnapshot)
	}
}

func TestCompatibilityFalseOmitsFieldWhenMarshalled(t *testing.T) {
	store := writeCompatibilityFixture(t, legacyEmployeeJSON, legacyRevisionJSON)
	record, err := store.Get(compatEmployeeID)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, legacyEmployeeJSON, record.Employee)
	if strings.Contains(marshalCanonical(t, record.Employee), `"automatic_recall"`) {
		t.Fatal("automatic_recall=false was serialized instead of omitted")
	}
}

func writeCompatibilityFixture(t *testing.T, employeeJSON, revisionJSON string) *Store {
	t.Helper()
	root := t.TempDir()
	employeeRoot := filepath.Join(root, compatEmployeeID)
	for _, path := range []string{
		filepath.Join(employeeRoot, "activity"), filepath.Join(employeeRoot, "revisions"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"index.json": `{"schema_version":1,"employees":[{"id":"employee-a","revision":1,"state":"active","name":"Test Employee","job_title":"Engineer","agent_profile":"coding","project_count":0,"created_at":"2026-07-28T00:00:00Z","updated_at":"2026-07-28T00:00:00Z"}]}`,
		filepath.Join(compatEmployeeID, "employee.json"):            employeeJSON,
		filepath.Join(compatEmployeeID, "projects.json"):            `{"schema_version":1,"bindings":[]}`,
		filepath.Join(compatEmployeeID, "activity", "events.jsonl"): "",
		filepath.Join(compatEmployeeID, "revisions", "1.json"):      revisionJSON,
	}
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func assertCanonicalJSON(t *testing.T, want string, value any) {
	t.Helper()
	if got := marshalCanonical(t, value); got != strings.TrimSpace(want) {
		t.Fatalf("canonical JSON mismatch:\n got %s\nwant %s", got, strings.TrimSpace(want))
	}
}

func marshalCanonical(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
