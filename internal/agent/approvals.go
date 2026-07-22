package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
)

// ApprovalDecisions delivers owner decisions for parked approval requests
// (ADR 0011). The presentation layer implements it (the web server uses an
// in-process broker); the runner stays presentation-free. A nil interface
// fails closed: no request is created and the denial data goes to the model.
type ApprovalDecisions interface {
	// Wait parks the runner until the decision for requestID arrives, the
	// request deadline in ctx passes, or the run is interrupted. The
	// sessionID binds the waiter to its session so cross-session decisions
	// are rejected by the delivering side.
	Wait(ctx context.Context, sessionID, requestID string) (approved bool, err error)
}

// awaitApproval handles one parked (approval-required) tool call on the
// runner goroutine — the single writer of the session for the whole run. It
// creates the durable request, waits for the owner decision, applies
// Decide/Consume to its own session object, checkpoints through the normal
// emit path, and returns the final tool result: the re-executed call on
// approval, or structured denial data so the run continues without the side
// effect. A non-nil error is a run-level interruption handled by the caller
// exactly like today's executor/model interruptions.
func (r *Runner) awaitApproval(runCtx context.Context, s *session.Session, run *session.Run, turn int, call model.ToolCall, parked tool.Result) (tool.Result, error) {
	paths := []string{"<command>"}
	summary := ""
	if parked.Approval != nil {
		if len(parked.Approval.Paths) > 0 {
			paths = parked.Approval.Paths
		}
		summary = parked.Approval.Summary
	}
	planRevision := 1
	if run.Plan != nil {
		planRevision = run.Plan.Revision
	}
	// The request ID is unique per attempt within this session so a retried
	// command after a denial is a fresh request, never a collision.
	req, err := approval.Create(approval.CreateSpec{
		RequestID:         fmt.Sprintf("apr-%s-%d", s.ID, len(s.ApprovalRequests)),
		SessionID:         s.ID,
		RunID:             run.ID,
		Tool:              call.Name,
		ResourcePaths:     paths,
		ArgsSummary:       summary,
		ArgsPayload:       string(call.Arguments),
		PolicyFingerprint: s.ConfigDigest,
		PlanRevision:      planRevision,
		TTL:               r.Config.ApprovalTTL,
	}, time.Now().UTC())
	if err != nil {
		// Fail closed: a request that cannot be created is a denial.
		return approvalDenialResult(call, "approval request could not be created: "+err.Error()), nil
	}
	s.ApprovalRequests = append(s.ApprovalRequests, req)
	if err = r.emit(s, r.approvalEvent(s, run, turn, event.ApprovalRequested, &req), true); err != nil {
		return tool.Result{}, err
	}

	waitCtx, cancel := context.WithDeadline(runCtx, req.ExpiresAt)
	approved, waitErr := r.Approvals.Wait(waitCtx, s.ID, req.RequestID)
	cancel()
	target := &s.ApprovalRequests[len(s.ApprovalRequests)-1]
	if waitErr != nil {
		if approval.IsExpired(target, time.Now().UTC()) {
			// Unattended default is deny: the deadline passed without a
			// decision, so the request expires and the model gets structured
			// denial data; the run continues.
			target.Status = approval.Expired
			if err = r.emit(s, r.approvalEvent(s, run, turn, event.ApprovalExpired, target), true); err != nil {
				return tool.Result{}, err
			}
			return approvalDenialResult(call, "approval request "+req.RequestID+" expired without a decision"), nil
		}
		return tool.Result{}, waitErr
	}
	if err = approval.Decide(target, approved, time.Now().UTC()); err != nil {
		// The decision raced the request's own expiry; fail closed as denied.
		return approvalDenialResult(call, err.Error()), nil
	}
	if err = r.emit(s, r.approvalEvent(s, run, turn, event.ApprovalDecided, target), true); err != nil {
		return tool.Result{}, err
	}
	if !approved {
		return approvalDenialResult(call, "the owner denied approval request "+req.RequestID), nil
	}
	if err = approval.Consume(target, time.Now().UTC()); err != nil {
		// Consume of a freshly decided, unexpired approval is impossible;
		// if it ever happens, fail closed instead of executing.
		return approvalDenialResult(call, "approval could not be consumed: "+err.Error()), nil
	}
	// The approval is durably consumed before the side effect runs, and the
	// executor's override path marks exactly this one invocation approved.
	if err = r.emit(s, r.approvalEvent(s, run, turn, event.ApprovalConsumed, target), true); err != nil {
		return tool.Result{}, err
	}
	return r.Executor.ExecuteApproved(runCtx, tool.Call{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
}

// approvalDenialResult is the structured tool result every non-executed
// parked call resolves to (ADR 0011: denial is data, never a blind failure).
func approvalDenialResult(call model.ToolCall, message string) tool.Result {
	return tool.Result{CallID: call.ID, Name: call.Name, Error: &tool.Error{Code: tool.CodeApprovalDenied, Message: message}}
}

// approvalEvent builds the bounded audit event for an approval lifecycle
// transition: request ID, tool name, and status only — never raw arguments.
func (r *Runner) approvalEvent(s *session.Session, run *session.Run, turn int, eventType event.Type, req *approval.Request) event.Event {
	e := event.New(eventType, s.ID)
	e.RunID, e.Turn = run.ID, turn
	e.Message = fmt.Sprintf("approval %s %s", req.RequestID, req.Status)
	e.Data, _ = json.Marshal(map[string]any{"request_id": req.RequestID, "tool": req.Tool, "status": req.Status})
	return e
}
