# ADR 0007: Separate durable Sessions from bounded Runs

## Status

Accepted.

## Context

The original task model marked a Session complete as soon as one model response contained no tool calls. The Web surface therefore created a new Session for every prompt, could not preserve a conversation, and could not distinguish a finished model turn from verified task completion.

## Decision

A Session is a durable conversation with fixed provider/model/Agent selection. Every user message creates one bounded Run. Run state owns execution, verification, interruption, cancellation, and failure. Session state remains open after a Run completes. Visible messages and sequenced events are append-only; bounded recovery state remains atomically checkpointed.

No-tool responses enter a deterministic completion gate. Recovery never replays completed tool calls, and external workspace changes require reconciliation instead of invalidating the whole Session.

## Consequences

- Web and future presentation layers can continue and reopen conversations.
- Completion and recovery semantics are auditable per Run.
- Schema v2 requires an explicit migration from v1.
- Switching model or Agent requires a new Session in this milestone.
- Multi-workspace, multi-agent orchestration, and interactive approval remain separate decisions.
