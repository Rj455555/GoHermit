# Roadmap

## v0.1.x hardening

- Add CI across macOS, Linux, and Windows.
- Add a first-class interactive permission confirmation channel.
- Add more provider-compatibility fixtures and session migration fixtures.
- Measure checkpoint write amplification and expose diagnostics without telemetry.
- Add optional OS sandbox launch profiles for plugins and shell/test processes.

## v0.2 development

- Stabilize the provider compatibility suite for Responses, DeepSeek thinking/tool calls, Qwen, and custom endpoints.
- Harden the local Web debugger with cancellation, permission, and reconnect tests.
- Add reproducible container/CLI release CI and opt-in live-provider smoke tests.

## v0.3 personal team

- Evaluate the Personal Agent Team against deterministic repository fixtures and one opt-in live smoke task.
- Add bounded review/repair iteration and accurate per-role token/cost accounting.
- Add server-validated per-role model overrides after capability and fallback semantics are specified.
- Add interactive approval before enabling Operator actions.

## v0.4 live plan

- Persist an owner-facing checklist on every Run and update it from real Agent/Team lifecycle events.
- Restore Plan revision and current step across refresh, SSE reconnect, timeout, and process recovery.
- Keep Plan content bounded and separate from model private reasoning.
- Refine deterministic phase titles into task-specific substeps only when each substep has an auditable execution mapping.

## v0.5 personal workbench — complete

- Durable event commits, review-first Plan approval, adaptive Team topology, scoped expiring Approval, task-specific substeps, per-role model selection, and bounded repair/reverify are implemented.

## v0.6 Loop Workbench — complete

- PR #28–#33 completed documentation calibration, the shared Control Plane, Loop Domain/Store, zero-side-effect Dry Run, Manual Invocation, and declarative Verification Recipe.
- The local Web Workbench now creates, imports, edits, and reviews owner-scoped Loop Definitions; revision and Invocation snapshots remain immutable.
- Invocation history and Timeline reuse the existing Session/Run, Live Plan, Team, Approval, Verification, recovery, and SSE journal.
- The first checked-in Loop template maintains canonical documentation without automatic commit, push, PR, merge, or deploy.
- Worktree Foundation remains postponed while ADR 0012 is unresolved.

## v0.7 Task Inbox and Shared Artifacts

- Add a bounded owner-visible Task Inbox without introducing a daemon or automatic execution.
- Link redacted Shared Artifacts/Reports to existing Session, Run, and Loop Invocation records.
- Preserve manual foreground launch and the current single-workspace writer gate in the first slice.

## Deferred

Vector/embedding memory, browser automation, MCP, marketplace, public/hosted UI, accounts, collaboration, cloud sync, telemetry, analytics, schedulers, daemons, automatic unapproved push/deploy, Kubernetes SDK integration, Go `.so` plugins, and a general workflow engine remain deferred. They require separate evidence and architecture decisions.
