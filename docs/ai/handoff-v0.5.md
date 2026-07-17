# GoHermit v0.5 handoff

## Milestone

- Version: `0.5.0-dev`
- Branch: `agent/adaptive-plan-v0.5`
- Parent milestone: v0.4 commit `edd2d59`
- Runtime boundary: local, single-owner, one workspace, one active executing Run, no automatic Git or external side effects

## Implemented

- Prepared Session/event commit journal with crash-stage injection tests and idempotent recovery.
- Durable-before-publish parent and hidden Worker events; raw tool arguments are not persisted.
- Presentation-neutral `internal/runcontrol` transitions and consistent resumable timeout versus terminal cancellation.
- Task-specific single/Team Plan titles and parallel read-only Team Plan state.
- Session Plan modes: `auto` and review-first, with durable queued state, approve API/UI, refresh recovery, and pre-execution cancellation.
- Intent-based Team topology, parallel read-only evidence/preflight, one writer, structured Handoffs, Verifier gate, and bounded repair/reverify.
- Playwright Chromium E2E and GitHub CI for Linux tests/race/vet, cross-builds, E2E, and Docker build.
- Capability eval definition: `docs/ai/evals/v0.5.md`.

## Verification on the Windows development host

- Focused and package tests for Session, Agent, app Worker, taskplan, Team, Web, and review approval passed during development.
- Playwright review/refresh/approval E2E: passed (`1/1`).
- `go vet ./...`: passed.
- Native CLI/Web build and Linux amd64, Windows amd64, Darwin arm64 CLI/Web cross-builds: passed.
- `git diff --check`: passed.
- The final aggregate `go test ./...` reached all packages, but Windows Application Control blocked newly generated transient `runcontrol.test.exe` and `web.test.exe`; this is an execution-policy failure, not a test assertion failure. The same packages passed focused runs earlier.
- Windows has no Docker CLI and the Mac mini SSH endpoint was offline. Linux race and Docker acceptance are therefore enforced by `.github/workflows/ci.yml` after push.

## Recovery and security facts

- `commit.json` is bounded, mode-0600, versioned, Session-ID checked, Plan-validated, and removed only after checkpoint and event persistence.
- Unknown/corrupt Session, Plan, or journal data fails closed.
- Review approval starts a queued Run but does not grant permission-required tools or Operator actions.
- Provider credentials, prompts, private reasoning, stream chunks, and raw unbounded tool output remain excluded from Session/event storage.
- `.codegraph/`, `.gohermit/`, browser artifacts, credentials, and debug binaries are not committed.

## Minimum next-agent reading

Read only `AGENTS.md`, `docs/ai/context.md`, and `docs/ai/harness.md`; add `docs/ai/plan-mode.md` or `docs/ai/team.md` when changing those domains. The next ordered work is `docs/ai/next-development-plan.md`.
