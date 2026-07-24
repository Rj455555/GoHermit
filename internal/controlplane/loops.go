package controlplane

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

// emptyGitStatusSHA256 is the sha256 of empty `git status --porcelain`
// output — the fingerprint session.GitState returns for a clean tree.
const emptyGitStatusSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// ListLoops returns every stored loop definition, sorted by id.
func (s *Service) ListLoops() ([]loop.Definition, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return nil, err
	}
	definitions, err := s.loopStore.ListDefinitions()
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	return definitions, nil
}

// GetLoop returns one stored loop definition.
func (s *Service) GetLoop(id string) (loop.Definition, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return loop.Definition{}, err
	}
	definition, err := s.loopStore.GetDefinition(id)
	if err != nil {
		return loop.Definition{}, classified(KindNotFound, err)
	}
	return definition, nil
}

func (s *Service) loopStoreAvailable() error {
	if s.loopStoreErr != nil {
		return classified(KindInternal, fmt.Errorf("loop store unavailable: %w", s.loopStoreErr))
	}
	if s.loopStore == nil {
		return &Error{Kind: KindInternal, Message: "loop store unavailable"}
	}
	return nil
}

// DryRunLoop inspects a loop definition and reports whether it could launch,
// without launching anything.
//
// DRY-RUN CONTRACT: this method is pure inspection. It MUST NOT call any
// model, construct a provider or runtime, create a Session, Run, or
// approval request, create a worktree, or write to the workspace. Its only
// IO is reading the loop definition, the team template, credential presence,
// and the read-only `git status --porcelain` fingerprint via
// session.GitState. The verdict is conservative: any doubt is a not-ready
// reason, never a silent pass.
func (s *Service) DryRunLoop(ctx context.Context, loopID string) (loop.DryRunReport, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return loop.DryRunReport{}, err
	}
	definition, err := s.loopStore.GetDefinition(loopID)
	if err != nil {
		return loop.DryRunReport{}, classified(KindNotFound, err)
	}
	report := loop.DryRunReport{
		LoopID:             definition.ID,
		DefinitionRevision: definition.Revision,
		WorkspaceIdentity:  definition.WorkspaceIdentity,
		TaskPrompt:         definition.TaskSource.Prompt,
		Agent:              definition.AgentSelection,
		TeamTemplateRef:    definition.TeamTemplateRef,
		Checks:             append([]loop.RecipeCheck(nil), definition.VerificationRecipe.Checks...),
		Budget:             definition.Budget,
		WriteScope:         writeScopeText(definition.WorkspacePolicy),
		RequiresApproval:   !definition.WorkspacePolicy.ReadOnly && definition.ApprovalPolicy.RequireForMutation,
		Ready:              true,
	}
	// The store validates on load; re-check conservatively so a report can
	// never claim validity the domain would deny.
	if validateErr := loop.ValidateDefinition(definition); validateErr != nil {
		report.Refuse("definition is invalid: " + validateErr.Error())
	} else {
		report.DefinitionValid = true
	}
	if !definition.Enabled {
		report.Refuse("definition is disabled")
	}
	// Workspace identity matching is deliberately the most conservative
	// rule: the definition's workspace_identity must equal the service's
	// cleaned absolute workspace path. Anything else — a relative path, a
	// repository slug, or simply another directory — is a mismatch.
	workspace, absErr := filepath.Abs(s.Workspace)
	if absErr == nil && filepath.Clean(strings.TrimSpace(definition.WorkspaceIdentity)) == filepath.Clean(workspace) {
		report.WorkspaceMatches = true
	} else {
		report.Refuse(fmt.Sprintf("workspace identity %q does not match this workspace %q", definition.WorkspaceIdentity, s.Workspace))
	}
	// Read-only git fingerprint: clean means an empty porcelain status.
	gitState := session.GitState(ctx, s.Workspace)
	report.GitClean = gitState == emptyGitStatusSHA256
	if definition.WorkspacePolicy.RequireCleanGit && !report.GitClean {
		if gitState == "not-a-repository" {
			report.Refuse("workspace is not a git repository but the definition requires a clean git workspace")
		} else {
			report.Refuse("workspace git tree is dirty but the definition requires a clean git workspace")
		}
	}
	roles, rolesErr := s.dryRunRoles(ctx, definition)
	if rolesErr != nil {
		report.Refuse("team template load failure: " + rolesErr.Error())
	} else {
		report.Roles = roles
		for _, role := range roles {
			if role.CredentialConfigured {
				continue
			}
			label := role.Access
			if role.Role != "" {
				label = role.Role + " (" + role.Access + ")"
			}
			report.Refuse(fmt.Sprintf("credential missing for %s: %s", label, role.Detail))
		}
	}
	report.Ready = len(report.Reasons) == 0
	if validateErr := loop.ValidateDryRunReport(report); validateErr != nil {
		return loop.DryRunReport{}, classified(KindInternal, validateErr)
	}
	return report, nil
}

// writeScopeText renders the workspace policy as owner-facing text.
func writeScopeText(policy loop.WorkspacePolicy) string {
	if policy.ReadOnly {
		return "read-only: the loop may inspect the workspace but never modify it"
	}
	if policy.RequireCleanGit {
		return "workspace-writable: the loop may modify the workspace and requires a clean git tree before launch"
	}
	return "workspace-writable: the loop may modify the workspace"
}

// dryRunRoles resolves the role selections the definition would launch with.
// A team agent resolves the five roles exactly like validateTeamSelections:
// the team template's effective selections, or — when the template is empty —
// the definition's own selection for every role (legacy behavior). Any other
// agent is the single definition selection.
func (s *Service) dryRunRoles(ctx context.Context, definition loop.Definition) ([]loop.RoleAvailability, error) {
	selection := definition.AgentSelection
	if selection.Agent != "team" {
		return []loop.RoleAvailability{s.roleAvailability(ctx, "", selection.Company, selection.Access, selection.Model)}, nil
	}
	template, err := s.loadTeamTemplate()
	if err != nil {
		return nil, err
	}
	selections := map[string]teamtemplate.RoleSelection{}
	if template.Empty() {
		for _, role := range teamValidationRoles {
			selections[role] = teamtemplate.RoleSelection{Company: selection.Company, Access: selection.Access, Model: selection.Model}
		}
	} else {
		selections = teamtemplate.EffectiveSelections(template)
	}
	roles := make([]loop.RoleAvailability, 0, len(teamValidationRoles))
	for _, role := range teamValidationRoles {
		roleSelection := selections[role]
		roles = append(roles, s.roleAvailability(ctx, role, roleSelection.Company, roleSelection.Access, roleSelection.Model))
	}
	return roles, nil
}

// roleAvailability reports credential presence for one selection through the
// same accessStatus check session creation uses. It never constructs a
// provider or runtime: the tool-call capability check stays a creation-time
// concern, so availability is all a dry run reports.
func (s *Service) roleAvailability(ctx context.Context, role, company, accessID, model string) loop.RoleAvailability {
	availability := loop.RoleAvailability{Role: role, Company: company, Access: accessID, Model: model}
	access, ok := config.AccessProfile(company, accessID)
	if !ok {
		availability.Detail = "unknown access preset"
		return availability
	}
	configured, source, detail := s.accessStatus(ctx, access)
	availability.CredentialConfigured = configured
	availability.Detail = detail
	if configured && source != "" {
		availability.Detail = source
	}
	return availability
}
