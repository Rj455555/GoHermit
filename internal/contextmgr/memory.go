package contextmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/storage"
)

const projectMemoryVersion = 1
const maxProjectMemoryBytes = 64 << 10

type MemoryFact struct {
	Value       string `json:"value"`
	SourceRunID string `json:"source_run_id"`
}

type ProjectMemory struct {
	SchemaVersion    int          `json:"schema_version"`
	UpdatedAt        time.Time    `json:"updated_at"`
	Architecture     []MemoryFact `json:"architecture_facts,omitempty"`
	Conventions      []MemoryFact `json:"conventions,omitempty"`
	VerifiedCommands []MemoryFact `json:"verified_commands,omitempty"`
	Decisions        []MemoryFact `json:"decisions,omitempty"`
	KnownIssues      []MemoryFact `json:"known_issues,omitempty"`
}

func LoadProjectMemory(workspace string) (ProjectMemory, error) {
	path := filepath.Join(workspace, ".gohermit", "memory", "project.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ProjectMemory{SchemaVersion: projectMemoryVersion}, nil
	}
	if err != nil {
		return ProjectMemory{}, err
	}
	var memory ProjectMemory
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&memory); err != nil {
		return ProjectMemory{}, fmt.Errorf("corrupt project memory: %w", err)
	}
	if memory.SchemaVersion != projectMemoryVersion {
		return ProjectMemory{}, fmt.Errorf("unsupported project memory schema version %d", memory.SchemaVersion)
	}
	return memory, nil
}

// UpdateProjectMemory records only bounded, verified facts. Semantic decisions
// come from the structured session state; raw prompts and tool output are never
// copied into project memory.
func UpdateProjectMemory(workspace string, s *session.Session, run session.Run) error {
	memory, err := LoadProjectMemory(workspace)
	if err != nil {
		return err
	}
	memory.SchemaVersion = projectMemoryVersion
	memory.UpdatedAt = time.Now().UTC()
	for _, path := range run.ModifiedFiles {
		memory.Architecture = mergeFact(memory.Architecture, "Touched workspace path: "+path, run.ID, 80)
	}
	for _, result := range s.TestResults {
		if result.Passed {
			memory.VerifiedCommands = mergeFact(memory.VerifiedCommands, result.Command, run.ID, 80)
		}
	}
	if len(s.CompletedSteps) > 0 {
		memory.Decisions = mergeFact(memory.Decisions, s.CompletedSteps[len(s.CompletedSteps)-1], run.ID, 80)
	}
	if run.Error != "" {
		memory.KnownIssues = mergeFact(memory.KnownIssues, run.Error, run.ID, 40)
	}
	data, err := json.MarshalIndent(memory, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxProjectMemoryBytes {
		return fmt.Errorf("project memory exceeds %d bytes", maxProjectMemoryBytes)
	}
	root := filepath.Join(workspace, ".gohermit", "memory")
	if err := storage.AtomicWrite(filepath.Join(root, "project.json"), append(data, '\n'), 0600); err != nil {
		return err
	}
	return storage.AtomicWrite(filepath.Join(root, "project.md"), []byte(renderProjectMemory(memory)), 0600)
}

func mergeFact(facts []MemoryFact, value, runID string, limit int) []MemoryFact {
	value = strings.TrimSpace(storage.Redact(value))
	if value == "" || strings.Contains(value, "[REDACTED]") {
		return facts
	}
	if len(value) > 300 {
		value = value[:300] + "…"
	}
	for i := range facts {
		if facts[i].Value == value {
			facts[i].SourceRunID = runID
			return facts
		}
	}
	facts = append(facts, MemoryFact{Value: value, SourceRunID: runID})
	if len(facts) > limit {
		facts = facts[len(facts)-limit:]
	}
	return facts
}

func renderProjectMemory(memory ProjectMemory) string {
	var b strings.Builder
	b.WriteString("# GoHermit project memory\n\n")
	b.WriteString(fmt.Sprintf("Schema: %d  \nUpdated: %s\n", memory.SchemaVersion, memory.UpdatedAt.Format(time.RFC3339)))
	sections := []struct {
		name  string
		facts []MemoryFact
	}{
		{"Architecture facts", memory.Architecture},
		{"Conventions", memory.Conventions},
		{"Verified commands", memory.VerifiedCommands},
		{"Confirmed decisions", memory.Decisions},
		{"Known issues", memory.KnownIssues},
	}
	for _, section := range sections {
		b.WriteString("\n## " + section.name + "\n\n")
		facts := append([]MemoryFact(nil), section.facts...)
		sort.SliceStable(facts, func(i, j int) bool { return facts[i].Value < facts[j].Value })
		if len(facts) == 0 {
			b.WriteString("- None recorded\n")
			continue
		}
		for _, fact := range facts {
			b.WriteString(fmt.Sprintf("- %s (run `%s`)\n", fact.Value, fact.SourceRunID))
		}
	}
	return b.String()
}
