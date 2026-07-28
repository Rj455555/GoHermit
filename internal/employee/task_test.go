package employee

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewEmployeeTaskSealsDeepImmutableQueuedSnapshot(t *testing.T) {
	now := time.Now().UTC()
	draft := validTaskDraft(t, now)
	task, err := NewTask(draft, now)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != TaskQueued || task.SchemaVersion != TaskSchemaVersion ||
		task.SessionID != "" || task.RunID != "" || task.CancelledAt != nil {
		t.Fatalf("unexpected queued Task: %#v", task)
	}
	if !task.VerifySnapshotDigest() {
		t.Fatal("new Task Snapshot Digest is invalid")
	}
	originalDigest := task.SnapshotDigest
	draft.EmployeeSnapshot.Employee.Name = "mutated"
	draft.Skills[0].Configuration[0] = 'x'
	draft.Knowledge[0].Citations[0].Path = "mutated"
	draft.MemoryFacts[0].FactID = "mutated"
	draft.ProjectBinding.Label = "mutated"
	draft.Policy.AllowedCapabilities[0] = "write"
	if task.EmployeeSnapshot.Employee.Name == "mutated" ||
		task.Skills[0].Configuration[0] == 'x' ||
		task.Knowledge[0].Citations[0].Path == "mutated" ||
		task.MemoryFacts[0].FactID == "mutated" ||
		task.ProjectBinding.Label == "mutated" ||
		task.Policy.AllowedCapabilities[0] == "write" ||
		task.SnapshotDigest != originalDigest {
		t.Fatal("Task did not deep-copy its immutable snapshot")
	}
}

func TestEmployeeTaskCancelIsIdempotentTerminalAndPreservesSnapshot(t *testing.T) {
	now := time.Now().UTC()
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	before := task.Clone()
	cancelled, err := CancelTask(task, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != TaskCancelled || cancelled.CancelledAt == nil ||
		!cancelled.CancelledAt.Equal(now.Add(time.Second)) {
		t.Fatalf("cancelled Task = %#v", cancelled)
	}
	if cancelled.SnapshotDigest != before.SnapshotDigest ||
		!reflect.DeepEqual(cancelled.EmployeeSnapshot, before.EmployeeSnapshot) ||
		!reflect.DeepEqual(cancelled.Skills, before.Skills) ||
		!reflect.DeepEqual(cancelled.Knowledge, before.Knowledge) ||
		!reflect.DeepEqual(cancelled.MemoryFacts, before.MemoryFacts) ||
		!reflect.DeepEqual(cancelled.ProjectBinding, before.ProjectBinding) ||
		!reflect.DeepEqual(cancelled.Policy, before.Policy) {
		t.Fatal("cancel changed immutable Task Snapshot")
	}
	again, err := CancelTask(cancelled, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, cancelled) {
		t.Fatal("repeated cancel is not idempotent")
	}
}

func TestEmployeeTaskJSONRoundTripPreservesSnapshotDigest(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 123456789, time.UTC)
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var roundTripped EmployeeTask
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatal(err)
	}
	expected, err := taskSnapshotDigest(roundTripped)
	if err != nil {
		t.Fatal(err)
	}
	if expected != roundTripped.SnapshotDigest {
		t.Fatalf("round-trip digest got %s want %s", roundTripped.SnapshotDigest, expected)
	}
	if _, err := CancelTask(roundTripped, roundTripped.UpdatedAt.Add(time.Second)); err != nil {
		t.Fatalf("cancel round-tripped Task: %v", err)
	}
}

func TestEmployeeTaskSnapshotDigestExcludesLifecycleAndExecutionProjection(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	base, err := taskSnapshotDigest(task)
	if err != nil {
		t.Fatal(err)
	}
	if base != task.SnapshotDigest {
		t.Fatalf("sealed digest got %s want %s", task.SnapshotDigest, base)
	}

	lifecycle := task.Clone()
	cancelledAt := now.Add(time.Minute)
	lifecycle.State = TaskCancelled
	lifecycle.UpdatedAt = cancelledAt
	lifecycle.CancelledAt = &cancelledAt
	assertTaskSnapshotDigest(t, lifecycle, base)

	executionProjection := task.Clone()
	executionProjection.SessionID = "session-phase6"
	executionProjection.RunID = "run-phase7"
	assertTaskSnapshotDigest(t, executionProjection, base)
}

func TestEmployeeTaskSnapshotDigestCoversEveryImmutableSelection(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	base := task.SnapshotDigest
	for name, mutate := range map[string]func(*EmployeeTask){
		"prompt":            func(value *EmployeeTask) { value.Prompt += " changed" },
		"employee snapshot": func(value *EmployeeTask) { value.EmployeeSnapshot.Employee.Name = "Changed" },
		"skill":             func(value *EmployeeTask) { value.Skills[0].Enabled = false },
		"knowledge":         func(value *EmployeeTask) { value.Knowledge[0].Citations[0].StartLine++ },
		"memory":            func(value *EmployeeTask) { value.MemoryFacts[0].FactID = "mem-b" },
		"project binding":   func(value *EmployeeTask) { value.ProjectBinding.Label = "Changed" },
		"task policy":       func(value *EmployeeTask) { value.Policy.Budget.MaxTokens-- },
	} {
		t.Run(name, func(t *testing.T) {
			changed := task.Clone()
			mutate(&changed)
			digest, err := taskSnapshotDigest(changed)
			if err != nil {
				t.Fatal(err)
			}
			if digest == base {
				t.Fatalf("%s did not change immutable Task Snapshot Digest", name)
			}
		})
	}
}

func TestValidateEmployeeTaskRejectsFutureExecutionBindings(t *testing.T) {
	now := time.Now().UTC()
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*EmployeeTask){
		"session": func(value *EmployeeTask) { value.SessionID = "session-phase6" },
		"run":     func(value *EmployeeTask) { value.RunID = "run-phase7" },
	} {
		t.Run(name, func(t *testing.T) {
			bound := task.Clone()
			mutate(&bound)
			if err := ValidateTask(bound); err == nil {
				t.Fatalf("Phase 5 accepted %s binding", name)
			}
		})
	}
}

func assertTaskSnapshotDigest(t *testing.T, task EmployeeTask, want string) {
	t.Helper()
	got, err := taskSnapshotDigest(task)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Task Snapshot Digest got %s want %s", got, want)
	}
}

func TestEmployeeTaskRejectsUnsafePromptAndPhase6State(t *testing.T) {
	now := time.Now().UTC()
	for name, mutate := range map[string]func(*EmployeeTask){
		"empty prompt":      func(value *EmployeeTask) { value.Prompt = "   " },
		"oversized prompt":  func(value *EmployeeTask) { value.Prompt = strings.Repeat("x", MaxTaskPromptBytes+1) },
		"NUL prompt":        func(value *EmployeeTask) { value.Prompt = "bad\x00prompt" },
		"invalid UTF-8":     func(value *EmployeeTask) { value.Prompt = string([]byte{0xff}) },
		"replacement rune":  func(value *EmployeeTask) { value.Prompt = "bad\uFFFDprompt" },
		"secret":            func(value *EmployeeTask) { value.Prompt = "authorization: bearer hidden-value" },
		"private reasoning": func(value *EmployeeTask) { value.Prompt = "private reasoning: hidden" },
		"raw tool data":     func(value *EmployeeTask) { value.Prompt = "raw tool arguments: hidden" },
		"session binding":   func(value *EmployeeTask) { value.SessionID = "session-phase6" },
		"run binding":       func(value *EmployeeTask) { value.RunID = "run-phase7" },
		"unknown state":     func(value *EmployeeTask) { value.State = TaskState("prepared") },
	} {
		t.Run(name, func(t *testing.T) {
			draft := validTaskDraft(t, now)
			mutate(&draft)
			if _, err := NewTask(draft, now); err == nil {
				t.Fatal("unsafe Task was accepted")
			}
		})
	}
}

func TestEmployeeTaskDetectsSnapshotAndSelectionTampering(t *testing.T) {
	now := time.Now().UTC()
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*EmployeeTask){
		"employee snapshot": func(value *EmployeeTask) { value.EmployeeSnapshot.Employee.Name = "tampered" },
		"skill digest":      func(value *EmployeeTask) { value.Skills[0].Digest = strings.Repeat("b", 64) },
		"knowledge digest":  func(value *EmployeeTask) { value.Knowledge[0].SourceDigest = strings.Repeat("e", 64) },
		"citation":          func(value *EmployeeTask) { value.Knowledge[0].Citations[0].StartLine++ },
		"memory":            func(value *EmployeeTask) { value.MemoryFacts[0].Digest = strings.Repeat("b", 64) },
		"project":           func(value *EmployeeTask) { value.ProjectBinding.Label = "tampered" },
		"policy":            func(value *EmployeeTask) { value.Policy.NetworkAllowed = true },
		"digest":            func(value *EmployeeTask) { value.SnapshotDigest = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			value := task.Clone()
			mutate(&value)
			if err := ValidateTask(value); err == nil {
				t.Fatal("tampered Task was accepted")
			}
		})
	}
}

func TestEmployeeTaskRejectsOversizedFile(t *testing.T) {
	now := time.Now().UTC()
	draft := validTaskDraft(t, now)
	large := json.RawMessage(`"` + strings.Repeat("x", MaxTaskFileBytes) + `"`)
	draft.Skills[0].Configuration = large
	if _, err := NewTask(draft, now); err == nil {
		t.Fatal("oversized Task file was accepted")
	}
}

func validTaskDraft(t *testing.T, now time.Time) EmployeeTask {
	t.Helper()
	employeeValue, err := Create(Employee{
		ID: "employee-a", Name: "Employee A", Avatar: Avatar{Kind: AvatarInitials},
		JobTitle: "Engineer", Charter: "Build bounded systems.",
		DefaultSelection: ModelSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		AgentProfile:     "coding",
		SkillBindings: []SkillBinding{{
			SkillID: "skill-a", Version: "1", Digest: strings.Repeat("a", 64),
			Configuration: json.RawMessage(`{"mode":"safe"}`), Enabled: true,
		}},
		ProjectBindingIDs: []string{"project-a"},
		PermissionPolicy:  PermissionPolicy{AllowedCapabilities: []string{"read", "write"}},
		BudgetPolicy: BudgetPolicy{
			MaxModelCalls: 8, MaxTokens: 100000, TimeoutSeconds: 3600,
		},
		ConcurrencyPolicy: ConcurrencyPolicy{MaxRunningTasks: 1},
		MemoryPolicy:      MemoryPolicy{Promotion: MemoryPromotionOwnerConfirmation},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	project, err := CreateProjectBinding(ProjectBinding{
		ID: "project-a", EmployeeID: employeeValue.ID, Label: "Current workspace",
		WorkspaceRealPath: t.TempDir(), ReadAllowed: true, MutationAllowed: true,
		AllowedToolCapabilities: []string{"read", "write"},
		BudgetOverride: &BudgetPolicy{
			MaxModelCalls: 4, MaxTokens: 50000, TimeoutSeconds: 1800,
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewRevisionSnapshot(employeeValue, []ProjectBinding{project})
	if err != nil {
		t.Fatal(err)
	}
	return EmployeeTask{
		ID: "task-a", EmployeeID: employeeValue.ID, EmployeeRevision: employeeValue.Revision,
		Prompt: "Implement the bounded inbox.", EmployeeSnapshot: snapshot,
		Skills: []SkillBinding{employeeValue.SkillBindings[0]},
		Knowledge: []TaskKnowledgeSnapshot{{
			SourceID: "source-a", SourceDigest: strings.Repeat("b", 64),
			Citations: []TaskCitationReference{{
				CitationID: "cite-a", Path: "docs/guide.md", Digest: strings.Repeat("c", 64),
				StartLine: 1, EndLine: 2,
			}},
		}},
		MemoryFacts:    []TaskMemoryFactSnapshot{{FactID: "mem-a", Digest: strings.Repeat("d", 64)}},
		ProjectBinding: project,
		Policy: TaskPolicy{
			AllowedCapabilities: []string{"read"}, NetworkAllowed: false,
			Budget: BudgetPolicy{MaxModelCalls: 2, MaxTokens: 20000, TimeoutSeconds: 900},
		},
	}
}

func TestCancelEmployeeTaskRejectsTimeTravel(t *testing.T) {
	now := time.Now().UTC()
	task, err := NewTask(validTaskDraft(t, now), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CancelTask(task, now.Add(-time.Second)); !errors.Is(err, ErrInvalidTaskTransition) {
		t.Fatalf("time-travel cancel error = %v", err)
	}
}
