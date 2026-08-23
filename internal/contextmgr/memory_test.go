package contextmgr

import (
	"encoding/json"
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
	run.Error = "verified issue remains open"
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
	memory, err := LoadProjectMemory(root)
	if err != nil {
		t.Fatal(err)
	}
	combined := string(jsonData) + string(markdown)
	if !strings.Contains(combined, "go test ./...") || !strings.Contains(combined, run.ID) || strings.Contains(combined, "do-not-store") {
		t.Fatalf("unexpected project memory: %s", combined)
	}
	if len(memory.Architecture) != 0 {
		t.Fatalf("modified path created Architecture Fact: %#v", memory.Architecture)
	}
	if len(memory.Decisions) != 0 {
		t.Fatalf("Completed Step created Decision: %#v", memory.Decisions)
	}
	if len(memory.VerifiedCommands) != 1 || memory.VerifiedCommands[0].Value != "go test ./..." {
		t.Fatalf("verified command was not preserved: %#v", memory.VerifiedCommands)
	}
	if len(memory.KnownIssues) != 1 || memory.KnownIssues[0].Value != "verified issue remains open" {
		t.Fatalf("Known Issues behavior changed: %#v", memory.KnownIssues)
	}
}

func TestUpdateProjectMemoryPreservesExistingFacts(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, ".gohermit", "memory")
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := ProjectMemory{
		SchemaVersion: projectMemoryVersion,
		UpdatedAt:     time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Architecture:  []MemoryFact{{Value: "Legacy architecture", SourceRunID: "run-old"}},
		Decisions:     []MemoryFact{{Value: "Legacy decision", SourceRunID: "run-old"}},
		KnownIssues:   []MemoryFact{{Value: "Legacy issue", SourceRunID: "run-old"}},
	}
	raw, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "project.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "project.md"), []byte(renderProjectMemory(existing)), 0o600); err != nil {
		t.Fatal(err)
	}
	sess, err := session.New("goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.NewRun("goal")
	if err != nil {
		t.Fatal(err)
	}
	sess.TestResults = []session.TestResult{{Command: "go vet ./...", Passed: true, Time: time.Now(), RunID: run.ID}}
	if err := UpdateProjectMemory(root, sess, *run); err != nil {
		t.Fatal(err)
	}
	after, err := LoadProjectMemory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Architecture) != 1 || after.Architecture[0].Value != "Legacy architecture" ||
		len(after.Decisions) != 1 || after.Decisions[0].Value != "Legacy decision" ||
		len(after.KnownIssues) != 1 || after.KnownIssues[0].Value != "Legacy issue" {
		t.Fatalf("existing Project Memory changed: %#v", after)
	}
}
