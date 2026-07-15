package contextmgr

import (
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
