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

func TestEmployeeContextFromCompactPreservesLayerOrder(t *testing.T) {
	snapshot := employee.CompactSnapshot{
		SchemaVersion: employee.CompactSnapshotSchemaVersion,
		EmployeeID:    "employee-a", EmployeeRevision: 3, TaskID: "task-a",
		TaskSnapshotDigest: strings.Repeat("a", 64),
		Identity: employee.CompactIdentity{
			Name: "Alice", JobTitle: "Reviewer", Charter: "Review changes.",
			Responsibilities: []string{"Inspect"}, BehaviorBoundaries: []string{"Stay bounded"},
		},
		EffectivePolicy: employee.EffectivePolicy{AllowedCapabilities: []string{"read"}},
		Budget:          employee.BudgetPolicy{MaxModelCalls: 1, MaxTokens: 1000, TimeoutSeconds: 60},
		Project: employee.CompactProject{
			BindingID: "project-a", WorkspaceFingerprint: strings.Repeat("b", 64),
			ReadAllowed: true, WorkspaceSummary: "Current service workspace",
		},
		Skills: []employee.CompactSkill{{
			SkillID: "review", Version: "1", Digest: strings.Repeat("c", 64),
			Instructions: "Review instructions.",
			References:   []employee.CompactSkillReference{{Path: "references/guide.md", Content: "Guide"}},
		}},
		Knowledge: []employee.CompactKnowledge{{
			SourceID: "source-a", SourceDigest: strings.Repeat("d", 64),
			CitationID: "citation-a", Digest: strings.Repeat("e", 64),
			Title: "Handbook", Path: "handbook.md", StartLine: 1, EndLine: 1, Snippet: "Bounded citation.",
		}},
		Memory: []employee.CompactMemory{{
			FactID: "memory-a", Digest: strings.Repeat("f", 64), Category: "preference",
			Value: "Prefer small changes.", Provenance: "owner:note",
		}},
	}
	if err := employee.SealCompactSnapshot(&snapshot); err != nil {
		t.Fatal(err)
	}
	context, err := EmployeeContextFromCompact(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := New(Config{MaxTokens: 8192, CompressionThreshold: .8, HardLimitThreshold: .95, ReserveOutputTokens: 512})
	messages, _, err := manager.BuildEmployeeRun(t.TempDir(), context, "Goal", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := make([]string, len(messages))
	for index := range messages {
		joined[index] = messages[index].Content
	}
	all := strings.Join(joined, "\n")
	positions := []int{
		strings.Index(all, "[source:employee:"),
		strings.Index(all, "[source:policy:"),
		strings.Index(all, "[source:skill:"),
		strings.Index(all, "[source:knowledge:"),
		strings.Index(all, "[source:employee-memory:"),
	}
	for index := range positions {
		if positions[index] < 0 || index > 0 && positions[index] <= positions[index-1] {
			t.Fatalf("compact layer order = %v", positions)
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

func TestEmployeeKnowledgeAndMemoryLayersAreOrderedAndIndependentlyBounded(t *testing.T) {
	manager, _ := New(Config{MaxTokens: 200000, CompressionThreshold: .8, HardLimitThreshold: .95, ReserveOutputTokens: 512})
	context := validEmployeeContext()
	context.PinnedSkills = []SkillContext{{SkillID: "skill", Version: "1", Digest: strings.Repeat("a", 64), Instructions: "Skill"}}
	for index := 0; index < 20; index++ {
		context.Knowledge = append(context.Knowledge, KnowledgeContext{
			CitationID: "cite-" + string(rune('a'+index)), SourceID: "source-a", Digest: strings.Repeat("b", 64),
			Title: "Reference", Excerpt: strings.Repeat("knowledge ", 1000),
		})
		context.Memory = append(context.Memory, MemoryContext{
			ID: "memory-" + string(rune('a'+index)), Digest: strings.Repeat("c", 64), Category: "preference",
			Value: strings.Repeat("memory ", 900), Provenance: "owner-confirmed",
		})
	}
	messages, _, err := manager.BuildEmployeeRun(t.TempDir(), context, "Goal", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	skillAt, knowledgeAt, memoryAt, goalAt := -1, -1, -1, -1
	knowledgeBytes, memoryBytes := 0, 0
	for index, message := range messages {
		switch {
		case strings.HasPrefix(message.Content, "[source:skill:"):
			skillAt = index
		case strings.HasPrefix(message.Content, "[source:knowledge:"):
			if knowledgeAt < 0 {
				knowledgeAt = index
			}
			knowledgeBytes += len(message.Content)
		case strings.HasPrefix(message.Content, "[source:employee-memory:"):
			if memoryAt < 0 {
				memoryAt = index
			}
			memoryBytes += len(message.Content)
		case message.Content == "Goal":
			goalAt = index
		}
	}
	if !(skillAt < knowledgeAt && knowledgeAt < memoryAt && memoryAt < goalAt) {
		t.Fatalf("context order skill=%d Knowledge=%d Memory=%d goal=%d", skillAt, knowledgeAt, memoryAt, goalAt)
	}
	if knowledgeBytes > maxKnowledgeContextBytes || memoryBytes > maxMemoryContextBytes {
		t.Fatalf("independent bounds Knowledge=%d Memory=%d", knowledgeBytes, memoryBytes)
	}
}

func TestEmployeeKnowledgeAndMemoryRejectSecretsAndInvalidDigests(t *testing.T) {
	manager, _ := New(Config{MaxTokens: 8192, CompressionThreshold: .8, HardLimitThreshold: .95, ReserveOutputTokens: 512})
	for name, mutate := range map[string]func(*EmployeeContext){
		"Knowledge secret": func(value *EmployeeContext) {
			value.Knowledge = []KnowledgeContext{{CitationID: "cite-a", SourceID: "source-a", Digest: strings.Repeat("a", 64), Title: "Title", Excerpt: "authorization: bearer hidden-value"}}
		},
		"Memory secret": func(value *EmployeeContext) {
			value.Memory = []MemoryContext{{ID: "memory-a", Digest: strings.Repeat("a", 64), Category: "fact", Value: "api_key=abcdefghijklmnopqrstuvwxyz123456", Provenance: "owner"}}
		},
		"noncanonical digest": func(value *EmployeeContext) {
			value.Memory = []MemoryContext{{ID: "memory-a", Digest: strings.Repeat("A", 64), Category: "fact", Value: "bounded", Provenance: "owner"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			context := validEmployeeContext()
			mutate(&context)
			if _, _, err := manager.BuildEmployeeRun(t.TempDir(), context, "Goal", "", nil, ""); err == nil {
				t.Fatal("unsafe Phase 4 context accepted")
			}
		})
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
