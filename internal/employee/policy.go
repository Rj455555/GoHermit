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
