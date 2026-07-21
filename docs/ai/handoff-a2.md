# A2 handoff: Reviewer severity gating for repair scheduling

## Goal

- Requested outcome: add a severity field (blocking / advisory) to Reviewer findings; schedule the repair WorkItem only when a blocking finding exists, replacing the previous unconditional repair pass.
- Scope actually handled: severity model and validation in `internal/team`, skip semantics (`WorkSkipped`) with recovery interplay, coordinator gating, Reviewer model I/O, prompt docs, fixture graders, unit tests, plus two consistency fixes found in review (preflight guard, plan requeue for un-skipped repairs).

## Completed

- Changes:
  - `internal/team/team.go`: `Severity` (`blocking`/`advisory`), `Finding`, `Handoff.Findings` (bounded ≤128, strict severity, fail-closed on unknown severity or empty summary), `Handoff.HasBlockingFindings`, new `WorkSkipped` status (satisfies dependents like `WorkCompleted`; never runnable; `Cancel` leaves it alone), `Mission.SkipRepairsAfterReview`, `RequeueAfterVerification` collects mutating dependencies in `WorkCompleted` **or** `WorkSkipped`.
  - `internal/team/coordinator.go`: after a Reviewer completes with no blocking findings, queued mutating dependents of that review are skipped; one `WorkItemDone` per skipped id (`审查无 blocking 发现，跳过修复`) is emitted **after** the reviewer's own completion so the plan completes the repair step from a clean state. The blocking path is byte-identical to before (bounded repair/re-verify, default max 3 attempts).
  - `internal/app/team_worker.go` (deviation, same whitelist reason as A1): `parseWorkerHandoff` accepts optional `findings`; the Reviewer assignment prompt documents `{severity, summary}` and the blocking/advisory semantics.
  - `prompts/coding.md`: Reviewer findings severity schema section.
  - Evals: 5 new `handoff_quality.json` scenarios (valid blocking/advisory, 128 boundary, invalid severity, empty summary, 129 rejected); `team_verification.json` gains `advisory-findings-skip-repair` (repair skipped, attempt 0, worker never called for repair, 4 handoffs, completed) and `blocking-findings-run-repair` (repair attempt 1, 5 handoffs, completed), graded with new `statuses`/`worker_calls` expectations.
  - Unit tests: `internal/team/review_gate_test.go` (skip match/no-match, completion with skipped repair, cancel interaction, requeue un-skip, findings bounds table, coordinator end-to-end advisory-skip and blocking-runs), `internal/team/coordinator_test.go` fixtures now carry a blocking finding to preserve pre-A2 assertions, `internal/app/team_worker_test.go`, `internal/runcontrol/controller_test.go`.
  - Docs: `docs/ai/team.md` (repair stage is severity-gated), `docs/ai/evals/v0.5.md` (capability-7 grader references).
- Files outside the predicted list, with reasons:
  - `internal/app/team_worker.go` — worker JSON parsing is whitelisted; without a `findings` key no real Reviewer model could report severity.
  - `internal/runcontrol/controller.go` — see Fixes below; required so the Live Plan stays consistent with execution when a skipped repair is requeued.
- Decisions or ADRs:
  - Findings fail closed: an unknown severity rejects the whole handoff instead of silently downgrading the repair gate.
  - Skipping is a status (`WorkSkipped`), not a removal: the audit trail, the Plan step, and the ability to requeue after a later verification failure are all preserved.
  - No new ADR; the gating rule is a small refinement of ADR 0010's bounded repair cycle.

## Fixes made during review (not in the original implementation)

1. **Preflight guard** (`internal/team/team.go`): the mutation topology's preflight Reviewer gates the primary `build` item (mutating, `DependsOn: [explore, preflight]`) and never has blocking findings, so the unguarded skip would have bypassed the entire implementation stage in every real mutation run. `SkipRepairsAfterReview` now only acts for post-implementation reviews — a review whose own dependencies include a mutating WorkItem. Regression test: `TestSkipRepairsAfterReviewIgnoresPreflightReview` (against the real `AdaptiveMission` graph).
2. **Plan requeue for un-skipped repairs** (`internal/runcontrol/controller.go`): when verification fails after an advisory-only skip, `RequeueAfterVerification` un-skips the repair with `Attempt` still 0, but `queuedVerificationRetry` required `Attempt > 0`, so the Plan failed the verify step while the Mission actually re-entered the bounded repair loop — Plan state diverging from execution facts. The attempt gate is removed (a queued mutating dependency of the verifier at that point can only come from a requeue). Regression test: `TestVerifierFailureReopensPlanForUnskippedRepairWithZeroAttempts`.

## Verification

- Focused tests: `go test ./internal/team/ ./internal/runcontrol/ -count=1` (incl. both new regression tests) ok.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/team/ ./internal/app/ ./internal/evals/ ./internal/runcontrol/ ./internal/web/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets in the diff.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns the full baseline on push.

## Acceptance mapping

1. A Reviewer handoff with no blocking findings skips repair entirely and goes straight to the Verifier: `advisory-findings-skip-repair` eval scenario (repair worker call count 0, status skipped, verify runs, mission completes) plus `TestCoordinatorSkipsRepairAfterAdvisoryReview`.
2. Blocking findings keep the existing bounded repair/re-verify loop (max 3 attempts, unchanged): `blocking-findings-run-repair` eval scenario, `TestCoordinatorRunsRepairAfterBlockingReview`, and the pre-existing `TestCoordinatorRepairsAndReverifiesWithinBound`.

## Repository state

- Branch: `agent/opc-a2`, based on `origin/main` after the A1 squash merge (`354af00`).
- Commit/PR: `feat: gate repair scheduling on blocking review findings`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Harness state

- Session/Run IDs used for live verification: none (deterministic tests only; no paid calls).
- Last event sequence and terminal Run state: not applicable.
- Project memory updated: no.
- Recovery or workspace-reconciliation notes: `WorkSkipped` persists via the existing Mission checkpoint and session schema v4 (`WorkItem.Status` is a string value; no migration). Recovery interrupts only running work; skipped items stay skipped, and a resumed verifier failure can still un-skip the repair.

## Remaining work

- Known limitations: severity comes from the Reviewer model's JSON; a model that mislabels severity cannot downgrade the gate below advisory-only-skip (unknown values fail closed), but it can over-report blocking (repair runs unnecessarily — safe direction).
- Next concrete step: owner review of the A2 PR, then A3 (per-role provider usage) on `agent/opc-a3`.
- Required user input or authority: PR review/merge remains with the owner.
