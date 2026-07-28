package employee

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateNormalizesEmployeeAndStartsRevisionOne(t *testing.T) {
	now := time.Date(2026, time.July, 28, 8, 0, 0, 0, time.FixedZone("test", 8*60*60))
	draft := validEmployeeDraft()
	draft.Name = "  Ada Lovelace  "
	draft.Avatar = Avatar{Kind: AvatarInitials}
	draft.PermissionPolicy.AllowedCapabilities = []string{"write_file", "read_file", "write_file"}
	draft.ProjectBindingIDs = []string{"project-b", "project-a"}

	got, err := Create(draft, now)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.SchemaVersion != SchemaVersion || got.Revision != 1 || got.State != StateActive {
		t.Fatalf("Create() metadata = version %d revision %d state %q", got.SchemaVersion, got.Revision, got.State)
	}
	if got.Name != "Ada Lovelace" || got.Avatar.Value != "AL" {
		t.Fatalf("Create() identity = name %q avatar %#v", got.Name, got.Avatar)
	}
	if got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
		t.Fatalf("Create() timestamps = created %v updated %v", got.CreatedAt, got.UpdatedAt)
	}
	if joined := strings.Join(got.PermissionPolicy.AllowedCapabilities, ","); joined != "read_file,write_file" {
		t.Fatalf("Create() capabilities = %q", joined)
	}
	if joined := strings.Join(got.ProjectBindingIDs, ","); joined != "project-a,project-b" {
		t.Fatalf("Create() projects = %q", joined)
	}
}

func TestCreateRejectsInvalidEmployeeData(t *testing.T) {
	now := time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*Employee)
	}{
		{"missing id", func(e *Employee) { e.ID = "" }},
		{"path-like id", func(e *Employee) { e.ID = "../ada" }},
		{"secret-like id", func(e *Employee) { e.ID = "sk-proj-secret" }},
		{"missing name", func(e *Employee) { e.Name = "" }},
		{"missing job title", func(e *Employee) { e.JobTitle = "" }},
		{"missing charter", func(e *Employee) { e.Charter = "" }},
		{"incomplete selection", func(e *Employee) { e.DefaultSelection.Model = "" }},
		{"missing agent profile", func(e *Employee) { e.AgentProfile = "" }},
		{"secret marker", func(e *Employee) { e.Charter = "api_key=do-not-store" }},
		{"remote avatar", func(e *Employee) { e.Avatar = Avatar{Kind: AvatarEmoji, Value: "https://example.test/a.png"} }},
		{"non emoji avatar", func(e *Employee) { e.Avatar = Avatar{Kind: AvatarEmoji, Value: "Ada"} }},
		{"too many running tasks", func(e *Employee) { e.ConcurrencyPolicy.MaxRunningTasks = 2 }},
		{"automatic memory promotion", func(e *Employee) { e.MemoryPolicy.Promotion = MemoryPromotionAutomatic }},
		{"duplicate skill", func(e *Employee) { e.SkillBindings = append(e.SkillBindings, e.SkillBindings[0]) }},
		{"duplicate project", func(e *Employee) { e.ProjectBindingIDs = append(e.ProjectBindingIDs, e.ProjectBindingIDs[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft := validEmployeeDraft()
			tt.mutate(&draft)
			if _, err := Create(draft, now); err == nil {
				t.Fatal("Create() error = nil")
			}
		})
	}
}

func TestLifecycleTransitionsAreExplicitAndArchivedIsTerminal(t *testing.T) {
	t0 := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	active, err := Create(validEmployeeDraft(), t0)
	if err != nil {
		t.Fatal(err)
	}

	disabledAt := t0.Add(time.Minute)
	disabled, err := Disable(active, disabledAt)
	if err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if disabled.State != StateDisabled || disabled.Revision != 2 || disabled.DisabledAt == nil || !disabled.DisabledAt.Equal(disabledAt) {
		t.Fatalf("Disable() = %#v", disabled)
	}
	if active.State != StateActive || active.Revision != 1 || active.DisabledAt != nil {
		t.Fatalf("Disable() mutated source = %#v", active)
	}
	if _, err = Disable(disabled, disabledAt.Add(time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second Disable() error = %v", err)
	}

	enabledAt := disabledAt.Add(time.Minute)
	enabled, err := Enable(disabled, enabledAt)
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if enabled.State != StateActive || enabled.Revision != 3 || enabled.DisabledAt != nil {
		t.Fatalf("Enable() = %#v", enabled)
	}

	archivedAt := enabledAt.Add(time.Minute)
	archived, err := Archive(enabled, archivedAt)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.State != StateArchived || archived.Revision != 4 || archived.ArchivedAt == nil || !archived.ArchivedAt.Equal(archivedAt) {
		t.Fatalf("Archive() = %#v", archived)
	}
	for name, transition := range map[string]func(Employee, time.Time) (Employee, error){
		"disable": Disable,
		"enable":  Enable,
		"archive": Archive,
	} {
		t.Run(name+" archived", func(t *testing.T) {
			if _, transitionErr := transition(archived, archivedAt.Add(time.Minute)); !errors.Is(transitionErr, ErrArchived) {
				t.Fatalf("%s archived error = %v", name, transitionErr)
			}
		})
	}
}

func TestArchiveFromDisabledRetainsDisableTimestamp(t *testing.T) {
	t0 := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	active, _ := Create(validEmployeeDraft(), t0)
	disabled, _ := Disable(active, t0.Add(time.Minute))
	archived, err := Archive(disabled, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.DisabledAt == nil || !archived.DisabledAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("Archive() disabled_at = %v", archived.DisabledAt)
	}
}

func TestRevisePreservesIdentityAndLifecycleMetadata(t *testing.T) {
	t0 := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	current, _ := Create(validEmployeeDraft(), t0)
	proposed := current
	proposed.Name = "Grace Hopper"
	proposed.Avatar = Avatar{Kind: AvatarInitials}
	proposed.JobTitle = "Compiler Engineer"

	revised, err := Revise(current, proposed, t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("Revise() error = %v", err)
	}
	if revised.ID != current.ID || revised.CreatedAt != current.CreatedAt || revised.State != current.State {
		t.Fatalf("Revise() changed immutable fields: %#v", revised)
	}
	if revised.Revision != 2 || revised.Name != "Grace Hopper" || revised.Avatar.Value != "GH" {
		t.Fatalf("Revise() = %#v", revised)
	}

	proposed.ID = "different"
	if _, err = Revise(current, proposed, t0.Add(time.Minute)); !errors.Is(err, ErrImmutableField) {
		t.Fatalf("Revise() identity error = %v", err)
	}
	archived, _ := Archive(current, t0.Add(time.Minute))
	if _, err = Revise(archived, archived, t0.Add(2*time.Minute)); !errors.Is(err, ErrArchived) {
		t.Fatalf("Revise() archived error = %v", err)
	}
}

func TestTransitionRejectsTimeMovingBackward(t *testing.T) {
	t0 := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	active, _ := Create(validEmployeeDraft(), t0)
	if _, err := Disable(active, t0.Add(-time.Second)); err == nil {
		t.Fatal("Disable() accepted a timestamp before updated_at")
	}
}

func TestValidateRejectsLifecycleTimestampAfterRevision(t *testing.T) {
	t0 := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	active, _ := Create(validEmployeeDraft(), t0)
	disabled, _ := Disable(active, t0.Add(time.Minute))
	future := disabled.UpdatedAt.Add(time.Minute)
	disabled.DisabledAt = &future
	if err := Validate(disabled); err == nil {
		t.Fatal("Validate() accepted disabled_at after updated_at")
	}
}

func TestProjectBindingValidationAndWorkspaceFingerprint(t *testing.T) {
	now := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	draft := validProjectBinding()
	got, err := CreateProjectBinding(draft, now)
	if err != nil {
		t.Fatalf("CreateProjectBinding() error = %v", err)
	}
	if got.WorkspaceFingerprint == "" || got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(now) {
		t.Fatalf("CreateProjectBinding() = %#v", got)
	}
	if !got.MatchesCanonicalWorkspace(got.WorkspaceRealPath) {
		t.Fatal("MatchesCanonicalWorkspace() = false for exact workspace")
	}
	if got.MatchesCanonicalWorkspace(got.WorkspaceRealPath + "-other") {
		t.Fatal("MatchesCanonicalWorkspace() = true for another workspace")
	}

	tests := []struct {
		name   string
		mutate func(*ProjectBinding)
	}{
		{"relative workspace", func(b *ProjectBinding) { b.WorkspaceRealPath = "relative/workspace" }},
		{"unclean workspace", func(b *ProjectBinding) { b.WorkspaceRealPath += "/../workspace" }},
		{"mutation without read", func(b *ProjectBinding) { b.ReadAllowed, b.MutationAllowed = false, true }},
		{"secret label", func(b *ProjectBinding) { b.Label = "password=secret" }},
		{"invalid override", func(b *ProjectBinding) { b.BudgetOverride.MaxModelCalls = MaxModelCalls + 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := validProjectBinding()
			tt.mutate(&binding)
			if _, err := CreateProjectBinding(binding, now); err == nil {
				t.Fatal("CreateProjectBinding() error = nil")
			}
		})
	}
}

func validEmployeeDraft() Employee {
	return Employee{
		ID:                 "employee-ada",
		Name:               "Ada Lovelace",
		Avatar:             Avatar{Kind: AvatarEmoji, Value: "🧮"},
		JobTitle:           "Software Engineer",
		Charter:            "Build bounded, verifiable changes.",
		Responsibilities:   []string{"Implementation", "Verification"},
		BehaviorBoundaries: []string{"Never publish automatically"},
		DefaultSelection:   ModelSelection{Company: "openai", Access: "openai-api", Model: "gpt-5.6"},
		AgentProfile:       "coding",
		SkillBindings:      []SkillBinding{{SkillID: "go-tdd", Version: "1", Digest: strings.Repeat("a", 64), Enabled: true}},
		ProjectBindingIDs:  []string{"project-main"},
		PermissionPolicy:   PermissionPolicy{AllowedCapabilities: []string{"read_file", "write_file"}},
		BudgetPolicy:       BudgetPolicy{MaxModelCalls: 20, MaxTokens: 200_000, TimeoutSeconds: 3_600},
		ConcurrencyPolicy:  ConcurrencyPolicy{MaxRunningTasks: 1},
		MemoryPolicy:       MemoryPolicy{CandidateGeneration: true, Promotion: MemoryPromotionOwnerConfirmation, MaxContextFacts: 32, MaxContextBytes: 32 << 10},
	}
}

func validProjectBinding() ProjectBinding {
	return ProjectBinding{
		ID:                      "project-main",
		EmployeeID:              "employee-ada",
		Label:                   "GoHermit",
		WorkspaceRealPath:       "/workspace/gohermit",
		ReadAllowed:             true,
		MutationAllowed:         true,
		AllowedToolCapabilities: []string{"read_file", "write_file"},
		NetworkAllowed:          false,
		BudgetOverride:          &BudgetPolicy{MaxModelCalls: 10, MaxTokens: 100_000, TimeoutSeconds: 1_800},
	}
}
