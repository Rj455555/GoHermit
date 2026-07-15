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

## Deferred

Vector/embedding memory, browser automation, MCP, marketplace, public/hosted UI, accounts, collaboration, cloud sync, telemetry, analytics, schedulers, daemons, automatic unapproved push/deploy, Kubernetes SDK integration, Go `.so` plugins, and a general workflow engine remain deferred. They require separate evidence and architecture decisions.
