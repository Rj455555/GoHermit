# ADR 0006: provider presets and a local Web debug surface

Status: accepted for `0.2.0-dev`.

## Decision

Keep one provider-neutral Agent Core and adapt Hermes's provider-catalog boundary: canonical provider slugs are runtime identity, company groups are display-only, and authentication type separates account/OAuth providers from API-key providers. Implement OpenAI Responses separately from Chat Completions because message, tool, reasoning-item, and SSE contracts differ. Adapt DeepSeek and Qwen through the Chat Completions adapter where compatible.

Add a local-only Web surface that reuses the same runtime assembly and structured events as the CLI. A validated per-run selection chooses provider, model, and Agent profile; credentials, base URLs, and workspace stay server-controlled. The `openai-codex` provider imports a Codex CLI login from a read-only `CODEX_HOME` mount instead of conflating a ChatGPT subscription with `OPENAI_API_KEY`. Package it with Docker Compose bound to host loopback.

## Consequences

- Provider selection needs no vendor branching in Agent Core.
- Agent profile is independent from provider; the review profile receives an enforced read-only tool registry.
- Provider-encrypted Responses continuation items and locally AES-GCM-encrypted DeepSeek continuation may be checkpointed so required reasoning survives tool turns; readable reasoning is not serialized, displayed, or logged.
- The Web server is not a multi-user product and has no authentication. Remote access uses SSH tunneling.
- A future hosted UI requires a separate threat model, authentication, authorization, tenancy, and secret-management design.
