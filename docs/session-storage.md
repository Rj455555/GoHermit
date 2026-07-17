# Session storage

## Schema

Schema v4 separates a durable `open`/`archived` Session from its Runs and adds an optional Team Mission plus a bounded public Run Plan. `session.json` records fixed provider/model/Agent selection, Run states and Plan revisions, active Run, recent bounded messages, summary, tool lifecycles, file hashes, tests, workspace/Git digests, the next event sequence, and bounded Mission/WorkItem/Handoff state. `messages.jsonl` stores only visible owner/Lead messages. `events.jsonl` stores sequenced audit events. `summary.md` is the human recovery view.

## Checkpoint lifecycle

State stays in memory during streaming. Checkpoints occur after completed tool calls, every configured number of turns, at final success/failure/cancellation, and before normal process exit. Stream chunks and full request bodies are never checkpointed. Event records are written in batches rather than one file or fsync per event.

## Atomic writes

JSON and Markdown snapshots are encoded completely, written to a temporary sibling file with mode `0600`, flushed, closed, and renamed over the destination. This gives either the previous or next complete file on filesystems with atomic same-directory rename. Tests verify no successful load accepts partial JSON.

## Versioning and migration

Schema versions `1`, `2`, and `3` have explicit, tested, one-way migrations to version `4`. Unknown versions remain rejected; Plan structure validates before save and after load.

Each Team WorkItem has a stable `execution_session_id` pointing to a hidden Session in the same store. Hidden Sessions do not appear in owner-facing lists, but retain the normal tool lifecycle and checkpoint semantics. Recovery marks both parent Mission work and child Runner work interrupted; completed child Sessions are converted to Handoffs without replay.

## Retention and logs

`hermit clean --older-than` removes only direct session children older than the cutoff. Logs rotate at the configured size and keep one previous file. Redaction runs before log buffering. Runtime artifacts remain inside the configured relative `.gohermit` directory.

## Corruption and external changes

Invalid JSON is reported as a corrupt checkpoint and workspace mismatch is rejected. Changed file hashes or Git state set `workspace_changed`; the next Run receives reconciliation context and must re-verify. A tool persisted as `completed` is never replayed. A `started` tool without completion becomes `uncertain`, requiring state inspection and replanning.

Persistent state/event updates first atomically write a bounded `commit.json` journal, then replace `session.json`/`summary.md`, append sequenced events idempotently, and remove the journal. Load completes a valid prepared journal after a crash and rejects corrupt, oversized, version-mismatched, or cross-Session journals. SSE publication occurs only after this commit returns. Hidden Team Workers use the same durable detached-event path for parent-visible activity.
