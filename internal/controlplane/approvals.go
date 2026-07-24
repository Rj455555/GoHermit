package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/session"
)

// approvalBroker is the in-process rendezvous between parked runners and
// DecideApproval (ADR 0011, C3). It exists to preserve THE single-writer
// invariant: while a run is active, only its runner goroutine mutates and
// checkpoints the Session. DecideApproval for an active run therefore never
// loads and saves session state — it delivers the decision through this
// broker (the channel send is the happens-before edge) and commits only the
// approval_decided event through the store's mutex-guarded journal. The
// parked runner wakes, applies Decide/Consume to its own session object, and
// persists at its own checkpoint, so no copy can overwrite another in either
// direction. Sessions without a waiter (interrupted, completed, or recovered
// after a crash) keep the C2 load-decide-save path.
type approvalBroker struct {
	mu      sync.Mutex
	waiters map[string]*approvalWaiter
}

type approvalWaiter struct {
	sessionID string
	ch        chan bool // buffered 1: delivery never blocks the decider
	decided   bool
}

func newApprovalBroker() *approvalBroker {
	return &approvalBroker{waiters: map[string]*approvalWaiter{}}
}

// Wait registers the parked runner's waiter and blocks until DecideApproval
// delivers or ctx (the request deadline, bounded by the run context) is
// done. A duplicate registration fails closed — request IDs carry their
// session ID, so only same-session retries could collide and they must
// become new requests instead.
func (b *approvalBroker) Wait(ctx context.Context, sessionID, requestID string) (bool, error) {
	b.mu.Lock()
	if _, exists := b.waiters[requestID]; exists {
		b.mu.Unlock()
		return false, fmt.Errorf("approval request %q already has a waiter", requestID)
	}
	waiter := &approvalWaiter{sessionID: sessionID, ch: make(chan bool, 1)}
	b.waiters[requestID] = waiter
	b.mu.Unlock()
	defer func() {
		// An undecided waiter (expiry, cancellation) removes itself; a decided
		// one stays so a duplicate decide gets a conflict until the run ends.
		b.mu.Lock()
		if current, ok := b.waiters[requestID]; ok && current == waiter && !waiter.decided {
			delete(b.waiters, requestID)
		}
		b.mu.Unlock()
	}()
	select {
	case approved := <-waiter.ch:
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// waiterFor returns the waiter registered for requestID, or nil.
func (b *approvalBroker) waiterFor(requestID string) *approvalWaiter {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.waiters[requestID]
}

// deliver marks the waiter decided and hands the decision to the parked
// runner. The second return is false when the request was already decided.
func (b *approvalBroker) deliver(requestID string, approve bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	waiter, ok := b.waiters[requestID]
	if !ok || waiter.decided {
		return false
	}
	waiter.decided = true
	waiter.ch <- approve
	return true
}

// Release drops every waiter of a session whose run has ended. After
// release a late decide takes the C2 path, which reads the runner's
// checkpointed terminal status and correctly conflicts.
func (b *approvalBroker) Release(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for requestID, waiter := range b.waiters {
		if waiter.sessionID == sessionID {
			delete(b.waiters, requestID)
		}
	}
}

// ListApprovals answers the session's approval requests filtered by status.
// Expiry is evaluated lazily in memory only: the result reports the
// effective status WITHOUT persisting it, so a read can never mutate the
// durable checkpoint — a lazy expiry becomes durable at the next decide or
// batch trigger that travels the commit path.
func (s *Service) ListApprovals(ctx context.Context, sessionID, filter string) ([]approval.Request, error) {
	sess, err := s.store.Load(ctx, sessionID)
	if err != nil {
		return nil, classified(KindNotFound, err)
	}
	filter = strings.TrimSpace(filter)
	switch approval.Status(filter) {
	case approval.Pending, approval.Approved, approval.Denied, approval.Expired, approval.Consumed:
	default:
		return nil, &Error{Kind: KindInvalid, Message: "status must be pending, approved, denied, expired, or consumed"}
	}
	now := time.Now().UTC()
	items := make([]approval.Request, 0, len(sess.ApprovalRequests))
	for _, req := range sess.ApprovalRequests {
		if approval.IsExpired(&req, now) {
			req.Status = approval.Expired
		}
		if string(req.Status) == filter {
			items = append(items, req)
		}
	}
	return items, nil
}

// DecideApproval records the owner decision for one approval request of this
// session. A request_id from any other session is a KindNotFound: approvals
// never cross session boundaries. Two mutually exclusive paths, chosen by
// whether the broker has a waiter: for an active run the decision rendezvous
// delivers through the broker and commits only the event (the runner is the
// single session writer); for any session without a waiter the C2 path below
// persists through the same durable-before-visible commit path as plan
// approval: mutated checkpoint plus a sequenced approval event, committed
// before anyone is notified. The only race — a decide landing between the
// runner's request checkpoint and its waiter registration — takes the C2
// path and fails closed: the runner keeps waiting until its deadline and the
// request expires (deny by default).
func (s *Service) DecideApproval(ctx context.Context, sessionID, requestID string, approve bool) (*approval.Request, event.Event, error) {
	if requestID == "" || len(requestID) > approval.MaxIDBytes {
		return nil, event.Event{}, &Error{Kind: KindInvalid, Message: "invalid approval request id"}
	}
	sess, err := s.store.Load(ctx, sessionID)
	if err != nil {
		return nil, event.Event{}, classified(KindNotFound, err)
	}
	var target *approval.Request
	for i := range sess.ApprovalRequests {
		if sess.ApprovalRequests[i].RequestID == requestID {
			target = &sess.ApprovalRequests[i]
			break
		}
	}
	if target == nil {
		return nil, event.Event{}, &Error{Kind: KindNotFound, Message: "approval request not found"}
	}
	now := time.Now().UTC()
	if waiter := s.approvals.waiterFor(requestID); waiter != nil {
		// Active-run rendezvous (ADR 0011, C3): the runner goroutine parked on
		// this request is the single writer of its session for the whole run,
		// so this path NEVER loads-and-saves session state — the loaded copy
		// above is read-only validation plus event payload. The decision
		// travels through the broker (the channel send is the happens-before
		// edge), and only the audit event is committed, through the store's
		// mutex-guarded journal against the latest persisted checkpoint. The
		// runner applies Decide/Consume to its own session object and persists
		// at its own checkpoint; nothing can be overwritten in either
		// direction. A crash between the event commit and the runner's
		// checkpoint leaves the request pending, and resume-time expiry (C2b)
		// forces a fresh request — ADR-consistent.
		if waiter.sessionID != sess.ID {
			// Approvals never cross session boundaries, exactly like the C2
			// path below.
			return nil, event.Event{}, &Error{Kind: KindNotFound, Message: "approval request not found"}
		}
		if approval.IsExpired(target, now) {
			// The parked runner's own wait deadline marks and commits the
			// expiry; an expired request can never be decided.
			return nil, event.Event{}, &Error{Kind: KindConflict, Message: "approval request expired", Request: target}
		}
		if !s.approvals.deliver(requestID, approve) {
			return nil, event.Event{}, &Error{Kind: KindConflict, Message: "approval request already decided", Request: target}
		}
		// Response-only status: this copy is never saved by the active path.
		target.Status = approval.Denied
		if approve {
			target.Status = approval.Approved
		}
		decided, commitErr := s.store.CommitDetachedEvent(context.Background(), sess.ID, s.approvalRuntimeEvent(sess, target, event.ApprovalDecided))
		if commitErr != nil {
			return nil, event.Event{}, classified(KindInternal, commitErr)
		}
		s.emit(decided)
		return target, decided, nil
	}
	if approval.IsExpired(target, now) {
		// An expired pending request becomes expired and can never be decided;
		// persist that lazily-detected expiry through the commit path.
		target.Status = approval.Expired
		expired := s.approvalRuntimeEvent(sess, target, event.ApprovalExpired)
		if _, err = s.commitAndPublish(sess, expired); err != nil {
			return nil, event.Event{}, classified(KindInternal, err)
		}
		return nil, event.Event{}, &Error{Kind: KindConflict, Message: "approval request expired", Request: target}
	}
	if err = approval.Decide(target, approve, now); err != nil {
		return nil, event.Event{}, &Error{Kind: KindConflict, Message: err.Error(), Request: target}
	}
	decided, err := s.commitAndPublish(sess, s.approvalRuntimeEvent(sess, target, event.ApprovalDecided))
	if err != nil {
		return nil, event.Event{}, classified(KindInternal, err)
	}
	return target, decided, nil
}

// approvalRuntimeEvent builds the bounded audit event for an approval
// lifecycle transition: request ID, tool name, and status only — never raw
// tool arguments.
func (s *Service) approvalRuntimeEvent(sess *session.Session, req *approval.Request, eventType event.Type) event.Event {
	runtimeEvent := event.New(eventType, sess.ID)
	runtimeEvent.RunID, runtimeEvent.MissionID, runtimeEvent.WorkItemID = req.RunID, req.MissionID, req.WorkItemID
	runtimeEvent.Message = fmt.Sprintf("approval %s %s", req.RequestID, req.Status)
	runtimeEvent.Data, _ = json.Marshal(map[string]any{"request_id": req.RequestID, "tool": req.Tool, "status": req.Status})
	return runtimeEvent
}

// appendApprovalExpiredEvents appends one approval_expired audit event per
// newly expired request ID, reusing the bounded approvalRuntimeEvent payload.
// The runcontrol Expire* triggers return the IDs they transitioned (terminal
// requests are never touched), so a trigger that expired nothing appends
// nothing and the caller's commit carries zero extra events.
func (s *Service) appendApprovalExpiredEvents(sess *session.Session, ids []string, runtimeEvents []event.Event) []event.Event {
	for _, id := range ids {
		for i := range sess.ApprovalRequests {
			if sess.ApprovalRequests[i].RequestID == id {
				runtimeEvents = append(runtimeEvents, s.approvalRuntimeEvent(sess, &sess.ApprovalRequests[i], event.ApprovalExpired))
				break
			}
		}
	}
	return runtimeEvents
}
