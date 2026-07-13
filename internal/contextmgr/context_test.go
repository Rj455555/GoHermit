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
