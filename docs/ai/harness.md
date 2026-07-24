# Agent Harness quick reference

Read this file for session, work-loop, context, memory, recovery, or Web conversation changes.

## State model

- A `Session` is a durable conversation. It is `open` until explicitly archived.
- Each user message creates one `Run`: `queued → running → verifying → completed`.
- Terminal alternatives are `failed`, `cancelled`, and recoverable `interrupted`.
- Provider company, access method, model, and Agent profile are fixed on Session creation.
- Plan mode is fixed on Session creation: `auto` executes immediately; `review` keeps the Run queued until explicit approval.
- The workspace permits one active Run globally; Session history remains readable during a Run.

Core entry points are `internal/session`, `internal/agent`, `internal/contextmgr`, and `internal/web`.

## Work loop and completion

Every model call rebuilds context in this order: safety/profile prompt, root `AGENTS.md`, project memory, Session summary, active Run state, current user message, and bounded recent messages/tool results.

A response without tool calls is only a completion candidate. Read-only work may finish immediately. Mutating work must pass `git diff --check`; non-document changes also require a successful test recorded after the latest mutation. A failed gate is returned to the model for at most three verification attempts, still bounded by Run turns and total timeout.

Tool calls persist `started` before execution and `completed` afterward. Recovery never replays completed calls. A call left `started` becomes `uncertain` and forces inspection/replanning.

Persistent events use a prepared `commit.json` journal. The checkpoint and ordered event batch are committed before an SSE subscriber sees the event; recovery replays the journal idempotently after crashes at journal/checkpoint/event boundaries. Hidden Worker activity uses the detached-event commit path instead of an in-memory buffer.

## Storage and memory

Schema v5 lives under `.gohermit/sessions/<session-id>/` and stores each Run's bounded public Live Plan:

- `session.json`: bounded current state, Runs, summary, recent model messages, digests.
- `messages.jsonl`: user-visible user/assistant transcript only.
- `events.jsonl`: persisted events with monotonic `sequence`; stream deltas are never stored.
- `summary.md`: compact recovery view.

Schema v1, v2, and v3 are migrated explicitly on load. Unknown versions, invalid Plans, and corrupt data fail closed. Workspace path mismatches fail; file/Git changes set `workspace_changed` and require reconciliation instead of rejecting recovery. Team Runs add a Mission and stable hidden Worker Sessions; see `docs/ai/team.md` and `docs/ai/plan-mode.md`.

Verified Runs update versioned `.gohermit/memory/project.json` and its compact `project.md` view. Memory contains bounded architecture facts, conventions, verified commands, decisions, issues, and source Run IDs. Secrets, private reasoning, full provider requests, raw tool output, and stream chunks are excluded.

## Web contract

- `POST /api/sessions`, `GET /api/sessions`, `GET /api/sessions/{id}`
- `POST /api/sessions/{id}/runs`
- `GET /api/sessions/{id}/events?after=<sequence>` (SSE replay plus live events)
- `POST /api/sessions/{id}/runs/{run}/cancel`
- `POST /api/sessions/{id}/runs/{run}/resume`
- `POST /api/sessions/{id}/runs/{run}/approve`

`POST /api/run` remains a compatibility endpoint. New UI work must use Session APIs. Run creation returns `202` with `session_id` and `run_id`; the browser reconnects by event sequence and reloads final visible messages from the Session.

## Safety invariants

- Keep Agent Core presentation-free and provider-neutral.
- Do not persist credentials, private reasoning, system/provider request bodies, stream chunks, or unbounded output.
- Do not weaken workspace/tool policy or introduce automatic commit/push/deploy.
- Keep loops, model calls, tools, SSE buffers, stored messages, summaries, and memory bounded and cancellable.
