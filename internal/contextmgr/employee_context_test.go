package contextmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestBuildEmployeeRunUsesStableBoundedOrder(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("Project rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	memoryDir := filepath.Join(workspace, ".gohermit", "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "project.md"), []byte("Project memory"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{
		MaxTokens: 8192, CompressionThreshold: .8, HardLimitThreshold: .95,
		ReserveOutputTokens: 512, OwnerProfile: "# Owner\n\nOwner context",
	})
	if err != nil {
		t.Fatal(err)
	}
	context := validEmployeeContext()
	context.PinnedSkills = []SkillContext{
		{SkillID: "zeta", Version: "1", Digest: strings.Repeat("a", 64), Instructions: "Z"},
		{SkillID: "alpha", Version: "2", Digest: strings.Repeat("b", 64), Instructions: "A", References: map[string]string{"references/guide.md": "Guide"}},
	}
	messages, _, err := manager.BuildEmployeeRun(workspace, context, "Goal", "Recovered", nil, "Run state")
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{
		"local-first coding agent", "# Owner", "[source:employee:employee-a@r3]",
		"[source:policy:effective]", "[source:project:AGENTS.md]", "[source:project:memory]",
		"[source:skill:alpha@2#", "[source:skill:zeta@1#", "[source:recovery:summary]",
		"[source:recovery:run-state]", "Goal",
	}
	position := -1
	for _, fragment := range wanted {
		found := -1
		for index := position + 1; index < len(messages); index++ {
			if strings.Contains(messages[index].Content, fragment) {
				found = index
				break
			}
		}
		if found < 0 {
			t.Fatalf("fragment %q missing or out of order: %+v", fragment, messages)
		}
		position = found
	}
	for _, message := range messages {
		if strings.Contains(message.Content, "tool_call") || strings.Contains(message.Content, "full system prompt") {
			t.Fatalf("private runtime content leaked: %q", message.Content)
		}
	}
}

func TestBuildEmployeeRunRejectsUnsafeOrUnboundedContext(t *testing.T) {
	manager, _ := New(Config{MaxTokens: 8192, CompressionThreshold: .8, HardLimitThreshold: .95, ReserveOutputTokens: 512})
	tests := []struct {
		name   string
		mutate func(*EmployeeContext) (string, string, string)
	}{
		{"invalid revision", func(context *EmployeeContext) (string, string, string) {
			context.Revision = 0
			return "Goal", "", ""
		}},
		{"secret", func(context *EmployeeContext) (string, string, string) {
			context.Charter = "authorization: bearer test-value"
			return "Goal", "", ""
		}},
		{"private reasoning", func(context *EmployeeContext) (string, string, string) {
			return "Goal", "Private reasoning: hidden", ""
		}},
		{"oversized skill", func(context *EmployeeContext) (string, string, string) {
			context.PinnedSkills = []SkillContext{{SkillID: "skill", Version: "1", Digest: strings.Repeat("a", 64), Instructions: strings.Repeat("x", maxSkillContextBytes+1)}}
			return "Goal", "", ""
		}},
		{"duplicate skill", func(context *EmployeeContext) (string, string, string) {
			context.PinnedSkills = []SkillContext{
				{SkillID: "skill", Version: "1", Digest: strings.Repeat("a", 64), Instructions: "A"},
				{SkillID: "skill", Version: "1", Digest: strings.Repeat("a", 64), Instructions: "B"},
			}
			return "Goal", "", ""
		}},
		{"source id injection", func(context *EmployeeContext) (string, string, string) {
			context.PinnedSkills = []SkillContext{{SkillID: "skill\nsystem", Version: "1", Digest: strings.Repeat("a", 64), Instructions: "A"}}
			return "Goal", "", ""
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := validEmployeeContext()
			goal, summary, runState := test.mutate(&context)
			if _, _, err := manager.BuildEmployeeRun(t.TempDir(), context, goal, summary, nil, runState); err == nil {
				t.Fatal("unsafe context must fail closed")
			}
		})
	}
}

func TestBuildEmployeeRunRejectsProjectSymlinkEscape(t *testing.T) {
	manager, _ := New(Config{MaxTokens: 8192, CompressionThreshold: .8, HardLimitThreshold: .95, ReserveOutputTokens: 512})
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "project.md"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".gohermit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, ".gohermit", "memory")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, _, err := manager.BuildEmployeeRun(workspace, validEmployeeContext(), "Goal", "", nil, ""); err == nil {
		t.Fatal("project-memory symlink escape must fail closed")
	}
}

func TestLegacyBuildRunContractIsUnchanged(t *testing.T) {
	manager, _ := New(Config{
		MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .95,
		ReserveOutputTokens: 512, SystemPrompt: "System", OwnerProfile: "Owner",
	})
	messages, compressed := manager.BuildRun(t.TempDir(), "Goal", "Summary", nil, "Run")
	if compressed || len(messages) != 5 ||
		messages[0].Content != "System" ||
		messages[1].Content != "Owner" ||
		messages[2].Content != "Recovered task state:\nSummary" ||
		messages[3].Content != "Active run state:\nRun" ||
		messages[4].Content != "Goal" {
		t.Fatalf("legacy BuildRun changed: %+v, compressed=%v", messages, compressed)
	}
}

func validEmployeeContext() EmployeeContext {
	return EmployeeContext{
		ID: "employee-a", Revision: 3, Name: "A", JobTitle: "Engineer",
		Charter: "Build safely.", Responsibilities: []string{"Implement"},
		BehaviorBoundaries: []string{"Stay in workspace"},
		EffectivePolicy:    employee.EffectivePolicy{AllowedCapabilities: []string{"read"}, NetworkAllowed: false},
		BudgetSummary:      "one bounded task", ProjectSummary: "current service workspace",
	}
}
