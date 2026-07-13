# ADR 0006: provider presets and a local Web debug surface

Status: accepted for `0.2.0-dev`.

## Decision

Keep one provider-neutral Agent Core and add a small preset registry at configuration boundaries. Implement OpenAI Responses separately from Chat Completions because message, tool, reasoning-item, and SSE contracts differ. Adapt DeepSeek and Qwen through the Chat Completions adapter where compatible, retaining explicitly documented provider fields needed for multi-turn tools.

Add a read-mostly, local-only Web surface that reuses the same runtime assembly and structured events as the CLI. Keep the workspace, model endpoint, and credentials server-controlled. Package it with Docker Compose bound to host loopback.

## Consequences

- Provider selection needs no vendor branching in Agent Core.
- Provider-encrypted Responses continuation items and locally AES-GCM-encrypted DeepSeek continuation may be checkpointed so required reasoning survives tool turns; readable reasoning is not serialized, displayed, or logged.
- The Web server is not a multi-user product and has no authentication. Remote access uses SSH tunneling.
- A future hosted UI requires a separate threat model, authentication, authorization, tenancy, and secret-management design.
