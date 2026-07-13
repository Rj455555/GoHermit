# ADR 0001: Use Go for the core runtime

Status: Accepted — 2026-07-13

## Context

The runtime needs portable binaries, explicit cancellation, safe process/HTTP/file primitives, low idle overhead, and straightforward race testing.

## Decision

Implement the CLI, agent, model boundary, built-in tools, sessions, storage, and plugin supervisor in Go. Cross-language ecological integrations run as child plugins rather than entering the core process.

## Consequences

The project gains a small deployable binary and strong concurrency tooling. Dynamic extension inside the process is intentionally limited; protocol boundaries require explicit serialization.
