# Session storage

## Schema

`session.json` records schema version, ID, goal, status/timestamps, turn count, recent bounded messages, structured summary, bounded tool records, modified-file hashes, steps, tests, last error, workspace, Git-state digest, and configuration digest. `summary.md` is the human recovery view. `events.jsonl` is the batched event audit trail.

## Checkpoint lifecycle

State stays in memory during streaming. Checkpoints occur after completed tool calls, every configured number of turns, at final success/failure/cancellation, and before normal process exit. Stream chunks and full request bodies are never checkpointed. Event records are written in batches rather than one file or fsync per event.

## Atomic writes

JSON and Markdown snapshots are encoded completely, written to a temporary sibling file with mode `0600`, flushed, closed, and renamed over the destination. This gives either the previous or next complete file on filesystems with atomic same-directory rename. Tests verify no successful load accepts partial JSON.

## Versioning and migration

v0.1 uses schema version `1` and rejects all other versions clearly. Future versions must add explicit, tested, one-way migration; silently interpreting unknown fields or versions is prohibited.

## Retention and logs

`hermit clean --older-than` removes only direct session children older than the cutoff. Logs rotate at the configured size and keep one previous file. Redaction runs before log buffering. Runtime artifacts remain inside the configured relative `.gohermit` directory.

## Corruption and external changes

Invalid JSON is reported as a corrupt checkpoint. Resume rejects a workspace mismatch and compares SHA-256 hashes for files changed by the agent; missing or externally changed files stop recovery with the exact path. Git state is recorded as an audit digest. Recovery never guesses through corruption.
