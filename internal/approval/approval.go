// Package approval defines the scoped, expiring approval contract for
// side-effecting calls (ADR 0011): pure types and pure state transitions,
// with no IO and no dependency on session, web, or tool code. The record is
// durable — it persists inside the Session checkpoint — and every transition
// is reachable from tests without any tool call producing requests.
package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TTL is the fixed approval-request lifetime (ADR 0011: 15 minutes).
const TTL = 15 * time.Minute

const (
	MaxResourcePaths = 16
	MaxPathBytes     = 1 << 10
	MaxSummaryBytes  = 2 << 10
	MaxIDBytes       = 256
)

type Status string

const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Denied   Status = "denied"
	Expired  Status = "expired"
	Consumed Status = "consumed"
)

// Request is one durable, bounded, one-shot approval request. It binds
// exactly one call: the named tool, the named resource path(s), and the
// argument digest. Raw arguments are never stored; only a redacted summary
// and a digest of the canonical payload.
type Request struct {
	RequestID         string    `json:"request_id"`
	SessionID         string    `json:"session_id"`
	RunID             string    `json:"run_id"`
	MissionID         string    `json:"mission_id,omitempty"`
	WorkItemID        string    `json:"work_item_id,omitempty"`
	Role              string    `json:"role,omitempty"`
	Tool              string    `json:"tool"`
	ResourcePaths     []string  `json:"resource_paths"`
	ArgsSummary       string    `json:"args_summary"`
	ArgsDigest        string    `json:"args_digest"`
	PolicyFingerprint string    `json:"policy_fingerprint"`
	PlanRevision      int       `json:"plan_revision"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	Status            Status    `json:"status"`
}

// CreateSpec is the input to Create. ArgsPayload is the canonical argument
// payload; only its sha256 digest is kept. RequestID is optional and is
// derived from the digest when empty (Create performs no IO). TTL optionally
// shortens the fixed 15-minute lifetime (e.g. for tests): zero or any value
// outside (0, TTL] falls back to the package TTL, so a caller can never
// extend the contract lifetime.
type CreateSpec struct {
	RequestID         string
	SessionID         string
	RunID             string
	MissionID         string
	WorkItemID        string
	Role              string
	Tool              string
	ResourcePaths     []string
	ArgsSummary       string
	ArgsPayload       string
	PolicyFingerprint string
	PlanRevision      int
	TTL               time.Duration
}

// Create validates the scope, computes the argument digest, stamps the
// expiry (CreateSpec.TTL, bounded by the fixed 15-minute contract lifetime),
// and returns a pending request. Real tool calls produce requests through it
// since C3.
func Create(spec CreateSpec, now time.Time) (Request, error) {
	if strings.TrimSpace(spec.SessionID) == "" || strings.TrimSpace(spec.RunID) == "" || strings.TrimSpace(spec.Tool) == "" {
		return Request{}, errors.New("approval request requires session, run, and tool")
	}
	if len(spec.ResourcePaths) == 0 {
		return Request{}, errors.New("approval request requires at least one resource path")
	}
	if len(spec.ResourcePaths) > MaxResourcePaths {
		return Request{}, errors.New("approval request exceeds the resource path limit")
	}
	paths := make([]string, 0, len(spec.ResourcePaths))
	for _, path := range spec.ResourcePaths {
		path = strings.TrimSpace(path)
		if err := validateResourcePath(path); err != nil {
			return Request{}, err
		}
		paths = append(paths, path)
	}
	if strings.TrimSpace(spec.PolicyFingerprint) == "" {
		return Request{}, errors.New("approval request requires a policy fingerprint")
	}
	if spec.PlanRevision < 1 {
		return Request{}, errors.New("approval request requires the current plan revision")
	}
	for _, id := range []string{spec.RequestID, spec.SessionID, spec.RunID, spec.MissionID, spec.WorkItemID, spec.Role, spec.Tool} {
		if len(id) > MaxIDBytes {
			return Request{}, errors.New("approval request identity field exceeds the size limit")
		}
	}
	now = now.UTC()
	ttl := spec.TTL
	if ttl <= 0 || ttl > TTL {
		ttl = TTL
	}
	digest := sha256.Sum256([]byte(spec.ArgsPayload))
	requestID := strings.TrimSpace(spec.RequestID)
	if requestID == "" {
		requestID = "apr-" + hex.EncodeToString(digest[:])[:16]
	}
	return Request{
		RequestID:         requestID,
		SessionID:         spec.SessionID,
		RunID:             spec.RunID,
		MissionID:         spec.MissionID,
		WorkItemID:        spec.WorkItemID,
		Role:              spec.Role,
		Tool:              spec.Tool,
		ResourcePaths:     paths,
		ArgsSummary:       clip(spec.ArgsSummary),
		ArgsDigest:        hex.EncodeToString(digest[:]),
		PolicyFingerprint: spec.PolicyFingerprint,
		PlanRevision:      spec.PlanRevision,
		CreatedAt:         now,
		ExpiresAt:         now.Add(ttl),
		Status:            Pending,
	}, nil
}

func validateResourcePath(path string) error {
	if path == "" || len(path) > MaxPathBytes {
		return errors.New("approval resource path must be non-empty and bounded")
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || strings.ContainsAny(path, "\x00") {
		return fmt.Errorf("approval resource path %q must be workspace-relative", path)
	}
	if len(path) >= 2 && path[1] == ':' {
		return fmt.Errorf("approval resource path %q must be workspace-relative", path)
	}
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("approval resource path %q escapes the workspace", path)
		}
	}
	return nil
}

// IsExpired is THE single expiry predicate: a pending request whose deadline
// has passed. Every decision, consumption, and batch trigger evaluates expiry
// through it.
func IsExpired(req *Request, now time.Time) bool {
	return req != nil && req.Status == Pending && !now.Before(req.ExpiresAt)
}

// Decide records the owner decision. Only a pending, unexpired request can be
// decided: an expired pending request becomes expired and errors (it can never
// be approved); terminal statuses error with the state unchanged.
func Decide(req *Request, approve bool, now time.Time) error {
	if req == nil {
		return errors.New("approval request is required")
	}
	if req.Status != Pending {
		return fmt.Errorf("approval request %q is already %s", req.RequestID, req.Status)
	}
	if IsExpired(req, now) {
		req.Status = Expired
		return fmt.Errorf("approval request %q expired and can no longer be decided", req.RequestID)
	}
	if approve {
		req.Status = Approved
	} else {
		req.Status = Denied
	}
	return nil
}

// Consume marks an approved request as spent by exactly one execution. It is
// irreversible and non-reentrant: an already consumed (or otherwise terminal)
// request errors with its state unchanged. An approved request whose deadline
// has passed becomes expired instead of consumable.
func Consume(req *Request, now time.Time) error {
	if req == nil {
		return errors.New("approval request is required")
	}
	if req.Status != Approved {
		return fmt.Errorf("approval request %q is %s, not approved", req.RequestID, req.Status)
	}
	if !now.Before(req.ExpiresAt) {
		req.Status = Expired
		return fmt.Errorf("approval request %q expired before execution", req.RequestID)
	}
	req.Status = Consumed
	return nil
}

// ExpireRunPending invalidates every pending request of a terminating Run.
// Terminal statuses are untouched; returns the IDs that changed.
func ExpireRunPending(requests []Request, runID string, now time.Time) []string {
	var expired []string
	for i := range requests {
		if requests[i].Status == Pending && requests[i].RunID == runID {
			requests[i].Status = Expired
			expired = append(expired, requests[i].RequestID)
		}
	}
	return expired
}

// ExpirePlanRevisionPending invalidates pending requests of the Run recorded
// under a stale plan revision. Terminal statuses are untouched.
func ExpirePlanRevisionPending(requests []Request, runID string, currentRevision int, now time.Time) []string {
	var expired []string
	for i := range requests {
		if requests[i].Status == Pending && requests[i].RunID == runID && requests[i].PlanRevision != currentRevision {
			requests[i].Status = Expired
			expired = append(expired, requests[i].RequestID)
		}
	}
	return expired
}

// ExpirePolicyPending invalidates pending requests recorded under a different
// policy fingerprint. Terminal statuses are untouched.
func ExpirePolicyPending(requests []Request, currentFingerprint string, now time.Time) []string {
	var expired []string
	for i := range requests {
		if requests[i].Status == Pending && requests[i].PolicyFingerprint != currentFingerprint {
			requests[i].Status = Expired
			expired = append(expired, requests[i].RequestID)
		}
	}
	return expired
}

func clip(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > MaxSummaryBytes {
		return value[:MaxSummaryBytes]
	}
	return value
}
