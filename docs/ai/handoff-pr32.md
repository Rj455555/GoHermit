# PR32 handoff: Manual Loop Invocation

## Goal

- Requested outcome: a separate Invocation state machine (never touching Run's) that snapshots the Definition, creates one independent Session, starts one Run through the PR29 application service, records the binding, recovers without duplicates, and fails closed on a dirty workspace for mutation invocations.
- Scope actually handled: `internal/controlplane/invocations.go` (start/reconcile/cancel), CLI `loop run|history|cancel`, and integration tests covering failure paths 4/5/6. Verification recipe enforcement deliberately left to PR33.

## Completed

- Changes:
  - `StartLoopInvocation`: load definition → pre-launch gates fail closed BEFORE any provider/session work (disabled → `definition_disabled`; workspace identity mismatch → `workspace_mismatch`; dirty/missing git when `require_clean_git` → `workspace_not_clean`, recorded as a `blocked` invocation) → prepared → one `CreateSession` (definition selection + plan_mode) → dispatched (session_id persisted) → one `StartRun` → attached (run_id persisted). The run proceeds async through the existing machinery (approvals/plan/SSE intact).
  - Reconcile-on-read (`GetInvocation`/`ListInvocations`): only dispatched/attached invocations are touched; the bound Session (source of truth) maps terminal run states to Complete/Fail/Cancel, persisted once; interrupted runs stay attached/resumable; a crash between dispatch and attach is healed via `run.StartedAt`. Reconciliation never creates anything.
  - `CancelLoopInvocation`: prepared → skipped; attached → `CancelRun` on the binding then reconcile; terminal → conflict.
  - CLI: `hermit loop run <id>` (polls bounded by the snapshot's budget timeout, prints binding/status/outcome), `hermit loop history <id>`, `hermit loop cancel <invocation-id>`; dry-run/list unchanged.
- Files/packages: `internal/controlplane`, `cmd/hermit` only (no `internal/loop` change needed).
- Decisions or ADRs:
  - **Documented limitation**: definition budget fields are NOT plumbed into run creation (`StartRun` accepts no budget today) — runs execute under existing defaults; only the CLI poll bound uses the snapshot timeout. Flagged for PR33/follow-up rather than half-wired.
  - Blocked gate outcomes return the invocation with nil error (the refusal IS the outcome); launch failures after the gates return error + skipped/failed status.

## Verification

- Failure-path 4 (immutability): definition edited mid-run bumps to revision 2; the in-flight invocation completes on revision 1 (snapshot, task text, provider-received prompt all old); the next invocation uses revision 2.
- Failure-path 5 (crash recovery): fresh Service + fresh loopstore over the same dirs — completed run reconciles with store.List() still 1 session/1 run, zero builds, zero extra provider calls; mid-run crash → run interrupted, invocation stays attached, no duplicates; original run later reconciles to completed.
- Failure-path 6 (dirty workspace): mutation definition + dirty git → `blocked`/`workspace_not_clean`, counting build stub proves 0 runtime builds and no session exists.
- Also: disabled/mismatch/unknown-id gates, cancel prepared/attached/terminal/unknown, CLI exit codes.
- Gates (actual): `gofmt -l .` clean, `go vet ./...` ok, `go test ./... -count=1` all packages ok, `go test -race ./... -count=1` all ok (controlplane 24.1s), both builds ok, `git diff --check` clean.
- Skipped checks and reason: none.

## Repository state

- Branch: `agent/pr32-loop-invocation`, based on `origin/main` (includes PR31).
- Commit/PR: `feat: add manual loop invocation lifecycle`, PR to `main`.
- Working tree: untracked owner files left exactly as found.
- External state changed: none.

## Remaining work

- Known limitations: budget fields unenforced (above); CLI cross-process cancel of a recovered interrupted run reports the request but the invocation stays attached/resumable (correct per reconciliation); CLI `run` exit-0 path is covered at service level only (real-provider build).
- Next concrete step: owner review/merge, then PR33 (Verification Recipe — final item).
- Required user input or authority: PR review/merge remains with the owner.
