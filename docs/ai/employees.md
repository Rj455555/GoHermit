# Electronic Employees architecture

This is the implementation map for GoHermit `0.7.0-dev`. Employee features
reuse the existing Session/Run kernel; they do not form another agent runtime.

## Domain identities

| Entity | Meaning | Durable truth |
|---|---|---|
| Employee | Owner-scoped identity, charter, defaults, policy, revision | Employee Store |
| EmployeeTask | Immutable owner request and pinned Employee context | Employee Store |
| Session | Conversation/recovery container | Session Store schema v6 |
| Run | One execution attempt and its Plan/Tool/Verification lifecycle | Session checkpoint/events |
| Team Role | Temporary Mission responsibility | TeamTemplate/Mission |
| TeamEmployeeAssignment | Immutable Role/WorkItem binding to one Employee revision | Parent Mission + hidden Worker compact snapshot |

Employee is not a Role, Session, Run, Model, AgentProfile, Skill, or Project.
Role selections without `employee_id` retain the legacy Team behavior.

## Employee and Task lifecycle

Employee lifecycle:

```text
active -> disabled -> active
active -> archived
disabled -> archived
archived -> terminal
```

Task Inbox creation yields only `queued`. Before a Run is bound, Owner cancel
yields terminal `cancelled`. Execution states are never independently written
as a second state machine:

```text
queued | prepared | waiting_owner | running | verifying |
interrupted | completed | failed | cancelled
```

After binding, these values are projected from the existing Session, Run,
Plan, Approval, and Verification data.

## Snapshot ownership

An EmployeeTask stores the complete immutable `RevisionSnapshot`, pinned
Skill ID/version/digest/configuration, Knowledge Source digest and Citation
references, accepted Memory Fact ID/digest, ProjectBinding, Task policy, prompt,
creation identity, and one immutable `SnapshotDigest`. The digest deliberately
excludes State/lifecycle timestamps and SessionID/RunID. Binding or recovery
must never recompute it.

Prepare creates a compact `CompactSnapshot` capped at 64 KiB. It contains
bounded model-ready identity, effective policy, project summary, Skill
instructions/references, cited Knowledge snippets, and accepted Memory facts.
The full Employee file (up to 256 KiB) is never copied to a Session checkpoint.

## Prepare and Start

`PrepareEmployeeTask`:

1. Reloads the Task and exact Employee revision.
2. Requires the Employee to be active and the ProjectBinding to match the
   service Workspace's canonical real path.
3. Revalidates Provider/Access/Model (including the live Codex catalog),
   AgentProfile, Skill identity/config/digest, Knowledge Source/Citations,
   accepted Memory Facts, and policy intersection.
4. Seals the compact context.
5. Writes a bounded dispatch journal with a stable Session ID, immutable Task
   digest, compact digest, and Workspace.
6. Reconciles exactly one prepared schema-v6 Session.

It never creates a Run, builds a Runtime, calls a Provider/model, executes a
Tool, acquires the execution lease, refreshes Knowledge, creates Memory, or
modifies the Workspace.

`POST /api/employee-tasks/{taskID}/start` is the only execution entrypoint. It
reuses preparation, persists one stable Run before Runner execution, binds the
Task idempotently, and reconciles every crash point. Concurrent/repeated Start
returns the same Run projection or a conflict; it cannot create a replacement
Run. Resume continues the original interrupted Run.

## Context and permission layers

Context order is fixed:

1. global/Role/Agent safety;
2. Owner profile;
3. immutable Employee identity and boundaries;
4. effective policy/budget/project;
5. workspace `AGENTS.md`;
6. Project Memory;
7. pinned Skill instructions;
8. cited Knowledge;
9. this Employee's private accepted Memory;
10. recovered Session/Run state;
11. Task goal;
12. bounded recent messages/tool results.

Knowledge and Memory have independent byte ceilings. Lower-authority context
cannot override policy. Permissions are:

```text
base =
    Global Policy
  ∩ AgentProfile ToolPolicy
  ∩ Employee Policy
  ∩ Project Policy
  ∩ Task Policy

effective = base                                  # no enabled Skill
effective = base ∩ union(Skill requested caps)   # enabled Skills
```

Native Skills are pinned by ID + version + lowercase SHA-256 digest. The
SKILL.md Adapter is configured-root-only, instruction-only, requests zero
capabilities, executes no scripts, and installs nothing.

## Knowledge and Memory

Knowledge supports bounded Manual Text and configured local file/directory
sources only. Indexing and Citation IDs are deterministic. Every load validates
canonical paths, documents, terms, citations, snippets, and full digests.
Network fetch, remote URLs, embeddings, background refresh, executable files,
symlinks, and Home scans are forbidden.

Employee Memory consists of verified Candidates and accepted Facts.
Verification-success provenance pins Employee, Task, Session, Run, and time.
A Candidate never becomes long-term Memory without explicit Owner acceptance.
Facts can be viewed, edited with provenance retained, and forgotten. Forget
removes the fact from subsequent context assembly. Project Memory remains a
separate workspace-scoped verified layer.

## Team Role mapping

TeamTemplate schema v2 adds optional `employee_id`. A dedicated v1 wire schema
rejects v2-only fields and migrates a valid v1 Role with an empty Employee ID.

For a bound Role, preflight completes before Mission/Session/Worker side
effects. Explicit RoleSelection provider/access/model is the Mission override;
when absent, the Employee default is used. No vendor/model fallback occurs.
The sealed `TeamEmployeeAssignment` is capped at 16 KiB per WorkItem and
64 KiB per Mission. Hidden Worker compact context is capped at 64 KiB.
Recovery and dynamic WorkItems reuse the original assignment rather than
rereading a mutable Employee.

Hidden Worker Sessions are internal. Every public Session detail, messages,
events/SSE, Run create/resume/cancel, Plan approve, and Approval list/decide
path returns generic 404 before reading or mutation. Internal TeamWorker
execution and recovery use the Store directly.

## Public API map

- Employees: `/api/employees`, lifecycle actions, `dry-run`, `activity`.
- Skills/Knowledge/Memory/Projects:
  `/api/employees/{id}/{skills|knowledge|memory|projects}` and bounded
  Candidate/Fact mutations.
- Inbox: `/api/employees/{id}/tasks`.
- Task detail/start/cancel/resume:
  `/api/employee-tasks/{taskID}` and action suffixes.
- Projects catalog: `/api/projects` returns only the current Service Workspace.
- Execution stream: the bound existing
  `/api/sessions/{sessionID}/events?after={sequence}`; there is no Task SSE.

All mutations are same-origin, strict JSON, UTF-8 validated, size bounded, and
map invalid/not-found/conflict/corruption to 400/404/409/500.

## Store layout

```text
<employee-root>/
  index.json
  <employee-id>/
    employee.json
    projects.json
    revisions/<revision>.json
    activity/events.jsonl
    tasks/index.json
    tasks/<task-id>.json
    knowledge/{sources,index}.json
    memory/{candidates,facts}.json
    dispatch/<task-id>.json
    artifacts/<task-id>.json

<workspace>/.gohermit/
  sessions/<session-id>/{session.json,messages.jsonl,events.jsonl,...}
  memory/{project.json,project.md}
```

Employee files use strict schemas, stable opaque pagination, bounded counts,
0600 files, safe path containment, and atomic same-directory replacement.
Activity stores only bounded lifecycle/reference metadata; it cannot drive
Task status, Session/Run recovery, SSE, or Tool replay.

## Recovery rules and accepted risks

- Dispatch and Session commit journals are scoped idempotency evidence, not a
  competing execution state machine.
- A persisted Run precedes provider execution. Completed Tool effects are
  suppressed only by canonical argument digest at the exact interrupted Turn.
- External Workspace changes and post-mutation verification are rechecked;
  code-changing Runs cannot complete without successful verification.
- Per-file writes are atomic but there is no cross-file transaction. Partial
  cross-file states fail closed and are reconciled only by the documented
  journals.
- v0.7 remains one process/Workspace with in-process locks. Cross-instance
  Employee Store sharing and locks are future design work.
