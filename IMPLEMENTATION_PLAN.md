# GoHermit v0.7 Electronic Employees Implementation Plan

## Executive Gate Summary

### Current approval status

- Revised plan: Owner-approved on 2026-07-28.
- Phase 1: Owner-approved on 2026-07-28.
- Phase 2: Owner-approved on 2026-07-28 after both security Gate revisions.
- Phase 3: `OWNER_APPROVED`; squash-merged through PR #36.
- Phase 4: `OWNER_APPROVED`; externally squash-merged through PR #37 into
  `origin/main@e65bc1196e73e0b8962b012be76c0852f48e8c3c`.
- Phase 5: `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER` on clean branch
  `agent/electronic-employees-v0.7-phase5`.
- Phase 6 and every later product-code change remain blocked.
- Required terminal status:
  `WAITING_FOR_PHASE_5_REAPPROVAL`.

### Fixed invariants

- Employee is a durable owner-scoped entity, distinct from Role, Session,
  model, Skill, and Project.
- EmployeeTask reuses exactly one existing Session/Run execution kernel. There
  is no second Agent, Run, Plan, Approval, Verifier, Event, or recovery state
  machine.
- v0.7 is one Service process for one startup-configured Workspace. It does not
  scan Home, dynamically open another Workspace, or manage multiple Session
  Stores.
- One Employee may have many queued Tasks but only one running Task. One
  Workspace has one mutation writer. Cross-Employee read-only concurrency is
  deferred.
- Skill, Tool, and Permission remain separate. AgentProfile ToolPolicy is part
  of the base permission intersection; Skills can only narrow it.
- Employee Activity is lifecycle/reference metadata only. Session/Run remains
  the execution truth and existing Session SSE remains the execution stream.
- Full Employee revision snapshots stay in EmployeeTask/Employee Store.
  Session/Team recovery snapshots are compact, immutable, and independently
  bounded.
- Employee Memory is private; verified Tasks produce Candidates that require
  explicit Owner acceptance before durable long-term Memory.
- GoHermit never automatically commits, pushes, creates PRs, merges, deploys,
  sends external messages, installs Skills, starts a daemon, or auto-starts a
  queued Task.

### Owner decisions

1. Task creation queues only; explicit Start is mandatory.
2. Disable is reversible; Archive is terminal and retains history.
3. Avatar supports generated initials and Emoji only—no upload, local path, or
   remote URL.
4. `GET /api/projects` returns only the current Service Workspace.
5. Verified Tasks create Memory Candidates; Owner acceptance is mandatory.
6. Concurrency remains conservative: one running Task per Employee, one
   mutation writer per Workspace, and no cross-Employee read-only concurrency.

### Phase table

| Phase | Scope | Gate state |
|---|---|---|
| 1 | Employee Domain and ADR | `OWNER_APPROVED` |
| 2 | Employee Store, Control Plane, and CRUD API | `OWNER_APPROVED` |
| 3 | Skill Catalog, SKILL.md Adapter, policy intersection, context contract | `OWNER_APPROVED` |
| 4 | Knowledge Base and Employee Memory | `OWNER_APPROVED` |
| 5 | Employee Task Inbox persistence and API | `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER` |
| 6 | Runtime Preparation | `BLOCKED_BY_GATE` |
| 7 | Manual Execution Lifecycle | `BLOCKED_BY_GATE` |
| 8 | Employees and Tasks Web UI | `BLOCKED_BY_GATE` |
| 9 | Team Role to Employee mapping | `BLOCKED_BY_GATE` |
| 10 | Evals, Docker, docs, and v0.7 release closeout | `BLOCKED_BY_GATE` |

### Current phase scope

- Phases 1 through 4 are Owner-approved and closed to further implementation.
- Revise only the Phase 5 immutable Task Snapshot Digest boundary. The digest
  covers immutable Task business content and excludes lifecycle state,
  timestamps, and future Session/Run execution bindings.
- Keep Phase 6 and all later phases blocked until the next explicit Owner Gate.

### Current prohibited work

- No prepared/running/completed Task state, Session schema v6, Task Runtime,
  model/provider/tool call, workspace lease, UI, Team Role Mapping, version,
  or release change.
- No Phase 6 implementation.
- Keep Draft PR #38 Open and Draft. No merge, auto-merge, force push, or
  replacement PR.
- No modification, deletion, movement, staging, or cleanup of protected
  untracked user files.

### Gate validation commands

```bash
git branch --show-current
git status --short
go test ./internal/employee ./internal/employeestore ./internal/controlplane ./internal/web -count=1
go test ./internal/loop ./internal/loopstore ./internal/session -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
git diff --check
```

### Next Gate

Phase 6 may start only after the Owner explicitly reapproves the Phase 5 Gate,
for example:

```text
批准 Phase 5，开始 Phase 6
```

Until then, stop with:

```text
STATUS: WAITING_FOR_PHASE_5_REAPPROVAL
```

---

Plan status: `WAITING_FOR_PHASE_5_REAPPROVAL`
Baseline: `origin/main@e65bc1196e73e0b8962b012be76c0852f48e8c3c`
Feature branch: `agent/electronic-employees-v0.7-phase5`
Last audited: 2026-07-28

This file is the only source of truth for v0.7 phase status, scope, evidence,
deviations, and remaining risk. The Executive Gate Summary plus the currently
authorized phase section is the minimum required reading path. Phases 1 through
4 are Owner-approved. Phase 5 implementation and Gate verification are
complete and awaiting Owner approval; Phase 6 through Phase 10 remain blocked.
Each Owner approval authorizes exactly one phase.

## 1. Current-state evidence

### 1.1 Audit methods and repository state

- The branch was created from the actual latest `origin/main`,
  `b2a187f feat: add v0.6 Loop Workbench (#34)`, with no tracked local changes.
- The repository's remote `.codegraph/` index was queried before source search.
  CodeGraph was used to trace the Team/Template/Context, Session/Run,
  Control Plane/Loop, Web/SSE, approval/tool-policy, and storage paths.
- Graphify 0.9.22 was run against a clean `git archive origin/main` snapshot in
  `/tmp`, not against the working tree. The no-key semantic pass refused the 73
  documents, so the audit failed over to `--code-only` without reading or
  requesting credentials. The resulting local-only graph contains 1,759 nodes,
  4,980 edges, and 75 communities. Its main integration hubs include
  `controlplane.Service`, `session.Session`, `team.Mission`, `app.TeamWorker`,
  `teamtemplate.Template`, `contextmgr.Manager`, `loop.Invocation`, and
  `web.Server`. Graphify outputs are audit evidence only and must never be
  committed.
- The protected untracked `.claude/`, `.codegraph/`, `.cursor/`, `.gemini/`,
  `.mcp.json`, untracked `docs/*` drafts, and `sandbox/.gohermit/` were not
  opened as product sources, modified, moved, staged, or cleaned.

### 1.2 Existing domain and runtime evidence

| Current fact | Concrete evidence | v0.7 consequence |
|---|---|---|
| A Role is a temporary Mission responsibility, not a durable person. | `internal/team/team.go:20` defines `type Role string`; `team.go:86` puts one Role on each `WorkItem`; `team.go:151` makes WorkItems part of a `Mission`. | Employee must be a new first-class owner-scoped entity. Role remains runtime-only. |
| Agent presets are static runtime policies. | `internal/config/config.go:93` defines `AgentPreset`; `config.go:161` declares `team`, `coding`, `review`, `devops`, and internal role presets; `ResolveSelectionWithModels` validates a runtime selection. | An Employee references an Agent profile but is not an AgentPreset or model. |
| TeamTemplate stores provider/model/budget by Role. | `internal/teamtemplate/template.go:33` defines `RoleSelection`; `template.go:44` defines schema-v1 `Template`; `SelectionForRole` and `EffectiveSelections` resolve overrides. | Phase 9 adds an optional Employee ID without removing the legacy selection path. |
| Owner Profile is owner-scoped, bounded, and secret-rejecting. | `internal/owner/profile.go:56` defines `Profile`; `NewStore` resolves outside the workspace; `save` uses mode-0600 atomic writes; `Validate` calls `LooksSecret`; `Markdown` emits confirmed context. | Employee identity/configuration follows the same owner-scoped safety posture, but uses independent per-employee directories. |
| Context currently has no Employee layer. | `internal/contextmgr/context.go:62` `BuildRun` orders system, Owner Profile, project `AGENTS.md`, Project Memory, recovered state, current goal, and recent messages. `SetOwnerProfile` is the only durable-personal injection point. | Employee, Skills, Knowledge, and private Memory require explicit bounded layers with deterministic precedence. |
| Project Memory is separate and verified. | `internal/contextmgr/memory.go:24` defines schema-v1 `ProjectMemory`; `UpdateProjectMemory` writes bounded/redacted facts from a completed Run into `.gohermit/memory/`. | Employee Memory must not reuse or leak through the Project Memory file. |
| Session/Run is the execution kernel. | `internal/session/session.go:48` defines Run states; `session.go:151` defines schema-v5 `Session`; `Session.NewRun` rejects a second active Run. `internal/controlplane/runs.go` owns create/start/resume/cancel. | EmployeeTask must bind to, not replace, one Session/Run. |
| Session recovery is durable-before-visible and explicitly migrated. | `session.Store.CommitEvents` and `commitLocked` prepare `commit.json`, atomically apply `session.json` and ordered events, then remove the journal. `Store.Load` accepts v1-v5, runs `migrateV1` through `migrateV4`, and fails on unknown versions. | Phase 6 uses an additive schema-v6 migration and the same event/recovery path. |
| Team workers already reuse hidden Sessions. | `internal/app/team_worker.go:61` `TeamWorker.Execute` resolves the Role runtime, injects Owner context, creates or recovers a stable hidden child Session, and runs the existing Runner. | Employee assignment must add a snapshot/context input to this adapter, not create another Worker engine. |
| One workspace writer is already enforced. | `internal/team/team.go:450` `Mission.Start` rejects another running mutating WorkItem; coordinator tests cover parallel readers and one writer. `controlplane.Service.TryAcquireRun` currently also provides a conservative service-wide run gate. | Employee concurrency must compose with, never weaken, these gates. Worktrees remain out of scope. |
| Approval is one-shot, scoped, expiring, and stored in Session. | `internal/agent/approvals.go:36` creates and durably emits an approval request; `internal/approval` owns lifecycle; ADR 0011 separates Plan approval from call approval. | Employee/Skill policy can only narrow the set of calls. It cannot pre-approve or broaden a call. |
| Tool and shell policy fail closed. | `internal/tool/tool.go:24` puts Permission and `MutatesWorkspace` on definitions; `Executor.ExecuteApproved` marks one invocation only. `internal/policy/policy.go` uses an allowlist and rejects traversal/operators/destructive commands. `builtin.Workspace.resolve` rejects absolute paths, traversal, credentials, `.git`, `.gohermit`, and symlink escapes. | Effective capabilities are an intersection before registry exposure, followed by the unchanged per-call policy and approval checks. |
| Loop Mode already maps a durable owner resource to one Session/Run. | `internal/loop/definition.go:126` and `internal/loop/invocation.go:42` define snapshotting resources; `controlplane.StartLoopInvocation` creates one Session and starts one Run; reconciliation derives Invocation state from Run state. | EmployeeTask should reuse this snapshot-and-bind pattern and its idempotency lessons. |
| Web is a thin Control Plane transport. | `internal/web/server.go:30` documents the boundary; `Handler` exposes Session, Approval, Loop, Owner, and Settings resources; `GET /api/sessions/{id}/events` is the shared SSE stream. | Employee HTTP handlers parse/validate/map errors only. Runtime state remains in Control Plane + Session/Run. |
| The current UI has four surfaces. | `internal/web/assets/index.html` and `loops.js` implement Dashboard / Agent / Loops / Settings. Existing Playwright covers review Plan, approvals, Loop Workbench, navigation, and refresh recovery. | v0.7 adds Employees and Tasks while preserving all existing surfaces and tests. |
| Store patterns are bounded and atomic. | `internal/storage/storage.go:15` provides `AtomicWrite`; Owner, TeamTemplate, Loop, and Session stores use mutexes, strict schema checks, bounded files, and mode-0600 writes. Loop/TeamTemplate tests cover corrupt data, concurrent access, stable order, import secrets, and compatibility. | Employee stores use the shared atomic primitive, strict JSON, explicit schema versions, bounded pagination, and fail-closed load. |

### 1.3 Existing decisions that remain binding

- ADR 0004: inspectable versioned file storage.
- ADR 0005: low-write, bounded, atomic persistence.
- ADR 0007: durable Session separated from bounded Run.
- ADR 0008: one-owner Team, structured Handoffs, one writer, no free-form
  persistent agent chat.
- ADR 0009: Live Plan is public execution state, not private reasoning.
- ADR 0010: durable Run control, review-first Plan approval, bounded repair.
- ADR 0011: scoped one-shot call approval; unattended means deny.
- ADR 0012 remains proposed and unresolved. Its WIP self-commit design conflicts
  with the no-auto-commit invariant, so no Worktree Foundation work is allowed
  in v0.7.
- `docs/ai/context.md`, `docs/ai/harness.md`, `docs/ai/team.md`,
  `docs/ai/plan-mode.md`, `docs/ai/next-development-plan.md`, and
  `docs/ai/handoff-v0.6-loop-workbench.md` define the current operational
  invariants and regression surface.

## 2. Capability statement

GoHermit v0.7 adds owner-created, long-lived Electronic Employees. An Employee
has durable identity, job charter, behavior boundaries, an explicit provider
selection, Agent profile, versioned Skill bindings, deterministic Knowledge
sources, private Employee Memory, Project bindings, permission/budget policy,
concurrency policy, Tasks, Artifacts, and activity history.

The capability is an owner-scoped control layer over the existing runtime:

```text
Employee definition
  -> immutable EmployeeRevisionSnapshot
  -> EmployeeTask
  -> exactly one existing Session
  -> exactly one active Run at a time in that Session
  -> existing Agent/Team, Plan, Approval, Verification, Event, and recovery
```

Success means the Owner can create several Employees, queue several Tasks per
Employee, manually launch a Task, recover its Session/Run after refresh or
restart, inspect bounded activity/history, and manage private Skills,
Knowledge, and Memory without changing the behavior of users who never use
Employees.

## 3. Terminology and domain boundaries

- **Employee**: durable owner-scoped identity and policy. It is not a Role,
  Session, model, Skill, Project, process, or free-running daemon.
- **Role**: temporary responsibility inside one Team Mission. One Employee may
  perform different Roles in different Tasks.
- **Agent Profile**: existing static behavior/tool boundary selected by ID.
- **Model selection**: provider/access/model names validated at execution time;
  credentials remain only in the existing server-side credential store.
- **Skill**: immutable, versioned, declarative method plus requested
  capabilities. It is not a tool and grants no permission.
- **Tool**: executable runtime operation already registered with a Permission
  and mutation flag.
- **Permission**: the effective side-effect ceiling for a Task; exact calls may
  still require ADR 0011 approval.
- **Knowledge Base**: bounded, cited, deterministic reference material. Its
  content is data, not trusted system instruction.
- **Employee Memory**: private long-term facts derived only from verified
  Employee Tasks and explicitly manageable by the Owner.
- **Project Memory**: shared workspace facts under `.gohermit/memory/`; it
  remains independent from Employee Memory.
- **ProjectBinding**: explicit grant connecting an Employee to a canonical
  workspace and a narrowing project policy.
- **EmployeeTask**: durable owner request, snapshot, queue record, and binding
  to the existing execution kernel. It does not own a second Run state machine.
- **Session / Run**: unchanged execution source of truth.
- **EmployeeArtifact**: bounded, redacted output reference linked to Task,
  Session, and Run; never raw unlimited output or an automatic publisher.
- **TeamEmployeeAssignment**: resolved Role-to-Employee snapshot for one
  Mission. It is not a mutable pointer consulted mid-run.

Required distinctions:

```text
Employee != Role
Employee != Session
Employee != Model
Employee != Skill
Employee != Project
Task status after dispatch = projection of Session/Run, not an independent engine
```

## 4. Fixed product decisions

These decisions are already approved and will not be reopened during phases:

1. Employee is a first-class persistent domain entity; Role remains a runtime
   responsibility.
2. TeamTemplate may later map `Role -> EmployeeID`; absent EmployeeID preserves
   exact legacy RoleSelection behavior.
3. Employee data is owner-scoped under `/data/employees/<employee-id>/`.
   Project source remains in the startup-configured Service Workspace and is
   accessed only through a matching ProjectBinding. The ProjectBinding model
   anticipates future projects, but one v0.7 Service executes one Workspace and
   owns one Session Store.
4. One Employee has at most one running Task; one Workspace has at most one
   mutation writer; cross-Employee read-only concurrency is deferred.
   Worktree Foundation remains postponed.
5. Skills are versioned and declarative, preferentially compatible with
   `SKILL.md`, `manifest.json`, and `references/`. A read-only compatibility
   Adapter discovers configured SKILL.md-only directories without running
   scripts or granting capabilities.
6. Skill, Tool, and Permission remain separate. Skills never hold credentials,
   install code, enable network, write outside a workspace, bypass Tool Policy,
   or bypass Approval.
7. The base capability intersection includes Global Policy,
   `AgentPreset.ToolPolicy`, Employee Policy, Project Policy, and Task Policy.
   With no enabled Skill, effective capability equals base. With enabled
   Skills, their requested-capability union may only narrow base.
8. Owner Memory, Employee Memory, Project Memory, Knowledge Base, and
   Task/Session Memory are separate stores/layers.
9. Employee Memory is private by default. Verified, source-linked, filtered,
   bounded Tasks may create Memory Candidates, but only explicit Owner
   acceptance writes a durable MemoryFact. Automatic promotion is deferred.
10. Knowledge v1 uses manual text and explicit local files/directories/project
    documents with deterministic retrieval and citations. Remote fetch and
    embeddings are deferred.
11. Task creation only queues. Explicit Start is mandatory; there is no
    automatic execution or auto-start-next. Every launched Task creates one
    independent existing Session.
12. Avatar v0.7 supports generated initials and Emoji only. Uploads, local file
    paths, and remote URLs are rejected.
13. Disable is reversible through explicit enable; Archive is terminal and
    never deletes Task, Session, Memory, Artifact, or Activity history.
14. `GET /api/projects` returns only the current Service Workspace. The Service
    does not scan Home, dynamically open Workspaces, or manage multiple Session
    Stores.
15. Employee Activity is bounded lifecycle/reference metadata, never execution
    authority or a duplicate Session event stream.
16. GoHermit product behavior never automatically commits, pushes, creates a
    PR, merges, deploys, sends external messages, installs external Skills, or
    starts a background scheduler.

## 5. Proposed architecture

### 5.1 Package and dependency shape

```text
internal/employee
  pure domain: Employee, state, policies, snapshots, ProjectBinding,
  EmployeeTask metadata, artifacts, validation and transitions

internal/employeestore
  owner-scoped directory persistence, revision ownership, task dispatch journal,
  lifecycle/reference activity index, paging, strict schemas, atomic mode-0600
  writes; never a Run/Event authority

internal/skill
  SkillManifest/SkillBinding validation, local read-only catalog, digest,
  capability intersection inputs; no installation

internal/knowledge
  source validation, canonical-path containment, bounded indexing/refresh,
  deterministic keyword retrieval and citations

internal/employeememory
  private MemoryFact/Candidate validation, explicit Owner accept/reject,
  edit/forget, filtering and limits

internal/controlplane
  Employee CRUD, dry run, Skill/Knowledge/Memory management, Task enqueue/start/
  cancel/reconcile, project catalog/bindings, runtime snapshot assembly

internal/contextmgr
  accepts already-bounded Employee context layers and preserves priority/order

internal/session
  additive compact EmployeeTask/Employee recovery snapshot on Session schema v6;
  unchanged Run/Plan/Approval/Event state machines

internal/teamtemplate + internal/team + internal/app/team_worker.go
  optional Role -> EmployeeID, resolved Mission snapshot, per-worker Employee
  context; legacy path unchanged when EmployeeID is empty

internal/web
  thin REST/SSE transport and embedded Employees/Tasks UI
```

Dependency rules:

- `employee` imports no Web, Control Plane, Session, model provider, or tool
  implementation packages.
- `skill`, `knowledge`, and `employeememory` expose bounded domain services;
  they do not start a Runner.
- `employeestore` owns revisions and persistence. Control Plane never writes
  its files directly.
- Web calls Control Plane. CLI integration is optional and not required for
  v0.7 unless a phase explicitly adds read-only commands.
- Full Employee revisions remain in EmployeeTask/Employee Store. Session does
  not load mutable Employee state and never embeds the full 256 KiB Employee
  revision. It receives only an immutable compact recovery snapshot from
  Control Plane.
- `activity/events.jsonl` is never imported by Session, Run control, recovery,
  SSE, or Task status projection.

### 5.2 Runtime integration

Control Plane prepares and then explicitly executes an EmployeeTask:

1. Load and validate the Employee and immutable Task snapshot.
2. Validate Employee state, provider availability, ProjectBinding, canonical
   current-Service Workspace match, budget, Skill digests, Knowledge refresh
   status, Memory selection, and effective capabilities without calling a
   model.
3. Build the immutable full Task snapshot plus a compact bounded
   Employee/Skill/Knowledge/Memory context snapshot.
4. Prepare an idempotent dispatch journal with a stable Session ID and
   create/recover a schema-v6 Session with no Run and no model call.
5. Only an explicit Start in Phase 7 acquires concurrency/writer leases and
   starts or recovers exactly one existing Run.
6. Derive Task execution state from Session/Run/Approval/Plan/Verification.
7. On verified success, produce bounded Memory Candidates and Artifacts.
   Candidates require a separate explicit Owner accept action before becoming
   MemoryFacts.

No new model loop, Plan, Approval broker, Verifier, event store, or recovery
state machine is introduced.

## 6. Data ownership and directory layout

Default store root:

```text
/data/employees/
  index.json                         # schema, stable employee IDs/order metadata
  <employee-id>/
    employee.json                    # current Employee revision
    revisions/
      <revision>.json                # immutable EmployeeRevisionSnapshot
    projects.json                    # explicit ProjectBindings
    skills.json                      # SkillBindings only
    knowledge/
      sources.json                   # source metadata/index status
      index.json                     # bounded deterministic terms/citations
    memory/
      facts.json                     # private Employee Memory
      candidates.json                # verified, pending Owner decisions
    tasks/
      index.json                     # bounded summaries/order
      <task-id>.json                 # Task snapshot/binding metadata
      <task-id>.dispatch.json        # temporary crash-recovery journal
    artifacts/
      index.json
      <artifact-id>.json             # bounded text/report metadata
    activity/
      events.jsonl                   # bounded lifecycle/reference activity, rotated
```

Rules:

- Default resolution is `GOHERMIT_EMPLOYEE_STORE`, then the user config
  directory. Compose will map it to `/data/employees`.
- Files are owner-scoped, mode 0600, strict JSON, versioned, atomically
  replaced, and never placed in a target repository.
- Project source is never copied permanently into Employee storage.
- Knowledge indexes contain bounded summaries, digests, term maps, and source
  references—not a duplicate source tree.
- Session/Run remains under the configured workspace `.gohermit/sessions`.
  EmployeeTask/Employee Store owns the complete immutable Employee revision
  snapshot. Session schema v6 stores only a compact recovery snapshot capped at
  64 KiB; it never repeats the complete Employee file on each checkpoint.
- `activity/events.jsonl` is a bounded owner-visible audit index, not a second
  Event Store. It may record only:
  - Employee create/update;
  - enable/disable/archive;
  - Skill or Knowledge binding changes;
  - Memory Candidate accept, Memory edit, and Memory forget;
  - references connecting a Task to its Session and Run.
- Employee Activity must never store Run-state truth, drive recovery, copy
  Session SSE, copy model/tool/approval/verification events, or make a state
  transition that can conflict with Session/Run. Task execution state is always
  projected from the existing Session/Run.
- Activity excludes prompts, private reasoning, provider payloads,
  credentials, tokens, stream chunks, raw tool arguments, Run events, tool
  events, and unbounded output.
- Every list endpoint uses stable newest-first ordering with ID as a
  deterministic tie-breaker and opaque cursor pagination.

## 7. Domain models

All sizes below are hard maxima, not targets. Exact constants are implemented
in the owning phase and may only be reduced without a plan re-approval.

| Model | Owner / storage | Schema and proposed limits | Secret and lifecycle rules | Existing relation / snapshot / migration |
|---|---|---|---|---|
| `Employee` | Owner; `<id>/employee.json` | v1; file 256 KiB; ID 128 B; each text 8 KiB; up to 64 Skill IDs, 64 Projects; policy lists max 128 | Reject credential markers/private keys. States `active`, `disabled`, `archived`. Store owns revision/timestamps. | References Agent profile and model names only. Snapshot required. New store, no migration. |
| `EmployeeRevisionSnapshot` | Owner; `<id>/revisions/<n>.json` and complete copy in EmployeeTask storage | v1; 256 KiB | Immutable, sanitized; includes identity, charter, boundaries, model/profile, policies, Skill bindings/digests, Project binding/policy, budget/concurrency/memory policy—not credentials or KB bodies. | EmployeeTask historical truth. Session and Team never embed this complete snapshot. New model. |
| `EmployeeStore` | Owner; `/data/employees` | store index v1; max 256 Employees; strict JSON; page max 100 | Mutex/atomic writes, stable order, duplicate ID conflict, corrupt/unknown schema fail closed. | Follows Owner/Loop/TeamTemplate patterns. New store. |
| `SkillManifest` | Catalog owner; explicitly configured local catalog, read-only | Native v1 manifest 64 KiB; `SKILL.md` 256 KiB; references total 2 MiB; ID/version 128 B; max 64 capabilities | Reject secrets, executable install hooks, remote URLs, traversal, symlink escape, workspace writes. Digest canonical manifest + content. SKILL.md-only Adapter synthesizes a zero-capability manifest. | Not copied into Employee. Immutable catalog revision; no migration. |
| `SkillBinding` | Employee owner; `<id>/skills.json`, also snapshotted | v1; max 64; configuration max 32 KiB total | Stores only `skill_id`, `version`, `digest`, bounded non-secret configuration, `enabled`. Cannot grant capability. | Snapshot required in Task/Mission. New model. |
| `SkillCatalog` | Service; configured local roots | catalog projection v1; max 512 manifests | Read-only scan of explicit roots. Duplicate `(id, version)` or digest mismatch fails closed. No internet/install. | Runtime service, no persistent authority beyond digests. |
| `KnowledgeSource` | Employee owner; `<id>/knowledge/sources.json` | v1; max 128; manual text 64 KiB/source; indexed source aggregate 8 MiB; path 4 KiB | Types: manual, file, directory, project-docs. No remote URL. Canonical path must remain inside an allowed ProjectBinding; reject symlink escape and credential-like files. | Source metadata is mutable; Task snapshot stores IDs/digests/citations, not full corpus. New model. |
| `KnowledgeBinding` | Employee owner; in source metadata and snapshot | v1; max 128 | Connects source to ProjectBinding or owner manual content, with digest, refresh status, error summary, timestamp. | Snapshot digest required; no migration. |
| `EmployeeMemory` | Employee owner; `<id>/memory/facts.json` | v1; file 512 KiB; max 512 facts | Private by default. Strict read/edit/forget. No automatic sharing. | Independent of `contextmgr.ProjectMemory`. New model. |
| `MemoryCandidate` | Employee owner; `<id>/memory/candidates.json` | v1; file 256 KiB; max 128 pending; value 8 KiB | Created only from verified Task outcomes, source-linked and secret-filtered. Requires explicit Owner accept/reject; never enters runtime Memory context before acceptance. | Phase 7 post-completion output; acceptance creates one MemoryFact idempotently. New model. |
| `MemoryFact` | Employee owner; within EmployeeMemory | v1; value 8 KiB; sources max 16 | Requires Employee ID, source Task ID, Session ID, Run ID, verified timestamp, category, value, digest. Filter secrets/full prompts/raw output/private reasoning. | Mutable by Owner; deletion is recorded as bounded activity. No Task snapshot rewrite. |
| `ProjectBinding` | Employee owner; `<id>/projects.json` | v1; max 64 future records; canonical path 4 KiB; policy lists max 128 | Data model anticipates future projects, but v0.7 execution accepts only a binding whose real path equals `controlplane.Service.Workspace`. No Home scan, dynamic Workspace open, or multi-Store manager. | References the current Service Workspace in v0.7; source stays in place. Snapshot required. New model. |
| `EmployeeTask` | Employee owner; `<id>/tasks/<task-id>.json` | v1; task prompt 16 KiB; file 512 KiB; max 10,000 retained metadata records with paged indexes | Pre-dispatch states are `queued` or `cancelled`. After binding, status is derived from Session/Run plus pending approvals. Owns a complete immutable Employee revision copy plus pinned Skill, Knowledge, Memory-selection, Project, and Task policy snapshot. | Binds `employee_id`, `project_binding_id`, `session_id`, `run_id`. Does not migrate old Sessions. |
| `EmployeeArtifact` | Employee owner; `<id>/artifacts` | v1; max 1 MiB/artifact, 256 retained index entries by default | Bounded redacted report/diff/test summary/source references only; no raw unbounded tool output or publication. | Links Task, Session, Run, optional Loop Invocation. New model. |
| `SessionEmployeeSnapshot` | Existing Session/hidden Worker checkpoint | Session schema v6; maximum 64 KiB per Session | Compact immutable recovery data only: Employee ID/name/job, revision, digest, Task ID, selected Agent/model names, effective-policy digest, and necessary bounded context snapshot. No complete Employee revision, binding catalog, Knowledge index, credentials, or raw Memory store. | Optional additive Session field. Explicit v5->v6 migration. |
| `TeamEmployeeAssignment` | Parent Mission metadata plus hidden Worker `SessionEmployeeSnapshot` | Parent assignment max 16 KiB each and 64 KiB total per Mission; hidden Worker compact snapshot max 64 KiB | Parent stores Role/WorkItem, Employee ID/revision/digest, Task/reference IDs, and policy/context digest. Hidden Worker stores the same compact immutable recovery snapshot contract as a normal Employee Session. | Optional. Old Missions/TeamTemplates continue without it. Complete revisions stay in Employee Store. |

`Employee` proposed shape:

```text
id, schema_version, revision, state
name, avatar {kind: initials|emoji, value}, job_title, charter,
responsibilities, behavior_boundaries
default_selection {company, access, model}
agent_profile
skill_bindings[]
project_binding_ids[]
permission_policy
budget_policy
concurrency_policy {max_running_tasks: 1}
memory_policy
created_at, updated_at, disabled_at?, archived_at?
```

Avatar v0.7 accepts generated initials or one bounded Emoji only. Binary
uploads, local paths, data URLs, and remote URLs are rejected.

## 8. Employee lifecycle

Proposed states:

```text
active -> disabled -> active
active -> archived
disabled -> archived
archived -> terminal
```

- Create validates all fields, configured Agent profile/model names, policy
  ceilings, and secret rules, then writes revision 1.
- Update of editable fields writes a new immutable revision snapshot and bumps
  the current revision. ID, created timestamp, and archived state are immutable.
- Disable is reversible only through a dedicated enable action. Disabled
  Employees remain readable; queued/history/Memory/Artifacts remain visible,
  but no new Task may launch and no new Team assignment may resolve.
- Archive is terminal in v0.7. It removes the Employee from default active
  lists but never deletes history, Sessions, Tasks, Memory, or Artifacts.
- Existing running Tasks are not force-cancelled merely by disable/archive.
  The action reports active Task IDs and requires the Owner to cancel them
  separately; future starts fail closed.
- A Dry Run validates readiness without model calls, Session creation,
  workspace mutation, Skill installation, Knowledge refresh, or memory write.

## 9. Task lifecycle and existing Run mapping

### 9.1 Queue and launch

Proposed manual foreground flow:

```text
POST /api/employees/{id}/tasks
  -> persist queued EmployeeTask and immutable Employee snapshot

POST /api/employee-tasks/{id}/start
  -> Phase 6 preparation: readiness + immutable compact context snapshot
  -> stable Session ID + dispatch journal + schema-v6 Session without a Run
  -> Phase 7 execution: acquire Employee/workspace leases
  -> explicitly create/start one existing Run
  -> bind Session ID and Run ID
```

Task creation never automatically calls a model. Queued Tasks never
automatically start after another Task finishes; no daemon/cron is introduced.

### 9.2 Status projection

Before a Session binding, Task owns only:

- `queued`
- `prepared` (stable Session exists with no Run and no model call)
- `cancelled` (cancelled before dispatch)
- `dispatch_error` (recoverable journal/preflight failure, with bounded reason)

After binding, API status is derived:

| EmployeeTask API status | Existing authority |
|---|---|
| `queued` | no prepared Session yet |
| `prepared` | stable schema-v6 Session exists with no Run |
| `waiting_owner` | Run queued in review mode or pending scoped Approval |
| `running` | Run running |
| `verifying` | Run verifying |
| `interrupted` | Run interrupted |
| `completed` | Run completed |
| `failed` | Run failed or verified acceptance failed |
| `cancelled` | Run cancelled or queued Task cancelled |

No independent transition method may contradict the bound Run.

### 9.3 At-most-once dispatch

Because Employee and Session data are separate file stores, Runtime Preparation
uses a small dispatch journal rather than pretending to have a cross-directory
transaction:

1. Persist `task.dispatch.json` with Task ID, stable pre-generated Session ID,
   full EmployeeTask snapshot digest, compact Session snapshot digest, and stage
   `prepared`. The full EmployeeTask snapshot digest always means the immutable
   Task business-snapshot digest; it excludes Session/Run execution bindings.
2. Create the schema-v6 Session with `EmployeeID`, `EmployeeTaskID`, revision,
   digest, and the compact snapshot (maximum 64 KiB), but no Run and no model
   call; update the journal to `session_created`.
3. Phase 6 stops at this verifiable prepared state.
4. Phase 7 explicit Start acquires leases, starts one existing Run, binds the
   returned Run ID, atomically updates the Task, and removes the journal.
   Phase 6 Session binding and Phase 7 Run binding must never rewrite the
   immutable EmployeeTask Snapshot Digest.
5. On restart, reconciliation checks the stable Session ID:
   - absent Session at `prepared`: safely recreate it with the same ID;
   - present Session without Run: remain `prepared`; never auto-start;
   - explicit Start against a present Session without Run: start exactly one
     Run;
   - present Session with Run: bind and project it;
   - corrupt/mismatched snapshot: fail closed and require Owner action.

The journal is an idempotency mechanism, not another execution state machine.

## 10. Skill contract and security model

Proposed local Skill layout:

```text
<catalog-root>/<skill-id>/<version>/
  manifest.json
  SKILL.md
  references/
```

The Catalog also supports a read-only compatibility layout:

```text
<configured-catalog-root>/<skill-directory>/
  SKILL.md
  references/   # optional bounded content; scripts are never executed
```

Minimum `SkillManifest`:

```text
schema_version
skill_id
version
title
description
requested_capabilities[]   # declarative names only
configuration_schema       # bounded JSON Schema subset
content_files[]            # relative allowlisted files
digest_algorithm           # sha256
digest
```

Catalog rules:

- Roots come only from explicit server configuration
  (`GOHERMIT_SKILL_CATALOG` or future typed config), never from the internet.
- Canonical real paths must remain below a catalog root. Absolute paths,
  `..`, symlink escape, device files, executable installers, and nested
  credential-like paths fail closed.
- `manifest.json` uses strict JSON and a known schema. Unsupported versions,
  duplicate IDs/versions, missing content, or digest mismatch fail closed.
- The Phase 3 SKILL.md compatibility Adapter discovers SKILL.md-only Skills
  only below explicitly configured Catalog Roots. It reads bounded frontmatter
  `name` and `description`, computes a SHA-256 content digest, and exposes a
  synthetic immutable version derived from that digest.
- A SKILL.md-only synthetic manifest requests zero additional capabilities.
  It contributes instruction context only and therefore cannot add tools,
  network, writes, approval scope, or any other permission.
- The Adapter never executes `scripts/`, never installs dependencies, never
  scans unconfigured directories, and never follows a symlink outside its
  configured Catalog Root.
- Skill content is instruction/reference context only. It is not executed.
- Configuration is validated against a bounded schema subset and screened for
  secrets before binding.
- A Task snapshot pins Skill ID, version, digest, configuration, and enabled
  state. Later catalog or binding changes cannot affect historical Tasks.

Effective capabilities first compute a Skill-independent base that includes the
existing `AgentPreset.ToolPolicy`:

```text
base =
    Global Policy
  ∩ AgentProfile ToolPolicy
  ∩ Employee permission ceiling
  ∩ ProjectBinding policy
  ∩ Task Policy
```

With no enabled Skill:

```text
effective = base
```

With one or more enabled Skills:

```text
effective =
    base
  ∩ union(enabled Skill requested capabilities)
```

Native manifests may request capabilities, but the intersection only removes
tools/capabilities. SKILL.md-only Adapter entries add nothing to the requested
capability union: when they are the only enabled Skills, effective tool
capability is empty and they are instruction-context-only; when native Skills
are also enabled, only those native requested capabilities participate. A call
then still passes:

1. existing workspace containment;
2. existing policy classification;
3. existing timeout/output bounds;
4. existing one-shot ADR 0011 Approval when required.

Plan approval and Task launch approval do not imply call approval. Network is
off unless every layer explicitly allows it, and no v0.7 Skill may install or
fetch anything.

## 11. Knowledge Base design

Supported v1 source kinds:

- `manual_text`: bounded owner-entered text.
- `file`: one explicit file under a ProjectBinding workspace.
- `directory`: explicit directory with bounded depth/file count/extensions.
- `project_docs`: deterministic allowlist such as tracked Markdown/text files
  under a ProjectBinding.

Refresh pipeline:

1. Resolve the ProjectBinding canonical workspace.
2. Resolve the requested relative path without following it outside the
   binding. Reject absolute path, traversal, symlink escape, `.git`,
   `.gohermit`, credential-like names, binaries, and oversized files.
3. Bound file count, aggregate bytes, per-file bytes, depth, and elapsed time.
4. Normalize text, compute SHA-256 digests, extract bounded headings/keywords
   and deterministic snippets, and record exact source citations.
5. Atomically replace the Employee's index and set refresh status
   `ready`, `stale`, or `failed` with a bounded public error.

Retrieval:

- Deterministic case-folded keyword/title/path scoring.
- Stable ordering by score, source ID, and citation.
- Per-call result count and byte/token budgets.
- Returned context marks Knowledge as cited reference data, not trusted system
  instructions.
- If a pinned Task digest no longer matches, launch fails closed or uses its
  already-snapshotted bounded excerpts; it never silently reads a new source.

Remote URLs, crawling, embeddings, vector databases, and background refresh
are non-goals.

## 12. Employee Memory design

`MemoryFact` required fields:

```text
id, schema_version, employee_id, category, value
source_task_id, source_session_id, source_run_id
verified_at, created_at, updated_at, digest
owner_edited
```

Candidate gate:

1. The Task is bound to the same Employee and the referenced Run is completed.
2. Existing Run verification evidence satisfies the normal completion gate.
3. Candidate content comes from bounded public Run outcomes
   (final summary, verified commands, confirmed decisions, issue resolution),
   never from full prompts, private reasoning, model continuation blobs, raw
   tool output, or stream chunks.
4. Shared secret detection and redaction run before validation; ambiguous
   credential-like content is rejected rather than silently preserved.
5. Per-fact, per-Task, and total Employee Memory limits apply.
6. An idempotency digest over Employee/Task/Run/category/value prevents
   duplicate Candidate generation after restart.
7. Passing the gate creates a bounded `MemoryCandidate`, not a MemoryFact.
   Candidate generation has no automatic promotion path.

Acceptance gate:

1. The Owner explicitly accepts one Candidate.
2. The server reloads and revalidates Employee, Task, Session, Run,
   verification evidence, provenance, capacity, digest, and secret filtering.
3. Acceptance atomically creates one MemoryFact and records only a bounded
   `memory_accept` Employee Activity entry.
4. Repeated acceptance is idempotent; rejected or expired Candidates never
   enter long-term Memory.

Read behavior:

- Only the selected Employee's private facts enter its context.
- Project Memory remains a separate layer and is never copied into Employee
  Memory automatically.
- Team Handoffs contain only their existing bounded public fields; they never
  include another Employee's private memory.
- Owner may list, edit, and forget a fact. Edit retains source IDs and marks
  `owner_edited`; Forget removes content and emits bounded activity metadata.
- Memory write failure does not rewrite a successful Run as failed. The Task
  reports a retryable post-completion warning and an idempotent reconciliation
  may retry.
- Automatic Memory promotion is explicitly deferred beyond v0.7.

## 13. Context assembly order

Proposed priority from highest authority to lowest:

1. Global GoHermit safety/system policy and existing Role/Agent profile.
2. Confirmed Owner Profile.
3. Immutable Employee identity, job charter, responsibilities, behavior
   boundaries, and snapshot revision.
4. Effective capability/budget/project boundary summary.
5. Project `AGENTS.md` and other existing project rules.
6. Project Memory.
7. Pinned Skill instructions, ordered by Skill ID and version.
8. Pinned Knowledge excerpts with citations, explicitly marked as reference
   data rather than policy.
9. This Employee's private Memory facts only.
10. Recovered Session summary and active Run/Plan state.
11. Current Task goal.
12. Bounded recent messages and tool results.

Rules:

- Global/Employee/Project policy always outranks Skill, Knowledge, and Memory.
- Each layer has its own byte/token ceiling plus a combined Employee-context
  ceiling. Lower-priority Knowledge then Memory entries are dropped first.
- Stable headings and source IDs keep deduplication deterministic.
- No context layer contains credentials, private reasoning, raw tool arguments,
  full prompts, or unbounded output.
- Legacy Sessions call the existing context path with no Employee layers and
  remain byte-for-byte behavior-compatible aside from internal refactoring
  proven by regression tests.

## 14. Project binding and workspace rules

`ProjectBinding` is an explicit Employee-to-workspace grant:

```text
id, employee_id, label
workspace_real_path
read_allowed, mutation_allowed
allowed_tool_capabilities[]
network_allowed
budget_override?
created_at, updated_at
```

- The data model can retain future ProjectBinding records, but v0.7 binding
  creation and Task preparation resolve only `controlplane.Service.Workspace`
  to an absolute real directory and record its fingerprint.
- A v0.7 Task binding must match the current Service Workspace exactly after
  canonicalization. A mismatch fails readiness before Session preparation.
- `GET /api/projects` returns exactly the current startup-configured Service
  Workspace. v0.7 never scans Home, registers arbitrary roots, dynamically
  opens another Workspace, or creates a multi-Workspace Session Store manager.
- Every Task references one binding. The Task snapshot records the canonical
  workspace and policy digest.
- One Employee may have only one running Task. The single Service does not run
  Tasks from different Employees concurrently in v0.7, even when they are
  read-only. Mutation Tasks additionally acquire the one writer lease for the
  current Workspace.
- Legacy non-Employee Sessions continue through the existing conservative
  service gate.
- Cross-project operation uses one GoHermit instance per Workspace and a shared
  owner-scoped Employee Store direction. Cross-instance sharing, revision
  coordination, and locking semantics require a later ADR and are not
  implemented in v0.7.
- Dirty-workspace rules from Loop/Run execution remain authoritative. No
  implicit stash, branch, commit, worktree, merge, or cleanup is added.

## 15. Team Role to Employee mapping

Phase 9 changes TeamTemplate with explicit migration:

```text
RoleSelection {
  existing company, access, model, max_model_calls, max_tokens
  employee_id?   # optional
}
```

Resolution:

1. Empty `employee_id`: execute the exact current RoleSelection path.
2. Non-empty ID: load active Employee, validate ProjectBinding and configured
   model, then resolve an immutable `TeamEmployeeAssignment`.
3. Explicit RoleSelection provider/model remains the Mission-specific override;
   otherwise the Employee default selection is used. No silent vendor fallback.
4. The complete Employee revision stays in EmployeeTask/Employee Store.
   Parent Mission/WorkItem stores only compact `TeamEmployeeAssignment`
   metadata (maximum 16 KiB each and 64 KiB total per Mission).
5. `TeamWorker.Execute` receives a compact immutable assignment and its hidden
   child Session stores one `SessionEmployeeSnapshot` capped at 64 KiB. It
   injects only that Employee's bounded context and never embeds the complete
   256 KiB Employee revision.
6. Handoffs stay bounded and public. Employee A private Memory never enters
   Employee B context through Mission state or Handoff.
7. Disabled/archived/missing Employee, stale digest, invalid policy
   intersection, or unavailable model fails Team Session preflight before any
   Session/Run/Worker side effect.

TeamTemplate schema moves from v1 to v2 with explicit v1 migration. Existing
files with no Employee IDs produce the same effective selections and budgets.

## 16. Web API

All mutation endpoints use the existing same-origin guard, strict JSON,
unknown-field rejection, bounded request bodies, service error classification,
and no secret echo. Web handlers remain transport-only.

### 16.1 Employee resources

| Endpoint | Layer | Notes |
|---|---|---|
| `GET /api/employees` | Control Plane + Store query | Cursor page, state filter, stable order. |
| `POST /api/employees` | Control Plane + Store CRUD | Validate, create revision 1. |
| `GET /api/employees/{id}` | Control Plane + Store CRUD | Current revision and bounded summaries. |
| `PUT /api/employees/{id}` | Control Plane + Store CRUD | ID match, optimistic revision precondition, new revision. |
| `POST /api/employees/{id}/dry-run` | Control Plane | Zero side effect readiness report. |
| `POST /api/employees/{id}/disable` | Control Plane + domain transition | Blocks future starts; history remains. |
| `POST /api/employees/{id}/enable` | Control Plane + domain transition | Proposed reversible disable action. |
| `POST /api/employees/{id}/archive` | Control Plane + domain transition | Terminal; no deletion. |
| `GET /api/employees/{id}/activity` | Control Plane + Store query | Bounded lifecycle/reference audit entries only; never Run truth or copied SSE/tool events. |

### 16.2 Task resources

| Endpoint | Layer | Notes |
|---|---|---|
| `GET /api/employees/{id}/tasks` | Control Plane + Store/projection | Cursor page; state derived from Run when bound. |
| `POST /api/employees/{id}/tasks` | Control Plane + Store CRUD | Creates queued Task and immutable snapshot only. |
| `GET /api/employee-tasks/{id}` | Control Plane projection | Joins Task metadata with Session/Run/Plan/Approval/Verification. |
| `POST /api/employee-tasks/{id}/start` | Control Plane runtime | Mandatory explicit start. Phase 6 preparation is idempotent/no-model; Phase 7 creates/starts at most one Run through the existing lifecycle. |
| `POST /api/employee-tasks/{id}/cancel` | Control Plane runtime | Queued cancel locally; bound cancel delegates to existing `CancelRun`. |

There is no Employee Task event stream. Task detail returns the bound Session
ID, and the client subscribes directly to the existing
`GET /api/sessions/{session_id}/events?after=<sequence>` endpoint.

### 16.3 Skills, Knowledge, Memory, Projects

| Endpoint | Layer | Notes |
|---|---|---|
| `GET /api/skills` | Control Plane + read-only SkillCatalog | Configured local catalog only; no install. |
| `GET /api/employees/{id}/skills` | Store CRUD | Bindings and current digest status. |
| `PUT /api/employees/{id}/skills` | Control Plane + Store CRUD | Validate catalog/digest/config/policy; new Employee revision. |
| `GET /api/employees/{id}/knowledge` | Store query | Source metadata, digest, refresh status, citations. |
| `POST /api/employees/{id}/knowledge` | Control Plane + Store CRUD | Add manual/local source; does not fetch remote content. |
| `POST /api/employees/{id}/knowledge/{sourceID}/refresh` | Control Plane | Synchronous bounded deterministic refresh. |
| `DELETE /api/employees/{id}/knowledge/{sourceID}` | Control Plane + Store CRUD | Removes index reference, not project source. |
| `GET /api/employees/{id}/memory` | Store query | Private facts for this Employee only. |
| `GET /api/employees/{id}/memory-candidates` | Store query | Verified bounded Candidates awaiting explicit Owner action. |
| `POST /api/employees/{id}/memory-candidates/{candidateID}/accept` | Control Plane + Store CRUD | Revalidate provenance/secret/capacity and create one MemoryFact idempotently. |
| `DELETE /api/employees/{id}/memory-candidates/{candidateID}` | Control Plane + Store CRUD | Reject Candidate without creating long-term Memory. |
| `PUT /api/employees/{id}/memory/{factID}` | Control Plane + Store CRUD | Owner edit; preserve provenance, re-filter. |
| `DELETE /api/employees/{id}/memory/{factID}` | Control Plane + Store CRUD | Forget content and record bounded activity. |
| `GET /api/projects` | Control Plane project catalog | Exactly the current startup-configured Service Workspace in v0.7. |
| `GET /api/employees/{id}/projects` | Store query | Explicit Employee bindings. |
| `PUT /api/employees/{id}/projects` | Control Plane + Store CRUD | Store future-shaped bindings, but v0.7 executable binding must canonically equal current Service Workspace. |

HTTP responses never expose API keys, OAuth tokens, system prompts, private
reasoning, full Skill/Knowledge files by default, unbounded tool output, or
filesystem paths outside an authorized ProjectBinding.

## 17. UI and user journeys

Primary navigation after Phase 8:

```text
Dashboard | Employees | Tasks | Agent | Loops | Settings
```

Existing Agent, Loops, Approval, Plan, provider Settings, and refresh behavior
remain available.

### 17.1 Create Employee wizard

1. **Identity**: name, generated initials or Emoji avatar, job title.
2. **Job and charter**: responsibilities and success definition.
3. **Model**: configured company/access/model plus Agent profile.
4. **Skills**: select pinned local catalog versions, show requested capability
   diff and digest.
5. **Knowledge**: add manual text or allowed Project sources; show refresh
   status and citations.
6. **Projects**: current Service Workspace binding and read/mutation mode.
7. **Permissions**: effective intersection preview; call Approval remains
   separate.
8. **Memory policy**: private-by-default Candidate review and capacity.
9. **Dry Run**: model/credential, Skill digest, Knowledge status, project path,
   policy, budget, concurrency, and blockers. Create/launch buttons remain
   disabled until applicable readiness succeeds.

### 17.2 Employee detail

Tabs:

```text
Overview | Tasks | Skills | Knowledge | Memory | Projects | Activity | Settings
```

- Overview shows identity, state, revision, model, Agent profile, effective
  policy, budget, active Task, and recent verified results.
- Tasks creates multiple queued Tasks, starts one manually, shows running
  Plan/Approval/Verification via the bound Session, and retains history.
- Skills shows version/digest/configuration and capability intersection.
- Knowledge shows source/digest/refresh/citation state and synchronous refresh.
- Memory lists verified Candidates for explicit accept/reject and private facts
  with Task/Run provenance, edit, and Forget.
- Projects manages the current Service Workspace binding and mutation policy;
  it never opens another Workspace.
- Activity shows bounded lifecycle metadata.
- Settings edits Employee, disables/enables, and archives with explicit impact
  confirmation.

### 17.3 Global Tasks page

- Filters by Employee, Project, status, and updated time.
- Shows queued, waiting Owner, running, interrupted, failed, completed, and
  cancelled Tasks.
- Opens the existing Session/Run timeline and Approval panel for a bound Task.
- Start/Resume/Cancel use Control Plane endpoints; no client-only state.

### 17.4 Recovery and actionable errors

- Selected Employee/Task IDs and last SSE sequence may be kept in localStorage;
  authoritative data is always reloaded.
- Refresh reconnects to the existing bound Session SSE sequence.
- Service restart reconciles dispatch journals and interrupted Runs before the
  UI enables action.
- Missing/unconfigured model errors identify the Employee field and link to
  Settings; no Session is created.
- Disabled/archived Employee, stale Skill digest, invalid ProjectBinding,
  Knowledge refresh failure, concurrency conflict, dirty workspace, and pending
  Owner approval each have distinct corrective text.
- Historical Tasks remain viewable after disable/archive.

## 18. Schema migration and backwards compatibility

| Schema | Change | Migration/compatibility |
|---|---|---|
| Employee store v1 | Entirely new owner-scoped store. | Missing root means empty catalog. Unknown/corrupt schema fails closed and is never overwritten. |
| Session v5 -> v6 | Optional `employee_id`, `employee_task_id`, revision, full-snapshot digest, and `SessionEmployeeSnapshot` capped at 64 KiB. Parent Team assignment metadata is capped at 16 KiB each/64 KiB total; hidden Workers use their own 64 KiB compact snapshot. | Explicit `migrateV5`; v1-v4 migration tests remain. Old Sessions load with empty Employee fields and identical behavior. Full EmployeeRevisionSnapshot remains in EmployeeTask/Employee Store and is never repeated in Session checkpoints. Historical Session/Run files are not rewritten eagerly. |
| TeamTemplate v1 -> v2 | Optional `employee_id` on RoleSelection. | Explicit v1 migration; empty IDs preserve exact provider/model/budget resolution. Unknown versions fail closed. |
| Skill/Knowledge/Memory/Task v1 | New per-Employee schemas. | No legacy migration. Every loader rejects unknown fields/version and never truncates newer data. |
| Loop schema v1 | No change. | Loop Workbench and Invocation snapshots remain unchanged. |
| Owner/Profile and ProjectMemory v1 | No change. | No Employee content is inserted into these stores. |

Compatibility requirements:

- Existing Agent single-session flow is unchanged.
- Existing Personal Agent Team and old TeamTemplates are unchanged until an
  Employee ID is explicitly configured.
- Existing Loop Workbench uses the same Control Plane/Session/Run paths.
- Existing Session/Run recovery, Plan, Approval, VerificationRecipe, Owner
  Profile, and Project Memory tests remain green.
- Old Sessions are not forced into EmployeeTask records.
- New Employee fields are optional in every runtime/recovery path.
- One Service continues to own one startup-configured Workspace and one Session
  Store. No multi-Workspace manager is introduced.
- No default test calls a paid model.

## 19. Failure and recovery semantics

| Failure | Required behavior |
|---|---|
| Corrupt/unknown Employee file | Fail closed, do not overwrite, expose bounded actionable error. Other Employees remain readable when safely possible. |
| Concurrent Employee revision update | Optimistic revision conflict; no lost update. Store mutex protects in-process writes. |
| Secret-like Employee/Skill/Knowledge/Memory data | Reject before persistence; response does not echo sensitive value. |
| Missing/changed Skill digest | Dry Run/start fail closed. Historical Task snapshot remains unchanged. |
| Knowledge path/symlink escape | Reject source/refresh; never read escaped content. |
| Knowledge refresh partial failure | Keep last valid index and mark source stale/failed; never publish a partial authoritative index. |
| Employee disabled/archived | No new Task/Team start. Existing Task history remains. Running Task requires explicit cancel. |
| Provider/credential unavailable | Preflight fails before Session creation/model call. |
| Task preparation crash | Phase 6 reconciles the stable Session ID and compact snapshot from the dispatch journal, but never starts a Run automatically. |
| Task execution crash/retry | Phase 7 reuses the prepared Session and starts/binds at most one Run; restart never duplicates it. |
| Process restart during Run | Existing `Session.Store.Recover` marks/interprets interrupted state; Task projects it and Owner explicitly resumes. |
| Pending Approval on restart | Existing ADR 0011 semantics remain; pending may be decided if unexpired, consumed never replays. |
| Cancel queued Task | Terminal local cancellation with no Session. |
| Cancel bound Task | Delegate to existing `CancelRun`; derive final Task state from Run. |
| Verification failure | Existing Run/Team verification and bounded repair decide outcome; Task projects failure and writes no Employee Memory. |
| Memory Candidate write failure after verified Run | Run stays completed; Task exposes retryable post-completion warning; idempotent Candidate generation may retry. No MemoryFact exists before Owner acceptance. |
| Artifact write failure | Run stays authoritative; Task exposes a bounded warning, with no Activity event or unbounded fallback. |
| Team Employee missing/invalid at launch | Team preflight fails before Mission/Session/Worker execution. |
| Service/client refresh | Reload Employee/Task and bound Session; reconnect by persisted event sequence; never restart automatically. |
| Activity write/read failure | Never changes or reconstructs Task/Run state. Session/Run and Session SSE remain authoritative; Activity reports a bounded non-authoritative warning. |

## 20. Test strategy

Tests use temporary directories, fake providers, deterministic fixtures, and
real local filesystem/Git operations where boundary behavior matters. Paid
models remain opt-in only.

### 20.1 Required matrix

| Area | Required tests |
|---|---|
| Domain | Create; revision bump; immutable snapshot; disable/enable/archive; illegal transitions; archived terminal; duplicate/invalid ID; text/list/file size limits; deep-copy isolation. |
| Store | Owner-scoped path resolution; mode 0600; atomic replacement; injected write failure leaves old data; corrupt JSON; unknown schema/fields; secret rejection without echo; concurrent update protection; optimistic revision conflict; cursor pagination and stable sort. |
| Skills | Native manifest strict schema; ID/version/digest; canonical content digest; SKILL.md-only frontmatter Adapter; synthetic digest version; zero-capability default; configured-root-only discovery; traversal/symlink escape; secret/config rejection; unsupported version fails closed; scripts/dependencies never execute/install; effective policy includes AgentProfile ToolPolicy and only narrows; catalog update does not change Task snapshot. |
| Knowledge | Manual/file/directory/project-doc sources; project binding enforcement; absolute/traversal/symlink/credential escape; file/depth/count/byte/time limits; deterministic refresh/retrieval/order/citations; partial refresh keeps old index; no credential leakage. |
| Memory | Employee A fact never enters B context; Project vs Employee Memory isolation; unverified/failed/cancelled Task creates no Candidate; verified provenance Task/Session/Run required; Candidate generation idempotent; no MemoryFact before explicit Owner accept; accept/reject/edit/Forget; secret/full prompt/raw output rejection; capacity; restart retry. |
| Tasks | Multiple queued Tasks per Employee; explicit start only; Phase 6 preparation has stable Session ID/schema-v6 compact snapshot/journal and zero Run/model calls; Phase 7 starts at-most-one Run under retry/crash/restart; one running Task per Employee; no cross-Employee read-only concurrency; one workspace mutation writer; status projection; waiting Owner; Cancel queued/bound; interruption/resume; verification failure; disabled Employee blocks new start/history remains. |
| Team | Role binds correct Employee/revision; RoleSelection model override precedence; Worker receives only assigned Employee model/Skills/Knowledge/Memory; Handoff has no private-memory leakage; disabled Employee preflight; Task snapshot stability; old TeamTemplate regression and v1 migration. |
| Web | Strict/same-origin/size/error mapping; create/edit/disable/enable/archive Employee; initials/Emoji-only Avatar; wizard Dry Run; Skill/Knowledge/current-Workspace Project configuration; multiple Task queue/explicit-start/cancel; actionable missing-model error; bound Session Timeline/Approval/Plan/Verification via existing Session SSE; refresh/restart recovery; Memory Candidate accept/reject plus fact view/edit/Forget; historical Tasks after disable. |
| Compatibility | Existing single Agent, Team, Loop, Session migrations, Approval, VerificationRecipe, Owner, Project Memory, and all current Playwright tests. |
| Security | Exact base/effective formulas including `AgentPreset.ToolPolicy`; no-Skill equals base; enabled-Skill union can only narrow; SKILL.md Adapter adds zero capabilities; approval cannot broaden scope; no endpoint response contains key/token/private prompt/raw output; no remote Skill install/fetch; no auto Git/publish action. |
| Activity | Only Employee lifecycle/binding/Memory acceptance or edit/forget and Task-to-Session/Run references are accepted; Run/tool/SSE event payloads are rejected; Activity cannot drive recovery or Task status. |
| Snapshot size | Full EmployeeRevisionSnapshot remains in EmployeeTask/Employee Store; ordinary/hidden Worker Session compact snapshots reject payloads over 64 KiB; parent Team assignment rejects entries over 16 KiB or aggregate over 64 KiB. |

### 20.2 Phase-local and final commands

Every phase runs:

```bash
gofmt -l <phase Go scope>
go test <phase packages> -count=1
go test <direct regression packages> -count=1
git diff --check
git status --short
```

Phase 10 must run:

```bash
gofmt -l .
go test ./...
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
pnpm install --frozen-lockfile
pnpm test:e2e
docker compose config
docker compose build
docker compose up -d
curl --fail http://127.0.0.1:8787/api/health
curl --fail http://127.0.0.1:8787/api/info
git diff --check
```

Docker acceptance snapshots the `/data` manifest before rebuild and verifies
existing Owner, credentials, TeamTemplate, Loop, Session, and Employee data are
unchanged unless a documented migration is intentionally exercised.

## 21. Phase breakdown

Only one phase may be `IN_PROGRESS`. Each completed phase updates this section
with actual files, exact commands/results, deviations, remaining risks, commit,
push, and Draft PR evidence, then stops for Owner approval.

### Phase status ledger

| Phase | Name | Status | Commit / PR / evidence |
|---|---|---|---|
| 1 | Employee Domain and ADR | `OWNER_APPROVED` | Owner-approved 2026-07-28 |
| 2 | Employee Store, Control Plane, and CRUD API | `OWNER_APPROVED` | Owner-approved 2026-07-28; now in `origin/main` via squash merge `9a75e8f` |
| 3 | Skill Catalog, SKILL.md Adapter, policy intersection, and context contract | `OWNER_APPROVED` | Squash-merged through PR #36 as `d31bcf3` |
| 4 | Knowledge Base and Employee Memory | `OWNER_APPROVED` | PR #37 externally squash-merged as `e65bc119`; full Phase 4 is in `origin/main` |
| 5 | Employee Task Inbox persistence and API | `IN_PROGRESS` | Clean branch `agent/electronic-employees-v0.7-phase5` from `origin/main@e65bc119` |
| 6 | Runtime Preparation | `BLOCKED_BY_GATE` | Not started |
| 7 | Manual Execution Lifecycle | `BLOCKED_BY_GATE` | Not started |
| 8 | Employees and Tasks Web UI | `BLOCKED_BY_GATE` | Not started |
| 9 | Team Role to Employee mapping | `BLOCKED_BY_GATE` | Not started |
| 10 | Evals, Docker, docs, and v0.7 release closeout | `BLOCKED_BY_GATE` | Not started |

### Phase 1: Employee Domain and ADR

Independent value:

- Establishes the reviewed vocabulary, state transitions, validation, immutable
  revision snapshots, policies, and ProjectBinding contract before persistence.
- Adds `docs/adr/0013-first-class-electronic-employees.md`.

Expected files:

```text
internal/employee/employee.go
internal/employee/snapshot.go
internal/employee/policy.go
internal/employee/employee_test.go
internal/employee/snapshot_test.go
docs/adr/0013-first-class-electronic-employees.md
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/employee -count=1
go test ./internal/team ./internal/config ./internal/teamtemplate -count=1
git diff --check
```

Non-goals: no Store, Control Plane, HTTP, Session schema, Skill catalog,
Knowledge, Memory, Tasks, TeamTemplate change, or UI.

Completion evidence (2026-07-28):

- Added only the planned Employee domain, policy, snapshot, tests, ADR 0013,
  and this plan.
- Pure-domain tests cover creation, validation, reversible disable/enable,
  terminal archive, revision updates, conservative policies, exact
  ProjectBinding ownership, deep-copy immutability, deterministic SHA-256
  snapshot digests, tamper detection, and 256 KiB limits.
- `go test ./internal/employee -cover -count=1`: pass, 83.4% statements.
- `go test ./internal/team ./internal/config ./internal/teamtemplate -count=1`:
  pass.
- `go test ./... -count=1`: pass.
- `go test -race ./... -count=1`: pass.
- `go vet ./...`: pass.
- `go build ./cmd/hermit` and `go build ./cmd/hermit-web`: pass; generated
  binaries removed after verification.
- `gofmt` and `git diff --check`: pass.
- Phase 1 implementation commit:
  `15b66bf39d960b420a00f86e165300dea4d8f579`.
- Deviations: none in product scope. This exact immutable implementation SHA
  is recorded by a plan-only closeout commit because a Git commit cannot
  contain its own SHA.
- Residual risk: Emoji validation uses a conservative Unicode-symbol rule
  rather than a third-party grapheme library; it accepts bounded common Emoji
  and rejects text, paths, and URLs. Complex Emoji compatibility can be
  expanded only with evidence and without weakening Avatar safety.

Exit: all pure-domain transitions/limits/snapshots pass; ADR 0013 records the
Employee/Role/Session boundaries; stop with
`STATUS: WAITING_FOR_PHASE_1_APPROVAL`.

### Phase 2: Employee Store, Control Plane, and CRUD API

Independent value:

- Persists owner-created Employees and revisions outside repositories.
- Exposes create/list/get/update/dry-run/disable/enable/archive/activity backend
  APIs without starting Tasks or Skills.

Expected files:

```text
internal/employeestore/*
internal/controlplane/employees.go
internal/controlplane/employees_test.go
internal/controlplane/service.go
internal/web/employees.go
internal/web/employees_test.go
internal/web/server.go
compose.yaml
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/employee ./internal/employeestore ./internal/controlplane ./internal/web -count=1
go test ./internal/owner ./internal/teamtemplate ./internal/loopstore -count=1
git diff --check
```

Non-goals: no Skill execution, Knowledge/Memory, Task start, Session schema, or
UI.

Exit: strict schema/atomic/concurrency/pagination/secret tests pass; missing
store is empty and corrupt/unknown store fails closed; no model call in Dry Run.
Activity tests accept only Employee create/update, enable/disable/archive,
Skill/Knowledge binding changes, Memory accept/edit/forget, and Task-to-
Session/Run references; Run/SSE/tool events are rejected and Activity cannot
drive recovery or Task status.

Phase 2 execution record (2026-07-28):

- Status: `COMPLETE_WAITING_FOR_OWNER`; Phase 1 is `OWNER_APPROVED`.
- Implementation commit:
  `2f309c8b14af05a564604cf3d377981bb86e6bab`
  (`feat(employees): add owner-scoped employee control plane`).
- Actual files:
  `compose.yaml`, `internal/employeestore/store.go`,
  `internal/employeestore/store_test.go`,
  `internal/controlplane/employees.go`,
  `internal/controlplane/employees_test.go`,
  `internal/controlplane/service.go`, `internal/web/employees.go`,
  `internal/web/employees_test.go`, `internal/web/server.go`, and this plan.
- Store layout:
  `<owner-store>/index.json`,
  `<owner-store>/<employee-id>/employee.json`,
  `<owner-store>/<employee-id>/projects.json`,
  `<owner-store>/<employee-id>/revisions/<revision>.json`, and
  `<owner-store>/<employee-id>/activity/events.jsonl`.
  Store/index/projects/activity schemas are version 1. Current records and
  immutable revision snapshots are each bounded to 256 KiB; activity is
  bounded to 1 MiB, 1,024 events, and 4 KiB per event; the store holds at most
  256 Employees and returns at most 100 records per stable ID cursor page.
- Persistence: every JSON/JSONL file uses same-directory temp-file write,
  permission `0600`, fsync, and rename through `storage.AtomicWrite`.
  A process-wide Store mutex serializes create/update/lifecycle/index/activity
  mutations. Revision files are create-once and a pre-existing revision fails
  closed. Missing store/index/activity is empty; corrupt, oversized,
  unknown-field, unknown-schema, digest mismatch, and index/current mismatch
  inputs fail closed.
- Control Plane and Web API: create/list/get/update/dry-run/disable/enable/
  archive/activity are exposed under `/api/employees`; `GET /api/projects`
  returns exactly the startup Service Workspace. Mutations are strict JSON,
  bounded, same-origin, and revision-conditional.
- Dry Run checks active Employee state, AgentProfile, static provider/access/
  model selection, credential readiness, ProjectBinding validity, exact
  current-Service Workspace equality, and Employee policy/config validity. It
  does not build a runtime, create Session/Run state, call a model, execute a
  Skill, refresh Knowledge, write Memory, or mutate a Workspace.
- Activity is a bounded lifecycle/binding/memory/reference audit file only. It
  has no status/payload field and rejects Run status, Session SSE, tool,
  approval, verification, recovery, and Task-state events. It is never read by
  Session/Run/Task recovery or status projection.
- Verification:
  - scoped Phase 2 package tests: pass;
  - owner/team-template/loop-store regression tests: pass;
  - `go test ./... -count=1`: pass;
  - `go test -race ./... -count=1`: pass;
  - `go vet ./...`: pass;
  - `go build ./cmd/hermit`: pass;
  - `go build ./cmd/hermit-web`: pass;
  - `gofmt`, `git diff --check`, and staged private-key/access-key pattern
    scan: pass.
- Plan deviation: the Phase 2 product implementation is one scoped commit and
  this evidence update is a separate plan-closeout commit so the plan can
  record the immutable implementation SHA. No expected product file or
  behavior was added outside Phase 2.
- Remaining risks: atomicity is per file, not a cross-file transaction.
  Interrupted multi-file mutation therefore fails closed on index/current
  mismatch and requires owner repair; adding a recovery journal here would
  violate the prohibition on a second recovery state machine. Store locking is
  intentionally in-process; owner-scoped sharing and cross-process lock
  semantics remain a later explicit design decision. No Phase 3 subsystem is
  present.

Phase 2 Gate revision record (2026-07-28):

- Status: `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`.
- Revision commit:
  `32a9f89602dd216d928c6e97855ea5e0eeee66f2`
  (`fix(employees): harden phase 2 persistence gate`).
- `LoadRevision` now validates a domain-compatible bounded Employee ID before
  constructing a path, rejects absolute, encoded, separator, dot-segment, and
  traversal forms, requires a positive Revision, requires the Employee in the
  strict Store index, verifies lexical and resolved-symlink containment, and
  requires the validated Snapshot Employee ID and Revision to equal the
  request. Existing embedded Employee identity, Revision, Digest, binding, and
  size validation remains mandatory.
- Activity IDs are Store-assigned only. Caller-supplied IDs are rejected;
  cryptographic-random failures propagate; generated IDs advance beyond the
  last persisted ID even if the clock does not; loaded Activity rejects
  malformed, duplicate, or non-increasing IDs. Stable cursor tests cover
  multiple pages, append, truncation, reopen, malformed cursors, and
  well-encoded invalid cursors.
- `List` now validates every indexed Employee and Projects record and fails
  closed on corrupt/oversized/unknown-schema content or index-record mismatch.
  `ErrCorrupt` maps to Control Plane `KindInternal` and HTTP 500; not-found and
  optimistic-revision conflicts retain 404 and 409 mappings.
- Added Store evidence for index/Employee/Projects/Snapshot/Activity strict
  schemas, malformed JSON, unknown fields and versions, size limits, swapped
  identity, Digest tampering, immutable revision collision, symlink escape,
  index-record disagreement, concurrent expected-revision conflict, Employee
  filtering/pagination, Activity ordering/pagination/truncation, `0600`
  permissions, missing Store, and secret-like input rejection.
- Added Control Plane and Web evidence for create/list/get/update/stale
  conflict/dry-run/disable/enable/archive/terminal archive/activity/projects,
  Workspace rejection, strict JSON, request size, same-origin, not-found,
  conflict, and corrupt-store mappings.
- Verification after the Gate revision:
  - `go test ./internal/employee ./internal/employeestore ./internal/controlplane ./internal/web -count=1`: pass;
  - `go test ./... -count=1`: pass;
  - `go test -race ./... -count=1`: pass;
  - `go vet ./...`: pass;
  - CLI and Web builds: pass;
  - `gofmt`, `git diff --check`, and secret-pattern scan: pass;
  - changed Store package statement coverage: 80.9%.
- Gate revision deviation: testing showed that stable fail-closed listing
  requires reading and validating each indexed current Employee and Projects
  record. This adds bounded read amplification (at most 256 records) but stays
  inside Phase 2 and is required by the accepted fail-closed contract.
- Remaining accepted risks are unchanged: writes are atomic per file rather
  than cross-file transactional, and locking is in-process only. No recovery
  journal or second state machine was added. No Phase 3 subsystem was started.

Phase 2 full-path containment revision record (2026-07-28):

- Status: `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`.
- Security revision commit:
  `9190433834b922672ec21ae99ce4f0a3fde4a00a`
  (`fix(employees): contain all store paths`).
- `NewStore` now resolves the deepest existing ancestor and stores a stable,
  canonical real root. It does not scan Home and still represents exactly one
  owner-scoped Employee Store.
- All index, Employee, Projects, Revision, and Activity reads use one secure
  path layer. It enforces lexical containment, walks every existing directory
  with `Lstat`, rejects parent symlinks, rejects final-file symlinks, and
  requires a bounded regular file before reading.
- All Store writes use the same path layer, create missing directories one
  component at a time only after checking the complete parent chain, reject
  symlink/non-regular final targets, and then delegate to the existing
  same-directory `storage.AtomicWrite` implementation with `0600`, fsync, and
  rename behavior unchanged. Immutable Revision targets retain exclusive
  create-once checks.
- `loadIndex` validates every Summary ID with the complete Store ID contract
  before any path use. Invalid traversal, absolute, encoded, or otherwise
  unsupported IDs are `ErrCorrupt`; caller-supplied invalid IDs are input
  errors. Control Plane/Web evidence verifies HTTP 500 and 400 respectively.
- `Get`, `List`, `Activity`, and lifecycle mutations validate the complete
  Employee layout, including the Employee directory, current files, Activity
  directory/file, Revisions directory, and every Revision entry. A symlink or
  non-regular entry anywhere in that known layout fails closed before a
  mutation can reach an external target.
- macOS tests executed real symlink attacks for external index, Employee
  directory, employee file, projects file, Activity directory/file, Revisions
  directory/file, plus a pre-positioned Employee-directory write target.
  Tests assert Get/List/Activity/lifecycle failure and byte-for-byte unchanged
  external targets. No symlink test was skipped.
- Verification after the containment revision:
  - `go test ./internal/employeestore ./internal/controlplane ./internal/web -count=1`: pass;
  - `go test ./... -count=1`: pass;
  - `go test -race ./... -count=1`: pass;
  - `go vet ./...`: pass;
  - CLI and Web builds: pass;
  - `gofmt`, `git diff --check`, and secret-pattern scan: pass;
  - Employee Store statement coverage: 81.8%.
- Scope deviation: none. The revision changes only Employee Store path
  handling and necessary error-mapping tests inside the authorized Phase 2
  file boundary.
- Remaining accepted risks are unchanged: writes are atomic per file, not a
  cross-file transaction, and locking is process-local. No Recovery Journal,
  second state machine, multi-Workspace manager, or Phase 3 subsystem was
  added.

### Phase 3: Skill Catalog, SKILL.md Adapter, policy intersection, and context contract

Independent value:

- Reads native manifest Skills and SKILL.md-only Skills from explicit local
  Catalog Roots, pins bindings and digests, computes the exact fail-closed
  capability intersection including `AgentPreset.ToolPolicy`, and defines
  bounded Employee context layers without launching Tasks.

Expected files:

```text
internal/skill/*
internal/employee/policy.go
internal/employeestore/*
internal/contextmgr/context.go
internal/contextmgr/context_test.go
internal/controlplane/employee_skills.go
internal/web/employee_skills.go
compose.yaml
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/skill ./internal/employee ./internal/contextmgr ./internal/controlplane ./internal/web -count=1
go test ./internal/tool ./internal/tool/builtin ./internal/policy ./internal/approval -count=1
git diff --check
```

Non-goals: no unconfigured-directory scan, script execution, dependency
installation, internet access, Knowledge, Memory, Task dispatch, Team Employee
mapping, or UI.

Exit: native manifest and SKILL.md Adapter path/frontmatter/synthetic
version/digest/zero-capability tests pass; scripts and dependencies are never
executed; policy formulas prove Skills can only remove capabilities; legacy
context ordering regresses cleanly.

Phase 3 Gate revision record (2026-07-28):

- Status: `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`. Phase 4 and every later
  product phase remain `BLOCKED_BY_GATE` and were not started.
- Clean branch:
  `agent/electronic-employees-v0.7-phase3`, created from
  `origin/main@9a75e8f021b2bd5de1522d93e7463b313318c99d`.
  The old audit branch `agent/electronic-employees-v0.7` remains unchanged at
  `ee3c386cf0e5778c4861bc989fdb727069e94a2f`.
- Phase 3 implementation was transplanted only from old commit
  `76906a2b42e49efd27320a866e474339ef61c625`; its clean-branch commit is
  `1fff90c`. No old plan-only commit was cherry-picked and no Phase 1/2 product
  commit was replayed.
- Gate implementation commit:
  `ada73f11573bdab13fd05a84154c99dfd8b14816`
  (`fix(skills): harden phase 3 catalog gate`).
- Evidence commit:
  `e6f8484320f10040aa10d66cad1fc0f32b40df21`
  (`docs(plan): record phase 3 gate revision`).
- Draft PR: [#36](https://github.com/Rj455555/GoHermit/pull/36), targeting
  `main`; it remains Draft and must not be merged before Owner reapproval.
- Digest root cause: native Manifest validation accepted uppercase SHA-256,
  Catalog retained the supplied case, Employee binding normalization persisted
  lowercase, and Control Plane drift comparison was exact. A valid uppercase
  Manifest therefore became `digest_drift` immediately after persistence.
  The Catalog now canonicalizes every validated native Digest once to lowercase
  before projection and uses exact comparison with the calculated lowercase
  Digest. Binding, immutable Revision Snapshot, reopened Store, API projection,
  and drift status now share one representation; no `EqualFold` comparison
  masks non-canonical persisted data.
- Executable-content root cause: the shared `readRegular` boundary rejected
  symlinks and non-regular files but did not inspect executable permission
  bits. It now rejects any Catalog file with
  `mode.Perm() & 0111 != 0`. Native and Adapter executable references fail
  closed before reading into context; `scripts/`, package metadata, installers,
  and dependencies remain ignored and are never executed or installed.
- Encoded-path root cause: the earlier `%2e...` regression fixture did not
  create the literal file or a matching Digest, so missing-file failure hid the
  validation gap. `validateContentPath` now directly rejects every path
  containing `%`, without URL decoding. The replacement test creates the
  literal `references/%2e%2e%2foutside` file and a matching Manifest Digest;
  Catalog still rejects the ambiguous path before content loading.
- New named regressions:
  - `TestNativeManifestDigestIsCanonicalLowercase`;
  - `TestUppercaseManifestDigestBindingRemainsCurrentAfterStoreReopen`;
  - existing `TestEmployeeSkillsReportsDigestDriftAndCatalogCorruption`
    continues to prove real drift;
  - `TestCatalogRejectsExecutableContentWithoutExecutingIt/native`;
  - `TestCatalogRejectsExecutableContentWithoutExecutingIt/adapter`;
  - `TestCatalogLoadsNonExecutableReadOnlyReference`;
  - `TestNativeManifestRejectsURLAndEncodedPaths/existing_encoded_path_with_valid_digest`.
  Marker-writing executable fixtures ran on macOS and no marker was created.
  Existing traversal, absolute-path, symlink, non-regular, and Catalog
  containment tests remain green with no symlink/executable-bit skip.
- Additional fail-closed tests cover exact `Catalog.Resolve`, invalid Catalog
  roots, composite bounded configuration types, and structural schema limits.
  `internal/skill` statement coverage is 80.8%.
- Verification:
  - targeted Digest/executable/encoded-path/symlink regressions: pass;
  - `go test ./internal/skill ./internal/employee ./internal/contextmgr ./internal/controlplane ./internal/web -count=1`: pass;
  - `go test ./internal/tool ./internal/tool/builtin ./internal/policy ./internal/approval -count=1`: pass;
  - `go test ./... -count=1`: pass;
  - `go test -race ./... -count=1`: pass;
  - `go vet ./...`: pass;
  - `go build ./cmd/hermit` and `go build ./cmd/hermit-web`: pass;
  - `gofmt`, `git diff --check`, credential-shaped secret-pattern scan, and
    independent `compose.yaml` YAML parse: pass.
- Clean-history evidence before plan closeout:
  `git log --oneline origin/main..HEAD` contained only `1fff90c` and
  `ada73f1`; `git diff --stat origin/main...HEAD` contained only the 16 Phase 3
  implementation/Gate files. The final plan-only evidence commits add this
  document and no product difference.
- Remaining risks are unchanged: Activity append and Employee revision update
  are separate atomic file operations under the accepted no-cross-file-
  transaction boundary; Catalog intentionally rereads and rehashes its bounded
  explicit root; the schema subset and capability vocabulary remain
  conservative; cross-process Employee Store locking remains deferred. No
  second Event, Run, Approval, Verification, or recovery state machine was
  introduced.

### Phase 4: Knowledge Base and Employee Memory

Independent value:

- Adds deterministic local Knowledge indexing/citations and private,
  provenance-linked Employee Memory plus Candidate accept/reject,
  view/edit/Forget management.

Expected files:

```text
internal/knowledge/*
internal/employeememory/*
internal/employeestore/*
internal/contextmgr/context.go
internal/contextmgr/context_test.go
internal/controlplane/employee_knowledge.go
internal/controlplane/employee_memory.go
internal/web/employee_knowledge.go
internal/web/employee_memory.go
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/knowledge ./internal/employeememory ./internal/contextmgr ./internal/controlplane ./internal/web -count=1
go test ./internal/tool/builtin ./internal/contextmgr -count=1
git diff --check
```

Non-goals: no remote URLs, embeddings, background refresh, Task execution,
Candidate generation from Runs, automatic Memory promotion, Team mapping, or
UI.

Exit: containment/capacity/citation/isolation/secret/edit/Forget tests pass;
Knowledge and Memory context layers are bounded and Employee-isolated.

#### Phase 4 Gate revision evidence

- Status: `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`. Phase 5 through Phase 10
  remain `BLOCKED_BY_GATE`; no Phase 5 product file was created or modified.
- Clean branch: `agent/electronic-employees-v0.7-phase4`, created directly from
  `origin/main@d31bcf3b1d9eaf324721cc9004bcf8b9ea95c521`.
- Clean Phase 4 implementation:
  `f5d24b0` (`feat(employees): add phase 4 knowledge and memory`), produced by
  cherry-picking only the authorized old implementation
  `6e3a23ca3eff1e710b88f319633e23df598b1f29`.
- Gate fix:
  `75ee1a39abd90233a36dc2e768b55d2f2ebc6fea`
  (`fix(employees): harden phase 4 persistence gate`).
- Evidence commit:
  `fa127a02b23a379eb0c706fca4f5644089be7d98`
  (`docs(plan): record phase 4 gate revision`).
- Replacement review: Draft PR
  [#37](https://github.com/Rj455555/GoHermit/pull/37), targeting `main`.
  It was verified Open, Draft, unmerged, and without auto-merge after push.
- Persisted Knowledge validation now treats the stored representation as
  untrusted. `ValidateSource(..., true)` applies file-independent canonical
  relative-path validation and rejects absolute/URL/scheme/host, `%`,
  backslash, CR/LF, traversal, empty/dot components, non-clean paths,
  forbidden components, and paths over 4 KiB. Title, ManualText, Error,
  Heading, and Snippet reject NUL, invalid UTF-8, secret-like values, private
  reasoning, chain-of-thought labels, raw tool arguments, and hidden/full
  system-prompt content.
- `ValidateIndex` now requires 1..256 unique strictly path-sorted Documents,
  canonical lowercase SHA-256 Document digests, at most 4,096 canonical
  strictly sorted unique terms per Document, 1..1,024 globally unique
  deterministic Citations, canonical Citation ordering, matching
  Employee/Source/path/digest metadata, valid line bounds, and bounded
  canonical Heading/Snippet text.
- The canonical Source/Index digest is recalculated over the Source identity
  and metadata plus every ordered Document path/digest, term, Citation identity
  and metadata, Heading, and Snippet. Persisted `Source.Digest` and
  `Index.SourceDigest` must both exactly equal that result. Changing a snippet,
  term, path, Citation field, Document digest, order, or cardinality therefore
  fails closed. Manual/local content line endings are canonicalized before
  indexing.
- ID semantics are explicit: `Source.ID` is an Owner/caller-provided stable
  bounded ID. A Document has no separate ID and is identified inside an Index
  by its canonical path plus canonical content digest. Citation IDs and
  promoted Memory Fact IDs are deterministic SHA-256-derived IDs.
- Phase 4 empty-body mutations use one 1 KiB bounded reader and verify actual
  EOF; they do not trust `ContentLength`. Knowledge refresh/delete, Candidate
  accept/reject, and Memory Forget reject chunked bodies, including
  `ContentLength == -1`. Knowledge search query text is capped at 4 KiB.
- Provenance digests sort by the complete tuple
  `(SourceType, SourceID, SourceTaskID, SourceSessionID, SourceRunID,
  VerifiedAt)`. Input order cannot affect Candidate/Fact digests; exact
  duplicate tuples are rejected. The same SourceType/SourceID may refer to
  different Runs only when the complete tuple differs.
- New focused tests include:
  `TestValidateSourceRejectsUnsafePersistedTextAndStaticPaths`,
  `TestValidateIndexRejectsCanonicalIntegrityTampering`,
  `TestValidateIndexRejectsUnsafeCitationTextWithRecomputedDigest`,
  `TestKnowledgeStoreRejectsPersistedSourcePathCorruption`,
  `TestKnowledgeStoreRejectsPersistedIndexCorruption`,
  `TestKnowledgeStoreRejectsInvalidUTF8Text`,
  `TestKnowledgeStoreReopenPreservesStableSearch`,
  `TestProvenanceDigestUsesCompleteCanonicalTuple`,
  `TestMemoryCandidateCanonicalProvenanceSurvivesStoreReopen`,
  `TestPhase4APIRejectsNULManualTextAndOversizedQuery`,
  `TestPhase4EmptyBodyMutationsRejectChunkedPayloads`, and
  `TestEmployeeKnowledgeAPICorruptPersistedSourceReturnsInternalError`.
- macOS validation completed on 2026-07-28:

```text
PASS gofmt on every changed Go file
PASS Phase 4 targeted corruption/body/provenance tests
PASS go test ./internal/knowledge ./internal/employeememory ./internal/employeestore ./internal/contextmgr ./internal/controlplane ./internal/web -count=1
PASS go test ./internal/tool/builtin ./internal/contextmgr -count=1
PASS go test ./... -count=1
PASS go test -race ./... -count=1
PASS go vet ./...
PASS CLI and Web builds to /tmp
PASS git diff --check
PASS credential-shaped secret-pattern scan
PASS compose.yaml parse
PASS macOS real symlink/executable-bit tests; executable marker never created
PASS Knowledge package coverage 84.4%; Employee Memory coverage 91.1%
PASS GitHub CI runs 30348124269 and 30348130054
PASS CI go jobs (gofmt, tests, race, vet, builds, and cross-platform checks)
PASS CI docker jobs (Compose configuration and Docker build)
PASS CI web-e2e jobs
EXPECTED SKIP live-smoke jobs
```

#### Phase 4 final UTF-8 Gate revision

- Status remains `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`; Phase 5 through
  Phase 10 remain `BLOCKED_BY_GATE` and Phase 5 has not started.
- Root cause: Go `encoding/json` replaces malformed UTF-8 bytes and isolated
  UTF-16 surrogate escapes in quoted strings with U+FFFD. The earlier
  post-decode `utf8.ValidString` checks therefore could not distinguish
  malformed input from accepted text.
- Implementation commit:
  `c7c7c25c66f9d357c88eff730f5912828a718614`
  (`fix(employees): reject invalid unicode in phase 4 JSON`).
- Gate evidence commit:
  `ec308d19c786033101eeb217f3916b093ea13c80`
  (`docs(plan): record phase 4 utf8 gate revision`).
- Employee Store `decodeStrict` now calls `utf8.Valid(raw)` before any JSON
  Decode. Unknown-field rejection, the single-value rule, bounded file reads,
  and corrupt-store error mapping remain unchanged.
- Phase 4 HTTP JSON decoding now reads through `MaxBytesReader`, validates the
  bounded raw body with `utf8.Valid`, and only then performs strict JSON Decode
  from a `bytes.Reader`. Invalid raw UTF-8 returns HTTP 400 before Control Plane
  or Store mutation.
- Knowledge Title, ManualText, Source Error, Citation Heading/Snippet and
  canonical Terms, plus Employee Memory Value, reject
  `unicode.ReplacementChar`. This deliberately also rejects an Owner-authored
  literal U+FFFD so malformed surrogate replacement cannot be persisted.
- Focused regression coverage:
  `TestDecodeStrictRejectsRawInvalidUTF8BeforeJSONDecode`,
  `TestKnowledgeStoreRejectsInvalidUTF8InNonDigestSourceError`,
  `TestPhase4JSONRejectsInvalidUTF8AndSurrogatesWithoutMutation`,
  `TestEmployeeKnowledgeAPIInvalidUTF8StoreReturnsInternalError`, and
  `TestPhase4MultilingualUTF8SurvivesReopenAndEntersContext`.
  Existing Knowledge and Memory validation tests also cover U+FFFD in Title,
  ManualText, Source Error, Heading, Snippet, and Memory Value.
- Failed Knowledge requests leave `sources.json` and `index.json` absent;
  failed Memory edit requests leave `facts.json` byte-for-byte unchanged.
  Valid Chinese, Emoji, and other well-formed multilingual UTF-8 survives
  persistence, Store reopen, and bounded Context assembly.
- macOS validation completed on 2026-07-28:

```text
PASS final UTF-8/surrogate targeted tests
PASS go test ./internal/knowledge ./internal/employeememory ./internal/employeestore ./internal/contextmgr ./internal/controlplane ./internal/web -count=1
PASS go test ./internal/tool/builtin ./internal/contextmgr -count=1
PASS go test ./... -count=1
PASS go test -race ./... -count=1
PASS go vet ./...
PASS CLI and Web builds to /tmp
PASS gofmt on every changed Go file
PASS git diff --check
PASS credential-shaped secret-pattern scan
PASS GitHub CI runs 30350404454 and 30350408674
PASS CI go jobs (gofmt, full tests, race, vet, builds, cross-platform checks)
PASS CI docker jobs (Compose configuration and Docker build)
PASS CI web-e2e jobs
EXPECTED SKIP live-smoke jobs
```

- Persistence retains the accepted single-file atomic/no-cross-file-transaction
  boundary. This Gate adds no Recovery Journal or second state machine.
- Remaining risks: digest integrity detects corruption but is not a keyed
  authenticity mechanism; an attacker able to rewrite every file and recompute
  every digest is outside the local corruption model. Document content itself
  remains at the explicit local root, so a persisted local Index can validate
  its canonical stored digest chain without requiring the original file to
  still exist. Task/Session/Run provenance verification remains Phase 7 work;
  Phase 4 only validates the complete bounded provenance tuple.

### Phase 5: Employee Task Inbox persistence and API

Gate start evidence:

- Phase 4 was Owner-approved after its final UTF-8 Gate and externally
  squash-merged through PR #37.
- Fixed base:
  `origin/main@e65bc1196e73e0b8962b012be76c0852f48e8c3c`.
- Clean Phase 5 branch:
  `agent/electronic-employees-v0.7-phase5`, created directly from that base.
- No Phase 4 implementation, Gate, or plan-only commit was cherry-picked.
- A new Draft PR may be created only after Phase 5 implementation, local
  verification, final push, and green push CI.

Independent value:

- Owners can queue, list, inspect, and cancel multiple pre-dispatch Tasks with
  immutable Employee/Skill/Knowledge/Project snapshots. Nothing executes.

Expected files:

```text
internal/employee/task.go
internal/employee/task_test.go
internal/employeestore/tasks.go
internal/employeestore/tasks_test.go
internal/controlplane/employee_tasks.go
internal/controlplane/employee_tasks_test.go
internal/web/employee_tasks.go
internal/web/employee_tasks_test.go
internal/web/server.go
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/employee ./internal/employeestore ./internal/controlplane ./internal/web -count=1
go test ./internal/loop ./internal/loopstore -count=1
git diff --check
```

Non-goals: no Session/Run creation, model call, scheduler, automatic start,
SSE, Memory Candidate generation, Team mapping, or UI.

Exit: multiple queued Tasks, stable paging, snapshot immutability, queued cancel,
disabled/archive gates, and restart persistence pass; assertions prove zero
Session/Run/model side effects.

#### Phase 5 implementation and Gate evidence

- Status: `GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`. Phase 6 through Phase 10
  remain `BLOCKED_BY_GATE`; Phase 6 has not started.
- Implementation commit:
  `ac01dce4c7bc49b2a196869b419230d9022fb500`
  (`feat-phase5-employee-task-inbox`).
- Evidence commit: `eac7ca13b622ad0311cbb77ee20cc5c883d5897d`.
- Snapshot Digest Gate revision commit:
  `6fa05473b602c2d6af962f0cf78b7876a1b23d3e`
  (`fix(phase5): decouple task snapshot digest from execution bindings`).
- Review target:
  Draft PR [#38](https://github.com/Rj455555/GoHermit/pull/38), base `main`,
  head `agent/electronic-employees-v0.7-phase5`. Creation is deliberately the
  final Gate action after the final document-only push CI is green; it must
  remain Open, Draft, unmerged, and without auto-merge.
- Actual product/test files:
  - `internal/employee/task.go`;
  - `internal/employee/task_test.go`;
  - `internal/employee/employee.go`;
  - `internal/employee/snapshot.go`;
  - `internal/employeestore/tasks.go`;
  - `internal/employeestore/tasks_test.go`;
  - `internal/employeestore/store.go`;
  - `internal/controlplane/employee_tasks.go`;
  - `internal/controlplane/employee_tasks_test.go`;
  - `internal/web/employee_tasks.go`;
  - `internal/web/employee_tasks_test.go`;
  - `internal/web/server.go`.
- Domain contract: schema-v1 `EmployeeTask` permits only `queued` and
  `cancelled`; `queued -> cancelled` is the only transition and repeated
  cancellation is idempotent. `SessionID` and `RunID` must be empty. The
  immutable Task Snapshot Digest covers schema version, Task/Employee identity,
  prompt, creation time, the complete Employee Revision Snapshot, pinned Skill
  ID/version/digest/config/enabled, selected Knowledge source
  digest/Citation references, accepted Memory Fact ID/digest, ProjectBinding,
  and Task policy. Lifecycle state/timestamps and Session/Run execution
  bindings are mutable projection fields outside that digest. Phase 5 still
  rejects non-empty Session/Run bindings; later binding must not rewrite the
  immutable digest.
- Phase 5 Snapshot Digest Gate revision:
  - root cause: schema-v1 digest input included empty `SessionID` and `RunID`,
    which would force Phase 6/7 binding either to rewrite the immutable digest
    or make Task validation disagree with dispatch/Session snapshots;
  - fix: remove Session/Run projection fields from `taskSnapshotDigest` while
    retaining all immutable Task business fields and the Phase 5 empty-binding
    validation rule;
  - forward-compatibility tests prove lifecycle cancellation fields and future
    Session/Run bindings do not change the low-level immutable digest, while
    prompt, Employee, Skill, Knowledge, Memory, ProjectBinding, and Task Policy
    changes do;
  - JSON round-trip, queued-to-cancelled persistence, and Store reopen preserve
    the same immutable digest.
- Persistence contract: each Employee owns
  `<employee-id>/tasks/index.json` and
  `<employee-id>/tasks/<task-id>.json`. Task records are exclusive atomic
  mode-0600 writes; index and cancellation updates reuse the existing
  same-directory atomic write primitive. The Store enforces a 512 KiB Task
  record, 10,000 summaries per Employee, maximum page size 100, newest-first
  stable opaque cursors, strict schema/JSON/raw UTF-8, identity/digest/index
  agreement, and lexical/realpath/symlink/non-regular containment. Global
  owner-scoped Task lookup fails closed on any ambiguous Task ID.
- Lifecycle contract: only active Employees create Tasks. Disabled or archived
  Employees cannot create, but historical Tasks remain readable and a queued
  Task remains cancellable. Multiple queued Tasks are allowed because Phase 5
  has no running state.
- API:
  - `POST /api/employees/{id}/tasks`;
  - `GET /api/employees/{id}/tasks`;
  - `GET /api/employee-tasks/{taskID}`;
  - `POST /api/employee-tasks/{taskID}/cancel`.
  Mutations enforce same-origin. JSON creation is bounded, strict,
  single-value, and raw-UTF-8 validated. Cancellation requires a truly empty,
  bounded body, including when `ContentLength == -1`. Detail projection omits
  the local workspace path and complete Employee revision body.
- Activity adds only `task_created` and `task_cancelled` bounded reference
  events containing Employee/Task/revision identity. It contains no prompt,
  Snapshot, Session/Run truth, SSE, Tool, Approval, or Verification payload and
  cannot drive Task/Session/Run recovery.
- Named regressions include:
  - `TestNewEmployeeTaskSealsDeepImmutableQueuedSnapshot`;
  - `TestEmployeeTaskJSONRoundTripPreservesSnapshotDigest`;
  - `TestEmployeeTaskSnapshotDigestExcludesLifecycleAndExecutionProjection`;
  - `TestEmployeeTaskSnapshotDigestCoversEveryImmutableSelection`;
  - `TestValidateEmployeeTaskRejectsFutureExecutionBindings`;
  - `TestTaskStoreMultipleQueuedStablePaginationFilterCancelAndReopen`;
  - `TestTaskStoreConcurrentCreateHasNoDuplicateOrLostIndex`;
  - `TestTaskStoreRejectsCorruptTaskAndIndex`;
  - `TestTaskStoreRejectsTaskDirectoryAndIndexSymlinkWithoutOutsideWrites`;
  - `TestTaskStoreRandomIDFailureDoesNotPersist`;
  - `TestEmployeeTaskControlPlanePinsSelectionsAndNeverExecutes`;
  - `TestEmployeeTaskInboxAPIQueuesListsGetsAndCancelsWithoutExecution`;
  - `TestEmployeeTaskAPIStrictBoundedUTF8SameOriginAndEmptyCancelBody`.
- Zero-execution evidence: Control Plane and HTTP tests assert no Session Store
  or Run is created, runtime builder/model/provider is never called, service
  run state stays inactive, Workspace bytes/tree stay unchanged, Knowledge is
  not refreshed, and no Memory Candidate is generated. No Phase 5 product
  file imports or calls Session/Run/Runner/model/tool/workspace-lease APIs.
- macOS local validation on 2026-07-28:

```text
PASS gofmt on every tracked Go file
PASS go test ./internal/employee ./internal/employeestore ./internal/controlplane ./internal/web -count=1
PASS go test ./internal/loop ./internal/loopstore ./internal/session -count=1
PASS go test ./... -count=1
PASS go test -race ./... -count=1
PASS go vet ./...
PASS CLI and Web builds to /tmp
PASS git diff --check
PASS credential-shaped secret-pattern scan
PASS independent compose.yaml YAML parse
PASS macOS real Task symlink/non-regular/containment tests with no skip
PASS scoped package coverage: employee 82.6%, employeestore 77.8%,
     controlplane 68.3%, web 68.7%; aggregate 73.6%
PASS push CI run 30356141023
PASS CI go, web-e2e, and docker jobs
EXPECTED SKIP CI live-smoke job
```

- Final Phase 5 Snapshot Digest Gate validation on 2026-07-28:

```text
PASS targeted forward-compatibility digest tests (after an expected RED)
PASS Phase 5 Employee/Store/Control Plane/Web scoped tests
PASS Loop/LoopStore/Session regressions
PASS go test ./... -count=1
PASS go test -race ./... -count=1
PASS go vet ./...
PASS CLI and Web builds to /tmp
PASS gofmt and git diff --check
PASS credential-shaped secret-pattern scan
PASS independent compose.yaml YAML parse
PASS macOS real symlink/non-regular/containment tests with no skip
```

- Deliberate Phase 5 deviation: JSON round-trip testing exposed that the
  existing deep-copy helpers collapsed non-nil empty slices to nil. That
  changed an otherwise immutable Task Snapshot Digest during cancellation
  after Store reopen. `cloneEmployee`, `cloneProjectBindings`, and Task clone
  helpers now preserve exact nil/empty representation, and Skill configuration
  comparison/digesting uses canonical JSON. This is a narrowly required
  snapshot-integrity fix, not a Phase 1 contract expansion.
- Remaining risks:
  - Task record, Task index, and Activity append are separate atomic files.
    There is intentionally no cross-file transaction or recovery journal;
    interrupted mismatch is detected and fails closed.
  - Concurrency remains process-local; cross-process Employee Store locking is
    deferred.
  - Global Task lookup validates bounded Employee indexes and records and can
    be linear in the bounded Employee/Task population. A separate global Task
    manager was intentionally not introduced.
  - Snapshot digests detect corruption but are not keyed authenticity proofs.
  - Docker is not installed on the macOS host; local Compose validation used
    an independent YAML parser. CI run `30356141023` performed both
    `docker compose config` and the Docker build successfully.

### Phase 6: Runtime Preparation

Independent value:

- Makes a queued EmployeeTask deterministically ready for later explicit
  execution without starting a model or Run.
- Validates Employee state/model/credential, Skill digests, Knowledge status,
  selected Employee Memory, exact current-Service Workspace binding, Task
  policy, and the base/effective capability formulas.
- Revalidates every pinned Knowledge Source digest and Citation reference
  against the current Knowledge Store, and every selected Memory Fact ID/digest
  against a still-existing accepted Fact. Refresh, delete, edit, Forget, or any
  mismatch fails closed before creating a Session, Run, or dispatch journal;
  Phase 5 creation-time selection is not sufficient readiness proof.
- Reads the complete immutable Employee revision already owned by
  EmployeeTask/Employee Store, pins any preparation-time selections there, and
  builds a separate compact Employee/Skill/Knowledge/Memory context snapshot
  for Session recovery.
- Adds Session schema v6, a stable Session ID, and the cross-store dispatch
  journal; creates or recovers a prepared Session with no Run.

Expected files:

```text
internal/session/session.go
internal/session/session_test.go
internal/controlplane/employee_tasks.go
internal/controlplane/employee_tasks_test.go
internal/controlplane/service.go
internal/contextmgr/*
internal/employeestore/dispatch.go
internal/employeestore/dispatch_test.go
internal/employee/snapshot.go
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/session ./internal/controlplane ./internal/contextmgr ./internal/employeestore ./internal/employee -count=1
go test ./internal/config ./internal/skill ./internal/knowledge ./internal/employeememory -count=1
git diff --check
```

Non-goals: no `Runner.Run`, provider/model call, Run creation, Task execution
status projection, concurrency/writer lease acquisition, cancel/resume,
verified Memory Candidate generation, Artifact generation, Team mapping, or UI.

Exit:

- v5->v6 migration and legacy Session recovery pass.
- Full EmployeeRevisionSnapshot remains only in EmployeeTask/Employee Store.
- Ordinary and hidden Worker Session compact snapshots reject more than
  64 KiB; Team parent assignment limits are reserved but not wired until
  Phase 9.
- Preparation assigns a stable Session ID, writes/reconciles the dispatch
  journal, and creates at most one Session with zero Runs.
- The dispatch journal and compact Session snapshot retain the Phase 5
  immutable EmployeeTask Snapshot Digest. Assigning a stable Session ID does
  not rewrite it.
- Missing or changed Knowledge Source/Citation or accepted Memory Fact
  readiness fails closed with zero Session, Run, dispatch-journal, model,
  provider, tool, lease, or Workspace side effects.
- Repeated preparation and restart are idempotent.
- Tests assert no runtime build, model call, workspace lease, or model-visible
  execution occurs.

### Phase 7: Manual Execution Lifecycle

Independent value:

- Explicit Start consumes the Phase 6 prepared state and creates/starts exactly
  one Run through the existing Run lifecycle in the stable Session.
- Projects EmployeeTask status from Session/Run, supports cancel/resume/restart,
  enforces one running Task per Employee and one Workspace mutation writer, and
  retains the conservative no-cross-Employee-read-only-concurrency rule.
- Produces verified bounded Memory Candidates and bounded Artifacts without
  changing Run authority. Candidates require explicit Owner acceptance.

Expected files:

```text
internal/controlplane/employee_tasks.go
internal/controlplane/employee_tasks_test.go
internal/controlplane/runs.go
internal/controlplane/service.go
internal/runcontrol/*
internal/employeestore/dispatch.go
internal/employeestore/artifacts.go
internal/employeestore/activity.go
internal/employeememory/*
internal/web/employee_tasks.go
internal/web/employee_tasks_test.go
internal/web/server.go
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/controlplane ./internal/runcontrol ./internal/employeestore ./internal/employeememory ./internal/web -count=1
go test ./internal/session ./internal/agent ./internal/approval ./internal/verification ./internal/loop ./internal/loopstore -count=1
git diff --check
```

Non-goals: no second Run/Event/Approval/Verification state machine, no Employee
Task SSE, no daemon, no auto-start-next, no cross-Employee read-only
concurrency, no automatic Memory acceptance, no Team Role mapping, and no UI.

Exit:

- Explicit Start creates at most one Run across retry/crash/restart.
- Bound status is always projected from Session/Run/Approval/Plan/Verification.
- Cancel queued/prepared/bound, waiting Owner, interruption, resume, restart,
  verification failure, Employee concurrency, and Workspace writer lease pass.
- Verified success creates bounded idempotent Candidates and Artifacts;
  MemoryFact remains absent until Owner acceptance.
- Activity contains only allowed lifecycle/reference metadata and cannot drive
  recovery or duplicate Session SSE/tool events.

### Phase 8: Employees and Tasks Web UI

Independent value:

- Delivers Dashboard/Employees/Tasks/Agent/Loops/Settings navigation, the
  nine-step Employee wizard, detail tabs, Task queue/timeline controls,
  Skill/Knowledge/Memory/Project management, and refresh recovery.

Expected files:

```text
internal/web/assets/index.html
internal/web/assets/styles.css
internal/web/assets/app.js
internal/web/assets/loops.js
internal/web/assets/employees.js
internal/web/assets/tasks.js
internal/web/server_test.go
tests/e2e/employees.spec.ts
tests/e2e/tasks.spec.ts
tests/e2e/static-server.mjs
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
node --check internal/web/assets/app.js
node --check internal/web/assets/loops.js
node --check internal/web/assets/employees.js
node --check internal/web/assets/tasks.js
pnpm test:e2e
go test ./internal/web -count=1
git diff --check
```

Non-goals: no TeamTemplate Employee mapping, no public hosting, no background
queue, no remote Skill/Knowledge operations.

Exit: Fake Provider E2E covers wizard, native/SKILL.md-only Skills, Knowledge,
current-Service Workspace binding, multiple Tasks, explicit Start,
timeline/approval, refresh, Memory Candidate accept/reject and Fact Forget,
disable/enable/archive history, plus every existing Agent/Loops/Settings
regression.

### Phase 9: Team Role to Employee mapping

Independent value:

- Optionally assigns persistent Employees to Team Roles while preserving
  legacy TeamTemplate behavior and private-memory isolation.

Expected files:

```text
internal/teamtemplate/template.go
internal/teamtemplate/template_test.go
internal/team/team.go
internal/team/team_test.go
internal/app/team_worker.go
internal/app/team_worker_test.go
internal/controlplane/team.go
internal/controlplane/server_test.go
internal/session/session.go
internal/session/session_test.go
internal/web/assets/*
tests/e2e/*
IMPLEMENTATION_PLAN.md
```

Tests:

```bash
go test ./internal/teamtemplate ./internal/team ./internal/app ./internal/controlplane ./internal/session -count=1
go test ./internal/contextmgr ./internal/agent ./internal/runcontrol ./internal/web -count=1
pnpm test:e2e
git diff --check
```

Non-goals: no new Roles, no free-form Employee chat, no Employee-to-Employee
memory sharing, no parallel writers/worktrees, no model fallback.

Exit: TeamTemplate v1 migration, optional assignment, model precedence,
16 KiB per-assignment/64 KiB parent aggregate limits, 64 KiB hidden Worker
compact snapshot/recovery, per-Worker context, cross-Employee isolation, and
old TeamTemplate regression tests pass.

### Phase 10: Evals, Docker, docs, and v0.7 release closeout

Independent value:

- Proves cross-cutting isolation, permission, recovery, idempotency, browser,
  and Docker behavior; completes versioned documentation and handoff.

Expected files:

```text
internal/evals/*
docs/adr/0013-first-class-electronic-employees.md
docs/ai/employees.md
docs/ai/context.md
docs/ai/next-development-plan.md
docs/ai/handoff-v0.7.md
docs/roadmap.md
CHANGELOG.md
version-bearing source files
compose.yaml / Dockerfile only if acceptance requires a scoped fix
IMPLEMENTATION_PLAN.md
```

Tests: the complete command set in section 20.2, repeated as necessary for
determinism, plus Docker `/data` before/after manifests and security scans.

Non-goals: every item in section 22.

Exit: all final tests and GitHub CI pass; container is healthy and Workbench is
browser-accessible; persistent data survives rebuild; docs/version/handoff are
consistent; no generated files or protected user files are committed; Phase 10
commit is pushed and Draft PR is ready for Owner review.

## 22. Non-goals

v0.7 does not implement:

- Worktree-based or any other parallel mutation writers.
- Automatic commit, push, PR creation, merge, deploy, external messaging, or
  publication.
- Cron, scheduler, background daemon, auto-start-next, or unattended queue.
- Multi-user accounts, organization tenancy, cloud control plane, public Web,
  or remote telemetry.
- Free-form Employee-to-Employee chat or unlimited child Agent creation.
- Automatic internet Skill discovery/install/update or installer execution.
- Remote Knowledge URL fetch, crawler, embedding model, or vector database.
- Unauthorized filesystem project discovery.
- Automatic sharing of private Employee Memory.
- A second Agent, Run, Plan, Approval, Verifier, Event Store, Session store, or
  recovery state machine.
- Automatic provider fallback or silent model substitution.
- Rewriting old Sessions into Employee Tasks.
- Resolving ADR 0012 or implementing its proposed self-commit behavior.

## 23. Risks

| Risk | Mitigation and phase evidence |
|---|---|
| Cross-store Task/Session crash creates duplicate execution. | Phase 6 proves stable Session ID/prepared journal idempotency with zero Runs; Phase 7 separately proves at-most-one Run across retry/restart. |
| Employee context overrides security/project rules. | Fixed context precedence, untrusted Knowledge labeling, token ceilings, and exact ordering tests. |
| Skill becomes an implicit permission grant. | Base includes Global + AgentProfile ToolPolicy + Employee + Project + Task; no-Skill equals base; enabled-Skill union only narrows; SKILL.md Adapter requests zero capabilities; registry/executor/approval remain authoritative. |
| Private Memory leaks across Employees or Handoffs. | Store path ownership, Employee ID checks, explicit context selection, no Memory in Handoff, isolation evals. |
| Knowledge path or symlink escapes workspace. | Canonical path containment copied from builtin Workspace principles, with real symlink tests. |
| Secrets enter durable Employee data. | Shared secret screening before write, strict limits, no echo, staged-diff and API response scans. |
| Task state drifts from Run state. | Bound Task status is always a projection; no independent terminal transition methods. |
| New concurrency weakens current one-writer safety. | v0.7 deliberately keeps one running Task per Employee, no cross-Employee read-only concurrency, and one Workspace mutation writer; legacy gate is preserved. |
| Employee edits alter historical Tasks/Team work. | Immutable revision/Skill/Knowledge/Project snapshots and deep-copy tests. |
| Schema changes break old Sessions or TeamTemplates. | Explicit v5->v6 and Template v1->v2 migrations plus existing fixture regressions. |
| Runtime preparation accidentally starts execution. | Phase 6 exit assertions require zero Runs, zero provider/runtime/model calls, and no lease acquisition; execution belongs only to Phase 7. |
| Compact snapshot grows into a repeated Employee copy. | Full revision remains in EmployeeTask/Employee Store; Session/hidden Worker snapshot hard-fails above 64 KiB; parent Team metadata has 16 KiB/64 KiB limits. |
| Activity becomes a conflicting Event Store. | Activity accepts only enumerated lifecycle/reference records and is excluded from recovery, SSE, Run projection, and tool events. |
| Future ProjectBinding shape is mistaken for multi-Workspace support. | Readiness requires canonical equality with `Service.Workspace`; projects API returns one Workspace; no dynamic Store manager exists. |
| Browser feature regresses Agent/Loops/Settings. | Existing E2E remains mandatory and new navigation tests cover every surface. |
| Docker store migration damages current data. | New directory only, before/after `/data` manifests, no eager rewrite of old stores. |
| Memory Candidates contain plausible but incorrect facts. | Verified provenance and bounds gate Candidate creation; no MemoryFact exists until explicit Owner acceptance; edit/Forget remain available. |

## 24. Recorded Owner decisions and deferred questions

There are no unresolved Gate 0 product decisions. The Owner resolved the prior
six questions:

1. Task creation queues only; explicit Start is required.
2. Disable is reversible; Archive is terminal and preserves history.
3. Avatar supports initials and Emoji only.
4. Project Catalog is current Service Workspace-only.
5. Verified Tasks generate Candidates; explicit Owner acceptance is required
   before long-term Memory.
6. v0.7 permits one running Task per Employee, no cross-Employee read-only
   concurrency, and one mutation writer in the current Workspace.

Future multi-Workspace shared-store/locking semantics, automatic Memory
promotion, Avatar uploads/URLs, and cross-Employee read-only concurrency are
deferred decisions requiring later evidence and explicit plan/ADR changes.

## 25. Definition of done

v0.7 is done only when:

- All 10 Phases have an Owner approval, one scoped commit, push evidence, Draft PR
  update, verification evidence, deviation/risk record, and a stop gate in this
  file.
- Employee is a durable owner-scoped entity distinct from Role, Session, model,
  Skill, and Project.
- Employee create/edit/revision/disable/enable/archive and Dry Run work with
  strict validation, initials/Emoji-only Avatar, and no secrets.
- Local Skills are versioned/digested/declarative and cannot expand policy or
  install/fetch code. Explicit Catalog Roots support native manifests and a
  read-only SKILL.md-only Adapter that executes no scripts or dependencies.
- Knowledge is local, deterministic, cited, bounded, and path-contained.
- Employee Memory is private, verified/provenanced, bounded, viewable,
  editable, forgettable, and isolated from Project Memory and other Employees;
  verified Candidates require explicit Owner acceptance.
- An Employee can hold multiple queued Tasks; Phase 6 prepares at most one
  stable Session per Task; explicit Phase 7 Start creates/starts at most one Run
  through the existing lifecycle; cancel/resume/restart are idempotent.
- `base = Global ∩ AgentProfile ToolPolicy ∩ Employee ∩ Project ∩ Task`;
  no-Skill effective equals base; enabled-Skill effective equals base
  intersected with the enabled requested-capability union. It never weakens
  workspace, tool, network, approval, or verification controls.
- One Service executes only its startup-configured Workspace, returns only that
  Workspace from `/api/projects`, and creates no dynamic multi-Workspace
  Session Store manager.
- Full Employee revisions remain in EmployeeTask/Employee Store. Session and
  hidden Worker compact snapshots are at most 64 KiB; Team parent assignment
  metadata is at most 16 KiB each/64 KiB aggregate.
- Employee Activity contains only the enumerated lifecycle/reference records
  and never owns Run state, recovery, Session SSE, or tool events.
- The UI provides Dashboard / Employees / Tasks / Agent / Loops / Settings,
  the nine-step wizard, Employee detail tabs, actionable readiness failures,
  and refresh/service-restart recovery.
- TeamTemplate optionally binds Roles to Employee snapshots while old templates
  and unassigned Roles behave exactly as before.
- Existing Agent, Team, Loop Workbench, Session recovery, Approval,
  VerificationRecipe, Owner Profile, and Project Memory behavior remains green.
- No product path automatically commits, pushes, creates PRs, merges, deploys,
  sends messages, installs Skills, or starts a background daemon.
- `docs/adr/0013-first-class-electronic-employees.md`,
  `docs/ai/employees.md`, `docs/ai/context.md`,
  `docs/ai/next-development-plan.md`, `docs/roadmap.md`, `CHANGELOG.md`, and
  `docs/ai/handoff-v0.7.md` accurately describe the shipped state.
- Version reports `0.7.0-dev`.
- The complete normal/race/vet/build/Playwright/Docker/health test matrix passes
  without paid-model calls by default; GitHub CI is green.
- Docker rebuild preserves existing `/data`; Compose remains loopback-only by
  repository default.
- Git contains no credential, runtime evidence, Graphify/CodeGraph/browser/test
  output, protected untracked file, or build artifact.

The Phase 3 Gate revision ends here. No Phase 4 implementation is authorized
until the Owner explicitly reapproves Phase 3, for example:

```text
批准 Phase 3 Gate 修订，开始 Phase 4
```

STATUS: WAITING_FOR_PHASE_4_REAPPROVAL
