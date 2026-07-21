package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
)

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
}

func (w *TeamWorker) Execute(ctx context.Context, assignment team.Assignment) (team.Result, error) {
	selection := w.Selection
	selection.Agent = profileForRole(assignment.WorkItem.Role)
	build := w.Build
	if build == nil {
		build = func(ctx context.Context, workspace, configPath string, options RuntimeOptions) (*Runtime, error) {
			return BuildRuntimeWithOptions(ctx, workspace, configPath, options, nil)
		}
	}
	runtime, err := build(ctx, w.Workspace, w.ConfigPath, RuntimeOptions{Selection: &selection, APIKey: w.APIKey, Models: w.Models})
	if err != nil {
		return team.Result{}, err
	}
	defer runtime.Close()
	runtime.Runner.Context.SetOwnerProfile(w.OwnerContext)
	prompt, err := assignmentPrompt(assignment)
	if err != nil {
		return team.Result{}, err
	}
	var child *session.Session
	childID := assignment.WorkItem.ExecutionSessionID
	if childID == "" {
		return team.Result{}, errors.New("worker execution session id is required")
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
			return team.Result{}, err
		}
	}
	relayMu.Lock()
	err = relayErr
	relayMu.Unlock()
	if err != nil {
		return team.Result{}, fmt.Errorf("relay worker event: %w", err)
	}
	return workerResult(child, assignment, prompt)
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
			handoff.Checks = append(handoff.Checks, team.Check{Command: result.Command, Passed: result.Passed, Summary: result.Summary})
		}
	}
	if assignment.WorkItem.Role == team.RoleVerifier && len(handoff.Checks) == 0 {
		handoff.Checks = []team.Check{{Command: "required deterministic verification", Passed: false, Summary: "Verifier did not record a test result"}}
	}
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
	return fmt.Sprintf("Owner goal:\n%s\n\nYour assigned role: %s\nWork item: %s\nSpecific goal:\n%s\n\nDependency handoffs (bounded JSON):\n%s\n\nComplete only this work item. Your final response must be JSON with keys summary, evidence, issues, and next_steps. Do not include private reasoning, credentials, full prompts, or raw tool output.", assignment.Goal, assignment.WorkItem.Role, assignment.WorkItem.Title, assignment.WorkItem.Goal, inputs), nil
}

func parseWorkerHandoff(value string) team.Handoff {
	value = strings.TrimSpace(value)
	payload := struct {
		Summary   string   `json:"summary"`
		Evidence  []string `json:"evidence"`
		Issues    []string `json:"issues"`
		NextSteps []string `json:"next_steps"`
	}{}
	if json.Unmarshal([]byte(value), &payload) == nil && strings.TrimSpace(payload.Summary) != "" {
		return team.Handoff{Summary: strings.TrimSpace(payload.Summary), Evidence: payload.Evidence, Issues: payload.Issues, NextSteps: payload.NextSteps}
	}
	if value == "" {
		value = "Worker completed without a textual summary."
	}
	return team.Handoff{Summary: value}
}
