package loop

import (
	"errors"
	"fmt"
)

const (
	// MaxDryRunReasons bounds the collected not-ready reasons.
	MaxDryRunReasons = 16
	// MaxDryRunRoles bounds the reported roles: the five team roles plus
	// headroom; a single-agent loop reports exactly one.
	MaxDryRunRoles = 8
)

// RoleAvailability reports one role's selection and whether its credential
// is configured. It is an availability statement only — a dry run never
// constructs a provider, so it says nothing about runtime capability.
type RoleAvailability struct {
	Role                 string `json:"role,omitempty"`
	Company              string `json:"company"`
	Access               string `json:"access"`
	Model                string `json:"model"`
	CredentialConfigured bool   `json:"credential_configured"`
	Detail               string `json:"detail,omitempty"`
}

// DryRunReport is the result of inspecting a loop definition without
// launching anything. The verdict is conservative (least capability): any
// doubt — an invalid or disabled definition, a workspace identity mismatch,
// a dirty git tree when the policy requires a clean one, a missing role
// credential, or a team template load failure — makes the report not ready.
type DryRunReport struct {
	LoopID             string             `json:"loop_id"`
	DefinitionRevision int                `json:"definition_revision"`
	DefinitionValid    bool               `json:"definition_valid"`
	WorkspaceIdentity  string             `json:"workspace_identity"`
	WorkspaceMatches   bool               `json:"workspace_matches"`
	GitClean           bool               `json:"git_clean"`
	TaskPrompt         string             `json:"task_prompt"`
	Agent              AgentSelection     `json:"agent"`
	TeamTemplateRef    string             `json:"team_template_ref,omitempty"`
	Roles              []RoleAvailability `json:"roles,omitempty"`
	// WriteScope is human-readable: read-only vs workspace-writable, with
	// the clean-git requirement spelled out when the policy carries it.
	WriteScope string        `json:"write_scope"`
	Checks     []RecipeCheck `json:"checks,omitempty"`
	Budget     Budget        `json:"budget"`
	// RequiresApproval is true when the workspace policy is not read-only
	// and the approval policy requires approval for mutation.
	RequiresApproval bool `json:"requires_approval"`
	Ready            bool `json:"ready"`
	// Reasons collects every not-ready reason found, bounded by
	// MaxDryRunReasons; a ready report always has none.
	Reasons []string `json:"reasons,omitempty"`
}

// Refuse records one not-ready reason. The verdict is derived from the
// reasons alone, so recording any reason makes the report not ready.
func (r *DryRunReport) Refuse(reason string) {
	if len(r.Reasons) < MaxDryRunReasons {
		r.Reasons = append(r.Reasons, reason)
	}
	r.Ready = false
}

// ValidateDryRunReport enforces the report contract: identifiers bounded,
// roles and reasons bounded, every free-text field bounded, and a ready
// report carrying no reasons.
func ValidateDryRunReport(r DryRunReport) error {
	if err := validateID("loop id", r.LoopID); err != nil {
		return err
	}
	if r.DefinitionRevision < 0 {
		return errors.New("dry run report revision must not be negative")
	}
	if len(r.WorkspaceIdentity) > MaxTextBytes || len(r.TaskPrompt) > MaxTextBytes || len(r.WriteScope) > MaxTextBytes {
		return errors.New("dry run report text field exceeds size limit")
	}
	if len(r.TeamTemplateRef) > MaxTextBytes {
		return errors.New("dry run report team template ref exceeds size limit")
	}
	if len(r.Roles) > MaxDryRunRoles {
		return fmt.Errorf("dry run report exceeds role limit %d", MaxDryRunRoles)
	}
	for _, role := range r.Roles {
		if len(role.Role) > MaxIDBytes || len(role.Company) > MaxTextBytes || len(role.Access) > MaxTextBytes || len(role.Model) > MaxTextBytes || len(role.Detail) > MaxTextBytes {
			return fmt.Errorf("dry run report role %q field exceeds size limit", role.Role)
		}
	}
	if len(r.Checks) > MaxChecks {
		return fmt.Errorf("dry run report exceeds check limit %d", MaxChecks)
	}
	if len(r.Reasons) > MaxDryRunReasons {
		return fmt.Errorf("dry run report exceeds reason limit %d", MaxDryRunReasons)
	}
	for _, reason := range r.Reasons {
		if clean(reason) == "" {
			return errors.New("dry run report reason must not be empty")
		}
		if len(reason) > MaxTextBytes {
			return errors.New("dry run report reason exceeds size limit")
		}
	}
	if r.Ready && len(r.Reasons) > 0 {
		return errors.New("dry run report cannot be ready with not-ready reasons")
	}
	return nil
}
