# ADR 0009: Durable owner-facing Live Plan

## Status

Accepted for GoHermit v0.4.0.

## Context

Mission WorkItems and low-level events expose internal execution, but they do not give the owner a compact Cursor-style checklist. Text plans in model messages are not reliable state: they cannot be validated, resumed, replayed by sequence, or distinguished from private reasoning.

## Decision

Every new Run owns one bounded, versioned public Plan. A Plan contains at most 16 ordered steps, one `in_progress` step, a monotonic revision, bounded public detail, and explicit active/completed/failed/cancelled state. It is stored in schema-v4 `session.json` and copied into sequenced `plan_created` and `plan_updated` events.

Single-Agent Runs use deterministic analysis, execution, verification, and report phases. Team Runs map Plan step IDs directly to the six durable Mission WorkItems. WorkItem start/completion/failure, verification evidence, cancellation, and interruption update the Plan; a UI animation or model claim never does. Refresh and SSE reconnect use the stored Plan plus event sequence, and resume continues the existing current step.

Plan content is owner-facing execution state, not chain-of-thought. It excludes private reasoning, hidden prompts, tool arguments, raw output, credentials, speculative confidence, and fabricated percentage completion. The initial version is runtime-owned and not user-editable. Dynamic task-specific substeps are deferred until they can be tied to auditable execution and stable recovery semantics.

## Consequences

- Session schema advances from v3 to v4 with an explicit one-way migration.
- Agent Core, Team Web orchestration, events, Session APIs, and the workbench share `internal/taskplan`.
- Existing sessions load without a Plan; their next new Run receives one.
- The role activity panel remains available for detail, while the Live Plan is the primary owner-facing progress surface.
