# ADR 0004: File-based session storage

Status: Accepted — 2026-07-13

## Context

Sessions must be local-first, recoverable, portable, and manually auditable without operating a database.

## Decision

Store versioned `session.json`, `summary.md`, and append-only `events.jsonl` under `.gohermit/sessions/<id>/`. Reject opaque language-specific binary snapshots.

## Consequences

Users can inspect and archive sessions with ordinary tools. Query performance and multi-process transactions are limited; v0.1 is a single foreground process and does not need a database.
