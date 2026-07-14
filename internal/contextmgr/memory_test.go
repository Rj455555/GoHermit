package contextmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/session"
)

func TestProjectMemoryPersistsOnlyBoundedRedactedFacts(t *testing.T) {
	root := t.TempDir()
	s, err := session.New("goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.NewRun("goal")
	if err != nil {
		t.Fatal(err)
	}
	s.ModifiedFiles["internal/agent/agent.go"] = "hash"
	run.ModifiedFiles = []string{"internal/agent/agent.go"}
	s.TestResults = []session.TestResult{{Command: "go test ./...", Passed: true, Time: time.Now(), RunID: run.ID}}
	s.CompletedSteps = []string{"Keep Session separate from Run", "api_key=do-not-store"}
	if err := UpdateProjectMemory(root, s, *run); err != nil {
		t.Fatal(err)
	}
	jsonData, err := os.ReadFile(filepath.Join(root, ".gohermit", "memory", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := os.ReadFile(filepath.Join(root, ".gohermit", "memory", "project.md"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(jsonData) + string(markdown)
	if !strings.Contains(combined, "go test ./...") || !strings.Contains(combined, run.ID) || strings.Contains(combined, "do-not-store") {
		t.Fatalf("unexpected project memory: %s", combined)
	}
}
