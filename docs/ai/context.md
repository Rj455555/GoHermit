# AI context: GoHermit 0.8

Read root `AGENTS.md` first. This file and `employees.md` are the compact
architecture map; `handoff-v0.7.md` records the verified delivery boundary.

## Product in one paragraph

GoHermit `0.8.0-dev` is a local-first, single-owner Agent workbench. The
existing Session/Run kernel still owns execution, Live Plan,
Approval, Verification, Tool lifecycle, Event/SSE, and recovery. v0.7 adds
owner-scoped Electronic Employees, pinned Skill/Knowledge/Memory/Project
context, a persistent Task Inbox with explicit Prepare and Start, bounded
Artifacts and Memory Candidates, Employees/Tasks Web UI, and optional
Team Role-to-Employee assignments. v0.8 adds Employee-owned recurring Loops
with durable Contract, State, and Invocation logs plus a single-process daily
scheduler; every execution still uses EmployeeTask, Session, and Run truth.
One service executes only its startup-configured Workspace; there is no
multi-instance scheduler, multi-user control plane, or automatic Git/publish
action.

## Read by topic

| Work | Start here | Then inspect |
|---|---|---|
| Employee/Task lifecycle | `docs/ai/employees.md` | `internal/employee`, `internal/employeestore` |
| Prepare/Start/recovery | `internal/controlplane/employee_tasks.go` | `employee_execution.go`, `internal/session`, `internal/agent` |
| Skill/Knowledge/Memory context | `docs/ai/employees.md` | `internal/skill`, `internal/knowledge`, `internal/employeememory`, `internal/contextmgr` |
| Team assignments | `docs/ai/employees.md` | `internal/controlplane/team_employees.go`, `internal/app/team_worker.go`, `internal/teamtemplate` |
| React Workbench, routes, i18n, API/SSE | `docs/ai/react-frontend.md` | `web/src`, `internal/web`, `tests/e2e-react` |
| Session/Run/SSE | `docs/ai/harness.md` | `internal/session`, `internal/controlplane`, `internal/web` |
| Team/Mission/Handoff | `docs/ai/team.md` | ADR 0008, ADR 0013 |
| Live Plan and Verification | `docs/ai/plan-mode.md` | ADR 0009–0011, `internal/runcontrol` |
| Employee recurring Loops | `docs/ai/employee-loops.md` | `internal/loop`, `internal/loopstore`, `internal/controlplane/employee_loops.go` |
| Legacy Loop Workbench | `docs/ai/handoff-v0.6-loop-workbench.md` | `internal/loop`, `internal/loopstore` |
| Docker/persistence | `compose.yaml` | `Dockerfile`, `internal/evals/docker_acceptance.sh` |
| Future work | `docs/ai/next-development-plan.md` | `docs/roadmap.md` |

## Execution and data model

```text
Employee revision --sealed into--> EmployeeTask
EmployeeTask --Prepare--> stable Session (no Run)
EmployeeTask --explicit Start--> one existing Run
EmployeeTask status <--projection-- Session/Run/Plan/Approval/Verification

Team Role --optional preflight--> TeamEmployeeAssignment
TeamEmployeeAssignment --sealed into--> hidden Worker Session
hidden Worker Session --internal only--> existing Worker/Run/recovery kernel
```

Employee is a durable identity, while Role is a temporary Mission
responsibility. Task is the immutable owner request plus mutable Session/Run
bindings. Session and Run remain the only execution truth.

```text
Employee --owns--> Loop contract
Loop invocation --creates--> EmployeeTask --Prepare/Start--> Session/Run
Loop state/logs <--project-- Invocation/Session/Run
```

## Security invariants

- Full Employee revision snapshots live only in EmployeeTask/Employee Store.
  Session and hidden Worker snapshots are compact and capped at 64 KiB.
- Task `SnapshotDigest` excludes State/timestamps and Session/Run bindings; it
  never changes when execution identities are attached.
- Permission evaluation is:

  ```text
  base = Global ∩ AgentProfile ToolPolicy ∩ Employee ∩ Project ∩ Task
  no Skill: effective = base
  enabled Skills: effective = base ∩ union(requested capabilities)
  ```

  A Skill can only narrow. A SKILL.md-only Adapter requests zero capabilities
  and cannot execute scripts or install dependencies.
- Knowledge is deterministic, local-only, cited, content-digested, and
  configured-root-only. Employee Memory is private and owner-confirmed;
  Project Memory is a separate workspace layer.
- Preparation rereads mutable Skill, Knowledge, Memory, model/access, Employee,
  and Project state before writing a dispatch journal or Session.
- Start is the only EmployeeTask execution entrypoint. It persists one stable
  Run before the Runner can call a Provider or Tool.
- Recovery uses canonical tool-argument digests only at the interrupted Turn
  frontier. Completed effects are not replayed; started/uncertain effects
  require reality inspection and replanning.
- Hidden Worker Sessions are omitted from lists and return the same 404 as an
  absent Session for every public detail/message/event/SSE/Run/Plan/Approval
  operation. TeamWorker internal Store/Runner recovery remains available.
- Private Employee Memory never enters another Employee context, a parent
  Mission, public Handoff, log, or public API projection.
- No durable store accepts credentials, private reasoning, raw tool arguments,
  full hidden prompts, or unbounded output.

## Persistence map

- Employee root: `GOHERMIT_EMPLOYEE_STORE`, with Employee records, immutable
  revisions, bindings, bounded activity references, Tasks, Knowledge indexes,
  Memory Candidates/Facts, Artifacts, and dispatch journals isolated below
  `<employee-id>/`.
- Session root: configured storage directory, normally
  `<workspace>/.gohermit/sessions`; schema v6 migrates v1–v5 one way and fails
  closed on unknown/corrupt data.
- Loop root: `GOHERMIT_LOOP_STORE`, including generated
  `contracts/{loop-id}/LOOP.md`, bounded `states/{loop-id}.json`, and existing
  Invocation logs.
- Project Memory: `<workspace>/.gohermit/memory`.
- TeamTemplate root: owner-scoped configuration store; schema v2 strictly
  migrates v1 and rejects a v1 document carrying `employee_id`.

Store reads and writes enforce lexical and realpath containment, reject
symlinks/non-regular files, use strict bounded JSON, and atomically replace one
file at a time. Cross-file transactions are intentionally not claimed.

## Current verified baseline

- Phases 1–9 are Owner-approved and merged through Phase 9 squash commit
  `36987d92291c2781bfa3b997bdaab8002bd9c019`.
- Phase 10 adds deterministic cross-module evals, actual Docker
  build/health/rebuild persistence acceptance, and documentation/version
  closeout only.
- The Web surface is one React + TypeScript + Vite application embedded from
  `internal/web/assets/dist`. Legacy HTML/JavaScript/CSS assets and their
  standalone test server have been removed. Browser execution state remains a
  projection of the existing Go API and Session-owned SSE journal.
- Default tests use deterministic Fake Providers. Paid Codex smoke remains
  workflow-dispatch-only and skips without an explicit secret.
- `internal/evals` contains the v0.7 cross-package regression manifest and the
  valid persistent Store fixture used by Docker CI.

## Accepted boundaries

- Single owner, one service, one configured Workspace, one mutation writer,
  and at most one running Task per Employee.
- In-process locks plus per-file atomicity; no cross-process transaction or
  recovery journal beyond the existing scoped dispatch/Session journals.
- The Web surface is unauthenticated and must remain loopback-only.
- Hidden Worker output remains durable for internal recovery but is not
  publicly addressable.
- Shell/plugins are policy constrained, not an OS sandbox.
- Global Tasks intentionally reads at most the newest 100 Tasks per Employee.
- Exact private-Memory echo detection is conservative and may reject a public
  Handoff containing the same text.

## First-class Weixin channel

The Weixin channel is an owner-scoped transport documented in
docs/ai/weixin-channel.md. It uses one cancellable bounded poller per account,
persists a per-account getUpdates cursor after inbox idempotency, and routes
only explicitly bound inbound text to a queued Employee Task. It never starts
execution: Owner Start remains the only Task execution entrypoint. Credentials
and context tokens are separate from public account metadata, and channel
delivery is not Session SSE or Run state.

## Standard verification

```bash
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm test
pnpm test:coverage
pnpm build
git diff --exit-code -- internal/web/assets/dist
pnpm test:e2e
test -z "$(gofmt -l $(git ls-files '*.go'))"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
docker compose config
bash internal/evals/docker_acceptance.sh
```
