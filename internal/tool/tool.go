// Package tool defines the tool registry and bounded executor.
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/model"
)

type Permission string

const (
	PermissionRead    Permission = "read"
	PermissionWrite   Permission = "write"
	PermissionExecute Permission = "execute"
)

type Definition struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	InputSchema      json.RawMessage `json:"input_schema"`
	Permission       Permission      `json:"permission"`
	MutatesWorkspace bool            `json:"mutates_workspace"`
	DefaultTimeout   time.Duration   `json:"default_timeout"`
	MaxOutputBytes   int             `json:"max_output_bytes"`
	AllowConcurrent  bool            `json:"allow_concurrent"`
}
type Call struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type Result struct {
	CallID       string `json:"call_id"`
	Name         string `json:"name"`
	Output       string `json:"output,omitempty"`
	Stdout       string `json:"stdout,omitempty"`
	Stderr       string `json:"stderr,omitempty"`
	Truncated    bool   `json:"truncated"`
	OriginalSize int    `json:"original_size"`
	ReturnedSize int    `json:"returned_size"`
	Error        *Error `json:"error,omitempty"`
}
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type Tool interface {
	Definition() Definition
	Execute(context.Context, Call) (Result, error)
}
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: make(map[string]Tool)} }
func (r *Registry) Register(t Tool) error {
	d := t.Definition()
	if d.Name == "" {
		return errors.New("tool name is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[d.Name]; ok {
		return fmt.Errorf("tool %q already registered", d.Name)
	}
	r.tools[d.Name] = t
	return nil
}
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}
func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Definition())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (r *Registry) ModelDefinitions() []model.ToolDefinition {
	defs := r.Definitions()
	out := make([]model.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, model.ToolDefinition{Name: d.Name, Description: d.Description, Parameters: d.InputSchema})
	}
	return out
}

type Executor struct {
	Registry       *Registry
	DefaultTimeout time.Duration
}

func (e Executor) Execute(ctx context.Context, call Call) (Result, error) {
	t, ok := e.Registry.Get(call.Name)
	if !ok {
		return failure(call, "tool_not_found", "tool is not registered", false), nil
	}
	d := t.Definition()
	timeout := d.DefaultTimeout
	if timeout <= 0 {
		timeout = e.DefaultTimeout
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := t.Execute(runCtx, call)
	if err != nil {
		res.CallID = call.ID
		res.Name = call.Name
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			res.Error = &Error{Code: "tool_timeout", Message: "tool execution timed out", Retryable: true}
		} else if errors.Is(runCtx.Err(), context.Canceled) {
			res.Error = &Error{Code: "cancelled", Message: "tool execution cancelled"}
		} else {
			res.Error = &Error{Code: "tool_error", Message: err.Error()}
		}
		limit := d.MaxOutputBytes
		if limit <= 0 {
			limit = 1 << 20
		}
		truncateResult(&res, limit)
		return res, nil
	}
	res.CallID = call.ID
	res.Name = call.Name
	limit := d.MaxOutputBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	truncateResult(&res, limit)
	return res, nil
}
func failure(c Call, code, message string, retry bool) Result {
	return Result{CallID: c.ID, Name: c.Name, Error: &Error{Code: code, Message: message, Retryable: retry}}
}
func truncateResult(r *Result, limit int) {
	combined := r.Output + r.Stdout + r.Stderr
	r.OriginalSize = len(combined)
	if len(combined) <= limit {
		r.ReturnedSize = len(combined)
		return
	}
	remaining := limit
	clip := func(s *string) {
		if len(*s) > remaining {
			*s = (*s)[:remaining]
			remaining = 0
		} else {
			remaining -= len(*s)
		}
	}
	clip(&r.Output)
	clip(&r.Stdout)
	clip(&r.Stderr)
	r.Truncated = true
	r.ReturnedSize = limit
}
func MarshalResult(r Result) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"error":{"code":"marshal_error","message":"could not encode tool result"}}`
	}
	return string(b)
}
