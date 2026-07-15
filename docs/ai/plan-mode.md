# Live Plan: AI quick reference

Read this file for checkbox progress, Plan state, Plan events, schema-v4 migration, or checklist UI changes. Read `AGENTS.md`, `docs/ai/context.md`, and `docs/ai/harness.md` first.

## Contract

Every new Run has one `taskplan.Plan` with at most 16 ordered steps. Plan status is `active`, `completed`, `failed`, or `cancelled`. Step status is `pending`, `in_progress`, `completed`, `failed`, or `cancelled`. At most one step is current. Every real change increments `revision`; idempotent recovery calls do not.

The Plan is public execution state, not model reasoning. Titles and details are bounded. Never store prompts, tool arguments, raw output, secrets, private reasoning, confidence, or invented percentages in it.

## Lifecycle mappings

- Single Agent: `analyze -> execute -> verify -> report`.
- Personal Agent Team: `explore -> build -> review -> repair -> verify -> lead`.
- Team Verifier completion is successful only when its durable Handoff contains at least one check and every check passed.
- Explicit cancellation marks the current/next step cancelled and makes the Plan terminal.
- Team timeout/process interruption keeps the current step in progress with a resumable detail; recovery continues the same Plan.
- Runtime failure marks the current or next pending step failed.

## Persistence and events

Session schema v4 stores `run.plan`; v1, v2, and v3 migrate explicitly. `taskplan.Validate` runs before checkpoint save and after load. Unknown Plan versions or inconsistent states fail closed.

`plan_created` and `plan_updated` are persisted sequenced events. `data.plan` is the bounded snapshot and `plan_step_id` identifies the changed step. The Web UI updates from SSE and reloads the authoritative Run Plan after terminal events or refresh.

## Code map

| Responsibility | Entry point |
|---|---|
| Plan model, transitions, validation, defaults | `internal/taskplan/plan.go` |
| schema-v4 persistence/migration | `internal/session/session.go` |
| single-Agent phase updates | `internal/agent/agent.go` |
| Team WorkItem-to-Plan updates | `internal/web/server.go` |
| Plan event contract | `internal/event/event.go` |
| checkbox rendering and SSE snapshot application | `internal/web/assets/app.js`, `index.html`, `styles.css` |

## Required invariants

- Do not mark a step complete before the corresponding execution fact occurs.
- Do not allow two current steps or rewrite completed history during recovery.
- Do not relay hidden Worker Plan events into the parent Team Plan.
- Do not derive progress from animation time, model prose, or token count.
- Dynamic substeps must have an auditable execution mapping before implementation.
