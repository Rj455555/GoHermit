// Package event defines runtime events independently from presentation.
package event

import (
	"encoding/json"
	"time"
)

type Type string

const (
	TaskStarted        Type = "task_started"
	TurnStarted        Type = "turn_started"
	ModelStarted       Type = "model_started"
	ModelDelta         Type = "model_delta"
	ModelCompleted     Type = "model_completed"
	ToolStarted        Type = "tool_started"
	ToolCompleted      Type = "tool_completed"
	PermissionRequired Type = "permission_required"
	CheckpointSaved    Type = "checkpoint_saved"
	RunVerifying       Type = "run_verifying"
	RunInterrupted     Type = "run_interrupted"
	WorkspaceChanged   Type = "workspace_changed"
	MemoryUpdated      Type = "memory_updated"
	SessionUpdated     Type = "session_updated"
	TaskCompleted      Type = "task_completed"
	TaskFailed         Type = "task_failed"
	TaskCancelled      Type = "task_cancelled"
	MissionStarted     Type = "mission_started"
	MissionCompleted   Type = "mission_completed"
	MissionFailed      Type = "mission_failed"
	WorkItemStarted    Type = "work_item_started"
	WorkItemCompleted  Type = "work_item_completed"
	WorkItemFailed     Type = "work_item_failed"
)

type Event struct {
	Type       Type            `json:"type"`
	Time       time.Time       `json:"time"`
	SessionID  string          `json:"session_id,omitempty"`
	RunID      string          `json:"run_id,omitempty"`
	MissionID  string          `json:"mission_id,omitempty"`
	WorkItemID string          `json:"work_item_id,omitempty"`
	AgentID    string          `json:"agent_id,omitempty"`
	Sequence   uint64          `json:"sequence,omitempty"`
	Turn       int             `json:"turn,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Message    string          `json:"message,omitempty"`
	Error      string          `json:"error,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type Sink func(Event)

func New(t Type, sessionID string) Event {
	return Event{Type: t, Time: time.Now().UTC(), SessionID: sessionID}
}
