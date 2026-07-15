# AI handoff: v0.3 Personal Agent Team

Updated 2026-07-16. This file records the exact milestone state.

## Goal and completed scope

- Added a bounded single-owner Personal Agent Team above the existing Harness: Explorer, Builder, Reviewer, repair Builder, Verifier, then owner-facing Lead.
- Added Mission/WorkItem/Handoff/budget state, schema v3 migration, stable hidden Worker Sessions, completed-work replay protection, one writer, verification gating, and terminal cancellation/failure cleanup.
- Added an Owner Profile outside repositories with confirmed facts, secret-pattern rejection, compact context injection, and view/edit/forget Web APIs.
- Added team activity and owner settings to the Codex-style Web workbench plus explicit offline/reconnecting state and a persistent Windows SSH-tunnel helper.
- Restricted read-only/verification roles to read-only non-mutating plugin tools. Team execution is currently Web Session/Run API only; CLI and legacy `/api/run` reject `team` explicitly.
- Accepted ADR 0008. The next milestone order is in `docs/ai/next-development-plan.md`.

## Verification completed on the current worktree

- Source formatting and `git diff --check` passed before the final documentation update; rerun both before commit.
- Focused team, Owner Profile, context, schema migration, Worker replay, app, and Web API tests passed during implementation.
- All 19 current packages, including every test package, compile with the isolated official Go 1.24.8 toolchain on Windows.
- An earlier v0.3 pass executed 14 of 15 package test binaries successfully; `internal/config` was blocked by Windows Application Control.
- After the latest batch-failure and plugin-policy tests were added, Windows Application Control blocked newly built unsigned test executables. Compilation succeeded, but those latest tests have not executed on Windows.
- After the final safety fixes, `go vet ./...` plus Windows amd64, Linux amd64, and Darwin arm64 CLI/Web builds passed. Front-end JavaScript syntax, tunnel-script parsing, `git diff --check`, and a high-confidence secret scan also passed.
- On the Mac arm64 development host, formatting inspection, `go test ./...`, `go test -race ./...`, `go vet ./...`, native CLI/Web builds, Linux amd64 and Windows amd64 cross-builds, and `docker compose config` all passed.
- Docker rebuilt and replaced the stale 11-hour-old container. `/api/health` reports `0.3.0-dev`; `/api/info` exposes `team,coding,review,devops`; the loopback page contains Team activity, Owner settings, and offline-recovery UI; the container is healthy.

## Pending verification and release work

- Run one explicit Codex live Team smoke only with the owner's approval/account; it is never a default test. That smoke should exercise a new Team Session, per-role activity, Lead-only final response, follow-up, cancel or interruption, refresh/SSE replay, and resume.
- Push the amended milestone commit and update the intended pull request.

## Repository and external state

- Branch: `agent/personal-team-v0.3`, based on `4d987c7`.
- The milestone commit is present locally and on the Mac worktree; this handoff update is folded into it before push.
- The Mac repository switched to the v0.3 branch and its loopback Docker service was rebuilt. A hidden Windows SSH-tunnel helper now reconnects port 8787 automatically.
- `sandbox/.gohermit/` is runtime data and must remain untracked.

## Recovery notes

- Parent Team Sessions persist Mission state in schema v3. Each WorkItem receives a deterministic hidden execution Session before it starts.
- Completed child Sessions are converted back to Handoffs without another provider call. Running parent/child state becomes `interrupted` after process loss. Explicit owner cancellation is terminal and not resumable.
- A Worker/budget/verification failure cancels any other running batch items so a terminal Mission cannot retain phantom `running` work.
