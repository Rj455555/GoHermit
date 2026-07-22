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

## A third copy of the same bug, found by live verification of this very fix

Rebuilding the container from just the `workerResult` fix above and replaying the owner's exact
scenario got further — the Mission itself completed — but the Run still failed, now with a
*different* error: `"team live plan completion gate failed"`
(`internal/web/server.go:1134`, gated on `run.Plan.Status != taskplan.Completed`).

Root cause: `internal/runcontrol/controller.go`'s `verifierHandoffPassed` — which decides
whether a `WorkItemDone` event for a Verifier moves the **Live Plan** step to `Complete` or
`Fail` — kept its own independent copy of the exact same "empty Checks always means not passed"
rule the coordinator-level fix (PR #26) had already corrected in `internal/team`. The two
packages had two separate, un-synced implementations of "did the verifier pass"; fixing one and
not the other meant the Mission/WorkItem layer agreed the run succeeded while the Live Plan layer
still marked the verify step failed, so the Plan never reached `Completed` and the Run was
reported as failed regardless.

Fixed by eliminating the duplication instead of patching it a third time: `team.HandoffChecksPassed`
and `team.MissionHasMutation` are now exported, and `internal/runcontrol.verifierHandoffPassed`
calls `team.HandoffChecksPassed` directly instead of keeping a second copy of the rule. There is
now exactly one place in the codebase that decides whether a Verifier handoff counts as passed.

## Verification completed

- New test `TestWorkerResultLeavesVerifierChecksEmptyWhenNoneRan`
  (`internal/app/team_worker_test.go`) — exercises the real `TeamWorker.Execute` → `workerResult`
  path (not a stub `team.Worker`, which is what let this slip through PR #26's own tests) with a
  scripted provider that returns a Verifier turn with no tool calls and an empty `issues` list;
  asserts `Result.Handoff.Checks` is genuinely empty (length 0).
- `internal/runcontrol`'s existing tests still pass unchanged against the new
  `team.HandoffChecksPassed`-backed `verifierHandoffPassed` — no behavior change intended or
  observed for the mutation path there.
- `go build/vet/test ./...` — 3 consecutive uncached runs, all green, after both rounds of fixes.
- `go test -race ./...` — clean.
- **Live-verified end to end**: rebuilt the container from the full branch (worker fix +
  runcontrol consolidation), replayed the exact owner scenario via the real HTTP API (`team`
  agent, `gpt-5.4-mini`, "hello, 你是什么模型") against the running container — Mission
  completed, Run completed, Lead produced a real answer. See the chat transcript for the raw
  request/response; not duplicated here.

## Lesson for next time (recorded so it isn't repeated)

Two lessons from this one bug, not one:

1. PR #26's own tests used a raw stub `team.Worker` that called `Result{Handoff: handoff}`
   directly, bypassing `internal/app.TeamWorker`/`workerResult` entirely — the exact layer the
   synthetic-check bug lived in. Passing unit tests at the `internal/team` level were not
   sufficient evidence the fix worked end-to-end.
2. Even after fixing `workerResult`, a *third* independent copy of the same pass/fail rule lived
   in `internal/runcontrol` and was never touched by either previous fix — found only by
   replaying the scenario against a real rebuilt container a second time. Whenever a rule like
   "does verification count as passed" seems to exist in exactly one place, grep for every
   place a Verifier's `Checks`/`Handoff` gets inspected across the whole repo, not just the
   package the bug report pointed at — and prefer exporting the one true implementation over
   re-deriving the same logic in a second package, which is exactly how this drifted apart in
   the first place.

For any future fix touching Team Run verification behavior: verify live against a rebuilt
container before declaring a user-reported bug closed, and re-verify live again after every
subsequent patch to the same bug — the first "it's fixed" was wrong twice in a row here.

## Repository state

- Branch: `fix/verifier-synthetic-check`, based on `origin/main` after PR #26 (`0ea22f4`).
