# C3 handoff: approval requests from real tool calls + concurrency-safe rendezvous

## Goal

- Requested outcome: wire ADR 0011 to a concrete tool (shell `ConfirmationRequired`) and make decisions delivered during an active Run concurrency-safe, with no lost state in either direction.
- Scope actually handled: approval-required tool contract, shell request production (Blocked untouched), runner-side create/wait/decide/consume, in-process broker, decide routing for active runs, and E2E tests for approved/denied/expired/Blocked plus the race path.

## The rendezvous design (why it cannot lose state)

**Chosen: single-writer — the active runner goroutine is the only writer of session state for its run.**

The failure mode being eliminated: the runner checkpoints its in-memory Session throughout the run, while C2's `decideApproval` did Load→mutate→Save on its own copy; concurrently these are two writers, and the later save silently discards the other's progress.

Mechanics:

1. The runner (single goroutine; the parked shell call executes inline on it) creates the request on ITS session object, checkpoints via its own emit path (`approval_requested` durable), then parks in `broker.Wait`. While parked it performs no checkpoints — the session object is quiescent.
2. `decideApproval` for an active run NEVER does Load+Save. It validates read-only (404/cross-session), delivers the decision through the broker's buffered channel (the channel send/receive is the Go happens-before edge), and commits ONLY the `approval_decided` event via `Store.CommitDetachedEvent` — the store's mutex-guarded journal path that leaves the latest persisted checkpoint untouched.
3. The runner wakes, applies `Decide`/`Consume` to its own session object, and persists at its own checkpoints. `session.json` is written by exactly one goroutine (active) or by the C2 path (inactive/no-waiter only) — never both.
4. Double-decide: the waiter stays registered (marked decided) until run end, so a second decide gets 409 rather than falling into the C2 path against a not-yet-checkpointed decision. `Release(sessionID)` at run end hands late decides back to the C2 path, which reads the checkpointed terminal status and conflicts correctly.

Residual window (documented): a decide arriving between the durable request checkpoint and broker registration falls to the C2 path; its Save is transient (the runner's next checkpoint rewrites newer state) and the decision is effectively lost — the request expires at TTL. Deny-by-default, no execution, no permanent state loss.

**ADR 0011 gap found and resolved**: the ADR never specified WHO persists a decision for an active run. Answer implemented: the runner persists state at its next checkpoint; the decide path commits only the event immediately. A crash between them leaves the request pending, and resume-time expiry (C2b) forces a fresh request — consistent with the ADR's "crash between approval and execution → uncertain, never re-executed" rule. A second gap: digest-derived request IDs could collide across sessions, so the broker keys waiters with session ID and the runner mints per-session sequential request IDs (`apr-<session>-<n>`).

## Completed

- `internal/approval`: `CreateSpec.TTL` (bounded override; tests use ~100ms for the expiry path).
- `internal/tool`: `CodeApprovalRequired`/`CodeApprovalDenied`, `ApprovalHint{Paths, Summary}`, `Result.Approval`, `Executor.ExecuteApproved` (unexported context key, unforgeable outside the executor, marks exactly one invocation).
- `internal/tool/builtin`: shell `ConfirmationRequired` → parked result with bounded hint (heuristic relative-path extraction, `<command>` fallback); `Blocked` byte-identical hard-deny — no approval path ever; approved override skips classification for that one invocation only.
- `internal/agent/approvals.go`: `ApprovalDecisions` interface (presentation-free), `awaitApproval` (create → durable request → wait with `WithDeadline(runCtx, ExpiresAt)` → Decide → durable decision → Consume → single approved re-execution; denied/expired → structured denial data, run continues). Nil broker → exact pre-C3 behavior (fail closed, no request).
- `internal/app`: `RuntimeOptions.Approvals` plumbed into runners; `TeamWorker.Approvals` per worker + `Release` on return.
- `internal/web`: `approvalBroker` (waiter registry, deliver, Release); broker constructed once in `New()` and injected into single-agent and team builds; `decideApproval` routes active-run decides through the rendezvous, inactive through C2's path.
- Events: requested/decided/expired/consumed all travel the durable commit path.

## Verification

- Approved E2E: `TestApprovalApprovedExecutesGatedShellCommandEndToEnd` — real shell + real decide API; command executes with workspace effect; fresh-store checkpoint shows `consumed` AND `RunCompleted` with full run progress (both-sides-merged); events requested→decided→consumed in order; second decide → 409.
- Denied E2E (the C2 gap, now closed): `TestApprovalDeniedContinuesRunWithStructuredDenial` — run completes, request denied, model received `approval_denied`.
- Expired E2E: `TestApprovalExpiryDeniesUnattendedDecision` (TTL 100ms) — unattended → expired, run completes, structured denial.
- Blocked: `TestApprovalBlockedCommandNeverProducesARequest` — identical denial data, zero requests.
- Fail-closed paths: nil broker, create failure, Decide/Consume errors after delivery → all denials, never execution.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/web/ ./internal/agent/ ./internal/tool/... -count=1` — the race run explicitly includes all four `TestApproval*` E2Es (verified by name); the parked-wait + decide path runs under `-race` with no findings.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets (arguments digest-only).
- Evals: the 7-package eval set passed 3 consecutive runs.

## Acceptance mapping

1. Approved → command really executes, request consumed, no second consume: approved E2E + `TestApprovedCallConsumesOneShotAndExecutesOnce`.
2. Denied/expired → Run does not fail, model gets structured denial: denied + expired E2Es.
3. Blocked never enters the approval flow: `TestApprovalBlockedCommandNeverProducesARequest`.
4. Decision arriving mid-run merges correctly with run progress (no overwrite either way): approved E2E's merged checkpoint assertions, under `-race`.

## Repository state

- Branch: `agent/opc-c3`, based on `origin/main` after C2b (`fe1f7a0`).
- Commit/PR: `feat: gate confirmation-required shell calls on owner approval`, PR to `main`.
- Working tree: `compose.yaml` is now pristine (the owner's binding lives in the gitignored `.env`); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: only the shell tool produces requests (filesystem/plugin tools can adopt the same contract later); the decide-vs-registration window (documented above) loses to expiry; the approval UI is API-level (the workbench has no approval prompt component yet).
- Next concrete step: owner review/merge closes Phase C. Remaining P2+ items: approval UI, Operator role operations (still disabled), then the 0.5.0 release goals.
- Required user input or authority: PR review/merge remains with the owner.
