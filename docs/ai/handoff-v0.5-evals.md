# v0.5 deterministic eval fixtures handoff

## Goal

- Requested outcome: turn `docs/ai/evals/v0.5.md` (P0 item 1) into checked-in deterministic repository fixtures and graders for Plan fidelity, Handoff quality, recovery, verification, and final owner summary.
- Scope actually handled: new `internal/evals` package with JSON fixtures and graders, one `eval_test.go` per package that owns an unexported hook, the eval-to-grader mapping in `docs/ai/evals/v0.5.md`, and verified-state updates. P0 items 2-5 were not in scope.

## Completed

- Changes:
  - `internal/evals`: generic `LoadFixture` (JSON, `DisallowUnknownFields`), fixture types with `Build()` converters, `GradeTransitionScript`/`GradeTeamEventScript` (plan fidelity) and `GradeHandoffScenario` (handoff quality). Count fields (`evidence_count` etc.) generate filler entries so boundary scenarios stay human-auditable.
  - Five fixtures under `internal/evals/testdata/`: `plan_fidelity.json` (4 transition + 4 team-event scripts), `handoff_quality.json` (12 scenarios incl. exact-boundary accept), `team_verification.json` (4 scenarios), `recovery.json` (3 journal crash stages), `owner_summary.json` (3 scenarios).
  - `internal/team/eval_test.go` (`package team_test`, scripted worker): fail-closed without verifier evidence, bounded repair/reverify with preserved audit handoffs, attempts exhausted, worker error.
  - `internal/session/eval_test.go` (in-package, `commitStageHook`): crash at each of the three journal stages, exactly-once events by sequence, idempotent repeated `Recover`, interrupted run resumable.
  - `internal/web/eval_test.go` (in-package, `finalTeamHandoff`/`missionModifiedFiles`/`runTeam`): last Lead handoff wins, empty fallback, aggregated checks, deduped sorted files, durable completion event carries no prompt marker and stays within `team.MaxTextBytes`.
  - `docs/ai/evals/v0.5.md`: every eval checked with its grader or existing test reference; `docs/ai/context.md` verified state and `docs/ai/next-development-plan.md` P0.1 updated.
- Files/packages: `internal/evals`, `internal/team`, `internal/session`, `internal/web`, `docs/ai/`.
- Decisions or ADRs:
  - No new exported production API: graders needing unexported hooks (`commitStageHook`, `finalTeamHandoff`, `teamWorker`) live as in-package test files; the team verification grader uses only exported API and sits in `package team_test` to avoid an import cycle with `internal/evals`.
  - Single fixture source in `internal/evals/testdata/`; in-package graders load it via `../evals/testdata/` (Go test CWD is always the package dir).
  - Negative plan-op fixtures avoid `taskplan.Reopen` partial-mutation paths by using unknown step IDs, so rejected-op state preservation holds.

## Verification

- Focused tests: `go test ./internal/evals/ ./internal/team/ ./internal/session/ ./internal/web/ -count=1` green; repeated 3 consecutive times (pass^3 acceptance) with no flakes.
- Full tests: `go test ./... -count=1` all packages ok on the Mac host.
- Race test: `go test -race ./... -count=1` all packages ok.
- Vet/build/cross-build: `gofmt -l` clean, `go vet ./...` ok, `go build ./cmd/hermit` and `./cmd/hermit-web` ok, `pnpm test:e2e` (1 Playwright test) ok, `docker compose config --quiet` ok.
- Skipped checks and reason: cross-builds and Docker image acceptance not rerun (no change to build/packaging surface; CI reruns them on push).

## Repository state

- Branch: `agent/adaptive-plan-v0.5`.
- Commit/PR: uncommitted at handoff; pending owner review before commit.
- Working tree: new `internal/evals/`, `internal/team/eval_test.go`, `internal/session/eval_test.go`, `internal/web/eval_test.go`, edited `docs/ai/evals/v0.5.md`, `docs/ai/context.md`, `docs/ai/next-development-plan.md`; `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Harness state

- Session/Run IDs used for live verification: none (all graders are deterministic tests on `t.TempDir()`).
- Last event sequence and terminal Run state: not applicable.
- Project memory updated: no.
- Recovery or workspace-reconciliation notes: none.

## Remaining work

- Known limitations: Playwright E2E still runs against a static server, not the live Go backend; browser-level SSE/refresh grading stays with `review-plan.spec.ts` plus web integration tests.
- Next concrete step: owner review and commit (suggested message: `test: add deterministic v0.5 eval fixtures and graders`), then push so CI reruns the Linux baseline; continue with P0 item 2 (Explorer-proposed bounded substeps).
- Required user input or authority: commit/push remain with the owner.
