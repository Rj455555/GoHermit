package loop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Rj455555/GoHermit/internal/owner"
)

// Status is the invocation's outer-loop scheduling and binding state. The
// Session/Run remains the source of truth for execution; the invocation only
// records dispatch and binding.
type Status string

const (
	Prepared   Status = "prepared"
	Dispatched Status = "dispatched"
	Attached   Status = "attached"
	Completed  Status = "completed"
	// Terminal states are final; no transition may leave them.
	Skipped   Status = "skipped"
	Blocked   Status = "blocked"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// Terminal reports whether the status is final.
func (s Status) Terminal() bool {
	switch s {
	case Completed, Skipped, Blocked, Failed, Cancelled:
		return true
	}
	return false
}

// Invocation is one outer-loop run of a loop definition. It binds at most
// one Session Run and embeds an immutable snapshot of the definition
// revision it was created from.
type Invocation struct {
	ID                 string     `json:"id"`
	LoopID             string     `json:"loop_id"`
	DefinitionRevision int        `json:"definition_revision"`
	DefinitionSnapshot Definition `json:"definition_snapshot"`
	Trigger            string     `json:"trigger"`
	TaskSnapshot       string     `json:"task_snapshot"`
	SessionID          string     `json:"session_id,omitempty"`
	RunID              string     `json:"run_id,omitempty"`
	Status             Status     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	FailureCode        string     `json:"failure_code,omitempty"`
	FailureSummary     string     `json:"failure_summary,omitempty"`
}

// NewInvocation prepares an invocation from a validated definition. The
// definition is deep-copied into the snapshot and its revision recorded, so
// later edits of the source definition never change this invocation.
func NewInvocation(def Definition, trigger, taskSnapshot string, now time.Time) (Invocation, error) {
	if err := ValidateDefinition(def); err != nil {
		return Invocation{}, err
	}
	trigger = clean(trigger)
	if trigger != TriggerManual {
		return Invocation{}, fmt.Errorf("unsupported trigger %q", trigger)
	}
	if clean(taskSnapshot) == "" {
		return Invocation{}, errors.New("invocation task snapshot is required")
	}
	if len(taskSnapshot) > MaxTextBytes {
		return Invocation{}, errors.New("invocation task snapshot exceeds size limit")
	}
	id, err := newID()
	if err != nil {
		return Invocation{}, fmt.Errorf("generate invocation id: %w", err)
	}
	return Invocation{
		ID:                 id,
		LoopID:             def.ID,
		DefinitionRevision: def.Revision,
		DefinitionSnapshot: deepCopyDefinition(def),
		Trigger:            trigger,
		TaskSnapshot:       taskSnapshot,
		Status:             Prepared,
		CreatedAt:          now.UTC(),
	}, nil
}

// Dispatch moves prepared → dispatched.
func (inv *Invocation) Dispatch() error {
	if inv.Status != Prepared {
		return fmt.Errorf("invocation cannot dispatch from %s", inv.Status)
	}
	inv.Status = Dispatched
	return nil
}

// Attach moves dispatched → attached, binding the invocation to its single
// Session Run and stamping the start time.
func (inv *Invocation) Attach(sessionID, runID string, now time.Time) error {
	if inv.Status != Dispatched {
		return fmt.Errorf("invocation cannot attach from %s", inv.Status)
	}
	if clean(sessionID) == "" || clean(runID) == "" {
		return errors.New("invocation attach requires session and run ids")
	}
	started := now.UTC()
	inv.SessionID, inv.RunID, inv.StartedAt, inv.Status = sessionID, runID, &started, Attached
	return nil
}

// Complete moves attached → completed.
func (inv *Invocation) Complete(now time.Time) error {
	if inv.Status != Attached {
		return fmt.Errorf("invocation cannot complete from %s", inv.Status)
	}
	inv.finish(Completed, now)
	return nil
}

// Skip moves prepared → skipped, for invocations dropped before dispatch.
func (inv *Invocation) Skip(summary string, now time.Time) error {
	if inv.Status != Prepared {
		return fmt.Errorf("invocation cannot skip from %s", inv.Status)
	}
	inv.FailureSummary = clean(summary)
	inv.finish(Skipped, now)
	return nil
}

// Block moves prepared → blocked, for invocations refused by a pre-launch
// gate (budget, approval, workspace policy).
func (inv *Invocation) Block(code, summary string, now time.Time) error {
	if inv.Status != Prepared {
		return fmt.Errorf("invocation cannot block from %s", inv.Status)
	}
	if clean(code) == "" {
		return errors.New("invocation block requires a failure code")
	}
	inv.FailureCode, inv.FailureSummary = clean(code), clean(summary)
	inv.finish(Blocked, now)
	return nil
}

// Fail moves dispatched or attached → failed.
func (inv *Invocation) Fail(code, summary string, now time.Time) error {
	if inv.Status != Dispatched && inv.Status != Attached {
		return fmt.Errorf("invocation cannot fail from %s", inv.Status)
	}
	if clean(code) == "" {
		return errors.New("invocation fail requires a failure code")
	}
	inv.FailureCode, inv.FailureSummary = clean(code), clean(summary)
	inv.finish(Failed, now)
	return nil
}

// Cancel moves any non-terminal state → cancelled.
func (inv *Invocation) Cancel(summary string, now time.Time) error {
	if inv.Status.Terminal() {
		return fmt.Errorf("invocation cannot cancel from %s", inv.Status)
	}
	inv.FailureSummary = clean(summary)
	inv.finish(Cancelled, now)
	return nil
}

func (inv *Invocation) finish(status Status, now time.Time) {
	finished := now.UTC()
	inv.Status, inv.FinishedAt = status, &finished
}

// ValidateInvocation enforces the invocation contract for persistence: all
// fields bounded, a known status, a valid embedded snapshot, and failure
// detail present exactly when the status carries it.
func ValidateInvocation(inv Invocation) error {
	if err := validateID("invocation id", inv.ID); err != nil {
		return err
	}
	if err := validateID("loop id", inv.LoopID); err != nil {
		return err
	}
	switch inv.Status {
	case Prepared, Dispatched, Attached, Completed, Skipped, Blocked, Failed, Cancelled:
	default:
		return fmt.Errorf("unsupported invocation status %q", inv.Status)
	}
	if inv.Trigger != TriggerManual {
		return fmt.Errorf("unsupported trigger %q", inv.Trigger)
	}
	if clean(inv.TaskSnapshot) == "" || len(inv.TaskSnapshot) > MaxTextBytes {
		return errors.New("invocation task snapshot is required and bounded")
	}
	if inv.DefinitionRevision != inv.DefinitionSnapshot.Revision {
		return errors.New("invocation definition revision does not match its snapshot")
	}
	if err := ValidateDefinition(inv.DefinitionSnapshot); err != nil {
		return fmt.Errorf("invocation snapshot: %w", err)
	}
	for _, field := range []SecretField{
		{"session id", inv.SessionID},
		{"run id", inv.RunID},
		{"failure code", inv.FailureCode},
		{"failure summary", inv.FailureSummary},
	} {
		if len(field.Value) > MaxTextBytes {
			return fmt.Errorf("invocation %s exceeds size limit", field.Label)
		}
		if owner.LooksSecret(field.Value) {
			return fmt.Errorf("invocation %s must not contain credentials or tokens", field.Label)
		}
	}
	if inv.Status == Blocked || inv.Status == Failed {
		if clean(inv.FailureCode) == "" {
			return fmt.Errorf("%s invocation requires a failure code", inv.Status)
		}
	}
	return nil
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b), nil
}
