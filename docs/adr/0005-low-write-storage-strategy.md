# ADR 0005: Low-write storage strategy

Status: Accepted — 2026-07-13

## Context

Token streams and verbose tool output can create excessive small writes and unbounded session growth.

## Decision

Keep stream chunks in memory, buffer events, checkpoint after complete model/tool units and every configured number of turns, atomically replace snapshots, bound recent records/output/logs, rotate logs, and clean sessions by retention. Unsafe full-prompt/stream/tool-output options are rejected in v0.1.

## Consequences

Crash recovery may lose the current incomplete model stream but retains the previous complete checkpoint. Disk activity and secret exposure are substantially reduced.
