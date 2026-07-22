# C2 handoff: approval request storage and lifecycle primitives

## Goal

- Requested outcome: implement ADR 0011's storage and lifecycle primitives only — new `internal/approval` package (pure types/logic), Session schema v5, event types, runcontrol trigger wiring, and a minimal list/decide API. No tool integration (that is C3).
- Scope actually handled: all of the above. No changes to tool builtin/policy; no real tool call creates requests; no Operator operations.

## Completed

- Changes:
  - `internal/approval/` (new, stdlib-only, no repo imports — no cycles): `Request` (request_id/session_id/run_id/optional mission triplet/tool/resource_paths/args_summary/args_digest/policy_fingerprint/plan_revision/created_at/expires_at=+15min/status), `Create` (scope validation: non-empty IDs/tool, ≥1 safe workspace-relative path, sha256 digest of the canonical payload, pending), `IsExpired` (the single expiry predicate), `Decide` (live pending → approved/denied; expired pending → expired + error; terminal → error unchanged), `Consume` (approved → consumed; consumed → error, irreversible/non-reentrant), and the three batch invalidators (run termination, plan-revision change, policy-fingerprint change — pending only, terminal untouched, idempotent).
  - `internal/runcontrol/approvals.go` (new; controller.go unmodified): `ExpireRunApprovals`, `ExpireApprovalsForPlanRevision`, `ExpireApprovalsForPolicy` — thin delegates documented as part of the same transition surface as ApplyTeamEvent/Interrupt/Cancel, so presentation layers wire them at those existing points instead of watching state independently.
  - `internal/session`: `SchemaVersion = 5`, `Session.ApprovalRequests`, header accepts v1–v5, `migrateV4` (v4 checkpoints load with the field empty; unknown versions still fail closed).
  - `internal/event`: `ApprovalRequested/ApprovalDecided/ApprovalExpired/ApprovalConsumed`, traveling the existing durable commit path.
  - `internal/web`: `GET /api/sessions/{id}/approvals?status=pending` (lazy expiry reported as effective status without persisting — documented choice) and `POST /api/sessions/{id}/approvals/{requestID}/decide` (same-origin guard; request resolved within this session only → 404 for unknown/cross-session ids; expired pending persisted as expired + `approval_expired` + 409; success → mutated checkpoint + `approval_decided` through `commitAndPublish`, durable-before-visible; event payloads carry ids/status only).
  - `docs/session-storage.md`: schema v5 paragraph (protocol change documented).
- Files/packages: `internal/approval`, `internal/runcontrol`, `internal/session`, `internal/event`, `internal/web`, `docs`.
- Decisions or ADRs:
  - The approval record lives in the Session checkpoint (session/run-scoped state), not an owner-level side file — mirroring taskplan (pure logic) + session (persistence) + runcontrol (transitions).
  - `Create` is invoked by tests only in C2; no tool-call request production exists yet.
  - Lazy expiry on GET is reported, not persisted; expiry becomes durable on decide/consume paths or batch triggers.

## Verification

- Focused tests: state-machine matrix (decide/consume/expiry), batch triggers (approval + runcontrol), v4→v5 migration (v4 file loads, field empty, versions fail closed), restart persistence (pending stays pending with expires_at intact; consumed stays consumed across a fresh Store), decide durability (approved checkpoint + event readable from a FRESH store before any subscriber), cross-session/unknown request id → failure, cross-origin → 403.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/approval/ ./internal/runcontrol/ ./internal/session/ ./internal/web/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns on the PR.

## Acceptance mapping

1. Deciding an expired pending fails and the request becomes expired: `TestDecideOnExpiredPendingMarksExpiredAndRejects` (+ endpoint 409 path).
2. Consuming a consumed request errors with state unchanged (irreversible, non-reentrant): `TestConsumeIsOneShotIrreversibleAndNonReentrant`.
3. Run termination / Plan revision change / policy fingerprint change batch-expire pending only: `TestBatchTriggersExpireOnlyTheMatchingPendingRequests` + runcontrol trigger tests.
4. Restart restores pending as pending (expires_at intact) and consumed as consumed: `TestApprovalRequestsSurviveRestartWithFreshStore`.
5. Unknown or other-session request_id fails to decide (no cross-session approval): `TestDecideApprovalRejectsUnknownAndCrossSessionIDs`.

## Explicitly not yet covered (as required to state)

**"A denied/expired approval lets the Run continue instead of failing wholesale" has NO behavior test yet.** No real tool call produces approval requests in C2 (`Create` is invoked by tests only), and no fake tool integration was built to force this. The behavior depends on C3's request production in the tool layer and must be verified there.

## Repository state

- Branch: `agent/opc-c2`, based on `origin/main` after ADR 0011 (`7d24a56`).
- Commit/PR: `feat: add approval request storage and lifecycle`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: `Consume` has no caller yet (no `approval_consumed` emitter); the three runcontrol triggers are not yet called from web transition points (nothing creates requests to expire — wire `ExpireRunApprovals` into cancel/interrupt/fail paths when C3 produces requests); GET-reported lazy expiry is not persisted.
- Next concrete step: owner review/merge, then C3 (request production from permission-required tools + denied/expired-continues-run behavior verification).
- Required user input or authority: PR review/merge remains with the owner.
