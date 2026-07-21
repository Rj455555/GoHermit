package contextmgr

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/model"
)

func TestBuildDeduplicatesAndBounds(t *testing.T) {
	m, err := New(Config{MaxTokens: 1024, CompressionThreshold: .5, HardLimitThreshold: .9, ReserveOutputTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	recent := []model.Message{{Role: model.RoleAssistant, Content: strings.Repeat("x", 3000)}, {Role: model.RoleAssistant, Content: strings.Repeat("x", 3000)}}
	messages, compressed := m.Build(t.TempDir(), "goal", "", recent)
	if !compressed {
		t.Fatal("expected compression")
	}
	if tokens(messages) > 824 {
		t.Fatalf("tokens=%d", tokens(messages))
	}
	count := 0
	for _, m := range messages {
		if strings.Contains(m.Content, "xxx") {
			count++
		}
	}
	if count > 1 {
		t.Fatalf("duplicate messages remained: %d", count)
	}
}

func TestBuildIncludesOwnerProfileBeforeProjectContext(t *testing.T) {
	m, err := New(Config{MaxTokens: 2048, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 256, OwnerProfile: "# Owner profile\n\n- Preferred language: Chinese"})
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := m.Build(t.TempDir(), "goal", "", nil)
	if len(messages) < 3 || !strings.Contains(messages[1].Content, "Owner profile") || messages[len(messages)-1].Content != "goal" {
		t.Fatalf("messages=%+v", messages)
	}
}

func TestBuildRunKeepsDistinctToolCallTurns(t *testing.T) {
	m, err := New(Config{MaxTokens: 8192, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 256})
	if err != nil {
		t.Fatal(err)
	}
	recent := []model.Message{
		{Role: model.RoleUser, Content: "goal"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "file.read", Arguments: json.RawMessage(`{"path":"a"}`)}}},
		{Role: model.RoleTool, ToolCallID: "c1", Content: "a"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c2", Name: "file.read", Arguments: json.RawMessage(`{"path":"a"}`)}}},
		{Role: model.RoleTool, ToolCallID: "c2", Content: "a"},
	}
	messages, _ := m.BuildRun(t.TempDir(), "goal", "", recent, "")
	assistants, tools := 0, 0
	for _, msg := range messages {
		if msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0 {
			assistants++
		}
		if msg.Role == model.RoleTool {
			tools++
		}
	}
	if assistants != 2 || tools != 2 {
		t.Fatalf("tool-call turns were deduplicated: assistants=%d tools=%d messages=%+v", assistants, tools, messages)
	}
}
