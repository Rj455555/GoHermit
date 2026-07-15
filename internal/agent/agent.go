// Package agent implements the bounded single-agent execution loop.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type Config struct {
	MaxTurns                   int
	Timeout                    time.Duration
	Model                      string
	Stream                     bool
	CheckpointEveryTurns       int
	CheckpointOnToolCompletion bool
	MaxVerificationAttempts    int
}
type Runner struct {
	Provider model.Provider
	Executor tool.Executor
	Context  *contextmgr.Manager
	Store    *session.Store
	Config   Config
	Sink     event.Sink
}

func (r *Runner) Run(ctx context.Context, s *session.Session) error {
	if r.Provider == nil || r.Executor.Registry == nil || r.Context == nil || r.Store == nil {
		return errors.New("agent dependencies are incomplete")
	}
	if r.Config.MaxTurns < 1 {
		return errors.New("max turns must be positive")
	}
	runCtx, cancel := context.WithTimeout(ctx, r.Config.Timeout)
	defer cancel()
	r.Store.SeedEventSequence(s.ID, s.NextEventSequence)
	active := s.ActiveRun()
	if active == nil {
		var err error
		active, err = s.NewRun(s.Goal)
		if err != nil {
			return err
		}
		if err = r.Store.AppendMessage(s.ID, session.MessageRecord{RunID: active.ID, Role: model.RoleUser, Content: active.Message}); err != nil {
			return err
		}
	}
	if active.Status != session.RunQueued && active.Status != session.RunInterrupted {
		return fmt.Errorf("run %s cannot start from status %s", active.ID, active.Status)
	}
	planCreated := false
	if active.Plan == nil {
		plan, err := taskplan.DefaultSingle(active.ID)
		if err != nil {
			return err
		}
		active.Plan = plan
		planCreated = true
	}
	active.Status = session.RunRunning
	active.UpdatedAt = time.Now().UTC()
	s.Status = session.Open
	started := event.New(event.TaskStarted, s.ID)
	started.RunID = active.ID
	r.emit(s, started, true)
	if planCreated {
		r.emitPlan(s, active, event.PlanCreated, "", "执行计划已创建")
	}
	r.preparePlan(s, active)
	if s.WorkspaceChanged {
		e := event.New(event.WorkspaceChanged, s.ID)
		e.RunID = active.ID
		e.Message = "workspace changed since the previous checkpoint; current state must be inspected and verified"
		r.emit(s, e, true)
		s.WorkspaceChanged = false
	}
	if err := r.Store.Save(context.WithoutCancel(runCtx), s); err != nil {
		return err
	}
	messages := append([]model.Message(nil), s.RecentMessages...)
	if len(messages) == 0 || messages[len(messages)-1].Role != model.RoleUser || messages[len(messages)-1].Content != active.Message {
		messages = append(messages, model.Message{Role: model.RoleUser, Content: active.Message})
	}
	maxVerification := r.Config.MaxVerificationAttempts
	if maxVerification <= 0 {
		maxVerification = 3
	}
	for runTurn := 1; runTurn <= r.Config.MaxTurns; runTurn++ {
		if err := runCtx.Err(); err != nil {
			return r.stop(runCtx, s, err)
		}
		s.Turns++
		turn := s.Turns
		active.EndTurn = turn
		active.UpdatedAt = time.Now().UTC()
		e := event.New(event.TurnStarted, s.ID)
		e.RunID = active.ID
		e.Turn = turn
		r.emit(s, e, true)
		runState := r.runState(s, active)
		assembled, compressed := r.Context.BuildRun(s.Workspace, active.Message, s.Summary, messages, runState)
		if compressed {
			r.compress(runCtx, s, active, messages)
			messages = boundedMessages(messages)
			assembled, _ = r.Context.BuildRun(s.Workspace, active.Message, s.Summary, messages, runState)
		}
		e = event.New(event.ModelStarted, s.ID)
		e.RunID = active.ID
		e.Turn = turn
		r.emit(s, e, true)
		response, err := r.Provider.Generate(runCtx, model.GenerateRequest{Model: r.Config.Model, Messages: assembled, Tools: r.Executor.Registry.ModelDefinitions(), Stream: r.Config.Stream, OnStream: func(delta model.StreamEvent) {
			if delta.Delta == "" {
				return
			}
			ev := event.New(event.ModelDelta, s.ID)
			ev.RunID = active.ID
			ev.Turn = turn
			ev.Message = delta.Delta
			r.emit(s, ev, false)
		}})
		if err != nil {
			if runCtx.Err() != nil {
				return r.stop(runCtx, s, runCtx.Err())
			}
			s.LastError = err.Error()
			return r.fail(runCtx, s, "model request failed", err)
		}
		active.ModelCalls++
		active.PromptTokens += response.Usage.PromptTokens
		active.CompletionTokens += response.Usage.CompletionTokens
		active.TotalTokens += response.Usage.TotalTokens
		e = event.New(event.ModelCompleted, s.ID)
		e.RunID = active.ID
		e.Turn = turn
		e.Message = response.FinishReason
		r.emit(s, e, true)
		messages = append(messages, response.Message)
		if len(response.Message.ToolCalls) == 0 {
			r.planComplete(s, active, "analyze", "上下文分析完成")
			r.planComplete(s, active, "execute", "执行阶段完成")
			r.planStart(s, active, "verify", "正在验证完成条件")
			active.Status = session.RunVerifying
			ve := event.New(event.RunVerifying, s.ID)
			ve.RunID = active.ID
			ve.Turn = turn
			r.emit(s, ve, true)
			passed, reason := r.verifyCandidate(runCtx, s, active)
			if !passed {
				active.VerificationAttempts++
				r.planNote(s, active, "verify", "验证未通过，正在修复后重试："+reason)
				if active.VerificationAttempts >= maxVerification {
					return r.fail(runCtx, s, "verification failed", errors.New(reason))
				}
				active.Status = session.RunRunning
				messages = append(messages, model.Message{Role: model.RoleUser, Content: "Completion verification did not pass: " + reason + "\nInspect the current workspace, fix the problem, run the required verification, and only then provide the final answer."})
				s.PendingSteps = []string{reason}
				continue
			}
			return r.complete(runCtx, s, active, response.Message.Content, messages)
		}
		r.planComplete(s, active, "analyze", "上下文分析完成")
		r.planStart(s, active, "execute", "正在执行任务")
		for _, call := range response.Message.ToolCalls {
			e = event.New(event.ToolStarted, s.ID)
			e.RunID = active.ID
			e.Turn = turn
			e.Tool = call.Name
			e.Data = call.Arguments
			r.emit(s, e, true)
			startedAt := time.Now().UTC()
			s.ToolCalls = appendBoundedTool(s.ToolCalls, session.ToolRecord{Time: startedAt, StartedAt: startedAt, RunID: active.ID, CallID: call.ID, Name: call.Name, Status: "started"}, 500)
			if err := r.Store.Save(context.WithoutCancel(runCtx), s); err != nil {
				return err
			}
			result, _ := r.Executor.Execute(runCtx, tool.Call{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
			if result.Error != nil && (result.Error.Code == "confirmation_required" || result.Error.Code == "blocked") {
				pe := event.New(event.PermissionRequired, s.ID)
				pe.RunID = active.ID
				pe.Turn = turn
				pe.Tool = call.Name
				pe.Message = result.Error.Message
				r.emit(s, pe, true)
			}
			content := tool.MarshalResult(result)
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: content})
			summary := summarizeResult(result)
			completedAt := time.Now().UTC()
			for i := len(s.ToolCalls) - 1; i >= 0; i-- {
				if s.ToolCalls[i].CallID == call.ID && s.ToolCalls[i].RunID == active.ID {
					s.ToolCalls[i].Summary = summary
					s.ToolCalls[i].IsError = result.Error != nil
					s.ToolCalls[i].Status = "completed"
					s.ToolCalls[i].CompletedAt = &completedAt
					break
				}
			}
			e = event.New(event.ToolCompleted, s.ID)
			e.RunID = active.ID
			e.Turn = turn
			e.Tool = call.Name
			e.Message = summary
			r.emit(s, e, true)
			if defTool, ok := r.Executor.Registry.Get(call.Name); ok && defTool.Definition().MutatesWorkspace {
				active.LastMutationTurn = turn
				r.snapshotChanges(runCtx, s, active)
			}
			if call.Name == "test.run" {
				passed := result.Error == nil
				s.TestResults = append(s.TestResults, session.TestResult{Command: "go test", Passed: passed, Summary: summary, Time: completedAt, RunID: active.ID, Turn: turn})
				if passed {
					active.LastVerificationTurn = turn
				}
			}
			s.RecentMessages = boundedMessages(messages)
			s.Summary = contextmgr.StructuredSummary(s)
			if r.Config.CheckpointOnToolCompletion {
				s.GitState = session.GitState(runCtx, s.Workspace)
				checkpoint := event.New(event.CheckpointSaved, s.ID)
				r.Store.BufferEvent(s.ID, checkpoint)
				if err := r.Store.Save(context.WithoutCancel(runCtx), s); err != nil {
					return err
				}
				if r.Sink != nil {
					r.Sink(checkpoint)
				}
			}
		}
		if r.Config.CheckpointEveryTurns > 0 && turn%r.Config.CheckpointEveryTurns == 0 {
			s.RecentMessages = boundedMessages(messages)
			s.Summary = contextmgr.StructuredSummary(s)
			s.GitState = session.GitState(runCtx, s.Workspace)
			checkpoint := event.New(event.CheckpointSaved, s.ID)
			r.Store.BufferEvent(s.ID, checkpoint)
			if err := r.Store.Save(context.WithoutCancel(runCtx), s); err != nil {
				return err
			}
			if r.Sink != nil {
				r.Sink(checkpoint)
			}
		}
	}
	s.LastError = fmt.Sprintf("maximum turns reached (%d)", r.Config.MaxTurns)
	return r.fail(runCtx, s, "maximum turns reached", errors.New(s.LastError))
}

func appendBoundedTool(records []session.ToolRecord, r session.ToolRecord, max int) []session.ToolRecord {
	records = append(records, r)
	if len(records) > max {
		return records[len(records)-max:]
	}
	return records
}

func (r *Runner) runState(s *session.Session, run *session.Run) string {
	return fmt.Sprintf("Run %s is %s. Turn %d. Last mutation turn: %d. Last verified turn: %d. Pending work: %s", run.ID, run.Status, s.Turns, run.LastMutationTurn, run.LastVerificationTurn, strings.Join(s.PendingSteps, "; "))
}

func (r *Runner) compress(ctx context.Context, s *session.Session, run *session.Run, messages []model.Message) {
	deterministic := contextmgr.StructuredSummary(s)
	request := []model.Message{
		{Role: model.RoleSystem, Content: "Compress the visible coding-session facts into JSON only. Never include secrets, private reasoning, full prompts, or raw tool output. Return exactly {\"summary\":\"markdown using the headings Current goal, Completed work, Modified files, Commands run, Test results, Confirmed decisions, Current problems, Remaining work, Resume information\"}."},
		{Role: model.RoleUser, Content: deterministic + "\n\nRecent visible context:\n" + visibleContext(messages)},
	}
	response, err := r.Provider.Generate(ctx, model.GenerateRequest{Model: r.Config.Model, Messages: request, Stream: false})
	if err != nil {
		s.Summary = deterministic
		return
	}
	run.ModelCalls++
	run.PromptTokens += response.Usage.PromptTokens
	run.CompletionTokens += response.Usage.CompletionTokens
	run.TotalTokens += response.Usage.TotalTokens
	var payload struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal([]byte(response.Message.Content), &payload) != nil || strings.TrimSpace(payload.Summary) == "" {
		s.Summary = deterministic
		return
	}
	payload.Summary = strings.TrimSpace(payload.Summary)
	if len(payload.Summary) > 16<<10 {
		payload.Summary = payload.Summary[:16<<10]
	}
	s.Summary = payload.Summary
	run.UpdatedAt = time.Now().UTC()
}

func visibleContext(messages []model.Message) string {
	start := max(0, len(messages)-12)
	var b strings.Builder
	for _, message := range messages[start:] {
		if message.Role != model.RoleUser && message.Role != model.RoleAssistant {
			continue
		}
		content := clip(message.Content, 1000)
		if content != "" {
			fmt.Fprintf(&b, "%s: %s\n", message.Role, content)
		}
	}
	return b.String()
}

func (r *Runner) verifyCandidate(ctx context.Context, s *session.Session, run *session.Run) (bool, string) {
	if run.LastMutationTurn == 0 {
		return true, ""
	}
	cmd := exec.CommandContext(ctx, "git", "diff", "--check")
	cmd.Dir = s.Workspace
	if output, err := cmd.CombinedOutput(); err != nil {
		return false, "git diff --check failed: " + clip(strings.TrimSpace(string(output)), 500)
	}
	now := time.Now().UTC()
	s.TestResults = append(s.TestResults, session.TestResult{Command: "git diff --check", Passed: true, Summary: "passed", Time: now, RunID: run.ID, Turn: s.Turns})
	if documentationOnly(run.ModifiedFiles) {
		run.LastVerificationTurn = s.Turns
		return true, ""
	}
	for i := len(s.TestResults) - 1; i >= 0; i-- {
		result := s.TestResults[i]
		if result.RunID == run.ID && result.Passed && result.Command != "git diff --check" && result.Turn >= run.LastMutationTurn {
			run.LastVerificationTurn = result.Turn
			return true, ""
		}
	}
	return false, "workspace code changed after the last successful test; run test.run and resolve any failures"
}

func documentationOnly(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" && ext != ".rst" && ext != ".adoc" {
			return false
		}
	}
	return true
}

func (r *Runner) complete(ctx context.Context, s *session.Session, run *session.Run, final string, messages []model.Message) error {
	r.planComplete(s, run, "verify", "验证已通过")
	r.planStart(s, run, "report", "正在整理交付结果")
	r.planComplete(s, run, "report", "结果已整理并交付")
	if run.Plan == nil || run.Plan.Status != taskplan.Completed {
		return r.fail(ctx, s, "live plan did not reach completion", errors.New("live plan completion gate failed"))
	}
	now := time.Now().UTC()
	run.Status = session.RunCompleted
	run.FinalMessage = final
	run.CompletedAt = &now
	run.UpdatedAt = now
	s.ActiveRunID = ""
	s.Status = session.Open
	s.LastError = ""
	s.CompletedSteps = append(s.CompletedSteps, "Run "+run.ID+" completed after verification")
	s.PendingSteps = nil
	s.RecentMessages = boundedMessages(messages)
	r.compress(ctx, s, run, messages)
	s.GitState = session.GitState(ctx, s.Workspace)
	if strings.TrimSpace(final) != "" {
		if err := r.Store.AppendMessage(s.ID, session.MessageRecord{RunID: run.ID, Role: model.RoleAssistant, Content: final, CreatedAt: now}); err != nil {
			return err
		}
	}
	if err := contextmgr.UpdateProjectMemory(s.Workspace, s, *run); err != nil {
		return fmt.Errorf("update project memory: %w", err)
	}
	memoryEvent := event.New(event.MemoryUpdated, s.ID)
	memoryEvent.RunID = run.ID
	r.emit(s, memoryEvent, true)
	done := event.New(event.TaskCompleted, s.ID)
	done.RunID = run.ID
	done.Turn = s.Turns
	done.Message = final
	r.emit(s, done, true)
	return r.Store.Save(context.WithoutCancel(ctx), s)
}
func boundedMessages(messages []model.Message) []model.Message {
	const max = 40
	if len(messages) > max {
		return append([]model.Message(nil), messages[len(messages)-max:]...)
	}
	return append([]model.Message(nil), messages...)
}
func summarizeResult(r tool.Result) string {
	if r.Error != nil {
		return r.Error.Code + ": " + clip(r.Error.Message, 300)
	}
	text := r.Output
	if text == "" {
		text = r.Stdout
	}
	if text == "" {
		text = "completed"
	}
	if r.Truncated {
		text += " (truncated)"
	}
	return clip(strings.ReplaceAll(text, "\n", " "), 300)
}
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
func (r *Runner) emit(s *session.Session, e event.Event, persist bool) {
	if persist && e.Type != event.ModelDelta {
		e = r.Store.BufferEvent(s.ID, e)
	}
	if r.Sink != nil {
		r.Sink(e)
	}
}

func (r *Runner) preparePlan(s *session.Session, run *session.Run) {
	if run == nil || run.Plan == nil || run.Plan.Status != taskplan.Active {
		return
	}
	if current := run.Plan.Current(); current != nil {
		r.planNote(s, run, current.ID, "正在从中断点恢复此步骤")
		return
	}
	if next := run.Plan.NextPending(); next != nil {
		r.planStart(s, run, next.ID, "正在处理此步骤")
	}
}

func (r *Runner) planStart(s *session.Session, run *session.Run, id, detail string) {
	if run == nil || run.Plan == nil {
		return
	}
	changed, err := run.Plan.Start(id, detail)
	if err == nil && changed {
		r.emitPlan(s, run, event.PlanUpdated, id, detail)
	}
}

func (r *Runner) planComplete(s *session.Session, run *session.Run, id, detail string) {
	if run == nil || run.Plan == nil {
		return
	}
	changed, err := run.Plan.Complete(id, detail)
	if err == nil && changed {
		r.emitPlan(s, run, event.PlanUpdated, id, detail)
	}
}

func (r *Runner) planNote(s *session.Session, run *session.Run, id, detail string) {
	if run == nil || run.Plan == nil {
		return
	}
	changed, err := run.Plan.Note(id, detail)
	if err == nil && changed {
		r.emitPlan(s, run, event.PlanUpdated, id, detail)
	}
}

func (r *Runner) planFailCurrent(s *session.Session, run *session.Run, detail string) {
	if run == nil || run.Plan == nil || run.Plan.Status != taskplan.Active {
		return
	}
	step := run.Plan.Current()
	if step == nil {
		step = run.Plan.NextPending()
	}
	if step == nil {
		return
	}
	changed, err := run.Plan.Fail(step.ID, detail)
	if err == nil && changed {
		r.emitPlan(s, run, event.PlanUpdated, step.ID, detail)
	}
}

func (r *Runner) emitPlan(s *session.Session, run *session.Run, eventType event.Type, stepID, message string) {
	data, err := json.Marshal(map[string]any{"plan": run.Plan})
	if err != nil {
		return
	}
	e := event.New(eventType, s.ID)
	e.RunID, e.PlanStepID, e.Message, e.Data = run.ID, stepID, message, data
	r.emit(s, e, true)
}

func (r *Runner) fail(ctx context.Context, s *session.Session, message string, cause error) error {
	s.Status = session.Open
	s.LastError = cause.Error()
	runID := s.ActiveRunID
	if run := s.ActiveRun(); run != nil {
		r.planFailCurrent(s, run, cause.Error())
		now := time.Now().UTC()
		run.Status = session.RunFailed
		run.Error = cause.Error()
		run.CompletedAt = &now
		run.UpdatedAt = now
		s.ActiveRunID = ""
	}
	s.GitState = session.GitState(context.WithoutCancel(ctx), s.Workspace)
	s.Summary = contextmgr.StructuredSummary(s)
	e := event.New(event.TaskFailed, s.ID)
	e.RunID = runID
	e.Message = message
	e.Error = cause.Error()
	r.Store.BufferEvent(s.ID, e)
	_ = r.Store.Save(context.WithoutCancel(ctx), s)
	if r.Sink != nil {
		r.Sink(e)
	}
	return cause
}
func (r *Runner) stop(ctx context.Context, s *session.Session, cause error) error {
	s.Status = session.Open
	runID := s.ActiveRunID
	if errors.Is(cause, context.DeadlineExceeded) {
		s.LastError = "total execution timeout"
	} else {
		s.LastError = "cancelled"
	}
	if run := s.ActiveRun(); run != nil {
		if run.Plan != nil {
			if changed, stepID := run.Plan.Cancel("运行已停止"); changed {
				r.emitPlan(s, run, event.PlanUpdated, stepID, "运行已停止")
			}
		}
		now := time.Now().UTC()
		run.Status = session.RunCancelled
		run.Error = s.LastError
		run.CompletedAt = &now
		run.UpdatedAt = now
		s.ActiveRunID = ""
	}
	s.GitState = session.GitState(context.WithoutCancel(ctx), s.Workspace)
	s.Summary = contextmgr.StructuredSummary(s)
	e := event.New(event.TaskCancelled, s.ID)
	e.RunID = runID
	e.Error = s.LastError
	r.Store.BufferEvent(s.ID, e)
	_ = r.Store.Save(context.WithoutCancel(ctx), s)
	if r.Sink != nil {
		r.Sink(e)
	}
	return cause
}
func (r *Runner) snapshotChanges(ctx context.Context, s *session.Session, run *session.Run) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	cmd.Dir = s.Workspace
	b, err := cmd.Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		path = filepath.FromSlash(strings.Trim(path, "\""))
		if strings.HasPrefix(filepath.ToSlash(path), ".gohermit/") {
			continue
		}
		_ = r.Store.SnapshotFile(s, path)
		slashed := filepath.ToSlash(path)
		found := false
		for _, existing := range run.ModifiedFiles {
			if existing == slashed {
				found = true
				break
			}
		}
		if !found {
			run.ModifiedFiles = append(run.ModifiedFiles, slashed)
		}
	}
}
