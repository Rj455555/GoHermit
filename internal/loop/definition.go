// Package loop defines the owner-scoped loop domain: versioned loop
// definitions and the invocations derived from them. It is pure domain —
// no storage, no transport, no IO.
package loop

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	SchemaVersion = 1
	// MaxTextBytes bounds every free-text field; prompts are text, never keys.
	MaxTextBytes = 8 << 10
	// MaxIDBytes bounds definition, invocation, session, and run identifiers.
	MaxIDBytes = 128

	// MaxChecks bounds the verification recipe size.
	MaxChecks = 16
	// MaxCommandArgs bounds one check's argv; commands are arrays by
	// construction so no shell string is ever stored or split.
	MaxCommandArgs = 8
	// MaxCheckTimeoutSeconds bounds one check's runtime.
	MaxCheckTimeoutSeconds = 3600
	// MaxRepairAttempts bounds verifier repair loops; zero means no repair.
	MaxRepairAttempts = 5

	// Budget ceilings mirror teamtemplate's per-role bounds so a tampered
	// definition cannot carry absurd limits.
	MaxModelCalls           = 1_000
	MaxTokens               = 10_000_000
	MaxBudgetTimeoutSeconds = 86_400
	// MaxReportBytes bounds the invocation report an output policy may keep.
	MaxReportBytes = 1 << 20
)

const (
	// TaskSourceFixedPrompt is the only supported task source for now;
	// validation fails closed on anything else and new types are added
	// explicitly later.
	TaskSourceFixedPrompt = "fixed_prompt"
	// TriggerManual is the only supported invocation trigger for now.
	TriggerManual = "manual"

	// PlanAuto and PlanReview mirror session.PlanMode.
	PlanAuto   = "auto"
	PlanReview = "review"
)

// TaskSource describes where an invocation's task comes from.
type TaskSource struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt"`
}

// AgentSelection pins the loop to one provider/model/agent tuple. It mirrors
// session.Selection and holds names, never keys.
type AgentSelection struct {
	Company string `json:"company"`
	Access  string `json:"access"`
	Model   string `json:"model"`
	Agent   string `json:"agent"`
}

// RecipeCheck is one deterministic verification command. Command is an argv
// array by construction — never a shell string — so execution needs no
// shell and no quoting rules.
type RecipeCheck struct {
	ID             string   `json:"id"`
	Command        []string `json:"command"`
	Required       bool     `json:"required"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// VerificationRecipe is the bounded, deterministic check list an invocation
// runs, plus the verifier policy around it.
type VerificationRecipe struct {
	Checks              []RecipeCheck `json:"checks,omitempty"`
	IndependentVerifier bool          `json:"independent_verifier"`
	MaxRepairAttempts   int           `json:"max_repair_attempts"`
}

// Empty reports whether the recipe declares nothing: no checks, no
// independent verifier, no repair bound. Sessions without a recipe keep the
// pre-recipe pipeline behavior exactly.
func (r VerificationRecipe) Empty() bool {
	return len(r.Checks) == 0 && !r.IndependentVerifier && r.MaxRepairAttempts == 0
}

// Budget caps one invocation's cost. All fields are positive and bounded.
type Budget struct {
	MaxModelCalls  int `json:"max_model_calls"`
	MaxTokens      int `json:"max_tokens"`
	TimeoutSeconds int `json:"timeout_seconds"`
}

// ApprovalPolicy is deliberately minimal: mutating loops always require the
// approval capability before they may change a workspace; read-only loops
// may run unattended.
type ApprovalPolicy struct {
	RequireForMutation bool `json:"require_for_mutation"`
}

// WorkspacePolicy constrains the workspace an invocation may touch.
// Mutation invocations require a clean git workspace; PR32 enforces it.
type WorkspacePolicy struct {
	ReadOnly        bool `json:"read_only"`
	RequireCleanGit bool `json:"require_clean_git"`
}

// OutputPolicy is deliberately minimal: whether the report includes the
// diff, and a bound on the report size.
type OutputPolicy struct {
	IncludeDiff    bool `json:"include_diff"`
	MaxReportBytes int  `json:"max_report_bytes"`
}

// Definition is the owner-scoped, versioned contract for one loop. It lives
// outside any repository; an Agent running in a workspace can never modify
// it. Every invocation snapshots the definition at creation, so editing a
// definition never changes an invocation already prepared.
type Definition struct {
	ID                 string             `json:"id"`
	SchemaVersion      int                `json:"schema_version"`
	Name               string             `json:"name"`
	Description        string             `json:"description,omitempty"`
	WorkspaceIdentity  string             `json:"workspace_identity"`
	Enabled            bool               `json:"enabled"`
	TaskSource         TaskSource         `json:"task_source"`
	AgentSelection     AgentSelection     `json:"agent_selection"`
	TeamTemplateRef    string             `json:"team_template_ref,omitempty"`
	PlanMode           string             `json:"plan_mode"`
	VerificationRecipe VerificationRecipe `json:"verification_recipe"`
	Budget             Budget             `json:"budget"`
	ApprovalPolicy     ApprovalPolicy     `json:"approval_policy"`
	WorkspacePolicy    WorkspacePolicy    `json:"workspace_policy"`
	OutputPolicy       OutputPolicy       `json:"output_policy"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	Revision           int                `json:"revision"`
}

// ValidateDefinition enforces the definition contract: every field bounded,
// name and workspace identity present, a complete agent selection, only the
// fixed_prompt task source with a non-empty bounded prompt, and no field
// that looks like a credential — definitions hold names and prompts, never
// keys.
func ValidateDefinition(d Definition) error {
	if err := validateID("id", d.ID); err != nil {
		return err
	}
	if d.Revision < 0 {
		return errors.New("loop definition revision must not be negative")
	}
	if clean(d.Name) == "" {
		return errors.New("loop definition name is required")
	}
	if clean(d.WorkspaceIdentity) == "" {
		return errors.New("loop definition workspace_identity is required")
	}
	if d.PlanMode != PlanAuto && d.PlanMode != PlanReview {
		return fmt.Errorf("loop definition plan_mode must be %q or %q", PlanAuto, PlanReview)
	}
	if err := validateTaskSource(d.TaskSource); err != nil {
		return err
	}
	if err := validateAgentSelection(d.AgentSelection); err != nil {
		return err
	}
	if err := validateRecipe(d.VerificationRecipe); err != nil {
		return err
	}
	if err := validateBudget(d.Budget); err != nil {
		return err
	}
	if d.OutputPolicy.MaxReportBytes < 1 || d.OutputPolicy.MaxReportBytes > MaxReportBytes {
		return fmt.Errorf("output policy max_report_bytes must be between 1 and %d", MaxReportBytes)
	}
	for _, field := range SecretFields(d) {
		if len(field.Value) > MaxTextBytes {
			return fmt.Errorf("loop definition %s exceeds size limit", field.Label)
		}
		if owner.LooksSecret(field.Value) {
			return fmt.Errorf("loop definition %s must not contain credentials or tokens", field.Label)
		}
	}
	return nil
}

func validateTaskSource(source TaskSource) error {
	if source.Type != TaskSourceFixedPrompt {
		return fmt.Errorf("unsupported task source %q", source.Type)
	}
	if clean(source.Prompt) == "" {
		return errors.New("fixed_prompt task source requires a non-empty prompt")
	}
	return nil
}

func validateAgentSelection(selection AgentSelection) error {
	if clean(selection.Company) == "" || clean(selection.Access) == "" || clean(selection.Model) == "" || clean(selection.Agent) == "" {
		return errors.New("agent selection requires company, access, model, and agent")
	}
	return nil
}

func validateRecipe(recipe VerificationRecipe) error {
	if len(recipe.Checks) > MaxChecks {
		return fmt.Errorf("verification recipe exceeds check limit %d", MaxChecks)
	}
	if recipe.MaxRepairAttempts < 0 || recipe.MaxRepairAttempts > MaxRepairAttempts {
		return fmt.Errorf("max_repair_attempts must be between 0 and %d", MaxRepairAttempts)
	}
	for _, check := range recipe.Checks {
		if clean(check.ID) == "" {
			return errors.New("verification check id is required")
		}
		if len(check.Command) == 0 || clean(check.Command[0]) == "" {
			return fmt.Errorf("verification check %q requires a command", check.ID)
		}
		if len(check.Command) > MaxCommandArgs {
			return fmt.Errorf("verification check %q exceeds command argument limit %d", check.ID, MaxCommandArgs)
		}
		if check.TimeoutSeconds < 1 || check.TimeoutSeconds > MaxCheckTimeoutSeconds {
			return fmt.Errorf("verification check %q timeout_seconds must be between 1 and %d", check.ID, MaxCheckTimeoutSeconds)
		}
	}
	return nil
}

func validateBudget(budget Budget) error {
	if budget.MaxModelCalls < 1 || budget.MaxModelCalls > MaxModelCalls {
		return fmt.Errorf("budget max_model_calls must be between 1 and %d", MaxModelCalls)
	}
	if budget.MaxTokens < 1 || budget.MaxTokens > MaxTokens {
		return fmt.Errorf("budget max_tokens must be between 1 and %d", MaxTokens)
	}
	if budget.TimeoutSeconds < 1 || budget.TimeoutSeconds > MaxBudgetTimeoutSeconds {
		return fmt.Errorf("budget timeout_seconds must be between 1 and %d", MaxBudgetTimeoutSeconds)
	}
	return nil
}

func validateID(label, id string) error {
	if clean(id) == "" {
		return fmt.Errorf("loop definition %s is required", label)
	}
	if len(id) > MaxIDBytes {
		return fmt.Errorf("loop definition %s exceeds size limit", label)
	}
	return nil
}

// SecretField pairs a bounded location label with a value to screen; labels
// name the field, never the value, so errors carry no secret content. The
// same list drives validation, export redaction, and import rejection, so
// the screened field set lives in exactly one place.
type SecretField struct{ Label, Value string }

// SecretFields returns every string field of d that must stay free of
// credential markers.
func SecretFields(d Definition) []SecretField {
	fields := []SecretField{
		{"name", d.Name},
		{"description", d.Description},
		{"workspace identity", d.WorkspaceIdentity},
		{"team template ref", d.TeamTemplateRef},
		{"task prompt", d.TaskSource.Prompt},
		{"agent selection company", d.AgentSelection.Company},
		{"agent selection access", d.AgentSelection.Access},
		{"agent selection model", d.AgentSelection.Model},
		{"agent selection agent", d.AgentSelection.Agent},
	}
	for _, check := range d.VerificationRecipe.Checks {
		fields = append(fields, SecretField{"check id", check.ID})
		for _, arg := range check.Command {
			fields = append(fields, SecretField{"check command", arg})
		}
	}
	return fields
}

// redact blanks any string field that matches owner.LooksSecret, keeping the
// document structure so the owner sees which fields to refill.
func redact(value string) string {
	if owner.LooksSecret(value) {
		return ""
	}
	return value
}

// redactDefinition returns a copy of d with every secret-looking string
// field blanked. A clean definition is returned unchanged.
func redactDefinition(d Definition) Definition {
	d.Name = redact(d.Name)
	d.Description = redact(d.Description)
	d.WorkspaceIdentity = redact(d.WorkspaceIdentity)
	d.TeamTemplateRef = redact(d.TeamTemplateRef)
	d.TaskSource.Prompt = redact(d.TaskSource.Prompt)
	d.AgentSelection.Company = redact(d.AgentSelection.Company)
	d.AgentSelection.Access = redact(d.AgentSelection.Access)
	d.AgentSelection.Model = redact(d.AgentSelection.Model)
	d.AgentSelection.Agent = redact(d.AgentSelection.Agent)
	for i := range d.VerificationRecipe.Checks {
		d.VerificationRecipe.Checks[i].ID = redact(d.VerificationRecipe.Checks[i].ID)
		for j := range d.VerificationRecipe.Checks[i].Command {
			d.VerificationRecipe.Checks[i].Command[j] = redact(d.VerificationRecipe.Checks[i].Command[j])
		}
	}
	return d
}

// deepCopyDefinition returns an independent copy of d so an invocation
// snapshot is immune to later edits of the source definition.
func deepCopyDefinition(d Definition) Definition {
	if d.VerificationRecipe.Checks != nil {
		checks := make([]RecipeCheck, len(d.VerificationRecipe.Checks))
		for i, check := range d.VerificationRecipe.Checks {
			checks[i] = check
			if check.Command != nil {
				checks[i].Command = append([]string(nil), check.Command...)
			}
		}
		d.VerificationRecipe.Checks = checks
	}
	return d
}

// RedactDefinition returns a copy of d with secret-looking strings blanked.
func RedactDefinition(d Definition) Definition {
	return redactDefinition(d)
}

func clean(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
}
