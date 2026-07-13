# ADR 0002: Single-agent runtime

Status: Accepted — 2026-07-13

## Context

The primary value is a reliable inspect/edit/test/recover loop. Multi-agent planning would multiply state, permissions, failure modes, and storage before the base loop is proven.

## Decision

v0.1 runs one bounded agent loop with one task state, provider conversation, tool registry, and session. Completion requires a model response with no tool calls; limits always override model behavior.

## Consequences

Execution and recovery remain understandable and testable. Parallel subagents and delegation are unavailable and require a later ADR.
