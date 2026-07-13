// Package agent implements the bounded single-agent execution loop.
package agent

import (
	"context"
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
	"github.com/Rj455555/GoHermit/internal/tool"
)

type Config struct {
	MaxTurns                   int
	Timeout                    time.Duration
	Model                      string
	Stream                     bool
	CheckpointEveryTurns       int
	CheckpointOnToolCompletion bool
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
	s.Status = session.Running
	r.emit(s, event.New(event.TaskStarted, s.ID), true)
	messages := s.RecentMessages
	if len(messages) == 0 {
		var compressed bool
		messages, compressed = r.Context.Build(s.Workspace, s.Goal, s.Summary, nil)
		if compressed {
			s.Summary = contextmgr.StructuredSummary(s)
		}
	}
	for turn := s.Turns + 1; turn <= r.Config.MaxTurns; turn++ {
		if err := runCtx.Err(); err != nil {
			return r.stop(runCtx, s, err)
		}
		s.Turns = turn
		e := event.New(event.TurnStarted, s.ID)
		e.Turn = turn
		r.emit(s, e, true)
		e = event.New(event.ModelStarted, s.ID)
		e.Turn = turn
		r.emit(s, e, true)
		response, err := r.Provider.Generate(runCtx, model.GenerateRequest{Model: r.Config.Model, Messages: messages, Tools: r.Executor.Registry.ModelDefinitions(), Stream: r.Config.Stream, OnStream: func(delta model.StreamEvent) {
			if delta.Delta == "" {
				return
			}
			ev := event.New(event.ModelDelta, s.ID)
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
		e = event.New(event.ModelCompleted, s.ID)
		e.Turn = turn
		e.Message = response.FinishReason
		r.emit(s, e, true)
		messages = append(messages, response.Message)
		if len(response.Message.ToolCalls) == 0 {
			s.Status = session.Completed
			s.CompletedSteps = append(s.CompletedSteps, "Model completed the task")
			s.PendingSteps = nil
			s.RecentMessages = boundedMessages(messages)
			s.Summary = contextmgr.StructuredSummary(s)
			s.GitState = session.GitState(runCtx, s.Workspace)
			done := event.New(event.TaskCompleted, s.ID)
			done.Turn = turn
			done.Message = response.Message.Content
			r.Store.BufferEvent(s.ID, done)
			if err := r.Store.Save(context.WithoutCancel(runCtx), s); err != nil {
				return err
			}
			if r.Sink != nil {
				r.Sink(done)
			}
			return nil
		}
		for _, call := range response.Message.ToolCalls {
			e = event.New(event.ToolStarted, s.ID)
			e.Turn = turn
			e.Tool = call.Name
			e.Data = call.Arguments
			r.emit(s, e, true)
			result, _ := r.Executor.Execute(runCtx, tool.Call{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
			if result.Error != nil && (result.Error.Code == "confirmation_required" || result.Error.Code == "blocked") {
				pe := event.New(event.PermissionRequired, s.ID)
				pe.Turn = turn
				pe.Tool = call.Name
				pe.Message = result.Error.Message
				r.emit(s, pe, true)
			}
			content := tool.MarshalResult(result)
			messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: content})
			summary := summarizeResult(result)
			s.ToolCalls = appendBoundedTool(s.ToolCalls, session.ToolRecord{Time: time.Now().UTC(), CallID: call.ID, Name: call.Name, Summary: summary, IsError: result.Error != nil}, 500)
			e = event.New(event.ToolCompleted, s.ID)
			e.Turn = turn
			e.Tool = call.Name
			e.Message = summary
			r.emit(s, e, true)
			if defTool, ok := r.Executor.Registry.Get(call.Name); ok && defTool.Definition().MutatesWorkspace {
				r.snapshotChanges(runCtx, s)
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
		r.Store.BufferEvent(s.ID, e)
	}
	if r.Sink != nil {
		r.Sink(e)
	}
}
func (r *Runner) fail(ctx context.Context, s *session.Session, message string, cause error) error {
	s.Status = session.Failed
	s.LastError = cause.Error()
	s.GitState = session.GitState(context.WithoutCancel(ctx), s.Workspace)
	s.Summary = contextmgr.StructuredSummary(s)
	e := event.New(event.TaskFailed, s.ID)
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
	s.Status = session.Cancelled
	if errors.Is(cause, context.DeadlineExceeded) {
		s.LastError = "total execution timeout"
	} else {
		s.LastError = "cancelled"
	}
	s.GitState = session.GitState(context.WithoutCancel(ctx), s.Workspace)
	s.Summary = contextmgr.StructuredSummary(s)
	e := event.New(event.TaskCancelled, s.ID)
	e.Error = s.LastError
	r.Store.BufferEvent(s.ID, e)
	_ = r.Store.Save(context.WithoutCancel(ctx), s)
	if r.Sink != nil {
		r.Sink(e)
	}
	return cause
}
func (r *Runner) snapshotChanges(ctx context.Context, s *session.Session) {
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
	}
}
