# ADR 0013: First-class Electronic Employees

## Status

Accepted for GoHermit v0.7 Phase 1.

## Context

GoHermit already has static Agent profiles, temporary Team Roles, durable
Sessions, bounded Runs, owner-scoped preferences, and revisioned Loop
Definitions. None of those is a durable worker identity:

- an Agent profile is a behavior and tool-policy preset;
- a Role is one temporary responsibility inside a Mission;
- a Session is one durable conversation;
- a Run is one bounded requested outcome;
- a model selection contains provider/access/model names, not a person;
- a Skill is declarative instruction context, not identity or permission; and
- a Project is a workspace, not an Employee.

v0.7 needs owner-created workers with stable identity, charter, boundaries,
policy, revisions, project grants, and historical snapshots. Adding that
concept must not create another execution engine, event store, approval model,
or recovery state machine.

## Decision

### 1. Employee is an owner-scoped domain entity

`employee.Employee` is the durable identity and policy aggregate. It stores
only non-secret names and policy data: identity, initials-or-Emoji Avatar, job
title, charter, responsibilities, behavior boundaries, provider/model names,
Agent profile ID, pinned Skill references, ProjectBinding IDs, bounded
permission/budget/concurrency/memory policy, lifecycle state, timestamps, and
revision.

It never stores provider credentials, OAuth tokens, private keys, private
reasoning, raw prompts, stream chunks, raw tool arguments, or unbounded output.
The Phase 1 domain rejects bounded fields containing common credential or
private-key markers. Persistence remains a Phase 2 responsibility.

The distinctions are binding:

```text
Employee != Role
Employee != Session
Employee != Run
Employee != Model
Employee != AgentProfile
Employee != Skill
Employee != Project
```

An Employee may later perform different Roles in different Tasks. A Team Role
continues to own its existing runtime responsibility and does not become a
durable person.

### 2. Lifecycle changes are explicit revision transitions

The v1 state graph is:

```text
active -> disabled -> active
active -> archived
disabled -> archived
archived -> terminal
```

Creation produces revision 1 in `active`. Editing creates a new revision while
preserving Employee ID, creation time, and lifecycle metadata. Disable and
enable are dedicated reversible transitions. Archive is terminal; no update,
enable, disable, or second archive can leave it.

Each accepted edit or state transition returns a new value, increments revision,
and advances `updated_at`. Domain functions do not mutate their input. Disable
or archive does not imply cancellation of a running Run and never deletes
history; later Control Plane phases must report and manage active Tasks through
the existing Session/Run lifecycle.

### 3. Revision Snapshot is complete, immutable historical truth

`employee.RevisionSnapshot` deep-copies one validated Employee revision and the
exact ProjectBindings referenced by that revision. It carries Employee ID,
revision, capture time, schema version, and a SHA-256 digest over the canonical
snapshot body. Digest verification detects any later mutation. Snapshot and
Employee documents each have a hard 256 KiB domain limit.

The full Revision Snapshot belongs only in the future EmployeeTask/Employee
Store. It is not a Session checkpoint payload. Session schema v6 and hidden
Team Workers will later carry a separate compact recovery snapshot, capped at
64 KiB, containing only the identities, revision/digests, Task ID, effective
policy digest, and necessary bounded context. A full 256 KiB Employee revision
must never be repeated at every Session checkpoint.

### 4. ProjectBinding is an explicit narrowing workspace grant

`employee.ProjectBinding` binds one Employee ID to one clean absolute canonical
workspace path, a path fingerprint, read/mutation/network flags, allowed
capability names, and an optional bounded budget override. Mutation requires
read permission. The binding stores no source content or credentials.

The data shape can retain future project records, but v0.7 remains one service
for one startup-configured Workspace:

- `/api/projects` will return only that Service Workspace;
- the service will not scan Home or dynamically open another Workspace;
- the service will not create a multi-Workspace Session Store manager; and
- Task readiness must resolve the binding and require exact equality with the
  current Service Workspace before Session preparation.

Cross-project use means one GoHermit instance per Workspace. A future design
may share the owner-scoped Employee Store across instances, but revision
coordination and cross-instance locks require a separate ADR.

### 5. Employee policy is a ceiling, never a grant

Phase 1 defines bounded Employee and Project policy values only. Runtime
capability calculation remains Phase 3 and must use:

```text
base =
    Global Policy
  ∩ AgentProfile ToolPolicy
  ∩ Employee Policy
  ∩ Project Policy
  ∩ Task Policy

effective = base
```

With enabled Skills:

```text
effective =
    base
  ∩ union(enabled Skill requested capabilities)
```

Skills can only narrow policy. They never install code, hold credentials,
enable network, grant a workspace mutation, bypass existing Tool Policy, or
bypass ADR 0011 call approval.

Concurrency is conservative in v0.7: one running Task per Employee, no
cross-Employee read-only execution, and one mutation writer for the Service
Workspace. Memory policy permits only disabled promotion or Candidate creation
with explicit Owner confirmation; automatic promotion is invalid.

### 6. Existing Session/Run remains the only execution truth

EmployeeTask will later bind an Employee revision to exactly one existing
Session/Run execution kernel. Task execution state will be projected from the
existing Session/Run, Plan, Approval, and Verification state. Employee Activity
will contain only bounded lifecycle/reference metadata and will never drive
Run recovery, duplicate Session SSE, copy tool events, or become another Event
Store.

Phase 1 therefore contains no Store, Control Plane, Web API, Session schema,
Skill Catalog, Knowledge, Memory store, Task runtime, UI, TeamTemplate mapping,
model call, or execution transition.

## Consequences

- The reviewed Employee vocabulary and terminal/reversible lifecycle are
  testable without persistence or presentation dependencies.
- Historical Tasks can pin complete, independently verifiable Employee
  revisions while Session checkpoints remain compact.
- ProjectBinding expresses future-compatible data without claiming
  multi-Workspace v0.7 execution.
- Existing Agent, Role, Session, Run, Approval, Verification, Event, and
  recovery behavior is unchanged.
- Phase 2 may add owner-scoped storage and CRUD around these values, but it may
  not reinterpret their state graph or snapshot ownership without amending
  this ADR.
