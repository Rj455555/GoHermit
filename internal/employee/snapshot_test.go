package employee

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRevisionSnapshotIsCompleteImmutableAndDigestVerified(t *testing.T) {
	now := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	draft := validEmployeeDraft()
	draft.SkillBindings[0].Configuration = json.RawMessage(`{"mode":"strict"}`)
	current, err := Create(draft, now)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := CreateProjectBinding(validProjectBinding(), now)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := NewRevisionSnapshot(current, []ProjectBinding{binding})
	if err != nil {
		t.Fatalf("NewRevisionSnapshot() error = %v", err)
	}
	if snapshot.SchemaVersion != SnapshotSchemaVersion || snapshot.EmployeeID != current.ID || snapshot.Revision != current.Revision {
		t.Fatalf("NewRevisionSnapshot() identity = %#v", snapshot)
	}
	if len(snapshot.Digest) != 64 || !snapshot.VerifyDigest() {
		t.Fatalf("NewRevisionSnapshot() digest = %q", snapshot.Digest)
	}
	if snapshot.CapturedAt != current.UpdatedAt {
		t.Fatalf("NewRevisionSnapshot() captured_at = %v", snapshot.CapturedAt)
	}

	current.Responsibilities[0] = "mutated source"
	current.PermissionPolicy.AllowedCapabilities[0] = "mutated_capability"
	current.SkillBindings[0].Configuration[2] = 'X'
	binding.AllowedToolCapabilities[0] = "mutated_tool"
	binding.BudgetOverride.MaxTokens = 1
	if snapshot.Employee.Responsibilities[0] == "mutated source" ||
		snapshot.Employee.PermissionPolicy.AllowedCapabilities[0] == "mutated_capability" ||
		strings.Contains(string(snapshot.Employee.SkillBindings[0].Configuration), "X") ||
		snapshot.ProjectBindings[0].AllowedToolCapabilities[0] == "mutated_tool" ||
		snapshot.ProjectBindings[0].BudgetOverride.MaxTokens == 1 {
		t.Fatalf("snapshot aliases mutable source: %#v", snapshot)
	}

	clone := snapshot.Clone()
	clone.Employee.Responsibilities[0] = "mutated clone"
	clone.ProjectBindings[0].AllowedToolCapabilities[0] = "mutated clone"
	if snapshot.Employee.Responsibilities[0] == "mutated clone" ||
		snapshot.ProjectBindings[0].AllowedToolCapabilities[0] == "mutated clone" {
		t.Fatal("Clone() aliases the snapshot")
	}
	if clone.VerifyDigest() {
		t.Fatal("VerifyDigest() accepted mutated clone")
	}
}

func TestRevisionSnapshotDigestIsDeterministic(t *testing.T) {
	now := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	current, _ := Create(validEmployeeDraft(), now)
	binding, _ := CreateProjectBinding(validProjectBinding(), now)
	first, err := NewRevisionSnapshot(current, []ProjectBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRevisionSnapshot(current, []ProjectBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("snapshot digests differ: %q != %q", first.Digest, second.Digest)
	}
}

func TestRevisionSnapshotRequiresExactEmployeeProjectBindings(t *testing.T) {
	now := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	current, _ := Create(validEmployeeDraft(), now)

	tests := []struct {
		name     string
		bindings []ProjectBinding
	}{
		{"missing", nil},
		{"foreign employee", []ProjectBinding{func() ProjectBinding {
			b := validProjectBinding()
			b.EmployeeID = "employee-other"
			b, _ = CreateProjectBinding(b, now)
			return b
		}()}},
		{"extra", []ProjectBinding{func() ProjectBinding {
			b, _ := CreateProjectBinding(validProjectBinding(), now)
			extra := b
			extra.ID = "project-extra"
			extra.WorkspaceFingerprint = ""
			extra, _ = CreateProjectBinding(extra, now)
			return extra
		}()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bindings := tt.bindings
			if tt.name == "extra" {
				main, _ := CreateProjectBinding(validProjectBinding(), now)
				bindings = append([]ProjectBinding{main}, bindings...)
			}
			if _, err := NewRevisionSnapshot(current, bindings); err == nil {
				t.Fatal("NewRevisionSnapshot() error = nil")
			}
		})
	}
}

func TestRevisionSnapshotRejectsOversizeDocument(t *testing.T) {
	now := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	draft := validEmployeeDraft()
	draft.ProjectBindingIDs = make([]string, MaxProjectBindings)
	bindings := make([]ProjectBinding, MaxProjectBindings)
	for i := 0; i < MaxProjectBindings; i++ {
		id := "project-" + strings.Repeat("x", 100) + strconv.Itoa(i)
		draft.ProjectBindingIDs[i] = id
		binding := validProjectBinding()
		binding.ID = id
		binding.Label = strings.Repeat("L", MaxTextBytes)
		suffix := strconv.Itoa(i)
		binding.WorkspaceRealPath = "/" + strings.Repeat(string(rune('a'+i%26)), MaxWorkspacePathBytes-len(suffix)-1) + suffix
		bindings[i], _ = CreateProjectBinding(binding, now)
	}
	current, err := Create(draft, now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err = NewRevisionSnapshot(current, bindings); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("NewRevisionSnapshot() error = %v", err)
	}
}
