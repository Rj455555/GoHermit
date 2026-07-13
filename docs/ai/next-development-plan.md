# Next development plan

The next target is **v0.1.1 hardening**, not feature expansion. Work remains single-agent and local-first.

## Milestone 1: CI and release reproducibility

Deliverables:

- GitHub Actions for Linux, macOS, and Windows builds/tests.
- Race tests on a supported runner and `go vet` on every change.
- Tagged release workflow producing checksummed binaries without publishing from GoHermit itself.
- Dependency/license inventory generated during release.

Acceptance:

- A clean clone passes the documented commands.
- Release artifacts report version, OS, architecture, and SHA-256.
- CI contains no API keys and makes no live model calls.

## Milestone 2: explicit interactive permissions

Deliverables:

- A small permission-broker interface owned by the agent/app boundary.
- Human CLI prompt for `confirmation_required` actions only when stdin is interactive.
- JSON mode emits a request and exits or waits according to an explicit flag; it never silently approves.
- Session audit records the requested action, decision, scope, and timestamp without secrets.

Acceptance:

- Allow-once, deny, cancellation, timeout, non-interactive, and replay tests pass.
- `blocked` operations remain impossible to approve.
- Workspace/write/network scopes cannot widen through malformed input.

## Milestone 3: recovery and storage hardening

Deliverables:

- Explicit schema v1-to-v2 migration framework with fixtures.
- Separate read-only status loading from strict resume validation.
- Bounded periodic event flush using `storage.flush_interval`, without per-token writes.
- Stronger rename/delete tracking and interrupted atomic-write tests.
- Measured write-count benchmark for representative 50-turn sessions.

Acceptance:

- Old fixtures migrate deterministically and unknown future versions still fail clearly.
- A simulated crash loses at most the documented in-memory window.
- Write amplification stays within the recorded budget.

## Milestone 4: provider compatibility suite

Deliverables:

- Reusable conformance fixtures for JSON, SSE, fragmented tool calls, rate limits, malformed events, cancellation, and timeout.
- Capability validation before starting a task.
- Documented provider compatibility matrix based on executed tests.
- Evaluate a separate Responses API provider only behind the existing neutral interface; do not rewrite Agent Core.

Acceptance:

- Provider errors have stable categories and never expose credentials/request bodies.
- Streaming and non-streaming paths produce equivalent neutral messages/tool calls.
- No provider-specific type leaks into `internal/agent`.

## Milestone 5: plugin hardening

Deliverables:

- Protocol conformance test kit reusable by plugin authors.
- Cancellation acknowledgement and forced-shutdown timing tests.
- Optional OS sandbox launch profiles, explicitly configured and platform-specific.
- Design ADR for a future streaming-event protocol version; do not change v1 framing in place.

Acceptance:

- Invalid JSON, oversized output, hangs, crashes, forked child processes, and concurrent calls remain bounded.
- Python and Node examples pass the same conformance suite.
- v1 clients reject incompatible protocol versions.

## Milestone 6: dogfooding and evaluation

Deliverables:

- Small local fixture repositories for inspect-only, patch-and-test, failing-test repair, permission denial, timeout, and resume.
- Deterministic fake-provider scenarios for CI plus an opt-in live-provider smoke script.
- A concise v0.1.1 completion report with failure rates and known limitations.

Acceptance:

- CI fixtures run offline and deterministically.
- Live smoke tests are never automatic and never persist prompts or keys.
- Every failure leaves a valid status and resumable or clearly non-resumable session.

## Explicitly deferred

Do not add multi-agent orchestration, vector memory, browser automation, MCP, TUI/web UI, accounts, cloud sync, telemetry, a scheduler/daemon, automatic Git publishing, deployment, or a general workflow engine during v0.1.1.

## Recommended order

Execute Milestones 1 → 2 → 3 → 4 → 5 → 6. Each milestone ends with focused tests, full tests, race tests, vet, cross-build checks where relevant, documentation updates, and a short handoff using `docs/ai/handoff-template.md`. Update `docs/ai/context.md` only when its compact facts change.
