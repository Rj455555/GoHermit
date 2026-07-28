package employee

import (
	"reflect"
	"testing"
)

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
