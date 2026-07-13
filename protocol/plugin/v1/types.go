// Package v1 defines the GoHermit plugin JSON-RPC 2.0 wire protocol.
package v1

import "encoding/json"

const ProtocolVersion = "1.0"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type Capabilities struct {
	Tools          bool `json:"tools"`
	Cancellation   bool `json:"cancellation"`
	Health         bool `json:"health"`
	MaxConcurrency int  `json:"max_concurrency"`
}
type InitializeRequest struct {
	ProtocolVersion string `json:"protocol_version"`
	ClientName      string `json:"client_name"`
	MaxMessageSize  int    `json:"max_message_size"`
}
type InitializeResponse struct {
	ProtocolVersion string       `json:"protocol_version"`
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Capabilities    Capabilities `json:"capabilities"`
}
type ToolDefinition struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	InputSchema      json.RawMessage `json:"input_schema"`
	TimeoutMS        int64           `json:"timeout_ms,omitempty"`
	Permission       string          `json:"permission,omitempty"`
	MutatesWorkspace bool            `json:"mutates_workspace,omitempty"`
}
type ToolsListResponse struct {
	Tools []ToolDefinition `json:"tools"`
}
type ExecuteRequest struct {
	RequestID string          `json:"request_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	TimeoutMS int64           `json:"timeout_ms"`
}
type ExecuteResponse struct {
	Output    string `json:"output,omitempty"`
	IsError   bool   `json:"is_error"`
	ErrorCode string `json:"error_code,omitempty"`
}
type CancelRequest struct {
	RequestID string `json:"request_id"`
}
type HealthResponse struct {
	Status string `json:"status"`
}

const (
	ErrorParse          = -32700
	ErrorInvalidRequest = -32600
	ErrorMethodNotFound = -32601
	ErrorInvalidParams  = -32602
	ErrorInternal       = -32603
	ErrorCancelled      = -32001
	ErrorTimeout        = -32002
	ErrorToolNotFound   = -32003
	ErrorTooLarge       = -32004
)
