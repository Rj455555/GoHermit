# GoHermit v0.7 AI handoff

## Delivery state

- Version: `0.7.0-dev` (development version; no tag or public release).
- Phases 1–9: Owner-approved and squash-merged.
- Phase 9 baseline: `36987d92291c2781bfa3b997bdaab8002bd9c019`.
- Phase 10: deterministic evals, Docker/persistence acceptance, docs/version
  closeout; no new product capability.
- Default test paths use Fake Providers and make no paid model calls.

Read root `AGENTS.md`, `docs/ai/context.md`, and `docs/ai/employees.md` before
changing v0.7 behavior.

## What shipped

1. Durable Employee domain with explicit active/disabled/archived lifecycle,
   immutable revision snapshots, ProjectBindings, policy/budget/concurrency,
   and initials/Emoji-only Avatar.
2. Owner-scoped fail-closed Employee Store and Control Plane CRUD/Dry Run APIs.
3. Native Skill Catalog plus instruction-only SKILL.md Adapter; exact
   ID/version/digest pinning and policy-only narrowing.
4. Deterministic local Knowledge/Citations and isolated Employee Memory with
   verified Candidates, explicit accept/reject, edit, and Forget.
5. Persistent Task Inbox. Creation queues only and produces no execution side
   effect.
6. Runtime Preparation with live readiness, stable Session ID, schema-v6
   compact snapshot, bounded dispatch journal, and restart reconciliation.
7. Explicit manual Start with at-most-one Run, projected state,
   cancel/resume/restart, one-Employee/one-Workspace gates, verified Candidate
   generation, and bounded Artifact metadata.
8. Employees and Tasks Web UI using real Control Plane APIs and existing
   Session SSE with sequence recovery and Task/Session isolation.
9. Team Role-to-Employee mapping with strict TeamTemplate v1→v2 migration,
   immutable assignment snapshots, model precedence, private per-Worker
   context, and non-addressable hidden Worker Sessions.

## Critical contracts

- Session/Run is the execution truth. There is no Task SSE or second
  Run/Event/Plan/Approval/Verification/recovery state machine.
- Prepare cannot create a Run or call a Provider/Tool. Start is explicit and
  persists the Run before execution.
- Task `SnapshotDigest` never includes mutable state or Session/Run bindings.
- Full Employee revisions stay in EmployeeTask/Employee Store. Session and
  hidden Worker compact snapshots are at most 64 KiB.
- Permission formula includes AgentProfile ToolPolicy. Skills never grant.
- Knowledge is local/configured-root-only; Memory Facts require explicit Owner
  acceptance.
- Activity stores bounded references only and never drives recovery/projection.
- One service executes one configured Workspace. `/api/projects` returns only
  that Workspace and Home is never scanned.
- Every public API treats a hidden Worker Session as absent before reading
  messages/events or allowing any Run/Plan/Approval mutation.
- Recovery semantic tool matching uses canonical JSON digest only at the
  interrupted Turn frontier; legacy empty-digest records use exact Call ID.

## Storage and compatibility

- Employee Store schema v1 is new and fail closed.
- Session schema v6 explicitly migrates v1–v5; old non-Employee Sessions keep
  the legacy path.
- TeamTemplate v2 strictly migrates a version-specific v1 wire shape; v1 with
  `employee_id` is corrupt.
- Loop, Owner, Project Memory, Agent, Approval, and Verification schemas remain
  compatible.
- Files are bounded, strict JSON/JSONL, path-contained, symlink-safe, 0600, and
  atomically replaced per file. No cross-file transaction is promised.

## Final acceptance

`internal/evals/v07_contracts_test.go` directly checks version, Employee/
Project snapshot isolation, Skill narrowing, Knowledge/Memory/Project Memory
separation, Forget, and a valid persistent Employee/Task/Knowledge/Memory/
Session/Loop fixture. Its regression manifest pins the cross-package tests for
explicit Start, no-Run Prepare, at-most-once recovery, completed Tool replay,
external Workspace changes, verification, immutable Team assignment, hidden
Session security, legacy migration, SSE, and browser refresh.

`internal/evals/docker_acceptance.sh` runs in the Docker CI job. It renders
Compose, builds and starts the image, waits on real health and `/api/info`,
requires loopback binding and `0.7.0-dev`, snapshots byte digests for `/data`
and workspace Session data, rebuilds/recreates the container, and requires
both manifests to remain identical. It also verifies Employee, Task,
Knowledge, Memory, Session, and Loop records and scans them for
credential-shaped content.

The authoritative final command/CI evidence and commit SHAs are recorded in
`IMPLEMENTATION_PLAN.md` and the Phase 10 Draft PR.

## Accepted risks

- In-process locks and per-file atomicity, not cross-process/cross-file
  transactions.
- One running Task per Employee, one service-wide execution task, one mutation
  writer, and no cross-Employee read-only concurrency.
- Hidden Worker output remains durable internally for recovery.
- The local Web server is unauthenticated and unsupported for public exposure.
- Shell/plugins are constrained but not an OS sandbox.
- Global Tasks reads the newest 100 records per Employee.
- Exact private-Memory echo rejection can conservatively reject a public
  Handoff containing the same text.
- Paid-provider behavior is covered by deterministic protocol fixtures; live
  Codex smoke is manual and opt-in.

## Next work

Use `docs/ai/next-development-plan.md`. Do not begin a v0.8 item without a new
Owner gate and, where indicated, a dedicated ADR.
