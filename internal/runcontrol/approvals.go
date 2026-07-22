package runcontrol

import (
	"time"

	"github.com/Rj455555/GoHermit/internal/approval"
)

// Approval expiry triggers belong to the same transition surface as
// ApplyTeamEvent, Interrupt, and Cancel: presentation layers call them at
// those exact transition points instead of watching approval state
// independently. Each is a thin delegate to the pure approval batch
// primitives — the controller adds no new state-listening path, and only
// pending requests of the transitioned scope expire; terminal statuses are
// never touched.

// ExpireRunApprovals invalidates the Run's pending approval requests. Call it
// alongside Interrupt/Cancel and every other Run-termination transition so
// one runcontrol function covers the whole transition.
func ExpireRunApprovals(requests []approval.Request, runID string, now time.Time) []string {
	return approval.ExpireRunPending(requests, runID, now)
}

// ExpireApprovalsForPlanRevision invalidates pending approval requests
// recorded under a stale plan revision. Call it after ApplyTeamEvent (or any
// plan mutation) bumps the Run's plan revision.
func ExpireApprovalsForPlanRevision(requests []approval.Request, runID string, revision int, now time.Time) []string {
	return approval.ExpirePlanRevisionPending(requests, runID, revision, now)
}

// ExpireApprovalsForPolicy invalidates pending approval requests recorded
// under a different policy fingerprint. Call it whenever the effective policy
// configuration changes; an approval is valid only under the fingerprint it
// was requested with.
func ExpireApprovalsForPolicy(requests []approval.Request, fingerprint string, now time.Time) []string {
	return approval.ExpirePolicyPending(requests, fingerprint, now)
}
