# GoHermit 0.8 development handoff

## Goal

- Turn the Knowledge Manager and other Electronic Employees into durable
  Agents that can own multiple recurring jobs.
- Give every job a Goal/Boundary/SOP contract, separate state, run logs,
  scheduling, and a contract-first Chinese/English UI.

## Completed

- Added Employee-owned Loop contracts, generated `LOOP.md`, bounded runtime
  state, manual/daily scheduling, and Invocation log projection.
- Routed every Loop invocation through the existing EmployeeTask Prepare/Start
  lifecycle and Session/Run kernel.
- Added at-most-once scheduled dispatch and definition-revision realignment.
- Rebuilt the Loops Workbench around When / Does / You get, short creation,
  Contract / State / Logs, and optional Advanced settings.
- Added a Loops tab to Employee details.
- Kept legacy non-Employee Loop Definitions compatible.

Key entry points are documented in `docs/ai/employee-loops.md`.

## Verification

- Focused domain, Store, Control Plane, Web API, scheduler, and recovery tests:
  passed.
- React unit/contract tests: 165 passed.
- React browser tests: 30 passed.
- Frontend coverage: 86.53% statements/lines, 80.61% branches, 80.07%
  functions.
- Full Go tests, race tests, vet, CLI build, Web build, TypeScript, lint, and
  deterministic production asset rebuild: passed.
- Paid-provider live inference was not part of deterministic verification.

## Repository state

- Development branch: `agent/employee-contract-loops-v0.8`.
- Version: `0.8.0-dev`.
- Protected local configuration and `sandbox/.gohermit/` remain untracked and
  untouched.

## Harness state

- Loop state is a rebuildable projection only. Invocation, EmployeeTask,
  Session, Run, Plan, Tool, Approval, Verification, and SSE journals remain
  authoritative.
- Completed Tool calls still use the existing recovery digest/turn frontier and
  are never replayed by the Loop layer.
- Verified Loop runs may use the existing bounded Artifact and owner-confirmed
  Memory Candidate paths; Loops never auto-promote Memory.

## Remaining work

- The scheduler is intentionally one process, one Workspace, and one writer.
- The first schedule vocabulary is manual or daily `HH:MM` with an IANA
  timezone; cron, event sources, and catch-up policies are future work.
- No automatic Git commit, push, deployment, or external publication was
  introduced.
