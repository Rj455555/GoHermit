# B3 handoff: per-role cost ceilings, retry ownership, fallback audit contract

## Goal

- Requested outcome: add a per-role cost ceiling layer to the existing Mission budget; make retry ownership explicit (the failing role's WorkItem retries under its own identity); define the audit event any future fallback must emit. Actual provider-to-provider fallback switching is explicitly out of scope.
- Scope actually handled: budget/contract changes in `internal/team` and the audit event type in `internal/event`, with tests. **Fallback switching is NOT implemented — this task is the contract layer only.**

## Completed

- Changes:
  - `internal/team/team.go`: `Budget.RoleLimits map[Role]Usage` (`role_limits,omitempty`; nil/empty = no enforcement, `DefaultBudget` unchanged, additive JSON so persisted missions load unchanged); `Budget.RoleLimit(role)`; `Mission.RoleBudgetExceeded(role)` returning a bounded reason (role, hit limit, counts only); retry-ownership contract comment on `RequeueAfterVerification` (requeued items retry under their own ID/role/Attempt, usage accrues only to their own role; re-dispatch to another identity is a contract violation).
  - `internal/team/coordinator.go`: per-role gates mirroring the existing global-budget gates — pre-start (before `mission.Start`) and post-outcome (before error handling, so failed-worker usage also counts): on exceed → `mission.FailMission(reason)` + `MissionFailed` event + checkpoint, identical termination semantics to global overflow.
  - `internal/event/event.go`: `ProviderFallback Type = "provider_fallback"` with the contract comment: any future fallback MUST commit this event BEFORE switching (bounded payload: role, work item, from/to provider names, reason — names and counts only). Nothing emits it yet.
  - Tests: `internal/team/role_budget_test.go` (post-outcome termination, pre-start block, under-limit unaffected, nil-limits legacy behavior, retry identity/ownership, RoleLimits JSON round-trip incl. legacy JSON without the key), `internal/event/event_test.go` (type round-trip).
- Files/packages: `internal/team`, `internal/event`.
- Decisions or ADRs:
  - Enforcement reads the A3 `UsageByRole` accumulation — no parallel accounting system.
  - `RoleBudgetExceeded` uses meet-or-exceed (`>=`): a role landing exactly on its ceiling terminates the mission, mirrored at both gates.
  - No new persistence mechanism: RoleLimits rides the existing Mission checkpoint (schema v4, additive field).

## Verification

- Focused tests: role-budget termination paths, retry-ownership test (`repair`/`verify` retry under same IDs with `Attempt == 2`, usage only in owning roles), persistence round-trip, event round-trip.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/team/ ./internal/event/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web/packaging change); CI reruns on the PR.

## Acceptance mapping

1. A role exceeding its ceiling terminates exactly like global budget overflow: `TestCoordinatorRoleBudgetExceededTerminatesMission` (post-outcome) and `TestCoordinatorRoleBudgetBlocksNextStart` (pre-start, item never runs, `MissionFailed` event).
2. No code path silently switches providers without a new audit event: no fallback path exists at all on this branch (contract-only); the mandatory `provider_fallback` event type is defined and its absence-before-switching is documented as a bug in the code contract.

## Repository state

- Branch: `agent/opc-b3`, based on `origin/main` after B2 (`b3be6b3`).
- Commit/PR: `feat: add per-role budget ceilings and fallback audit contract`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: RoleLimits are not yet populated from the Team Template (no template editor exists); retry ownership is contractual — the coordinator never retried failed workers under another identity before either. **Fallback switching logic is not implemented, contract layer only** — implementing real provider fallback requires a separately reviewed task.
- Next concrete step: owner review/merge, then B4 (template export/import credential redaction) on `agent/opc-b4`.
- Required user input or authority: PR review/merge remains with the owner.
