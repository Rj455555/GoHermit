# Testing

## Layers

- Unit tests cover configuration, context budgets, policy, redaction, registry/executor, and provider conversion.
- Component tests use temporary workspaces, symlinks, Git commands, HTTP servers, session files, and real Python plugin processes.
- Agent tests inject deterministic providers/tools to cover turns, timeouts, completion, and error feedback.
- Harness tests cover schema v1/v2-to-v3 migration, Session/Run transitions, message/event history, external-change reconciliation, per-call context, project-memory redaction, mutation verification gates, and Session API/SSE replay.
- Team tests cover dependency order, adaptive topology, structured Handoffs, parallel readers, the single writer lease, bounded repair/reverify, terminal batch failure, model budgets, Verifier gating, durable hidden Worker relay, completed Worker replay protection, parent Session completion, Owner Profile secret rejection, Owner Web APIs, and restricted plugin-tool filtering.
- Live Plan tests cover transition legality, single and parallel current steps, reopen-after-verification, task-specific titles, review approval, completion/failure/cancellation, schema-v3 migration, invalid checkpoint rejection, single-Agent lifecycle events, Team WorkItem mapping, Verifier failure, and SSE sequence ordering.
- Playwright covers review-first Session creation, task-specific checkbox rendering, refresh recovery, approval, and post-approval controls with deterministic mocked APIs.
- CLI build and smoke commands verify packaging and exit behavior.

## Commands

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/hermit
pnpm install
pnpm exec playwright install chromium
pnpm test:e2e
```

The race detector is required because provider callbacks, plugin read/wait loops, pending requests, log buffers, and cancellation cross goroutines. Every test that starts HTTP or child-process work must have a deadline and cleanup.

Paid provider calls remain opt-in live smoke tests and are never part of the default suite.

## Cross-platform coverage

Path tests include traversal, absolute paths, Windows drive syntax, and symlink escape. Symlink creation may be skipped on Windows without developer privileges, but Windows path classification must still run everywhere. `.github/workflows/ci.yml` runs Linux normal/race/vet tests, CLI/Web cross-builds, Chromium E2E, and Docker build.

## Plugin tests

Tests cover initialize, health, discovery, execution, graceful shutdown, cancellation/timeout, crash, invalid JSON, and bounded messages. Python and Node examples have no package-install step.

## Low-write verification

Tests and review confirm that streaming deltas are not persisted, checkpoints atomically replace a small fixed file set, events append in batches, logs rotate, and tool/message histories are bounded. Filesystem-level write-count benchmarks are a post-v0.1 hardening item.
