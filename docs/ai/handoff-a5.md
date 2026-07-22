# A5 handoff: widen flaky agent test timeouts

## Goal

- Requested outcome: `TestMaximumTurns` and `TestToolErrorReturnedToModel` flaked on CI because a 1-second total timeout raced machine speed (both passed on rerun; unrelated to recent changes). Widen the time tolerance without changing what the tests verify.
- Scope actually handled: two timeout literals in `internal/agent/agent_test.go`. Pure test-infrastructure change; no product code, no security boundary, no ADR.

## Completed

- Changes:
  - `TestMaximumTurns`: runner total timeout `time.Second` → `30 * time.Second`. The test still asserts the run stops with "maximum turns" after exactly 2 turns; the turn cap, not the clock, must win the race.
  - `TestToolErrorReturnedToModel`: runner total timeout `time.Second` → `30 * time.Second`. The test still asserts the tool error is returned to the model and the run completes.
  - Both scripted providers answer immediately, so the wider bound only guards pathological hangs and cannot slow the suite.
- Files/packages: `internal/agent/agent_test.go` only.
- Decisions or ADRs: none required (no behavior change, no new shared constant — the timeouts remain local to each test).

## Verification

- Focused tests: `go test ./internal/agent/ -run "TestMaximumTurns|TestToolErrorReturnedToModel" -count=10` — 20/20 pass.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/agent/ -count=3` ok.
- Vet/build: `gofmt -l` clean, `go vet ./internal/agent/` ok; `git diff --check` clean; no secrets.
- Skipped checks and reason: CI-load reproduction is not possible locally; the fix removes the 1s race window, and CI reruns on the PR.

## Repository state

- Branch: `agent/opc-a5`, based on `origin/main` after Phase A (`8b1c058`).
- Commit/PR: `test: widen flaky agent test timeouts`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: other agent tests still use 1-second-scale timeouts but have not flaked; widen only if CI proves them flaky.
- Next concrete step: owner review/merge, then Phase B (B1 teamtemplate storage).
- Required user input or authority: PR review/merge remains with the owner.

## Addendum (A5b): remaining agent test timeouts

CI later flaked two more tests of the same class — `TestMutationRequiresSuccessfulTestBeforeCompletion` and `TestNormalStopAndToolResultReturned` — so the remaining 1s/3s runner timeouts in `internal/agent/agent_test.go` were widened to 30s as well (six call sites). `TestTotalTimeout`'s 10ms timeout is unchanged: it tests the timeout mechanism itself. Verified with `go test ./internal/agent/ -count=10`, `-race -count=3`, and the full suite.

## Addendum (A5c): web test deadlines

The same timing-flake class reached `internal/web`: `TestReviewPlanWaitsForApprovalBeforeTeamExecution` flaked on CI because a 2-second wall-clock deadline raced the full team run on a loaded runner. The three tight deadlines in `internal/web/server_test.go` (1s publish wait, two 2s completion deadlines) were widened to 30s — they only bound pathological hangs; the fake workers finish in milliseconds. The 20ms cancellation timers and the 10s SSE deadline are unchanged by design.

## Addendum (A5d): plugin lifecycle test timeouts

`TestNodeEchoLifecycle` flaked on CI (`internal/plugin`): spawning the node plugin process and the 1s shutdown deadline raced a loaded runner. Startup contexts widened 3s → 30s, shutdown/lifecycle contexts 1s → 10s. The 20ms timeout-behavior assertions in `TestPluginTimeoutCancellation` are unchanged — they test cancellation, not wall clock.
