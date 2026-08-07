# GoHermit Task Workspace Audit

Date: 2026-08-04  
Baseline: `origin/main` / `05e759d7f05ea6df012c312f02c10de534953e2c`  
Working branch: `feat/task-workspace-board`  
Worktree: `/Users/yuanxin/Developer/GoHermit-task-workspace-board`

## Scope and repository safety

The source repository's primary worktree is currently on `feat/weixin-channel`
with uncommitted and untracked changes. It was not modified. The target branch
was absent locally and remotely, so this work is isolated in the worktree above,
created directly from `origin/main`. The existing `.codegraph` directory was
checked read-only; the configured CodeGraph binary reported that no index exists,
so no index was rebuilt or changed.

The production service at port 8787 is outside this worktree and is not replaced
by this task. Preview/deployment work, if later approved, must use a separate
port such as 8791.

## Current authoritative data flow

```text
Employee revision
  -> EmployeeTask immutable snapshot (Employee Store)
  -> PrepareEmployeeTask: readiness + compact snapshot + dispatch journal
  -> stable Session (Session Store; no Run)
  -> explicit StartEmployeeTask: stable persisted Run
  -> existing Session/Run/Plan/Approval/Verification/Event/Artifact truth
  -> EmployeeTask API projection
  -> React Tasks Workbench
```

Recurring Employee Loops already create ordinary EmployeeTasks and route through
the same Prepare/Start path. Loop Definition/Revision/Invocation and scheduler
ownership remain in `internal/loop`, `internal/loopstore`, and
`internal/controlplane/employee_loops.go`; this feature must not introduce a
second workflow runtime.

## State ownership

| Object | Current owner | Board consequence |
| --- | --- | --- |
| EmployeeTask identity, prompt, pinned Employee/Skill/Knowledge/Memory/Project snapshot | `internal/employee/task.go` + `internal/employeestore` | Board references it; does not duplicate the execution state machine |
| Prepare readiness and dispatch journal | `internal/controlplane/employee_tasks.go` + `employeestore/dispatch.go` | Board may show prepared only from the existing projection |
| Session, event sequence, messages, recovery | `internal/session` | Detail and SSE continue to use Session ID |
| Run status, plan, tools, verification, cancellation/resume | `internal/session` + `internal/controlplane/employee_execution.go` | Board maps the real Run status; it never writes a Run status |
| Approval lifecycle | `internal/approval`, `internal/runcontrol`, `controlplane/approvals.go` | Board only invokes existing approve/deny APIs |
| Verification and modified-file metadata | Session/Run projection; bounded Employee artifacts | Board displays references and summaries only |
| Loop contract, revision, invocation, schedule | `internal/loop` + `internal/loopstore` | Workflow view must render these existing records |
| Employee activity | `employeestore/activity` | Lifecycle/reference timeline only; it cannot recover or drive a Run |
| Board columns, ordering, labels, filters, preferences, references | new bounded Board Store | View metadata only; no execution truth |

## Current schemas, APIs, and UI components

### EmployeeTask

`EmployeeTask` is schema v1. Durable states are intentionally only `queued` and
`cancelled`. It carries stable `SessionID` and `RunID` bindings, an immutable
`SnapshotDigest`, full bounded revision/policy references, and no copied Run
state. `BindTask` is idempotent and rejects different bindings. `TaskIndex` is
owner/Employee scoped and pages newest-first.

The control-plane `EmployeeTaskView` is the safe public projection. Its `State`
is derived as follows: queued/prepared from dispatch/session preparation;
waiting_owner from pending Approval or unapproved Plan review; running,
verifying, interrupted, completed, failed, and cancelled from the bound Run.
Artifacts are only surfaced after verified completion. The projection omits the
local workspace path and private runtime data.

Existing endpoints:

- `POST/GET /api/employees/{id}/tasks`
- `GET /api/employee-tasks/{taskID}`
- `POST /api/employee-tasks/{taskID}/start`
- `POST /api/employee-tasks/{taskID}/cancel`
- `POST /api/employee-tasks/{taskID}/resume`
- `GET /api/sessions/{sessionID}` and the existing Session-backed events SSE
- Session-scoped Approval list/decide endpoints

### React Workbench

`web/src/features/tasks/TasksWorkbench.tsx` already owns:

- Employee-aware Task creation with pinned Skills, Knowledge citations, Memory
  facts, ProjectBinding, and policy;
- URL-owned Employee/Project/State/time filters;
- responsive Table/Card list rendering;
- Task detail with Summary, Session/Run references, Session event Timeline,
  Plan, Tools, Approval, Verification, Artifacts;
- Prepare, explicit Start, Resume, Cancel, Approval decision, and one shared
  Session-keyed `useSessionEvents` subscription filtered by Run ID.

`web/src/api/sessionEvents.ts` has the required Session ID registry key,
Session-keyed local high-water sequence, duplicate filtering, reconnect, and
subscriber reference counting. A Board must not create one EventSource per
card; it can refresh aggregate projections or reuse this registry only for
visible bound Sessions.

The existing Ant Design 5 `ConfigProvider`/Layout/Card/Drawer/Tabs/Timeline/
Badge/Tag/Dropdown/Segmented/Select/Modal/Empty/Result/Skeleton/Alert/Tooltip
system and `web/DESIGN.md` are the visual source of truth.

## Reusable modules

1. `controlplane.ListEmployeeTasks` and `GetEmployeeTask` for authoritative
   per-task projections.
2. `projectEmployeeTask` in `employee_execution.go` for the Board Projection
   Mapper's status inputs.
3. Existing Session/Run, Approval, Verification, Artifact, and Loop APIs and
   stores; no Board-local execution or SSE model.
4. Existing React API decoders, URL search params, `useSessionEvents`, Ant Design
   responsive primitives, i18n enum labels, and mutation conflict handling.

## Gaps and new boundaries

1. There is no owner/workspace-scoped aggregate Task API. The current UI loads
   up to 100 Tasks per Employee, which is sufficient for the existing boundary
   but not a durable Board projection or efficient 500+ card view.
2. There is no persistent view metadata for columns, manual rank, labels,
   priority, due dates, notes, pinned state, filters, or templates.
3. `EmployeeTask` has no note type or Board metadata. The first safe slice keeps
   note/task classification in Board metadata; Notes never create Sessions/Runs
   and can later convert to an EmployeeTask through an explicit API.
4. The current public task projection lacks compact Employee name/provider,
   comment/activity counts, blocker/dependency metadata, Approval/Verification
   summary, and explicit source Loop references. These must be added as bounded
   projections or Board metadata, not inferred from card position or recent
   Session.
5. Existing detail routes do not yet expose a dedicated Board Inspector model;
   the current Task Detail can be extended with real Session/Run and activity
   links without copying sensitive content.

## Board projection contract

The initial mapper will classify from authoritative EmployeeTask plus, when
bound, Session/Run/Approval/Verification data:

| Authoritative condition | Default column |
| --- | --- |
| Board note/backlog metadata | Backlog |
| queued or prepared with no started Run | Todo |
| Run running/cancelling | In progress |
| Run terminal but pending Approval/Verification | Review |
| verified/approved/completed | Done |
| failed/interrupted | Original business column + failure marker |
| blocked metadata or unresolved dependency | Original business column + blocked marker |
| archived metadata | Archived, hidden by default |

The mapper must return the reason, source object IDs, and authoritative update
time for every card. It must explicitly identify stale/partial data rather than
silently treating it as a new Run state.

## Board Store design constraints

The Board Store will be owner/workspace scoped, schema-versioned, bounded,
strictly decoded, path-safe, symlink rejecting, atomically replaced, and
migratable without deleting or overwriting EmployeeTask data. It may persist
only:

- schema/version and Board definitions;
- columns, manual rank, labels, priority, due date, note/task kind, pinned;
- view preferences and non-sensitive filters;
- Task/Session/Run/Loop references.

It must never persist Run truth, Session sequence/events, Tool events, Approval
payloads, private reasoning, full prompts, secrets, or complete Artifact content.

## Phased implementation plan

### Phase 0 — audit and characterization

- This document records the baseline, ownership, mappings, gaps, and safety
  constraints.
- Add projection/store characterization tests before changing the UI.

### Phase 1 — Board Projection and Store

- Add a strict Board schema and repository under the configured single-owner
  workspace state root.
- Add a read/write Board API that stores view metadata only.
- Build a projection mapper from current EmployeeTask and Session/Run truth.
- Add migration, atomic-write, corruption, symlink, size-bound, and idempotence
  tests. Do not change Run/Loop state machines.

### Phase 2 — Task Board UI

- Preserve the current List view and Task Detail route.
- Add Board/List switching, URL-safe filters, columns, high-density cards,
  manual ordering, Note creation, and explicit Start confirmation for any drop
  into In Progress.
- Use refresh/bounded polling for aggregate changes; reuse Session SSE only for
  visible bound Sessions.

### Phase 3 — Inspector and Activity

- Extend the existing Task Detail/Drawer with authoritative Session, Run,
  Approval, Verification, Artifact, Loop, and bounded activity references.
- Keep sensitive fields out of all public projections.

### Phase 4 — Templates and Note conversion

- Persist Board templates and explicit Note -> EmployeeTask conversion with
  source/history references. Notes never create Session/Run on their own.

### Phase 5 — Loop Canvas

- Render and edit existing Loop Definition/Revision/Invocation records. All
  execution continues through Loop -> EmployeeTask -> explicit Start ->
  Session/Run; no second workflow runtime.

### Phase 6 — scale, accessibility, migration, E2E

- Verify 500+ cards, 375/768/1024/1440 layouts, keyboard/focus, reduced motion,
  offline/stale/error states, deterministic builds, Docker config, and full Go/
  Vitest/Playwright coverage.

## Non-negotiable acceptance checks

- Task creation is queued only and never auto-starts.
- Dragging to In Progress always opens Owner confirmation and calls existing
  Start only after confirmation.
- Board state is derived from real Session/Run/Approval/Verification state.
- One Session has at most one EventSource; Board cards never create SSE
  connections.
- Board metadata cannot mutate or replace EmployeeTask/Run/Session data.
- Notes do not create Sessions/Runs; conversion is explicit and preserves the
  source reference.
- Workspace writer lease and Employee concurrency remain enforced by the
  existing execution path.

