package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/verification"
)

// RoleRuntime carries the validated runtime inputs for one team role when the
// team template pins that role to a different provider/model than the session.
type RoleRuntime struct {
	Selection config.RuntimeSelection
	APIKey    string
	Models    []config.ModelOption
}

// TeamWorker adapts the existing bounded single-Agent Runner into one role of
// a Mission. Child Sessions are durable for recovery but hidden from the
// owner's top-level conversation list.
type TeamWorker struct {
	Workspace       string
	ConfigPath      string
	Selection       config.RuntimeSelection
	APIKey          string
	Models          []config.ModelOption
	OwnerContext    string
	ParentSessionID string
	ParentRunID     string
	ParentStore     *session.Store
	Sink            event.Sink
	Build           func(context.Context, string, string, RuntimeOptions) (*Runtime, error)
	// Approvals, when set, is threaded into every worker runtime so a parked
	// confirmation-required call in a work item can wait for the owner (ADR
	// 0011). Nil keeps every worker deny-by-default.
	Approvals agent.ApprovalDecisions
	// RoleSelections optionally pins individual roles to their own validated
	// selection, credential, and catalog (the team template). A nil or empty
	// map keeps the session-level inputs for every role.
	RoleSelections map[string]RoleRuntime
	// VerificationRecipe, when set, makes the Verifier run the recipe's
	// deterministic checks through the policy-gated runner before its result
	// is returned. The results join the child session's TestResults and flow
	// into the handoff through the existing Checks mapping — no second
	// verification channel.
	VerificationRecipe *loop.VerificationRecipe
}

func (w *TeamWorker) Execute(ctx context.Context, assignment team.Assignment) (team.Result, error) {
	selection := w.Selection
	apiKey, models := w.APIKey, w.Models
	if override, ok := w.RoleSelections[string(assignment.WorkItem.Role)]; ok {
		selection, apiKey, models = override.Selection, override.APIKey, override.Models
	}
	selection.Agent = profileForRole(assignment.WorkItem.Role)
	build := w.Build
	if build == nil {
		build = func(ctx context.Context, workspace, configPath string, options RuntimeOptions) (*Runtime, error) {
			return BuildRuntimeWithOptions(ctx, workspace, configPath, options, nil)
		}
	}
	runtime, err := build(ctx, w.Workspace, w.ConfigPath, RuntimeOptions{Selection: &selection, APIKey: apiKey, Models: models, Approvals: w.Approvals})
	if err != nil {
		return team.Result{}, err
	}
	defer runtime.Close()
	runtime.Runner.Context.SetOwnerProfile(w.OwnerContext)
	prompt, err := assignmentPrompt(assignment)
	if err != nil {
		return team.Result{}, err
	}
	if w.VerificationRecipe != nil && assignment.WorkItem.Role == team.RoleVerifier && len(w.VerificationRecipe.Checks) > 0 {
		prompt += recipePrompt(w.VerificationRecipe.Checks)
	}
	var child *session.Session
	childID := assignment.WorkItem.ExecutionSessionID
	if childID == "" {
		return team.Result{}, errors.New("worker execution session id is required")
	}
	// A decided approval waiter stays registered until its run ends so late
	// duplicate decisions get a conflict; release the child's entries here.
	// Undecided waiters already remove themselves when Wait returns.
	if releaser, ok := w.Approvals.(interface{ Release(string) }); ok {
		defer releaser.Release(childID)
	}
	if runtime.Store.Has(childID) {
		child, err = runtime.Store.Recover(ctx, childID)
	} else {
		child, err = session.New(prompt, runtime.Workspace, session.ConfigDigest(runtime.Config))
		if err == nil {
			child.ID = childID
			child.Title = assignment.WorkItem.Title
			child.Hidden = true
			child.ParentSessionID = w.ParentSessionID
			child.ParentRunID = w.ParentRunID
			child.WorkItemID = assignment.WorkItem.ID
			child.Selection = session.Selection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent}
			child.GitState = session.GitState(ctx, runtime.Workspace)
		}
	}
	if err != nil {
		return team.Result{}, err
	}
	var relayMu sync.Mutex
	var relayErr error
	runtime.Runner.Sink = func(runtimeEvent event.Event) {
		if err := w.relay(assignment, runtimeEvent); err != nil {
			relayMu.Lock()
			if relayErr == nil {
				relayErr = err
			}
			relayMu.Unlock()
		}
	}
	if !workerAlreadyCompleted(child) {
		if err = runtime.Runner.Run(ctx, child); err != nil {
			return workerPartialResult(child), err
		}
	}
	relayMu.Lock()
	err = relayErr
	relayMu.Unlock()
	if err != nil {
		return team.Result{}, fmt.Errorf("relay worker event: %w", err)
	}
	// The Verifier's deterministic recipe checks run after its model run and
	// before the result is mapped, so the evidence joins the child session's
	// TestResults and reaches the handoff through the existing Checks path.
	if w.VerificationRecipe != nil && assignment.WorkItem.Role == team.RoleVerifier {
		if err = runRecipeChecks(ctx, runtime.Store, w.Workspace, w.VerificationRecipe, child); err != nil {
			return team.Result{}, err
		}
	}
	return workerResult(child, assignment, prompt)
}

// runRecipeChecks executes the recipe deterministically and appends one
// bounded TestResult per check to the verifier child session, tagged with
// its run id so workerResult maps them into the handoff's Checks.
func runRecipeChecks(ctx context.Context, store *session.Store, workspace string, recipe *loop.VerificationRecipe, child *session.Session) error {
	if len(child.Runs) == 0 {
		return errors.New("verifier completed without a run record")
	}
	runID := child.Runs[len(child.Runs)-1].ID
	now := time.Now().UTC()
	for _, result := range verification.RunChecks(ctx, workspace, recipe.Checks) {
		child.TestResults = append(child.TestResults, session.TestResult{
			Command:    verification.CommandString(result.Command),
			Passed:     result.Passed,
			Summary:    result.Output,
			ExitCode:   result.ExitCode,
			DurationMS: result.DurationMS,
			Time:       now,
			RunID:      runID,
		})
	}
	return store.Save(context.WithoutCancel(ctx), child)
}

// recipePrompt renders the declared deterministic checks for the verifier's
// assignment. The recipe is bounded by loop validation, so the text is
// bounded by construction.
func recipePrompt(checks []loop.RecipeCheck) string {
	var b strings.Builder
	b.WriteString("\n\nDeclared deterministic verification checks (id, command, required); they are executed independently and their results are part of your evidence:")
	for _, check := range checks {
		fmt.Fprintf(&b, "\n- %s: %s (required=%t)", check.ID, verification.CommandString(check.Command), check.Required)
	}
	return b.String()
}

// workerPartialResult reports the usage a failed child run actually recorded
// so the coordinator can aggregate it. Unlike workerResult it applies no
// minimum or token estimation; zero is reported as zero.
func workerPartialResult(child *session.Session) team.Result {
	if child == nil || len(child.Runs) == 0 {
		return team.Result{}
	}
	run := child.Runs[len(child.Runs)-1]
	return team.Result{ModelCalls: run.ModelCalls, Tokens: run.TotalTokens}
}

func workerAlreadyCompleted(child *session.Session) bool {
	return child != nil && child.ActiveRunID == "" && len(child.Runs) > 0 && child.Runs[len(child.Runs)-1].Status == session.RunCompleted
}

func workerResult(child *session.Session, assignment team.Assignment, prompt string) (team.Result, error) {
	if len(child.Runs) == 0 {
		return team.Result{}, errors.New("worker completed without a run record")
	}
	run := child.Runs[len(child.Runs)-1]
	if run.Status != session.RunCompleted {
		return team.Result{}, fmt.Errorf("worker run ended in status %s", run.Status)
	}
	handoff := parseWorkerHandoff(run.FinalMessage)
	handoff.ID = "handoff-" + assignment.WorkItem.ID
	handoff.WorkItemID = assignment.WorkItem.ID
	handoff.Role = assignment.WorkItem.Role
	handoff.ModifiedFiles = append([]string(nil), run.ModifiedFiles...)
	for _, result := range child.TestResults {
		if result.RunID == run.ID {
			handoff.Checks = append(handoff.Checks, team.Check{Command: result.Command, Passed: result.Passed, Summary: result.Summary, ExitCode: result.ExitCode, DurationMS: result.DurationMS})
		}
	}
	// A Verifier that ran no test leaves Checks genuinely empty — do not
	// fabricate a synthetic failing entry here. internal/team.handoffChecksPassed
	// already treats empty Checks as unconditionally unverified for any
	// mission with a mutating WorkItem (the exact case this used to force),
	// and empty Checks is the correct, honest signal for a purely read-only
	// mission whose Verifier had nothing to run — forcing a fake failing
	// Check there defeats that path entirely regardless of Issues.
	tokens := run.TotalTokens
	if tokens == 0 {
		tokens = contextmgr.EstimateTokens(prompt) + contextmgr.EstimateTokens(run.FinalMessage)
	}
	return team.Result{Handoff: handoff, ModelCalls: max(1, run.ModelCalls), Tokens: tokens}, nil
}

func (w *TeamWorker) relay(assignment team.Assignment, runtimeEvent event.Event) error {
	switch runtimeEvent.Type {
	case event.TaskStarted, event.TaskCompleted, event.TaskFailed, event.TaskCancelled, event.RunInterrupted, event.ModelDelta, event.PlanCreated, event.PlanUpdated:
		return nil
	}
	runtimeEvent.SessionID = w.ParentSessionID
	runtimeEvent.RunID = w.ParentRunID
	runtimeEvent.MissionID = assignment.MissionID
	runtimeEvent.WorkItemID = assignment.WorkItem.ID
	runtimeEvent.AgentID = string(assignment.WorkItem.Role)
	runtimeEvent.Sequence = 0
	if runtimeEvent.Type == event.ToolStarted {
		runtimeEvent.Data = nil
	}
	if w.ParentStore != nil {
		var err error
		runtimeEvent, err = w.ParentStore.CommitDetachedEvent(context.Background(), w.ParentSessionID, runtimeEvent)
		if err != nil {
			return err
		}
	}
	if w.Sink != nil {
		w.Sink(runtimeEvent)
	}
	return nil
}

func profileForRole(role team.Role) string {
	switch role {
	case team.RoleLead:
		return "lead"
	case team.RoleExplorer:
		return "explorer"
	case team.RoleBuilder:
		return "coding"
	case team.RoleReviewer:
		return "review"
	case team.RoleVerifier:
		return "verifier"
	case team.RoleOperator:
		return "devops"
	default:
		return "review"
	}
}

func assignmentPrompt(assignment team.Assignment) (string, error) {
	inputs, err := json.Marshal(assignment.Inputs)
	if err != nil {
		return "", err
	}
	prompt := fmt.Sprintf("Owner goal:\n%s\n\nYour assigned role: %s\nWork item: %s\nSpecific goal:\n%s\n\nDependency handoffs (bounded JSON):\n%s\n\nComplete only this work item. Your final response must be JSON with keys summary, evidence, issues, and next_steps. Do not include private reasoning, credentials, full prompts, or raw tool output.", assignment.Goal, assignment.WorkItem.Role, assignment.WorkItem.Title, assignment.WorkItem.Goal, inputs)
	if assignment.WorkItem.Role == team.RoleExplorer {
		prompt += "\n\nAs the Explorer you may optionally propose bounded follow-up substeps by adding a `substeps` key to your final JSON: an array of at most 8 objects {id, title, goal, role, depends_on}. Rules: role must be one of explorer, reviewer, or verifier and substeps are always read-only; ids must be unique snake_case without '/', '\\', or '..' and must not reuse any existing work item id; depends_on may reference queued or running work item ids or peer substep ids, but never completed work item ids. Substeps are optional; omit the key when the existing topology suffices."
	}
	if assignment.WorkItem.Role == team.RoleReviewer {
		prompt += "\n\nAs the Reviewer you must report findings by adding a `findings` key to your final JSON: an array of objects {severity, summary} where severity is \"blocking\" or \"advisory\". blocking means the issue must be fixed before delivery and schedules a bounded repair stage; advisory means an optional improvement that does not block delivery. Omit the key or use advisory-only findings when nothing must be fixed."
	}
	return prompt, nil
}

func parseWorkerHandoff(value string) team.Handoff {
	value = strings.TrimSpace(value)
	payload := struct {
		Summary   string             `json:"summary"`
		Evidence  []string           `json:"evidence"`
		Issues    []string           `json:"issues"`
		NextSteps []string           `json:"next_steps"`
		Substeps  []team.SubstepSpec `json:"substeps"`
		Findings  []team.Finding     `json:"findings"`
	}{}
	if json.Unmarshal([]byte(value), &payload) == nil && strings.TrimSpace(payload.Summary) != "" {
		return team.Handoff{Summary: strings.TrimSpace(payload.Summary), Evidence: payload.Evidence, Issues: payload.Issues, NextSteps: payload.NextSteps, Substeps: payload.Substeps, Findings: payload.Findings}
	}
	if value == "" {
		value = "Worker completed without a textual summary."
	}
	return team.Handoff{Summary: value}
}
