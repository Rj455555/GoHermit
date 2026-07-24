package controlplane

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/verification"
)

// Pre-launch gate failure codes recorded on blocked invocations.
const (
	failDefinitionDisabled = "definition_disabled"
	failWorkspaceMismatch  = "workspace_mismatch"
	failWorkspaceNotClean  = "workspace_not_clean"
	failRunStart           = "run_start_failed"
	failRunFailed          = "run_failed"
	// failVerification records that the invocation's verification evidence
	// failed acceptance; the invocation is refused (blocked), never completed.
	failVerification = "verification_failed"
)

// StartLoopInvocation runs one manual invocation of a loop definition: it
// snapshots the definition, creates exactly one Session, starts exactly one
// Run through the existing session/run machinery, and records the binding.
// The Session/Run remains the source of truth for execution; the invocation
// only tracks the outer dispatch/binding lifecycle.
//
// Pre-launch gates fail closed BEFORE any provider, credential, or session
// work: a disabled definition, a workspace identity mismatch, or — when the
// definition's workspace_policy requires it — a dirty or missing git tree
// records a blocked invocation and returns it with a nil error. A launch
// failure after the gates (session creation, run start) transitions the
// invocation to skipped/failed and returns it alongside the error.
//
// Known limitation (PR33 follow-up): the definition's budget fields are not
// plumbed into run creation — StartRun accepts no budget today, so the run
// executes under the existing mission/run defaults. The CLI bounds its wait
// by the snapshot's budget timeout, but no enforcement reaches the run.
// Verification recipe enforcement is also PR33 scope.
func (s *Service) StartLoopInvocation(ctx context.Context, loopID string) (loop.Invocation, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return loop.Invocation{}, err
	}
	definition, err := s.loopStore.GetDefinition(loopID)
	if err != nil {
		return loop.Invocation{}, classified(KindNotFound, err)
	}
	now := time.Now().UTC()
	invocation, err := loop.NewInvocation(definition, loop.TriggerManual, definition.TaskSource.Prompt, now)
	if err != nil {
		// The store validates on load, so this is defense in depth: an
		// invalid definition can never be snapshotted or launched.
		return loop.Invocation{}, classified(KindInternal, fmt.Errorf("loop definition %q is invalid: %w", loopID, err))
	}
	if code, summary, refused := s.invocationGate(ctx, definition); refused {
		if err = invocation.Block(code, summary, now); err != nil {
			return loop.Invocation{}, classified(KindInternal, err)
		}
		if err = s.loopStore.SaveInvocation(invocation); err != nil {
			return loop.Invocation{}, classified(KindInternal, err)
		}
		return invocation, nil
	}
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return loop.Invocation{}, classified(KindInternal, err)
	}
	selection := definition.AgentSelection
	sess, err := s.CreateSession(ctx, CreateSessionInput{
		Title:    definition.Name,
		Company:  selection.Company,
		Access:   selection.Access,
		Model:    selection.Model,
		Agent:    selection.Agent,
		PlanMode: definition.PlanMode,
	})
	if err != nil {
		if skipErr := invocation.Skip("session creation failed: "+err.Error(), time.Now().UTC()); skipErr == nil {
			_ = s.loopStore.SaveInvocation(invocation)
		}
		return invocation, err
	}
	invocation.SessionID = sess.ID
	// The invocation snapshot is authoritative: the session carries a deep
	// copy of its verification recipe so the team pipeline executes exactly
	// what this invocation declared, immune to later definition edits.
	recipe := deepCopyRecipe(invocation.DefinitionSnapshot.VerificationRecipe)
	sess.VerificationRecipe = &recipe
	if err = s.store.Save(ctx, sess); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = invocation.Dispatch(); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation, classified(KindInternal, err)
	}
	runID, err := s.StartRun(ctx, sess.ID, invocation.TaskSnapshot)
	if err != nil {
		if failErr := invocation.Fail(failRunStart, err.Error(), time.Now().UTC()); failErr == nil {
			_ = s.loopStore.SaveInvocation(invocation)
		}
		return invocation, err
	}
	if err = invocation.Attach(sess.ID, runID, time.Now().UTC()); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation, classified(KindInternal, err)
	}
	return invocation, nil
}

// invocationGate evaluates the fail-closed pre-launch gates. The clean-git
// requirement follows the definition's workspace_policy field exactly:
// read-only definitions skip it only when require_clean_git is false.
func (s *Service) invocationGate(ctx context.Context, definition loop.Definition) (code, summary string, refused bool) {
	if !definition.Enabled {
		return failDefinitionDisabled, "loop definition is disabled", true
	}
	workspace, absErr := filepath.Abs(s.Workspace)
	if absErr != nil || filepath.Clean(strings.TrimSpace(definition.WorkspaceIdentity)) != filepath.Clean(workspace) {
		return failWorkspaceMismatch, fmt.Sprintf("workspace identity %q does not match this workspace %q", definition.WorkspaceIdentity, s.Workspace), true
	}
	if definition.WorkspacePolicy.RequireCleanGit {
		switch gitState := session.GitState(ctx, s.Workspace); gitState {
		case emptyGitStatusSHA256:
		case "not-a-repository":
			return failWorkspaceNotClean, "workspace is not a git repository but the definition requires a clean git workspace", true
		default:
			return failWorkspaceNotClean, "workspace git tree is dirty but the definition requires a clean git workspace", true
		}
	}
	return "", "", false
}

// GetInvocation returns one invocation, reconciling it against the bound
// Session/Run first.
func (s *Service) GetInvocation(ctx context.Context, id string) (loop.Invocation, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return loop.Invocation{}, err
	}
	invocation, err := s.loopStore.GetInvocation(id)
	if err != nil {
		return loop.Invocation{}, classified(KindNotFound, err)
	}
	return s.reconcileInvocation(ctx, invocation), nil
}

// ListInvocations returns the invocations of one loop, each reconciled
// against its bound Session/Run first.
func (s *Service) ListInvocations(ctx context.Context, loopID string) ([]loop.Invocation, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return nil, err
	}
	invocations, err := s.loopStore.ListInvocations(loopID)
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	for i := range invocations {
		invocations[i] = s.reconcileInvocation(ctx, invocations[i])
	}
	return invocations, nil
}

// reconcileInvocation maps the bound run's terminal state onto a dispatched
// or attached invocation, persisting the transition once. The Session/Run is
// the source of truth: reconciliation never creates a Session, Run, or any
// other state, and an interrupted run leaves the invocation attached so it
// stays resumable. Anything that cannot be reconciled is returned unchanged.
func (s *Service) reconcileInvocation(ctx context.Context, invocation loop.Invocation) loop.Invocation {
	if invocation.Status != loop.Dispatched && invocation.Status != loop.Attached {
		return invocation
	}
	if invocation.SessionID == "" || invocation.RunID == "" {
		return invocation
	}
	sess, err := s.store.Load(ctx, invocation.SessionID)
	if err != nil {
		return invocation
	}
	var run *session.Run
	for i := range sess.Runs {
		if sess.Runs[i].ID == invocation.RunID {
			run = &sess.Runs[i]
			break
		}
	}
	if run == nil {
		return invocation
	}
	// Queued, running, verifying, and interrupted runs are not terminal: the
	// invocation stays as-is (an interrupted run stays attached, resumable).
	if run.Status != session.RunCompleted && run.Status != session.RunFailed && run.Status != session.RunCancelled {
		return invocation
	}
	now := time.Now().UTC()
	// A crash between dispatch and attach can leave the invocation dispatched
	// while its run already finished; bind it first so the terminal
	// transition is legal.
	if invocation.Status == loop.Dispatched {
		started := run.StartedAt
		if started.IsZero() {
			started = now
		}
		if err := invocation.Attach(invocation.SessionID, invocation.RunID, started); err != nil {
			return invocation
		}
	}
	var transitioned bool
	switch run.Status {
	case session.RunCompleted:
		// Acceptance evaluation over the existing evidence (the mission's
		// verifier handoff): a refused invocation is blocked, never completed.
		if code, summary, accepted := invocationAcceptance(sess, invocation); !accepted {
			transitioned = invocation.Reject(code, summary, now) == nil
			break
		}
		transitioned = invocation.Complete(now) == nil
	case session.RunFailed:
		// A run that failed because independent verification did not pass is
		// the same acceptance refusal, surfaced from inside the pipeline.
		if !invocation.DefinitionSnapshot.VerificationRecipe.Empty() && sess.Mission != nil && strings.Contains(sess.Mission.Error, team.VerificationFailureMessage) {
			transitioned = invocation.Reject(failVerification, clipSummary(sess.Mission.Error), now) == nil
			break
		}
		transitioned = invocation.Fail(failRunFailed, clipSummary(run.Error), now) == nil
	case session.RunCancelled:
		transitioned = invocation.Cancel("run was cancelled", now) == nil
	}
	if !transitioned {
		return invocation
	}
	if err := s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation
	}
	return invocation
}

// CancelLoopInvocation cancels one invocation: a prepared invocation is
// skipped before dispatch; a dispatched or attached invocation cancels its
// bound run through the existing CancelRun path and is then reconciled from
// the Session/Run. Cancelling a terminal invocation is a conflict.
func (s *Service) CancelLoopInvocation(ctx context.Context, id string) (loop.Invocation, error) {
	if err := s.loopStoreAvailable(); err != nil {
		return loop.Invocation{}, err
	}
	invocation, err := s.loopStore.GetInvocation(id)
	if err != nil {
		return loop.Invocation{}, classified(KindNotFound, err)
	}
	if invocation.Status.Terminal() {
		return invocation, &Error{Kind: KindConflict, Message: "invocation is already terminal"}
	}
	if invocation.Status == loop.Prepared {
		if err = invocation.Skip("cancelled before dispatch", time.Now().UTC()); err != nil {
			return invocation, classified(KindInternal, err)
		}
		if err = s.loopStore.SaveInvocation(invocation); err != nil {
			return invocation, classified(KindInternal, err)
		}
		return invocation, nil
	}
	if invocation.SessionID != "" && invocation.RunID != "" {
		// A conflict means the run is no longer active (e.g. already
		// terminal); reconciliation below maps whatever the run records.
		if _, err = s.CancelRun(ctx, invocation.SessionID, invocation.RunID); err != nil {
			var serviceErr *Error
			if !errors.As(err, &serviceErr) || serviceErr.Kind != KindConflict {
				return invocation, err
			}
		}
		return s.GetInvocation(ctx, invocation.ID)
	}
	// Dispatched without a run binding: nothing to cancel downstream.
	if err = invocation.Cancel("cancelled before the run started", time.Now().UTC()); err != nil {
		return invocation, classified(KindInternal, err)
	}
	if err = s.loopStore.SaveInvocation(invocation); err != nil {
		return invocation, classified(KindInternal, err)
	}
	return invocation, nil
}

// clipSummary bounds a run error to the invocation text limit.
func clipSummary(summary string) string {
	if len(summary) > loop.MaxTextBytes {
		return summary[:loop.MaxTextBytes]
	}
	return summary
}

// invocationAcceptance evaluates a completed run against the invocation's
// verification recipe using only the existing evidence — the mission's
// verifier handoff. A mutation invocation is accepted only when every
// required recipe check is present in the verifier handoff's Checks and
// passed (with no required checks declared, at least one real check must
// have passed). A read-only invocation may run zero checks but the verifier
// must report no issues. Sessions without a recipe are always accepted here
// and keep pre-recipe behavior unchanged.
func invocationAcceptance(sess *session.Session, invocation loop.Invocation) (code, summary string, accepted bool) {
	recipe := invocation.DefinitionSnapshot.VerificationRecipe
	if recipe.Empty() {
		return "", "", true
	}
	var verifier *team.Handoff
	if sess.Mission != nil {
		for i := len(sess.Mission.Handoffs) - 1; i >= 0; i-- {
			if sess.Mission.Handoffs[i].Role == team.RoleVerifier {
				verifier = &sess.Mission.Handoffs[i]
				break
			}
		}
	}
	if invocation.DefinitionSnapshot.WorkspacePolicy.ReadOnly {
		if verifier != nil && len(verifier.Issues) > 0 {
			return failVerification, "read-only verifier reported unresolved issues", false
		}
		return "", "", true
	}
	// Mutation: fail closed when no independent verifier ran at all.
	if verifier == nil {
		return failVerification, "mutation run completed without an independent verifier handoff", false
	}
	required := 0
	for _, check := range recipe.Checks {
		if !check.Required {
			continue
		}
		required++
		command := verification.CommandString(check.Command)
		passed := false
		for _, evidence := range verifier.Checks {
			if evidence.Command == command && evidence.Passed {
				passed = true
				break
			}
		}
		if !passed {
			return failVerification, fmt.Sprintf("required verification check %q did not run and pass", check.ID), false
		}
	}
	if required == 0 {
		// No required checks declared: a mutation still demands at least one
		// real passing deterministic check (the PR #26/#27 rule).
		for _, evidence := range verifier.Checks {
			if evidence.Passed {
				return "", "", true
			}
		}
		return failVerification, "mutation run completed without any passing verification check", false
	}
	return "", "", true
}

// deepCopyRecipe returns an independent copy of recipe so the session's
// recipe cannot drift from the invocation snapshot it came from.
func deepCopyRecipe(recipe loop.VerificationRecipe) loop.VerificationRecipe {
	if recipe.Checks != nil {
		checks := make([]loop.RecipeCheck, len(recipe.Checks))
		for i, check := range recipe.Checks {
			checks[i] = check
			if check.Command != nil {
				checks[i].Command = append([]string(nil), check.Command...)
			}
		}
		recipe.Checks = checks
	}
	return recipe
}
