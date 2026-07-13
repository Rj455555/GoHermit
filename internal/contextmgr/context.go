// Package contextmgr builds bounded, deduplicated model context.
package contextmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
)

const DefaultSystem = `You are GoHermit, a local-first coding agent. Inspect before editing. Use tools for facts and changes. Stay inside the workspace. Run relevant tests. Stop only when the task is complete or a hard runtime limit is reached. Never reveal secrets.`

type Config struct {
	MaxTokens                                int
	CompressionThreshold, HardLimitThreshold float64
	ReserveOutputTokens                      int
}
type Manager struct{ cfg Config }

func New(c Config) (*Manager, error) {
	if c.MaxTokens < 1024 || c.ReserveOutputTokens <= 0 || c.ReserveOutputTokens >= c.MaxTokens {
		return nil, fmt.Errorf("invalid context budget")
	}
	return &Manager{cfg: c}, nil
}
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len([]byte(text)) + 3) / 4
}
func (m *Manager) Build(workspace, goal, summary string, recent []model.Message) ([]model.Message, bool) {
	layers := []model.Message{{Role: model.RoleSystem, Content: DefaultSystem}}
	if b, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md")); err == nil {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Project rules:\n" + string(b)})
	}
	if b, err := os.ReadFile(filepath.Join(workspace, ".gohermit", "memory", "project.md")); err == nil {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Project memory:\n" + string(b)})
	}
	if summary != "" {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Recovered task state:\n" + summary})
	}
	layers = append(layers, model.Message{Role: model.RoleUser, Content: goal})
	layers = append(layers, dedupe(recent)...)
	limit := m.cfg.MaxTokens - m.cfg.ReserveOutputTokens
	compressed := tokens(layers) > int(float64(limit)*m.cfg.CompressionThreshold)
	for tokens(layers) > limit && len(layers) > 2 {
		layers = append(layers[:1], layers[2:]...)
	}
	if tokens(layers) > limit {
		last := &layers[len(layers)-1]
		otherTokens := tokens(layers) - EstimateTokens(last.Content)
		bytes := max(0, (limit-otherTokens)*4-3)
		if len(last.Content) > bytes {
			last.Content = last.Content[len(last.Content)-bytes:]
		}
	}
	return layers, compressed
}
func dedupe(in []model.Message) []model.Message {
	seen := map[string]bool{}
	out := make([]model.Message, 0, len(in))
	for _, m := range in {
		key := string(m.Role) + "\x00" + m.Content
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}
func tokens(messages []model.Message) int {
	n := 0
	for _, m := range messages {
		n += EstimateTokens(m.Content) + 8
		for _, c := range m.ToolCalls {
			n += EstimateTokens(string(c.Arguments)) + EstimateTokens(c.Name) + 8
		}
	}
	return n
}
func StructuredSummary(s *session.Session) string {
	return fmt.Sprintf("# Current goal\n\n%s\n\n# Completed work\n\n%s\n\n# Modified files\n\n%s\n\n# Commands run\n\n%s\n\n# Test results\n\n%s\n\n# Confirmed decisions\n\n- Workspace-only execution\n- No automatic commit, push, deployment, or telemetry\n\n# Current problems\n\n%s\n\n# Remaining work\n\n%s\n\n# Information required to resume\n\nSession %s in %s, turn %d.\n", s.Goal, bullets(s.CompletedSteps), mapKeys(s.ModifiedFiles), toolNames(s.ToolCalls), tests(s.TestResults), empty(s.LastError), bullets(s.PendingSteps), s.ID, s.Workspace, s.Turns)
}
func bullets(v []string) string {
	if len(v) == 0 {
		return "- None recorded"
	}
	return "- " + strings.Join(v, "\n- ")
}
func mapKeys(v map[string]string) string {
	if len(v) == 0 {
		return "- None"
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "- " + strings.Join(keys, "\n- ")
}
func toolNames(v []session.ToolRecord) string {
	if len(v) == 0 {
		return "- None"
	}
	out := make([]string, len(v))
	for i, r := range v {
		out[i] = r.Name + ": " + r.Summary
	}
	return bullets(out)
}
func tests(v []session.TestResult) string {
	if len(v) == 0 {
		return "- None"
	}
	out := make([]string, len(v))
	for i, r := range v {
		state := "failed"
		if r.Passed {
			state = "passed"
		}
		out[i] = r.Command + ": " + state + " — " + r.Summary
	}
	return bullets(out)
}
func empty(v string) string {
	if v == "" {
		return "- None"
	}
	return "- " + v
}
