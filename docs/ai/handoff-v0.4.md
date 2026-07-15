# GoHermit v0.4 Live Plan handoff

## Outcome

- Added a durable Cursor-style checkbox Plan to every new Run.
- Single-Agent Plans expose `analyze -> execute -> verify -> report`; Team Plans mirror `explore -> build -> review -> repair -> verify -> lead` WorkItems.
- Plan state is schema-versioned, bounded, atomically checkpointed with the Run, streamed as replayable SSE snapshots, and restored after refresh or process recovery.
- A Run cannot be marked completed unless its Plan has reached the matching completed state.
- The Web workbench shows current step, bounded public detail, completed count, progress bar, failures, and cancellation without exposing prompts or private reasoning.

## Key implementation

- Domain and invariants: `internal/taskplan/plan.go`
- Single-Agent lifecycle bridge: `internal/agent/agent.go`
- Team lifecycle bridge and SSE snapshots: `internal/web/server.go`
- Hidden Worker event filtering: `internal/app/team_worker.go`
- Session schema v4 and v1/v2/v3 migrations: `internal/session/session.go`
- Web checklist: `internal/web/assets/index.html`, `app.js`, and `styles.css`
- Contract and decision: `docs/ai/plan-mode.md`, `docs/adr/0009-durable-live-plan.md`

## Verification

Verified on the Mac development host with Go 1.26.5:

- formatting inspection and frontend JavaScript syntax check
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- native CLI and Web builds
- Linux amd64 and Windows amd64 CLI/Web cross-builds
- `docker compose config`
- local Docker image rebuild, healthy container, `/api/health`, `/api/info`, and static Live Plan asset acceptance

The Docker workbench at `127.0.0.1:8787` reports `0.4.0-dev`. A paid or real Codex model call was deliberately not performed; it remains an explicit opt-in smoke test.

## Repository state

- Branch: `agent/live-plan-v0.4`
- Initial implementation commit: `c81fdb3`
- Draft PR: https://github.com/Rj455555/GoHermit/pull/3
- PR base: `agent/personal-team-v0.3`
- Runtime-only `sandbox/.gohermit/` remains untracked and must not be committed.

## Next work

Read only `AGENTS.md`, `docs/ai/context.md`, `docs/ai/plan-mode.md`, and the target package first. The ordered v0.5 direction is in `docs/ai/next-development-plan.md`: task-specific Plan refinement, repair-loop evaluation, clearer token/budget UX, and opt-in live smoke coverage. Do not turn public Plan text into hidden chain-of-thought or allow model text alone to assert progress.
