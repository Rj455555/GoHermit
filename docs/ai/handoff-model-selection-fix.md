# AI handoff: model-selection catalog reset fix

Written 2026-07-22 by Claude while Kimi Code's weekly quota was exhausted. Small, scoped
frontend bug fix — not part of the OPC Phase A-F track, found during an unrelated review
session.

## Goal and completed scope

- Symptom (owner report): switching the company/access/model dropdowns in the web workbench
  (e.g. picking Kimi K3) produced no error, but silently reverted to the default selection
  within a few seconds.
- Root cause: `internal/web/assets/app.js` polls `/api/health` every 5 seconds
  (`setInterval(checkHealth, 5000)`). Every successful poll called `setConnectivity('online')`,
  which unconditionally called `renderCatalog()` whenever `catalog` was loaded — not only on an
  actual offline-to-online transition. `renderCatalog()` resets the `#company`/`#access`/`#model`
  `<select>` values to `catalog.selection` (the last-loaded server/config default), discarding
  whatever the owner had just picked. This is pre-existing code from commit `f6fc258` ("add
  personal agent team orchestration"), well before the OPC Phase A-C work — not something any of
  the recent PRs introduced.
- Fix: `setConnectivity()` now captures whether the connection was already online before
  reassigning `connectionState`, and only calls `renderCatalog()` on a genuine transition into
  online (`!wasOnline`). This mirrors the `wasOffline` gating pattern the same function's caller
  (`checkHealth`) already uses for its own `loadInfo()`/`loadSessions()` refresh. `loadInfo()`
  itself still calls `renderCatalog()` directly whenever catalog data actually changes (initial
  load, settings changes, Codex login, etc.) — that path is untouched and still refreshes the
  dropdowns correctly when there's new data to show.

## Verification completed

- `go build ./...`, `go vet ./...` pass (Go side untouched).
- `node -c internal/web/assets/app.js` — syntax valid.
- Full Playwright suite (`npx playwright test tests/e2e/`) — all 4 existing tests pass
  (3 approval-panel tests + review-plan), no regression.
- Not verified: an actual live browser session confirming the dropdown now stays on the
  owner's chosen selection past a 5-second tick (no browser automation tool was available this
  session). The owner should confirm this visually once the container is rebuilt.

## Repository state

- Branch: `fix/model-selection-catalog-reset`, based on `origin/main` after the UI approval
  panel (`beeabbb`).
- Unrelated to the D0 ADR branch (`agent/opc-d0`) — kept as a separate PR since it's a
  different, unrelated change.
