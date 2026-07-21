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
	SystemPrompt                             string
	OwnerProfile                             string
}
type Manager struct{ cfg Config }

func New(c Config) (*Manager, error) {
	if c.MaxTokens < 1024 || c.ReserveOutputTokens <= 0 || c.ReserveOutputTokens >= c.MaxTokens {
		return nil, fmt.Errorf("invalid context budget")
	}
	return &Manager{cfg: c}, nil
}

// PromptForProfile returns the durable behavior prompt for a validated agent profile.
func PromptForProfile(profile string) string {
	switch profile {
	case "lead", "team":
		return DefaultSystem + " Act as the single owner-facing Lead Agent. Coordinate from structured handoffs, do not modify files, and report verified outcomes and unresolved risks concisely."
	case "explorer":
		return DefaultSystem + " Act as a read-only Explorer. Inspect the workspace and return a compact evidence-backed implementation brief for another agent."
	case "review":
		return DefaultSystem + " Act as a code reviewer. Inspect evidence and report prioritized findings. Do not modify files or execute mutating commands."
	case "verifier":
		return DefaultSystem + " Act as an independent Verifier. Do not modify implementation files. Run relevant deterministic tests after the final mutation and report explicit pass or fail evidence."
	case "devops":
		return DefaultSystem + " Focus on builds, tests, Git state, deployment configuration, and operational diagnosis. Make only workspace-scoped changes required by the task."
	default:
		return DefaultSystem
	}
}
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len([]byte(text)) + 3) / 4
}
func (m *Manager) Build(workspace, goal, summary string, recent []model.Message) ([]model.Message, bool) {
	return m.BuildRun(workspace, goal, summary, recent, "")
}

// BuildRun assembles fresh context for every model call. Persistent project
// rules and the active run state are never displaced by old conversation.
func (m *Manager) BuildRun(workspace, goal, summary string, recent []model.Message, runState string) ([]model.Message, bool) {
	systemPrompt := m.cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = DefaultSystem
	}
	layers := []model.Message{{Role: model.RoleSystem, Content: systemPrompt}}
	if profile := strings.TrimSpace(m.cfg.OwnerProfile); profile != "" {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: profile})
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md")); err == nil {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Project rules:\n" + string(b)})
	}
	if b, err := os.ReadFile(filepath.Join(workspace, ".gohermit", "memory", "project.md")); err == nil {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Project memory:\n" + string(b)})
	}
	if summary != "" {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Recovered task state:\n" + summary})
	}
	if runState != "" {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "Active run state:\n" + runState})
	}
	layers = append(layers, model.Message{Role: model.RoleUser, Content: goal})
	baseCount := len(layers)
	layers = dedupe(append(layers, recent...))
	if baseCount > len(layers) {
		baseCount = len(layers)
	}
	limit := m.cfg.MaxTokens - m.cfg.ReserveOutputTokens
	compressed := tokens(layers) > int(float64(limit)*m.cfg.CompressionThreshold)
	hardLimit := int(float64(limit) * m.cfg.HardLimitThreshold)
	if hardLimit <= 0 || hardLimit > limit {
		hardLimit = limit
	}
	for tokens(layers) > hardLimit && len(layers) > baseCount {
		layers = append(layers[:baseCount], layers[baseCount+1:]...)
	}
	for tokens(layers) > hardLimit && baseCount > 2 {
		// Project rules, memory and old summaries may be clipped, but the system
		// policy and current user goal remain present.
		candidate := 1
		if candidate >= len(layers)-1 {
			break
		}
		if len(layers[candidate].Content) > 256 {
			layers[candidate].Content = layers[candidate].Content[len(layers[candidate].Content)/2:]
		} else {
			layers = append(layers[:candidate], layers[candidate+1:]...)
			baseCount--
		}
	}
	return layers, compressed
}

// SetOwnerProfile attaches bounded, explicit single-owner context to this
// runtime. Runtime instances are not shared between concurrent Runs.
func (m *Manager) SetOwnerProfile(profile string) {
	m.cfg.OwnerProfile = strings.TrimSpace(profile)
}
func dedupe(in []model.Message) []model.Message {
	seen := map[string]bool{}
	out := make([]model.Message, 0, len(in))
	for _, m := range in {
		key := dedupeKey(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

// dedupeKey distinguishes tool-call turns that share an empty text content:
// dropping one of them would orphan the matching tool result message, which
// strict function-calling APIs reject with a tool_call_id error.
func dedupeKey(m model.Message) string {
	var b strings.Builder
	b.WriteString(string(m.Role))
	b.WriteString("\x00")
	b.WriteString(m.Content)
	b.WriteString("\x00")
	b.WriteString(m.ToolCallID)
	for _, c := range m.ToolCalls {
		b.WriteString("\x00")
		b.WriteString(c.ID)
		b.WriteString("\x00")
		b.WriteString(c.Name)
		b.WriteString("\x00")
		b.WriteString(string(c.Arguments))
	}
	return b.String()
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
