package employee

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	MaxPolicyItems          = 128
	MaxModelCalls           = 1_000
	MaxTokens               = 10_000_000
	MaxBudgetTimeoutSeconds = 86_400
	MaxMemoryContextFacts   = 512
	MaxMemoryContextBytes   = 256 << 10
)

// PermissionPolicy is an Employee-owned capability ceiling. Runtime policy,
// AgentProfile ToolPolicy, Project policy, and Task policy can only narrow it.
type PermissionPolicy struct {
	AllowedCapabilities []string `json:"allowed_capabilities,omitempty"`
	NetworkAllowed      bool     `json:"network_allowed"`
}

// BudgetPolicy bounds model use for one Employee or ProjectBinding.
type BudgetPolicy struct {
	MaxModelCalls  int `json:"max_model_calls"`
	MaxTokens      int `json:"max_tokens"`
	TimeoutSeconds int `json:"timeout_seconds"`
}

// ConcurrencyPolicy is deliberately fixed to one running Task in v0.7.
type ConcurrencyPolicy struct {
	MaxRunningTasks int `json:"max_running_tasks"`
}

type MemoryPromotion string

const (
	MemoryPromotionDisabled          MemoryPromotion = "disabled"
	MemoryPromotionOwnerConfirmation MemoryPromotion = "owner_confirmation"
	// MemoryPromotionAutomatic is reserved so persisted input fails with a
	// precise domain error. v0.7 never accepts or executes this mode.
	MemoryPromotionAutomatic MemoryPromotion = "automatic"
)

// MemoryPolicy controls only bounded context selection and Candidate creation.
// It does not store facts or implement promotion.
type MemoryPolicy struct {
	CandidateGeneration bool            `json:"candidate_generation"`
	Promotion           MemoryPromotion `json:"promotion"`
	MaxContextFacts     int             `json:"max_context_facts"`
	MaxContextBytes     int             `json:"max_context_bytes"`
}

// ProjectPolicy is the narrowing policy carried by one ProjectBinding.
type ProjectPolicy struct {
	ReadAllowed             bool          `json:"read_allowed"`
	MutationAllowed         bool          `json:"mutation_allowed"`
	AllowedToolCapabilities []string      `json:"allowed_tool_capabilities,omitempty"`
	NetworkAllowed          bool          `json:"network_allowed"`
	BudgetOverride          *BudgetPolicy `json:"budget_override,omitempty"`
}

func validatePermissionPolicy(policy PermissionPolicy) error {
	if len(policy.AllowedCapabilities) > MaxPolicyItems {
		return fmt.Errorf("employee permission policy exceeds %d capabilities", MaxPolicyItems)
	}
	if err := validateUniqueCapabilities(policy.AllowedCapabilities); err != nil {
		return fmt.Errorf("employee permission policy: %w", err)
	}
	return nil
}

func validateBudgetPolicy(policy BudgetPolicy) error {
	if policy.MaxModelCalls < 1 || policy.MaxModelCalls > MaxModelCalls {
		return fmt.Errorf("max_model_calls must be between 1 and %d", MaxModelCalls)
	}
	if policy.MaxTokens < 1 || policy.MaxTokens > MaxTokens {
		return fmt.Errorf("max_tokens must be between 1 and %d", MaxTokens)
	}
	if policy.TimeoutSeconds < 1 || policy.TimeoutSeconds > MaxBudgetTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between 1 and %d", MaxBudgetTimeoutSeconds)
	}
	return nil
}

func validateConcurrencyPolicy(policy ConcurrencyPolicy) error {
	if policy.MaxRunningTasks != 1 {
		return errors.New("max_running_tasks must be exactly 1 in v0.7")
	}
	return nil
}

func validateMemoryPolicy(policy MemoryPolicy) error {
	switch policy.Promotion {
	case MemoryPromotionDisabled:
		if policy.CandidateGeneration {
			return errors.New("candidate generation requires owner-confirmation promotion")
		}
	case MemoryPromotionOwnerConfirmation:
	default:
		return errors.New("memory promotion must be disabled or owner_confirmation")
	}
	if policy.MaxContextFacts < 0 || policy.MaxContextFacts > MaxMemoryContextFacts {
		return fmt.Errorf("max_context_facts must be between 0 and %d", MaxMemoryContextFacts)
	}
	if policy.MaxContextBytes < 0 || policy.MaxContextBytes > MaxMemoryContextBytes {
		return fmt.Errorf("max_context_bytes must be between 0 and %d", MaxMemoryContextBytes)
	}
	return nil
}

func validateProjectPolicy(policy ProjectPolicy) error {
	if policy.MutationAllowed && !policy.ReadAllowed {
		return errors.New("project mutation permission requires read permission")
	}
	if len(policy.AllowedToolCapabilities) > MaxPolicyItems {
		return fmt.Errorf("project policy exceeds %d capabilities", MaxPolicyItems)
	}
	if err := validateUniqueCapabilities(policy.AllowedToolCapabilities); err != nil {
		return fmt.Errorf("project policy: %w", err)
	}
	if policy.BudgetOverride != nil {
		if err := validateBudgetPolicy(*policy.BudgetOverride); err != nil {
			return fmt.Errorf("project budget override: %w", err)
		}
	}
	return nil
}

func normalizePermissionPolicy(policy PermissionPolicy) PermissionPolicy {
	policy.AllowedCapabilities = normalizeCapabilities(policy.AllowedCapabilities)
	return policy
}

func normalizeProjectPolicy(policy ProjectPolicy) ProjectPolicy {
	policy.AllowedToolCapabilities = normalizeCapabilities(policy.AllowedToolCapabilities)
	if policy.BudgetOverride != nil {
		override := *policy.BudgetOverride
		policy.BudgetOverride = &override
	}
	return policy
}

func normalizeCapabilities(values []string) []string {
	if values == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = clean(value)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validateUniqueCapabilities(values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateCapability(value); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate capability %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateCapability(value string) error {
	if value == "" || len(value) > MaxIDBytes {
		return errors.New("capability must be non-empty and bounded")
	}
	for _, r := range value {
		if isASCIIAlphaNumeric(r) || strings.ContainsRune("_-.:", r) {
			continue
		}
		return fmt.Errorf("capability %q contains an unsupported character", value)
	}
	return nil
}

// CapabilityIntersection contains every policy ceiling that participates in
// Phase 3 capability calculation. The zero value fails closed.
type CapabilityIntersection struct {
	Global             []string
	AgentToolPolicy    string
	Employee           []string
	Project            []string
	Task               []string
	GlobalNetwork      bool
	EmployeeNetwork    bool
	ProjectNetwork     bool
	TaskNetwork        bool
	EnabledSkillGrants []SkillCapabilityGrant
}

// SkillCapabilityGrant is a capability request, never an authorization.
// InstructionOnly is true for the read-only SKILL.md compatibility adapter.
type SkillCapabilityGrant struct {
	Enabled         bool
	InstructionOnly bool
	Requested       []string
}

type EffectivePolicy struct {
	AllowedCapabilities []string `json:"allowed_capabilities"`
	NetworkAllowed      bool     `json:"network_allowed"`
}

var knownCapabilities = map[string]struct{}{
	"read": {}, "write": {}, "execute": {}, "network": {},
	"read_file": {}, "write_file": {},
	"filesystem.read": {}, "filesystem.list": {}, "filesystem.search": {}, "filesystem.write": {},
	"patch.apply": {}, "shell.execute": {}, "git.status": {}, "git.diff": {}, "git.log": {}, "test.run": {},
}

// ValidateRequestedCapabilities rejects duplicate and unknown capability
// names. It is exported so the catalog and binding boundary share one
// fail-closed vocabulary.
func ValidateRequestedCapabilities(values []string) error {
	if len(values) > MaxPolicyItems {
		return fmt.Errorf("capability set exceeds %d items", MaxPolicyItems)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateCapability(value); err != nil {
			return err
		}
		if _, known := knownCapabilities[value]; !known {
			return fmt.Errorf("unknown capability %q", value)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("duplicate capability %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

// ResolveEffectivePolicy implements:
//
//	base = Global ∩ AgentProfile ToolPolicy ∩ Employee ∩ Project ∩ Task
//
// With no enabled Skill, effective is base. With enabled Skills, effective is
// base intersected with the union of native Skill requests. Adapter Skills
// contribute no capabilities and therefore can only narrow the result.
func ResolveEffectivePolicy(input CapabilityIntersection) (EffectivePolicy, error) {
	layers := [][]string{input.Global, input.Employee, input.Project, input.Task}
	for _, layer := range layers {
		if err := ValidateRequestedCapabilities(layer); err != nil {
			return EffectivePolicy{}, err
		}
	}
	agent, err := agentPolicyCapabilities(input.AgentToolPolicy)
	if err != nil {
		return EffectivePolicy{}, err
	}
	layers = append(layers[:1], append([][]string{agent}, layers[1:]...)...)
	base := intersectCapabilities(layers...)

	enabled := false
	requested := make(map[string]struct{})
	for _, grant := range input.EnabledSkillGrants {
		if !grant.Enabled {
			continue
		}
		enabled = true
		if grant.InstructionOnly {
			if len(grant.Requested) != 0 {
				return EffectivePolicy{}, errors.New("instruction-only Skill requested capabilities")
			}
			continue
		}
		if err := ValidateRequestedCapabilities(grant.Requested); err != nil {
			return EffectivePolicy{}, fmt.Errorf("Skill capability request: %w", err)
		}
		for _, capability := range grant.Requested {
			requested[capability] = struct{}{}
		}
	}
	if enabled {
		filtered := base[:0]
		for _, capability := range base {
			if _, requestedBySkill := requested[capability]; requestedBySkill {
				filtered = append(filtered, capability)
			}
		}
		base = filtered
	}
	network := input.GlobalNetwork && input.EmployeeNetwork && input.ProjectNetwork && input.TaskNetwork &&
		containsCapability(base, "network")
	return EffectivePolicy{AllowedCapabilities: base, NetworkAllowed: network}, nil
}

func agentPolicyCapabilities(policy string) ([]string, error) {
	switch policy {
	case "full", "team":
		return capabilityUniverse(), nil
	case "read":
		return []string{"filesystem.list", "filesystem.read", "filesystem.search", "git.diff", "git.log", "git.status", "read", "read_file"}, nil
	case "verify":
		return []string{"execute", "filesystem.list", "filesystem.read", "filesystem.search", "git.diff", "git.log", "git.status", "read", "read_file", "test.run"}, nil
	default:
		return nil, fmt.Errorf("unknown AgentProfile ToolPolicy %q", policy)
	}
}

func capabilityUniverse() []string {
	result := make([]string, 0, len(knownCapabilities))
	for capability := range knownCapabilities {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

func intersectCapabilities(layers ...[]string) []string {
	if len(layers) == 0 {
		return []string{}
	}
	counts := make(map[string]int)
	for index, layer := range layers {
		seen := make(map[string]struct{}, len(layer))
		for _, capability := range layer {
			if _, duplicate := seen[capability]; duplicate {
				continue
			}
			seen[capability] = struct{}{}
			if index == 0 || counts[capability] == index {
				counts[capability] = index + 1
			}
		}
	}
	result := make([]string, 0)
	for capability, count := range counts {
		if count == len(layers) {
			result = append(result, capability)
		}
	}
	sort.Strings(result)
	return result
}

func containsCapability(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}
