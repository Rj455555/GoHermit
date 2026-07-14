# Session storage

## Schema

Schema v2 separates a durable `open`/`archived` Session from its Runs. `session.json` records fixed provider/model/Agent selection, Run states, active Run, recent bounded messages, summary, tool lifecycles, file hashes, tests, workspace/Git digests, and the next event sequence. `messages.jsonl` stores only visible user/assistant messages. `events.jsonl` stores sequenced audit events. `summary.md` is the human recovery view.

## Checkpoint lifecycle

State stays in memory during streaming. Checkpoints occur after completed tool calls, every configured number of turns, at final success/failure/cancellation, and before normal process exit. Stream chunks and full request bodies are never checkpointed. Event records are written in batches rather than one file or fsync per event.

## Atomic writes

JSON and Markdown snapshots are encoded completely, written to a temporary sibling file with mode `0600`, flushed, closed, and renamed over the destination. This gives either the previous or next complete file on filesystems with atomic same-directory rename. Tests verify no successful load accepts partial JSON.

## Versioning and migration

Schema version `1` has an explicit, tested, one-way migration to version `2`. Unknown versions remain rejected; no version or unknown field is interpreted silently.

## Retention and logs

`hermit clean --older-than` removes only direct session children older than the cutoff. Logs rotate at the configured size and keep one previous file. Redaction runs before log buffering. Runtime artifacts remain inside the configured relative `.gohermit` directory.

## Corruption and external changes

Invalid JSON is reported as a corrupt checkpoint and workspace mismatch is rejected. Changed file hashes or Git state set `workspace_changed`; the next Run receives reconciliation context and must re-verify. A tool persisted as `completed` is never replayed. A `started` tool without completion becomes `uncertain`, requiring state inspection and replanning.
