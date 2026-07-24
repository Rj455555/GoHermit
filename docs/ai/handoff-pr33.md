# PR33 handoff: Verification Recipe

## Goal

- Requested outcome: a declarative verification recipe (checks as argv arrays, required flag, timeouts, independent_verifier, max_repair_attempts) that feeds the EXISTING Verifier/repair pipeline — command arrays only, executed through the same policy allowlist as shell commands, no new bypass; mutation invocations need at least one required check that really ran and passed; read-only invocations may run zero checks but need Issues == []; all results flow into the existing Handoff/Run timeline/events.
- Scope actually handled: `policy.ClassifyArgv`, new `internal/verification` deterministic runner, recipe plumbing through Session → runTeam → TeamWorker verifier path, invocation acceptance evaluation over existing evidence, additive evidence fields, and the failure-path tests.

## Completed

- Changes:
  - `internal/policy`: `ClassifyArgv` — the SAME allowlist/deny tables as `ClassifyShell` (shared verbatim), argv form with token-based destructive matching and path-escape checks; shell metacharacters in argv entries are literal data; no shell string is ever built.
  - `internal/verification` (new): `RunChecks` — policy-gated (non-Safe → PolicyDenied result, never executed), `exec.CommandContext` on the argv array directly (no shell), workspace-rooted, per-check clamped timeout, 8 KiB bounded output with truncation/timeout markers.
  - Evidence channel: `team.Check` and `session.TestResult` gain additive `exit_code`/`duration_ms`; `Session` gains additive `VerificationRecipe` (set from the invocation's definition snapshot at creation; snapshot stays authoritative). TeamWorker runs the recipe deterministically for Verifier work items and appends results to the child's TestResults — the EXISTING workerResult mapping turns them into handoff Checks, so evidence flows through the existing Handoff/Run timeline/events with no new report format.
  - `internal/controlplane`: runTeam applies recipe MaxRepairAttempts (bounded) and passes the recipe to the TeamWorker; `invocationAcceptance` on reconcile — mutation: every required check present AND passed in the verifier handoff (no required declared → at least one real passing check per PR #26/#27); read-only: verifier Issues == []. Refusal → new `loop.Invocation.Reject` (attached → blocked, `verification_failed`), never completed.
  - `team.VerificationFailureMessage` exported so a verification-caused run failure reconciles to blocked rather than failed; non-verification failures keep `run_failed`.
- Files/packages: `internal/policy`, `internal/verification`, `internal/session`, `internal/team`, `internal/app`, `internal/controlplane`, `internal/loop` (Empty + Reject only).
- Decisions or ADRs:
  - `Reject` was added to the loop state machine because `Block` is prepared-only — the minimal change for post-execution refusal.
  - Recipe enforcement lives in deterministic execution + acceptance evaluation over existing verifier evidence — deliberately NOT a second verifier framework; `HandoffChecksPassed`/`verificationPassed` semantics untouched.

## Verification

- Failure-path 2: read-only invocation, zero checks → completes ONLY when verifier Issues == []; a fake verifier returning an issue → mission fails per the existing rule → invocation `blocked`/`verification_failed`, never completed.
- Failure-path 3: mutation invocation with failing required check → verifier Attempt == recipe max_repair_attempts (bounded repair capped by the recipe), mission fails, invocation `blocked`/`verification_failed`, never completed; failing-check evidence (exit code ≠ 0) on the verifier handoffs.
- Integration: passing required check (`go version`) → handoff Checks carry exit_code/duration, session TestResults show the same evidence, invocation completed.
- Policy/runner: allowlisted argv Safe, unknown program denied, `rm -rf`/`shutdown`/`kubectl delete`/absolute/`..`/empty argv Blocked; policy-denied commands never execute (marker-file proof); `$(…)` argv entries arrive literally (no shell interpretation); timeout and 8 KiB truncation enforced.
- Gates (actual): `gofmt -l .` clean, `go vet ./...` ok, `go test ./... -count=1` all packages ok, `go test -race ./... -count=1` all ok, `go build ./cmd/hermit` and `./cmd/hermit-web` ok, `git diff --check` clean.
- Skipped checks and reason: none.

## Repository state

- Branch: `agent/pr33-verification-recipe`, based on `origin/main` (includes PR32).
- Commit/PR: `feat: add declarative verification recipe for loops`, PR to `main`.
- Working tree: untracked owner files left exactly as found.
- External state changed: none.

## Remaining work

- Known limitations: the recipe allowlist covers Go/Git-inspection programs only (conservative; widening needs its own review); definition budget fields remain unenforced (PR32 note); Loop Workbench UI (PR35) and worktree isolation (PR34, gated on the ADR 0012 owner decision) remain open.
- Next concrete step: owner review/merge — this completes the PR28–PR33 batch. Follow-ups: PR34 (blocked on ADR 0012 amendment), PR35 Loop UI.
- Required user input or authority: PR review/merge remains with the owner.
