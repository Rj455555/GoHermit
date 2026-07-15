# Testing

## Layers

- Unit tests cover configuration, context budgets, policy, redaction, registry/executor, and provider conversion.
- Component tests use temporary workspaces, symlinks, Git commands, HTTP servers, session files, and real Python plugin processes.
- Agent tests inject deterministic providers/tools to cover turns, timeouts, completion, and error feedback.
- Harness tests cover schema v1/v2-to-v3 migration, Session/Run transitions, message/event history, external-change reconciliation, per-call context, project-memory redaction, mutation verification gates, and Session API/SSE replay.
- Team tests cover dependency order, structured Handoffs, parallel readers, the single writer lease, terminal batch failure, model budgets, Verifier gating, stable hidden Worker replay protection, parent Session completion, Owner Profile secret rejection, Owner Web APIs, and restricted plugin-tool filtering.
- Live Plan tests cover transition legality, one current step, completion/failure/cancellation, schema-v3 migration, invalid checkpoint rejection, single-Agent lifecycle events, Team WorkItem mapping, Verifier failure, and SSE sequence ordering.
- CLI build and smoke commands verify packaging and exit behavior.

## Commands

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/hermit
```

The race detector is required because provider callbacks, plugin read/wait loops, pending requests, log buffers, and cancellation cross goroutines. Every test that starts HTTP or child-process work must have a deadline and cleanup.

Paid provider calls remain opt-in live smoke tests and are never part of the default suite.

## Cross-platform coverage

Path tests include traversal, absolute paths, Windows drive syntax, and symlink escape. Symlink creation may be skipped on Windows without developer privileges, but Windows path classification must still run everywhere. CI should eventually cover macOS, Linux, and Windows.

## Plugin tests

Tests cover initialize, health, discovery, execution, graceful shutdown, cancellation/timeout, crash, invalid JSON, and bounded messages. Python and Node examples have no package-install step.

## Low-write verification

Tests and review confirm that streaming deltas are not persisted, checkpoints atomically replace a small fixed file set, events append in batches, logs rotate, and tool/message histories are bounded. Filesystem-level write-count benchmarks are a post-v0.1 hardening item.
