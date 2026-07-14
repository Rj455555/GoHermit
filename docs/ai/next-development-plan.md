# Next development plan

This file starts after the `0.2.0-dev` provider, local Web, and persistent Agent Harness milestones. Read it only when planning new work. The Session/Run split, per-call context assembly, project memory, verification gate, recovery, and Session UI are implemented; do not plan them again.

## P0: stabilize the provider boundary

1. Add opt-in live smoke tests for each provider; keep paid calls outside the default test suite.
2. Add live provider model discovery with a bounded cache and checked-in fallback, following Hermes's catalog order without copying its Python runtime.
3. Add capability flags for reasoning effort, images, structured output, and provider-specific limits.
4. Add contract fixtures captured from sanitized official API examples so protocol drift is easy to detect.
5. Decide whether provider profiles should support ordered fallback. Do not add fallback until cost, retry, and audit semantics are specified.
6. Add credential-store encryption or OS keychain integration before any non-local deployment is considered.

Acceptance criteria: neutral Agent Core types remain vendor-free; unsupported capabilities fail during configuration rather than mid-task; secrets and private reasoning never appear in logs.

## P1: interactive approval

1. Define a transport-neutral approval request/response contract.
2. Let CLI and local Web approve one bounded action without turning the shell into an unrestricted terminal.
3. Add crash/restart fixtures around pending approvals while preserving the existing completed-tool replay guard.

Acceptance criteria: non-interactive mode remains deny-by-default; approvals are scoped, expiring, auditable, and cannot bypass workspace policy.

## P2: context quality and evaluation

1. Replace approximate-only compression decisions with provider token counters where available.
2. Add deterministic repository-task fixtures and score completion, edits, tests, turns, tokens, and recovery behavior.
3. Tune recent-message retention and structured summaries from evaluation data.

Acceptance criteria: quality changes are measured against checked-in fixtures; raw prompts, reasoning, and unbounded tool output are not retained.

## P3: plugin protocol v2 proposal

Draft, do not implement first: streaming notifications, cancellation IDs, capability negotiation, and compatibility rules. Preserve protocol v1 until v2 has conformance tests for GoHermit plus the Python and Node examples.

## Explicitly deferred

- Public or multi-user hosting
- Accounts, organization policy, and cloud secret management
- Multi-agent orchestration
- Background daemon and scheduler
- Automatic commit, push, or pull-request creation
- Telemetry or remote control plane

Any of these changes requires its own threat model and ADR before implementation.
