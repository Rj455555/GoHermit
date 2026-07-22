# AI handoff: read-only Team Run verification gate fix

Written 2026-07-22 by Claude while Kimi Code's weekly quota was exhausted. Found and fixed
during live investigation of an owner-reported bug — not part of the OPC Phase A-F track, and
not related to the model-selection dropdown fix from earlier the same day.

## Goal and completed scope

- Owner report: asking the `team` agent a plain conversational question ("hello, 你是什么模型")
  against an empty, non-git workspace produced a failed Mission — the Live Plan showed
  `交叉验证分析证据` (the Verifier step) failing with "independent verification did not pass",
  and Lead never ran.
- Root cause, confirmed by direct reproduction (curl against the running container) and by
  reading the exact code path:
  - `mutationRequested("hello, 你是什么模型")` correctly returns `false` (no mutation markers),
    so `AdaptiveMission` (`internal/team/coordinator.go`) correctly selects the read-only
    topology: `explore → review → verify → lead`, with **no repair WorkItem** in that topology.
  - The Verifier's goal for this topology is "cross-check both handoffs... without modifying
    files" — for a plain informational question with no code and no repository, there is
    nothing a deterministic command could check, so a correctly-behaving Verifier returns a
    Handoff with an empty `Checks` list.
  - `handoffChecksPassed`/`verificationPassed` (same file) treated *any* empty `Checks` list as
    "not verified", with zero distinction between "nothing to check" and "verification wasn't
    attempted". Since the read-only topology has no repair WorkItem for
    `RequeueAfterVerification` to requeue, `repairIDs` comes back empty and the coordinator
    (`runBatch`) fails the Mission immediately with exactly the message the owner saw.
  - This is not the "simple questions need a lightweight pre-router before the agent loop"
    architecture the owner hypothesized — a bare `coding`-agent "hello" was reproduced cleanly
    (2 model calls, no tools, `RunCompleted`) before this bug was found. The actual defect is
    narrower: the `team` agent's verification gate had only one definition of "passed" (a real,
    explicitly passing Check), which is the right and necessary rule for a **mutation** mission
    (never treat an unrun test as a passing one) but is the wrong rule for a **read-only**
    mission that may have nothing to run at all.

## Fix

- `internal/team/coordinator.go`: added `missionHasMutation(mission)` (true iff any WorkItem has
  `MutatesWorkspace`). `verificationPassed` and `handoffChecksPassed` (now takes `*Mission`) keep
  their exact existing behavior for mutation missions — empty `Checks` is still unconditionally
  unverified there, no change to that safety property. For a mission with **no** mutating
  WorkItem, an empty `Checks` list now falls back to `len(handoff.Issues) == 0`: the Verifier's
  own cross-check reporting no problems is the pass signal. A Verifier that genuinely finds an
  unsupported claim must still report it via `Issues`, which still fails the mission (read-only
  topology has no repair path — this is intentionally a hard failure, not a retry).
- `prompts/coding.md`: added a "Verifier checks on read-only Team Runs" section instructing the
  Verifier to leave `checks` empty (never fabricate a command) and use `issues` instead when
  there's nothing a mutation mission would have to actually test.

## Verification completed

- New tests in `internal/team/readonly_verification_test.go`:
  - `TestReadOnlyMissionPassesVerificationWithNoChecksAndNoIssues` — reproduces the exact bug
    (`AdaptiveMission` with a non-mutation goal, Verifier returns empty Checks + empty Issues) —
    Mission now completes.
  - `TestReadOnlyMissionFailsVerificationWhenVerifierReportsIssues` — confirms the fix is narrow:
    a Verifier that reports a real Issue still fails the mission (no repair path exists for
    read-only topology, by design).
  - `TestMutationMissionStillFailsVerificationWithNoChecks` — regression guard: an
    `AdaptiveMission` **mutation** topology with a Verifier that returns empty Checks still fails
    exactly as before. `TestCoordinatorBlocksLeadWithoutVerifierEvidence` (pre-existing, untouched)
    covers the same guarantee against `DefaultMission`.
- `go build/vet/test ./...` — full suite, 3 consecutive uncached runs, all green.
- `go test -race ./...` — full repo, clean (the read-only test's stub worker needed a mutex
  around its call-log slice since explore/review genuinely run concurrently in that topology —
  `-race` caught this on the first attempt, fixed before this handoff was written).
- Not yet verified: an actual live reproduction of the exact owner scenario (team agent,
  gpt-5.4-mini, "hello, 你是什么模型") against the rebuilt container. The unit tests exercise the
  identical `AdaptiveMission`/`Coordinator.Run` path the live server uses, so this is expected to
  be resolved, but should be confirmed against the real deployed container.

## Repository state

- Branch: `fix/readonly-verification-gate`, based on `origin/main` after the D0 ADR and
  model-selection fix (`d206f4b`).
- Unrelated to `agent/opc-d0` (ADR 0012) — a different, unrelated bug found the same session.
