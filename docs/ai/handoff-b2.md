# B2 handoff: pre-creation provider/credential validation for team Sessions

## Goal

- Requested outcome: before a team Session is created, validate every role's selected provider/model from the Team Template — credentials present and provider capabilities sufficient — failing synchronously before any Session/Mission object exists.
- Scope actually handled: per-role effective-selection expansion in `internal/teamtemplate`, the validation hook in `createSession`, and tests. No changes to launch/run paths or role behavior.

## Completed

- Changes:
  - `internal/teamtemplate/template.go`: `Template.Empty()`, `Template.SelectionForRole(role)`, `EffectiveSelections(t)` (exactly the five overridable roles).
  - `internal/web/server.go`: `Server.teamTemplates` store (+ deferred `teamTemplatesErr` so a resolution failure fails closed at request time, never at startup). In `createSession`, when `agent == "team"` and after the existing selection/credential checks, `validateTeamSelections` runs BEFORE `s.build`/`session.NewConversation`: empty template → legacy behavior (all roles on the session-level selection, already validated); otherwise each role's effective selection is checked in a fixed role order (deduped by identical selection) through the SAME existing logic — `validateSelection` (catalog, incl. live Codex catalog), `AccessProfile` + `accessStatus` (credential presence), and the production `s.build` provider construction with `Capabilities().ToolCalls` required (runtime closed immediately). Errors are bounded 400s naming the role, without secrets.
  - `internal/web/server_test.go`: 6 tests — missing-credential override → 400 naming the role; unknown model → 400; valid template → 201; absent template → 201 (backward compatible); invalid template + non-team agent → 201 (gate only applies to team); injected provider without ToolCalls → 400, and rejection paths assert `server.store.List()` is empty.
- Files/packages: `internal/teamtemplate`, `internal/web`.
- Decisions or ADRs:
  - Empty template = backward compatible (no template configured → session-level selection governs every role, as before B1).
  - Capability is verified via the real production build path rather than static metadata, so future providers are gated by their declared `Capabilities()`.
  - Validation reuses existing catalog/credential/capability checks; only the checkpoint moved earlier and widened to per-role selections (no bypass, no duplication).

## Verification

- Focused tests: new `server_test.go` cases (400-before-creation, no persisted session on rejection, capability rejection via injected build).
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/web/ ./internal/teamtemplate/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns on the PR.

## Acceptance mapping

1. Unconfigured-credential or capability-insufficient selections fail synchronously before Session/Mission creation: the hook sits before `s.build`/`NewConversation`; rejection tests assert the session store is empty after the 400.
2. No orphaned/partially-initialized Session or Mission: Mission creation only happens later in `launchSessionRun`; the 400 returns before any persistence call.

## Repository state

- Branch: `agent/opc-b2`, based on `origin/main` after B1 (`bcd1e29`).
- Commit/PR: `feat: validate team template selections before session creation`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: `launchSessionRun` does not re-run the template validation at run start (a template edited between session creation and first run is re-checked only via the session-level selection); per-role credentials are validated for presence but workers still share one runtime selection at execution time (per-role execution wiring is a later task).
- Next concrete step: owner review/merge, then B3 (per-role cost ceilings, retry ownership, fallback audit contract) on `agent/opc-b3`.
- Required user input or authority: PR review/merge remains with the owner.
