# ADR 0008: Single-owner multi-agent team orchestration

## Status

Accepted for GoHermit v0.3.0.

## Context

GoHermit v0.2 runs one model/tool loop per user-message Run. Changing an Agent profile selects one behavior and tool boundary; it does not create collaboration, independent review, or bounded delegation. The product is intended for one owner who needs durable preferences and a private development team rather than a general multi-user agent platform.

## Decision

Keep `agent.Runner` as the bounded Worker execution engine and add a presentation-neutral orchestration layer above it. A Session remains the owner's conversation and a Run remains one requested outcome. A team Run owns one Mission containing dependency-ordered WorkItems. Each WorkItem binds one role, tool policy, budget, lifecycle, and structured Handoff.

The initial team contains Lead, Explorer, Builder, Reviewer, and Verifier roles. Operator exists as a disabled-by-default privileged role. Only Lead communicates a final result to the owner. Agents do not persist free-form private conversations with one another; Handoffs contain bounded summaries, evidence, modified files, checks, issues, and next steps.

Read-only WorkItems may execute concurrently. One workspace has a single writer lease, so Builder and Operator never mutate concurrently. A Run cannot complete until required review and post-mutation verification succeed. Every model call, WorkItem, Mission, event stream, budget, and checkpoint remains bounded and cancellable.

Owner profile and personal memory live outside repositories and are never committed. Project memory remains workspace-scoped. Neither store accepts credentials, private reasoning, raw prompts, stream chunks, or unbounded tool output.

## Consequences

- Session schema and events gain Mission, WorkItem, Agent, and Handoff identity.
- Existing single-Agent Sessions remain loadable and continue to use the v0.2 Runner path.
- Team templates replace arbitrary agent-to-agent chat with auditable workflows.
- Parallel writing and automatic Git merge are deferred until isolated-worktree semantics have a separate decision and tests.
- Background scheduling, external messages, deploys, commits, pushes, and pull requests remain approval-gated or operator-initiated.
