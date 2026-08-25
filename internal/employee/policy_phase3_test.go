package employee

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestDefaultMemoryPolicyAndCreateCompatibility(t *testing.T) {
	if got, want := DefaultMemoryPolicy(), (MemoryPolicy{
		CandidateGeneration: true,
		Promotion:           MemoryPromotionOwnerConfirmation,
		AutomaticRecall:     true,
		MaxContextFacts:     12,
		MaxContextBytes:     16 << 10,
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultMemoryPolicy() = %#v, want %#v", got, want)
	}

	created, err := Create(validEmployeeDraftWithMemoryPolicy(MemoryPolicy{}), time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.MemoryPolicy, DefaultMemoryPolicy()) {
		t.Fatalf("zero creation policy = %#v, want default %#v", created.MemoryPolicy, DefaultMemoryPolicy())
	}

	disabled := MemoryPolicy{Promotion: MemoryPromotionDisabled}
	created, err = Create(validEmployeeDraftWithMemoryPolicy(disabled), time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.MemoryPolicy, disabled) {
		t.Fatalf("explicit disabled policy = %#v, want %#v", created.MemoryPolicy, disabled)
	}

	explicit := MemoryPolicy{
		CandidateGeneration: true,
		Promotion:           MemoryPromotionOwnerConfirmation,
		AutomaticRecall:     false,
		MaxContextFacts:     4,
		MaxContextBytes:     4 << 10,
	}
	created, err = Create(validEmployeeDraftWithMemoryPolicy(explicit), time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.MemoryPolicy, explicit) {
		t.Fatalf("explicit non-zero policy = %#v, want %#v", created.MemoryPolicy, explicit)
	}
}

func TestReviseDoesNotApplyNewMemoryDefaultsAndLegacySnapshotDigestSurvives(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	legacyPolicy := MemoryPolicy{
		CandidateGeneration: true,
		Promotion:           MemoryPromotionOwnerConfirmation,
		MaxContextFacts:     16,
		MaxContextBytes:     32 << 10,
	}
	current, err := Create(validEmployeeDraftWithMemoryPolicy(legacyPolicy), now)
	if err != nil {
		t.Fatal(err)
	}
	proposed := current
	proposed.Name = "Updated Employee"
	revised, err := Revise(current, proposed, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(revised.MemoryPolicy, legacyPolicy) {
		t.Fatalf("Revise() changed legacy policy = %#v, want %#v", revised.MemoryPolicy, legacyPolicy)
	}

	binding, err := CreateProjectBinding(validProjectBinding(), now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewRevisionSnapshot(current, []ProjectBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RevisionSnapshot
	if err := json.Unmarshal(legacyJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Employee.MemoryPolicy.AutomaticRecall {
		t.Fatal("legacy snapshot unexpectedly enabled automatic recall")
	}
	if !decoded.VerifyDigest() || decoded.Digest != snapshot.Digest {
		t.Fatalf("legacy snapshot digest changed: decoded=%s original=%s", decoded.Digest, snapshot.Digest)
	}
}

func validEmployeeDraftWithMemoryPolicy(policy MemoryPolicy) Employee {
	draft := validEmployeeDraft()
	draft.MemoryPolicy = policy
	return draft
}

func TestEffectivePolicyIncludesAgentProfileAndSkillsOnlyNarrow(t *testing.T) {
	base := CapabilityIntersection{
		Global: []string{"read", "write", "network"}, AgentToolPolicy: "full",
		Employee:      []string{"read", "write", "network"},
		Project:       []string{"read", "write", "network"},
		Task:          []string{"read", "write", "network"},
		GlobalNetwork: true, EmployeeNetwork: true, ProjectNetwork: true, TaskNetwork: true,
	}
	got, err := ResolveEffectivePolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.AllowedCapabilities, []string{"network", "read", "write"}) || !got.NetworkAllowed {
		t.Fatalf("base = %#v", got)
	}

	base.EnabledSkillGrants = []SkillCapabilityGrant{
		{Enabled: true, Requested: []string{"read", "network"}},
		{Enabled: false, Requested: []string{"write"}},
	}
	got, err = ResolveEffectivePolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.AllowedCapabilities, []string{"network", "read"}) || !got.NetworkAllowed {
		t.Fatalf("native effective = %#v", got)
	}

	base.EnabledSkillGrants = []SkillCapabilityGrant{{Enabled: true, InstructionOnly: true}}
	got, err = ResolveEffectivePolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AllowedCapabilities) != 0 || got.NetworkAllowed {
		t.Fatalf("adapter expanded policy = %#v", got)
	}
}

func TestEffectivePolicyFailsClosed(t *testing.T) {
	valid := CapabilityIntersection{
		Global: []string{"read", "write"}, AgentToolPolicy: "full",
		Employee: []string{"read", "write"}, Project: []string{"read", "write"}, Task: []string{"read", "write"},
	}
	tests := []struct {
		name   string
		mutate func(*CapabilityIntersection)
	}{
		{"unknown layer capability", func(input *CapabilityIntersection) { input.Project = []string{"unknown"} }},
		{"duplicate layer capability", func(input *CapabilityIntersection) { input.Task = []string{"read", "read"} }},
		{"unknown agent policy", func(input *CapabilityIntersection) { input.AgentToolPolicy = "anything" }},
		{"unknown skill capability", func(input *CapabilityIntersection) {
			input.EnabledSkillGrants = []SkillCapabilityGrant{{Enabled: true, Requested: []string{"unknown"}}}
		}},
		{"adapter capability", func(input *CapabilityIntersection) {
			input.EnabledSkillGrants = []SkillCapabilityGrant{{Enabled: true, InstructionOnly: true, Requested: []string{"read"}}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := ResolveEffectivePolicy(input); err == nil {
				t.Fatal("invalid policy must fail closed")
			}
		})
	}
}

func TestAgentProfileToolPolicyNarrows(t *testing.T) {
	input := CapabilityIntersection{
		Global: []string{"read", "write", "execute"}, AgentToolPolicy: "read",
		Employee: []string{"read", "write", "execute"},
		Project:  []string{"read", "write", "execute"}, Task: []string{"read", "write", "execute"},
	}
	got, err := ResolveEffectivePolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.AllowedCapabilities, []string{"read"}) {
		t.Fatalf("read ToolPolicy did not narrow: %#v", got)
	}
}
