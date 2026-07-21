# A3 handoff: consistent per-role provider usage accounting

## Goal

- Requested outcome: count failed calls, retried calls, and the Lead's final summary call in provider usage — not only successful main calls; aggregate usage per role without leaking prompt text.
- Scope actually handled: provider attempt counting in `internal/model`, fail-path accounting in `internal/agent`, partial-usage propagation for failed workers in `internal/app/team_worker.go`, per-role aggregation on the Mission, persistence round-trip, and tests.

## Completed

- Changes:
  - `internal/model/types.go`: `GenerateResponse.Attempts` (attempts behind the response, including retried failures) and `ProviderError.Attempts` (attempts made before giving up).
  - `internal/model/openai.go`, `internal/model/responses.go`: retry loops set `Attempts = attempt + 1` on success (JSON and streaming paths) and stamp `Attempts: maxRetries + 1` on the exhausted ProviderError. Wire behavior unchanged.
  - `internal/agent/agent.go`: `modelAttempts(err)` helper (`errors.As` → `max(1, ProviderError.Attempts)`, else 1). Main loop counts attempts on the Generate fail path before `r.fail`; success adds `max(1, response.Attempts)`. `compress()` gets identical treatment and keeps the deterministic-summary fallback. Token accounting stays provider-reported only — nothing fabricated for failed attempts.
  - `internal/app/team_worker.go`: a failed child run now returns its actually recorded `ModelCalls`/`TotalTokens` alongside the error (no `max(1,…)` floor, no token estimation — zero reported as zero).
  - `internal/team/team.go`: `Mission.UsageByRole map[Role]Usage` (`usage_by_role,omitempty`, initialized in NewMission; old sessions load with a nil map).
  - `internal/team/coordinator.go`: the results loop accumulates calls/tokens into `mission.Usage` and `mission.UsageByRole[outcome.role]` for every outcome, including failed workers. Budget gates unchanged.
  - `docs/ai/team.md`: one line documenting per-role usage including failed/retried calls, counts only.
- Files outside the predicted list, with reasons:
  - `internal/agent/agent.go` — the failed-main-call and failed-summary-call accounting gap lives in the Runner's error paths; no other layer sees these attempts.
  - `internal/app/team_worker.go` — the worker error path discarded the child run's accumulated usage; only this file can propagate it.
- Decisions or ADRs:
  - Failed attempts count as calls, never tokens: providers return no usage for failed attempts and fabricating token numbers would corrupt budget accounting.
  - The interrupt path (context cancelled mid-call) deliberately does not count the in-flight attempt: the call's completion is unknown, and counting it would be fabrication.
  - The Lead's summary call needs no special path: the Lead is a normal WorkItem run through the same Runner, so its failed/retried/summary (compress) calls are covered by the agent-level accounting.
  - `workerResult`'s token estimation for zero-usage streaming providers is unchanged (orthogonal, budget-tested behavior).

## Verification

- Focused tests: provider retry tests per provider (2× retryable failure then success → `Attempts == 3`; all-fail → `ProviderError.Attempts == maxRetries + 1`; streaming variants), agent fail-path and compress accounting, worker partial-usage propagation, coordinator per-role exactness including a failed worker, session round-trip of `usage_by_role`, and a no-prompt-leak test (usage JSON is role-keyed numbers only; prompt marker absent).
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/model/ ./internal/agent/ ./internal/app/ ./internal/team/ ./internal/session/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets in the diff.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns the full baseline on push.

## Acceptance mapping

1. A mission containing one retried call and one failed call yields accurate per-role aggregates: `internal/team/coordinator_test.go` acceptance test (retried worker `ModelCalls: 2, Tokens: 200` + failed worker with partial `3, 150` → totals `{5, 350}`, both roles exact in `UsageByRole`), backed by the provider attempt-count tests.
2. Usage records contain no prompt text: usage structs are integer counts keyed by role; enforced by the session no-prompt-leak test.

## Repository state

- Branch: `agent/opc-a3`, based on `origin/main` after the A1 squash merge (`354af00`). A2 (`agent/opc-a2`) is a parallel branch; both touch `internal/team/coordinator.go` in different hunks of `runBatch` and are expected to merge cleanly in either order.
- Commit/PR: `feat: record per-role provider usage for failed and retried calls`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Harness state

- Session/Run IDs used for live verification: none (deterministic tests with local HTTP fixtures only; no paid calls).
- Last event sequence and terminal Run state: not applicable.
- Project memory updated: no.
- Recovery or workspace-reconciliation notes: `usage_by_role` persists via the existing session checkpoint (schema v4; additive field, no migration). Loading a new session with an old binary would reject the unknown key — acceptable within a single-version local deployment.

## Remaining work

- Known limitations: per-role values are model-call and token counts only (no prompt/completion split); interrupted in-flight calls are not counted by design; UI does not render per-role usage yet (the session detail JSON exposes it).
- Next concrete step: owner review of the A3 PR, then A4 (opt-in Codex live smoke) on `agent/opc-a4` after A1-A3 merge.
- Required user input or authority: PR review/merge remains with the owner.
