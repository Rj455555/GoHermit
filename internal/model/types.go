// Package model defines provider-neutral model types and providers.
package model

import (
	"context"
	"encoding/json"
	"fmt"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

type GenerateRequest struct {
	Model    string            `json:"model"`
	Messages []Message         `json:"messages"`
	Tools    []ToolDefinition  `json:"tools,omitempty"`
	Stream   bool              `json:"stream"`
	OnStream func(StreamEvent) `json:"-"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
type GenerateResponse struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
	Usage        Usage   `json:"usage"`
}
type StreamEvent struct {
	Delta    string    `json:"delta,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	Done     bool      `json:"done,omitempty"`
}
type Capabilities struct {
	Streaming bool `json:"streaming"`
	ToolCalls bool `json:"tool_calls"`
}

type Provider interface {
	Generate(context.Context, GenerateRequest) (GenerateResponse, error)
	Capabilities() Capabilities
}

type ErrorKind string

const (
	ErrorAuthentication ErrorKind = "authentication"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorInvalidRequest ErrorKind = "invalid_request"
	ErrorUnavailable    ErrorKind = "unavailable"
	ErrorTimeout        ErrorKind = "timeout"
	ErrorProtocol       ErrorKind = "protocol"
)

type ProviderError struct {
	Kind      ErrorKind
	Status    int
	Retryable bool
	Message   string
	Cause     error
}

func (e *ProviderError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("model provider %s error (HTTP %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("model provider %s error: %s", e.Kind, e.Message)
}
func (e *ProviderError) Unwrap() error { return e.Cause }
