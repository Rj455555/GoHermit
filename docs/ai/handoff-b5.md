# B5 handoff: wire Team Template into execution and budgets

## Goal

- Requested outcome: close the gap found while reviewing Phase B — the Team Template affected only storage and pre-creation validation, not real execution. Per-role template overrides must drive the actual runtime of each role, and per-role cost limits must reach `Mission.Budget.RoleLimits`. **This is a gap fix for Phase B review findings, not new scope.**
- Scope actually handled: per-role cost limits in the template schema, a shared resolver reusing B2's validation logic, per-role runtime overrides in `TeamWorker`, budget wiring at Mission creation, and tests including one end-to-end override proof.

## Completed

- Changes:
  - `internal/teamtemplate/template.go`: `RoleSelection` gains `MaxModelCalls`/`MaxTokens` (`omitempty`, zero = unlimited, additive — old files load unchanged; negative/over-cap rejected; survives export/import round-trip).
  - `internal/web/server.go`: extracted `resolveTeamRoleSelection` (catalog + live models + credential resolution — the B2 logic, reused not duplicated; `validateTeamRoleSelection` now calls it and keeps its exact behavior). New shared `resolveTeamRolePlan` (fail-closed on store/load error; nil for an empty template; resolves + dedupes the five roles' effective selections and converts non-zero limits) used by both `launchSessionRun` (fills `Budget.RoleLimits` only when limits exist) and `runTeam` (builds the override table; failures go through `failLaunchedRun`).
  - `internal/app/team_worker.go`: `RoleRuntime{Selection, APIKey, Models}` and `TeamWorker.RoleSelections map[string]RoleRuntime`; `Execute` uses the role's override when present (still replacing only the Agent profile field), otherwise the session-level inputs — nil map is byte-identical to before. The child execution session records the effective (override) selection.
- Files/packages: `internal/teamtemplate`, `internal/web`, `internal/app`.
- Decisions or ADRs:
  - `launchSessionRun` and `runTeam` each call the shared resolver (a duplicate template load) rather than changing `runTeam`'s signature — zero churn for existing direct-call tests.
  - Empty template means no overrides and nil `RoleLimits`: the default path is unchanged by construction, not by special-casing.

## Verification

- Focused tests: `TestTeamTemplateOverrideReachesBuilderExecutionSession` (E2E: template pins Builder to a different configured provider; after a real team run the builder's hidden execution session records the TEMPLATE selection while the explorer's records the session default), `TestTeamTemplateLimitsReachMissionBudget`, `TestTeamTemplateWithoutLimitsKeepsDefaultBudget`, `TestTeamRunFailsClosedWhenTemplateUnloadable`, `TestTeamWorkerRoleOverrideSelectsTemplateRuntime`.
- Full tests: `go test ./... -count=1` all packages ok; no existing test was modified to accommodate.
- Race test: `go test -race ./internal/web/ ./internal/app/ ./internal/teamtemplate/ ./internal/team/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets (RoleRuntime keys live in memory only, never persisted).
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E browser/Compose not rerun (no web-asset or packaging change); CI reruns on the PR.

## Acceptance mapping

1. Builder override reaches the hidden execution session's Selection: `TestTeamTemplateOverrideReachesBuilderExecutionSession` (end-to-end, not a struct test).
2. Template limits fill `Mission.Budget.RoleLimits` at creation and feed B3's `RoleBudgetExceeded` with real data: `TestTeamTemplateLimitsReachMissionBudget` (wiring) + existing B3 enforcement tests.
3. Empty template → behavior identical: entire pre-B5 suite passes unmodified; nil-override path is byte-identical in `Execute`.

## Repository state

- Branch: `agent/opc-b5`, based on `origin/main` after Phase B + A5b (`f6bb1c2`).
- Commit/PR: `feat: wire team template into role execution and budgets`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: overrides are resolved at run launch; editing the template mid-run does not re-resolve (consistent with the session-lock model). Operator remains unavailable.
- Next concrete step: owner review/merge, then Phase C (P2 approval contract) starting with the C1 ADR.
- Required user input or authority: PR review/merge remains with the owner.
