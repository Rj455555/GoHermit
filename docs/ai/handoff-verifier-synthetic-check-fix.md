# AI handoff: remove synthetic failing Verifier check

Written 2026-07-22 by Claude. Direct follow-up to
`docs/ai/handoff-readonly-verification-fix.md` (PR #26) — that fix was necessary but not
sufficient; this closes the gap the live redeploy verification caught.

## Goal and completed scope

- After PR #26 merged and the container was rebuilt, live reproduction of the exact owner
  scenario (`team` agent, `gpt-5.4-mini`, "hello, 你是什么模型") **still failed** with
  "independent verification did not pass" — the coordinator-level fix alone did not resolve it.
- Root cause: `internal/app/team_worker.go`'s `workerResult` had a second, earlier layer that
  the coordinator-level fix never touched: whenever a `RoleVerifier` assignment produced zero
  real `TestResults`, it force-injected one synthetic Check —
  `{Command: "required deterministic verification", Passed: false, Summary: "Verifier did not
  record a test result"}` — onto the Handoff, *before* it ever reached
  `internal/team.handoffChecksPassed`. This meant `handoff.Checks` was never actually empty for
  a Verifier in practice; it always had at least one entry, and that entry always failed —
  completely bypassing the read-only-mission branch PR #26 added, regardless of Issues.
- This synthetic injection was not covered by any existing test (confirmed by grep — no test
  referenced its exact strings or asserted this behavior at any level), so nothing caught it
  until live redeploy verification against the real container.

## Fix

- Deleted the synthetic-check injection block in `workerResult`
  (`internal/app/team_worker.go`). A Verifier that ran no test now produces genuinely empty
  `Checks`, exactly as `parseWorkerHandoff` and the model's own JSON response describe.
- No coordinator-level change needed: `internal/team.handoffChecksPassed`'s existing rule
  already produces the correct outcome for both cases directly from real data —
  - Mutation mission + genuinely empty Checks → still unconditionally unverified (the exact
    behavior the deleted synthetic check was manufacturing, now reached honestly instead of
    through fabricated data).
  - Read-only mission + genuinely empty Checks + empty Issues → passes (the actual bug).

## Verification completed

- New test `TestWorkerResultLeavesVerifierChecksEmptyWhenNoneRan`
  (`internal/app/team_worker_test.go`) — exercises the real `TeamWorker.Execute` → `workerResult`
  path (not a stub `team.Worker`, which is what let this slip through PR #26's own tests) with a
  scripted provider that returns a Verifier turn with no tool calls and an empty `issues` list;
  asserts `Result.Handoff.Checks` is genuinely empty (length 0).
- `go build/vet/test ./...` — 3 consecutive uncached runs, all green.
- `go test -race ./...` — clean.
- **Live-verified this time**: rebuilt the container from this fix, replayed the exact owner
  scenario via the real HTTP API (`team` agent, `gpt-5.4-mini`, "hello, 你是什么模型") against
  the running container — Mission completed, Lead produced a real answer. See the chat
  transcript for the raw request/response; not duplicated here.

## Lesson for next time (recorded so it isn't repeated)

PR #26's own tests used a raw stub `team.Worker` that called `Result{Handoff: handoff}`
directly, bypassing `internal/app.TeamWorker`/`workerResult` entirely — the exact layer this bug
lived in. Passing unit tests at the `internal/team` level were not sufficient evidence the fix
worked end-to-end; only replaying the scenario against the real deployed container surfaced this
second layer. For any future fix touching Team Run behavior, prefer (or add alongside) a test
that goes through the real `TeamWorker.Execute`/`workerResult` path, and where feasible, verify
live against a rebuilt container before declaring a user-reported bug closed.

## Repository state

- Branch: `fix/verifier-synthetic-check`, based on `origin/main` after PR #26 (`0ea22f4`).
