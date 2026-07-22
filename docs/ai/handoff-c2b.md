# C2b handoff: wire approval-expiry triggers into real transition paths

## Goal

- Requested outcome: **call-chain gap fix found when reviewing C2 (not new scope)** — the three batch-expiry functions in `internal/runcontrol/approvals.go` were only invoked by tests. Wire them into the real Run-termination, Plan-revision, and policy-fingerprint change paths, reusing C2's persistence+event pattern.
- Scope actually handled: one shared expire-and-publish helper plus wiring at every terminal path, the team-event sink, and run launch; seven new end-to-end tests. Only `internal/web/` changed.

## Completed

- Changes:
  - `Server.appendApprovalExpiredEvents(sess, ids, events)` — one bounded `approval_expired` event per newly expired id via C2's `approvalRuntimeEvent` (payload: request_id/tool/status only), committed through the existing `commitAndPublish`/`commitAndPublishMany` (durable-before-visible); zero expirations → zero extra events.
  - Run termination wiring (ADR 0011 treats interruption as termination — a resumed run must request fresh approvals): team normal completion (`runTeam`), team cancel + coordinator deadline (`finishTeamCancelled`), launch/build failure (`failLaunchedRun`), queued review-plan cancel (`cancelSessionRun`, real API path), single-agent completion/failure/cancel/deadline (the `launchSessionRun` goroutine expires after every `Runner.Run` return), and crash-recovered runs at `resumeSessionRun`.
  - Plan revision: the team-event sink captures `run.Plan.Revision` around `ApplyTeamEvent`; on change, `ExpireApprovalsForPlanRevision` runs in the same commit batch — requests on the new revision survive.
  - Policy fingerprint: `session.ConfigDigest` (no better existing signal — config is only re-read at creation/launch). `launchSessionRun` rebuilds the runtime exactly like `createSession`, compares digests, and on mismatch expires old-fingerprint pending requests synchronously before the run executes. The stored digest is deliberately NOT rewritten (provenance stays with creation; C3 owns fingerprint stamping).
- Files/packages: `internal/web` only.
- Decisions or ADRs:
  - Legacy `POST /api/run` intentionally unwired: it creates a fresh session per request, so no approvals can ever exist there.
  - On commit failure after in-memory expiry, the existing house pattern (`sess.LastError`) is followed — same tradeoff as the pre-existing team paths.

## Verification

- Focused tests (all end-to-end, not pure-function): `TestCancelQueuedReviewRunExpiresItsPendingApprovals` (real cancel API; pending ~14min from TTL expire immediately; 2 durable events from a fresh store; terminal and other-run requests untouched), `TestPlanRevisionBumpExpiresStalePendingApprovals` (real team sink; revision-1 request expires on the 1→2 bump, revision-2 survives, completion expires it later), `TestLaunchExpiresApprovalsWhenPolicyFingerprintChanges` (config change between creation and launch), `TestTeamTerminationExpiresPendingApprovals` (deadline → interrupted, cancel → cancelled), `TestTeamRunCompletionExpiresPendingApprovals`, `TestSingleAgentRunCompletionExpiresPendingApprovals`, `TestFailLaunchedRunExpiresPendingApprovals` — every test seeds approved/denied/consumed requests and asserts they are byte-unchanged.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/web/ ./internal/runcontrol/ ./internal/session/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns on the PR.

## Acceptance mapping

1. Cancelling a run with pending approvals expires them immediately (E2E, real API path): `TestCancelQueuedReviewRunExpiresItsPendingApprovals`.
2. Plan-revision change expires old-revision pending, spares new-revision: `TestPlanRevisionBumpExpiresStalePendingApprovals`.
3. Policy fingerprint change expires old-fingerprint pending: `TestLaunchExpiresApprovalsWhenPolicyFingerprintChanges` (ConfigDigest as the signal).
4. Terminal requests unaffected by every trigger: asserted in all of the above.

## Repository state

- Branch: `agent/opc-c2b`, based on `origin/main` after C2 (`cf046aa`).
- Commit/PR: `feat: wire approval expiry triggers into run transitions`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: single-agent mid-run plan-revision bumps (inside `internal/agent`) are not wired — wiring there requires agent-package changes and was stopped per scope; stale single-agent requests still expire at run termination. Startup `Store.Recover` does not emit expiry events at boot (covered at resume instead).
- Next concrete step: owner review/merge, then C3 (request production from permission-required tools; verify denied/expired-continues-run behavior).
- Required user input or authority: PR review/merge remains with the owner.
