# UI handoff: approval prompt component (minimal)

## Goal

- Requested outcome: a minimal workbench component so the owner can see and handle C3's real approval requests in the browser instead of hand-writing curl.
- Scope actually handled: frontend only (`internal/web/assets/` + Playwright spec). Backend untouched (one non-breaking addition: the `request()` helper now carries `error.status` for 409 detection).

## Completed

- Changes:
  - `index.html`: `<section id="approval-panel">` between messages and plan panel; hidden by default.
  - `app.js`: `loadApprovals()` (GET pending on session open/refresh; failures degrade to empty so a missing endpoint never breaks session load), `renderApprovals()` (empty → panel fully hidden, no placeholder), `approvalCard()` (shows ONLY tool / resource_paths / args_summary / countdown — `textContent`, no injection), `decideApproval()` (in-flight disable, POST decide, 409 → "该请求已被处理或已过期" toast + list refresh), cosmetic 1s countdown from `expires_at` (zero → "已过期，等待确认" styling, buttons disabled; the backend stays authoritative).
  - SSE: the four `approval_*` types added to the existing connection; `approval_requested` re-fetches the pending list (payload carries only ids), decided/expired/consumed remove the entry in place. No polling.
  - `styles.css`: amber panel + expired red tint in the existing design language, responsive rules matching plan-panel.
  - `tests/e2e/approvals.spec.ts` (new, 3 tests, static-server + route-mock harness, page-object pattern): panel renders tool/paths/summary/countdown, approve click posts the correct body and clears, empty state shows no panel. `review-plan.spec.ts` unmodified and passing.
- Files/packages: `internal/web/assets`, `tests/e2e`.
- Decisions or ADRs: on `approval_requested` the panel re-fetches rather than trusting the event payload (events carry only request_id/tool/status); frontend countdown is cosmetic only.

## Verification

- `node --check internal/web/assets/app.js` ok; `gofmt -l` clean; `go vet ./...` ok; `go build ./...` ok; `go test ./... -count=1` all packages ok (backend untouched).
- E2E: `npx pnpm@11.9.0 test:e2e` — 4 passed (3 new + existing review-plan).
- `git diff --check` clean; no secrets.
- Skipped checks and reason: race/compose not rerun (no Go change); the manual browser path (acceptance 1) was not exercised here — covered by the mocked e2e instead.

## Acceptance mapping

1. Visible panel, approve executes, prompt disappears: covered by the mocked e2e (decide body + clear) over the same endpoints the real backend serves; manual browser verification left to the owner.
2. Deny/expiry close out cleanly: 409 refresh + countdown-expired styling + SSE removal paths.
3. No requests → panel absent entirely: `renderApprovals` hidden branch + e2e empty-state test.

## Repository state

- Branch: `agent/opc-ui-approvals`, based on `origin/main` after C3 (`8248962`).
- Commit/PR: `feat: add approval prompt panel to workbench`, PR to `main`.
- Working tree: `sandbox/.gohermit/` untracked runtime data; nothing else.
- External state changed: none.

## Remaining work

- Known limitations: the panel lists only pending requests (no history view); `approval_requested` triggers a refetch (one extra GET per request).
- Next concrete step: owner review/merge, then Phase D (isolated worktrees) — the first ADR must be numbered **0012** (0011 is the approval contract).
- Required user input or authority: PR review/merge remains with the owner.
