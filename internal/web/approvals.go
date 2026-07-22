package web

import (
	"context"
	"fmt"
	"sync"
)

// approvalBroker is the in-process rendezvous between parked runners and
// decideApproval (ADR 0011, C3). It exists to preserve THE single-writer
// invariant: while a run is active, only its runner goroutine mutates and
// checkpoints the Session. decideApproval for an active run therefore never
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

// Wait registers the parked runner's waiter and blocks until decideApproval
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
