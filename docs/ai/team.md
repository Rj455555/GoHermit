# Personal Agent Team: AI quick reference

Read this file for Owner Profile, Mission, WorkItem, Handoff, role policy, team recovery, or team Web UI changes. Read `AGENTS.md`, `docs/ai/context.md`, and `docs/ai/harness.md` first.

## Product boundary

GoHermit v0.5 remains a local, single-owner service. It is not a general multi-user or free-form multi-agent platform. One owner-facing Session contains Runs. A Run selected with Agent `team` owns one adaptive Mission and one public Live Plan. A Mission contains bounded dependency-ordered WorkItems and structured Handoffs.

Only Lead produces the visible final answer. Worker private reasoning, full prompts, stream chunks, and arbitrary agent chat are never stored or shown. Read-only WorkItems may run concurrently; one workspace has one writer lease.

## Adaptive workflows

Read-only intent: parallel `Explorer + Reviewer → Verifier → Lead`.

Mutation intent: parallel `Explorer + preflight Reviewer → Builder → Reviewer → repair Builder → Verifier → Lead`. The Reviewer reports severity-tagged findings; the repair stage runs only when at least one finding is blocking and is otherwise skipped so verification runs directly.

- Explorer: read-only project and constraint inspection.
- Builder: workspace-scoped implementation and focused checks.
- Reviewer: independent read-only diff review.
- Repair Builder: addresses blocking review findings; skipped when the review is clean or advisory-only, and still requeued if a later verification fails.
- Verifier: read-only inspection plus `test.run`; it has no file write, patch, or general shell tools.
- Lead: synthesizes only the bounded Handoffs for the owner.
- Operator: reserved and disabled by default for future approval-gated operations.

The Lead WorkItem cannot start without at least one explicit successful Verifier check. WorkItems, model calls, estimated/provider tokens, Mission duration, Handoffs, evidence, files, and checks are bounded. A failed mutation Verifier requeues its repair dependency and itself, preserving prior Handoffs, for at most three verification attempts and within the existing Mission budget.

## Persistence and recovery

Session schema v4 stores the parent `mission` plus each Run's optional Live Plan. Each WorkItem gets a deterministic hidden `execution_session_id`. Hidden Worker Sessions use the normal Runner checkpoint and completed-tool replay guard but do not appear in the owner's Session list.

Coordinator checkpoints immediately after assigning an execution Session and starting a WorkItem. Parent events and relayed child activity are committed before SSE delivery. On restart, parent and child running state becomes `interrupted`. Resume reloads the same child Session. A completed child result is converted to the missing Handoff without another model call; an interrupted child resumes through the existing Runner.

## Owner profile

`internal/owner` stores schema-v1 `owner.json` outside repositories. Default resolution uses `GOHERMIT_OWNER_STORE`, then the user config directory. Compose sets `/data/owner.json` in the private data volume.

The profile contains identity, communication/coding/Git/verification/risk preferences, environment aliases, and explicit facts. Only confirmed facts enter `# Owner profile` context. The Web API supports full profile replacement plus fact upsert/delete. Size limits and secret-pattern rejection apply before atomic mode-0600 writes.

Context order is system/role policy → Owner Profile → project `AGENTS.md` → project memory → recovered Session/Run state → current goal and recent bounded context.

## Code map

| Responsibility | Entry point |
|---|---|
| team domain, graph, budgets, writer lease | `internal/team/team.go` |
| scheduler and default workflow | `internal/team/coordinator.go` |
| existing Runner-to-Worker adapter | `internal/app/team_worker.go` |
| Owner Profile storage and compact context | `internal/owner/profile.go` |
| parent Mission persistence and migration | `internal/session/session.go` |
| team launch, events, owner APIs | `internal/web/server.go` |
| role prompts and tool policies | `internal/contextmgr/context.go`, `internal/config/config.go`, `internal/tool/builtin/builtin.go` |
| team/owner/offline Web UI | `internal/web/assets/` |

## Events and API

Existing Session/Run endpoints remain unchanged. Team selection uses `agent: "team"`. Session detail includes `session.mission`.

Team events add `mission_id`, `work_item_id`, and `agent_id`: `mission_started`, `mission_completed`, `mission_failed`, `work_item_started`, `work_item_completed`, and `work_item_failed`. Those real WorkItem transitions update the Run's public Plan; hidden Worker Plan events, model deltas, and terminal events are not relayed into the owner conversation.

Owner endpoints:

- `GET /api/owner`
- `PUT /api/owner`
- `PUT /api/owner/facts/{id}`
- `DELETE /api/owner/facts/{id}`

## Required invariants

- Do not bypass the single writer lease, restricted plugin-tool filter, or the Verifier gate.
- Do not replace Handoffs with persistent free-form agent chat.
- Do not expose internal role profiles in the public Agent picker.
- Do not replay a completed execution Session.
- Do not put Owner Profile data inside a repository or include secrets.
- Commits, pushes, deploys, network operations, and future Operator work remain approval-gated.

Team start/resume currently belongs to the Web Session/Run API. CLI `run`/`resume` and legacy `/api/run` fail explicitly for `team` so they cannot silently execute the wrong single-Agent behavior.
