# ADR 0003: stdio JSON-RPC plugin protocol

Status: Accepted — 2026-07-13

## Context

Extensions may require Python, TypeScript/Node, Rust, or platform libraries. Go's native plugin mechanism is platform-limited and shares crash/address space.

## Decision

Use child processes with JSON-RPC 2.0, one JSON object per stdout line, stderr logging, a versioned handshake, bounded messages/concurrency, timeouts, cancellation, health checks, and graceful shutdown.

## Consequences

Plugins are language-neutral and crashes are isolated. Serialization and lifecycle overhead are accepted. Plugins remain an explicit OS-level trust boundary.
