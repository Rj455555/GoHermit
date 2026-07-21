# A4 handoff: opt-in Codex live smoke (Phase A closeout)

## Goal

- Requested outcome: add a smoke test against the real Codex backend that is strictly opt-in (env or flag), never runs in default `go test ./...` or default CI, and never causes a paid call by default.
- Scope actually handled: the opt-in smoke test and its default-on guard test in `internal/evals`, a manual-dispatch-only CI job, and docs. This is the Phase A closeout task.

## Completed

- Changes:
  - `internal/evals/live_smoke_test.go`: `TestLiveCodexSmoke` — skips unless `GOHERMIT_LIVE_CODEX_SMOKE=1` (exact value, via shared `liveSmokeEnabled()`); skips (never fails) when `auth.ResolveCodex` finds no credentials; otherwise builds the Responses provider through the same constructor path as `app.NewProvider` and makes one bounded non-streaming Generate (trivial prompt, 60s timeout), asserting non-empty reply, `Attempts >= 1`, and non-negative usage; logs usage numbers only. `TestLiveSmokeRequiresExplicitOptIn` runs by default and asserts the gate rejects `""`, `"0"`, `"true"`, `"yes"` and opens only for `"1"`.
  - `.github/workflows/ci.yml`: `workflow_dispatch` trigger added; new `live-smoke` job gated on `if: github.event_name == 'workflow_dispatch'` — skipped on every push/PR. It writes the optional `GOHERMIT_CODEX_AUTH_JSON` secret to `$HOME/.codex/auth.json` (mode 600, the path the real auth chain reads) and runs the smoke; without the secret the test skips cleanly and the job stays green. The go/web-e2e/docker jobs are byte-for-byte unchanged.
  - `docs/ai/evals/v0.5.md`, `docs/model-providers.md`: the concrete opt-in command and CI mechanism.
- Files/packages: `internal/evals`, `.github/workflows/`, `docs/`.
- Decisions or ADRs:
  - Missing credentials are an environment fact, not a test failure: the smoke skips rather than fails, so the owner-controlled dispatch job is green both with and without the secret configured.
  - The smoke uses the production auth resolution and provider construction paths so it exercises the real chain end to end.

## Verification

- Focused tests: `TestLiveCodexSmoke` skips by default and skips with `GOHERMIT_LIVE_CODEX_SMOKE=1` + empty `CODEX_HOME` (no network egress, no paid call); `TestLiveSmokeRequiresExplicitOptIn` passes.
- Full tests: `go test ./... -count=1` all packages ok (live test skipped inside the evals package).
- Race test: not affected (no production code changed); the merged-main race suite was green before this branch.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; diff audited — no tokens, keys, or emails (only the `${{ secrets.GOHERMIT_CODEX_AUTH_JSON }}` placeholder); ci.yml parses (`yaml.safe_load`).
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: the live path itself was not exercised — no real credentials are available in this environment, by design. Acceptance for the live path rests on the verified skip logic, the production-code-path construction, and the documented owner-run command.

## Phase A acceptance (whole-phase check at closeout)

- Deterministic evals pass three consecutive runs: confirmed on merged main after A1-A3 and again on this branch (7-package set, 3/3).
- Plan state never outruns execution facts: enforced by the plan-fidelity graders plus the A2 review fix (`queuedVerificationRetry` no longer requires `Attempt > 0`, so an un-skipped repair reopens instead of falsely failing the Plan).
- Recovery never duplicates completed tools or WorkItems: enforced by the recovery graders (exactly-once event replay across all three journal crash stages, idempotent repeated Recover).

## Repository state

- Branch: `agent/opc-a4`, based on `origin/main` after the A1-A3 squash merges (`f6ea5a8`).
- Commit/PR: `test: add opt-in live codex smoke`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none (the CI job takes effect on merge but only fires on manual dispatch).

## Harness state

- Session/Run IDs used for live verification: none (no live calls made here).
- Last event sequence and terminal Run state: not applicable.
- Project memory updated: no.
- Recovery or workspace-reconciliation notes: none.

## Remaining work

- Known limitations: the `GOHERMIT_CODEX_AUTH_JSON` repository secret must be configured by the owner for the dispatch job to make the real call; without it the job is a green no-op. The smoke covers one bounded non-streaming call; streaming/replay live behavior remains covered by local-server tests only.
- Next concrete step: owner review/merge of this PR closes Phase A; Phase B (Team Templates, P1) follows.
- Required user input or authority: PR review/merge and the live-secret configuration remain with the owner.
