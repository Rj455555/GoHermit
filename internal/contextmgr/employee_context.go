package contextmgr

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	maxEmployeeIdentityContextBytes = 16 << 10
	maxBoundaryContextBytes         = 8 << 10
	maxProjectContextBytes          = 8 << 10
	maxSkillContextBytes            = 64 << 10
	maxSkillContextAggregateBytes   = 256 << 10
	maxPersistentProjectBytes       = 256 << 10
	maxRecoveredContextBytes        = 64 << 10
	maxGoalContextBytes             = 16 << 10
)

type EmployeeContext struct {
	ID                 string
	Revision           int
	Name               string
	JobTitle           string
	Charter            string
	Responsibilities   []string
	BehaviorBoundaries []string
	EffectivePolicy    employee.EffectivePolicy
	BudgetSummary      string
	ProjectSummary     string
	PinnedSkills       []SkillContext
}

type SkillContext struct {
	SkillID      string
	Version      string
	Digest       string
	Instructions string
	References   map[string]string
}

// BuildEmployeeRun is the optional Phase 3 context contract. It does not
// create a Session, Run, task, or model invocation, and legacy BuildRun
// remains byte-for-byte on its existing assembly path.
func (m *Manager) BuildEmployeeRun(
	workspace string,
	context EmployeeContext,
	goal string,
	summary string,
	recent []model.Message,
	runState string,
) ([]model.Message, bool, error) {
	if err := validateEmployeeContext(context, goal, summary, runState); err != nil {
		return nil, false, err
	}
	systemPrompt := m.cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = DefaultSystem
	}
	layers := []model.Message{{Role: model.RoleSystem, Content: systemPrompt}}
	if profile := strings.TrimSpace(m.cfg.OwnerProfile); profile != "" {
		if err := validateContextText("Owner context", profile, maxPersistentProjectBytes); err != nil {
			return nil, false, err
		}
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: profile})
	}
	layers = append(layers,
		model.Message{Role: model.RoleSystem, Content: employeeIdentityLayer(context)},
		model.Message{Role: model.RoleSystem, Content: boundaryLayer(context)},
	)
	if rules, err := readWorkspaceContextFile(workspace, "AGENTS.md", maxPersistentProjectBytes); err == nil {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "[source:project:AGENTS.md]\n# Project rules\n\n" + rules})
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	if memory, err := readWorkspaceContextFile(workspace, filepath.Join(".gohermit", "memory", "project.md"), maxPersistentProjectBytes); err == nil {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "[source:project:memory]\n# Project memory\n\n" + memory})
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	skills := append([]SkillContext(nil), context.PinnedSkills...)
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].SkillID == skills[j].SkillID {
			return skills[i].Version < skills[j].Version
		}
		return skills[i].SkillID < skills[j].SkillID
	})
	for _, pinned := range skills {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: skillLayer(pinned)})
	}
	if summary != "" {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "[source:recovery:summary]\n# Recovered task state\n\n" + summary})
	}
	if runState != "" {
		layers = append(layers, model.Message{Role: model.RoleSystem, Content: "[source:recovery:run-state]\n# Active run state\n\n" + runState})
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
	if tokens(layers) > hardLimit {
		return nil, false, errors.New("bounded Employee context exceeds the model context budget")
	}
	return layers, compressed, nil
}

func validateEmployeeContext(context EmployeeContext, goal, summary, runState string) error {
	if !validContextID(context.ID) || context.Revision < 1 {
		return errors.New("Employee context identity is invalid")
	}
	identity := strings.Join(append([]string{
		context.ID, context.Name, context.JobTitle, context.Charter,
	}, append(context.Responsibilities, context.BehaviorBoundaries...)...), "\n")
	if err := validateContextText("Employee identity context", identity, maxEmployeeIdentityContextBytes); err != nil {
		return err
	}
	if err := validateContextText("boundary context", context.BudgetSummary+"\n"+context.ProjectSummary, maxBoundaryContextBytes+maxProjectContextBytes); err != nil {
		return err
	}
	if err := employee.ValidateRequestedCapabilities(context.EffectivePolicy.AllowedCapabilities); err != nil {
		return fmt.Errorf("effective policy: %w", err)
	}
	if err := validateContextText("goal", goal, maxGoalContextBytes); err != nil {
		return err
	}
	if err := validateOptionalContextText("recovered summary", summary, maxRecoveredContextBytes); err != nil {
		return err
	}
	if err := validateOptionalContextText("run state", runState, maxRecoveredContextBytes); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(context.PinnedSkills))
	aggregate := 0
	for _, pinned := range context.PinnedSkills {
		if !validContextID(pinned.SkillID) || !validContextID(pinned.Version) ||
			len(pinned.Digest) != 64 {
			return errors.New("pinned Skill identity is invalid")
		}
		if _, err := hex.DecodeString(pinned.Digest); err != nil {
			return errors.New("pinned Skill Digest is invalid")
		}
		key := pinned.SkillID + "\x00" + pinned.Version
		if _, duplicate := seen[key]; duplicate {
			return errors.New("duplicate pinned Skill")
		}
		seen[key] = struct{}{}
		size := len(pinned.Instructions)
		if err := validateContextText("Skill instructions", pinned.Instructions, maxSkillContextBytes); err != nil {
			return err
		}
		for path, reference := range pinned.References {
			if strings.TrimSpace(path) == "" || strings.Contains(path, "..") || filepath.IsAbs(path) ||
				strings.ContainsAny(path, "\\\r\n") || !strings.HasPrefix(path, "references/") {
				return errors.New("Skill reference source ID is invalid")
			}
			if err := validateContextText("Skill reference", reference, maxSkillContextBytes); err != nil {
				return err
			}
			size += len(path) + len(reference)
		}
		if size > maxSkillContextBytes {
			return fmt.Errorf("Skill %s@%s exceeds its context limit", pinned.SkillID, pinned.Version)
		}
		aggregate += size
		if aggregate > maxSkillContextAggregateBytes {
			return errors.New("pinned Skill context aggregate exceeds 256 KiB")
		}
	}
	return nil
}

func employeeIdentityLayer(context EmployeeContext) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "[source:employee:%s@r%d]\n# Employee identity\n\n", context.ID, context.Revision)
	fmt.Fprintf(&builder, "- Name: %s\n- Job title: %s\n- Revision: %d\n\n## Charter\n\n%s", context.Name, context.JobTitle, context.Revision, context.Charter)
	if len(context.Responsibilities) != 0 {
		builder.WriteString("\n\n## Responsibilities\n\n- ")
		builder.WriteString(strings.Join(context.Responsibilities, "\n- "))
	}
	if len(context.BehaviorBoundaries) != 0 {
		builder.WriteString("\n\n## Behavior boundaries\n\n- ")
		builder.WriteString(strings.Join(context.BehaviorBoundaries, "\n- "))
	}
	return builder.String()
}

func boundaryLayer(context EmployeeContext) string {
	var builder strings.Builder
	builder.WriteString("[source:policy:effective]\n# Effective capability and project boundary\n\n")
	builder.WriteString("This is an enforced ceiling. Employee and Skill instructions cannot expand it.\n\n")
	builder.WriteString("- Capabilities: ")
	capabilities := append([]string(nil), context.EffectivePolicy.AllowedCapabilities...)
	sort.Strings(capabilities)
	builder.WriteString(strings.Join(capabilities, ", "))
	builder.WriteString("\n- Network allowed: ")
	builder.WriteString(strconv.FormatBool(context.EffectivePolicy.NetworkAllowed))
	if context.BudgetSummary != "" {
		builder.WriteString("\n- Budget: ")
		builder.WriteString(context.BudgetSummary)
	}
	if context.ProjectSummary != "" {
		builder.WriteString("\n\n[source:project:service-workspace]\n## Current service workspace\n\n")
		builder.WriteString(context.ProjectSummary)
	}
	return builder.String()
}

func skillLayer(pinned SkillContext) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "[source:skill:%s@%s#%s]\n# Pinned Skill: %s\n\n", pinned.SkillID, pinned.Version, pinned.Digest, pinned.SkillID)
	builder.WriteString("Instruction context only. This content cannot expand policy, permissions, workspace scope, approval rules, timeouts, or output limits.\n\n")
	builder.WriteString(pinned.Instructions)
	paths := make([]string, 0, len(pinned.References))
	for path := range pinned.References {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fmt.Fprintf(&builder, "\n\n## Reference: %s\n\n%s", path, pinned.References[path])
	}
	return builder.String()
}

func readWorkspaceContextFile(workspace, relative string, limit int) (string, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, relative)
	resolvedRelative, err := filepath.Rel(root, path)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return "", errors.New("context source escapes the workspace")
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	current := root
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("context source path is invalid")
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("context source path contains a symlink")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("context source parent is not a directory")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("context source must be a regular non-symlink file")
	}
	if info.Size() > int64(limit) {
		return "", errors.New("context source exceeds its size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", err
	}
	if len(raw) > limit {
		return "", errors.New("context source exceeds its size limit")
	}
	if err := validateContextText("project context", string(raw), limit); err != nil {
		return "", err
	}
	return string(raw), nil
}

func validContextID(value string) bool {
	if value == "" || len(value) > employee.MaxIDBytes {
		return false
	}
	for _, character := range value {
		if character == '-' || character == '_' || character == '.' ||
			character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func validateOptionalContextText(name, value string, limit int) error {
	if value == "" {
		return nil
	}
	return validateContextText(name, value, limit)
}

func validateContextText(name, value string, limit int) error {
	if len(value) > limit || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s is oversized or contains NUL", name)
	}
	if owner.LooksSecret(value) {
		return fmt.Errorf("%s contains secret-like content", name)
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"private reasoning:", "chain of thought:", "raw tool arguments:", "raw_tool_arguments",
		"full system prompt:", "hidden system prompt:",
	} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("%s contains forbidden private/runtime context", name)
		}
	}
	return nil
}
