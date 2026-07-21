# Model providers

GoHermit follows the provider-selection logic used by Hermes: provider slugs are the runtime identity, company groups are display-only, and `auth_type` decides whether credentials come from an account login or an API key. The Web picker therefore follows:

```text
company group → provider/access slug → provider model → Agent profile
```

## Catalog

| Company group | Provider slug | Authentication | Protocol | Default endpoint |
|---|---|---|---|---|
| OpenAI | `openai-codex` | Codex CLI / ChatGPT account OAuth | Responses | `https://chatgpt.com/backend-api/codex` |
| OpenAI | `openai-api` | `OPENAI_API_KEY` | Responses | `https://api.openai.com/v1` |
| DeepSeek | `deepseek` | `DEEPSEEK_API_KEY` | Chat Completions | `https://api.deepseek.com` |
| Alibaba / Qwen | `alibaba` (`qwen` config alias) | `DASHSCOPE_API_KEY` | Chat Completions | DashScope compatible endpoint |
| Alibaba / Qwen | `alibaba-coding-plan` | `ALIBABA_CODING_PLAN_API_KEY` | Chat Completions | Alibaba Coding Plan endpoint |
| Moonshot AI / Kimi | `kimi-coding-plan` | `KIMI_API_KEY` | Chat Completions | `https://api.kimi.com/coding/v1` |

Legacy `codex`, `openai`, `openai-chat`, and `openai-compatible` TOML presets remain valid for CLI compatibility. In the Web catalog, OpenAI Codex Plan and OpenAI API are deliberately separate providers.

## Kimi Code 编程套餐

The `kimi-coding-plan` provider targets the Kimi Code (coding plan) membership endpoint `https://api.kimi.com/coding/v1`, an OpenAI-compatible Chat Completions API. It uses the plan-issued API key from `KIMI_API_KEY` (or a key pasted in Web Provider Settings, which is stored server-side and takes precedence over the environment). The default model is `kimi-for-coding`; the Web catalog also offers `kimi-for-coding-highspeed` and `k3` under the Moonshot AI / Kimi group. A ready-made TOML preset ships as `configs/kimi-coding-plan.toml`.

Kimi rejects function names containing dots, so this preset enables outbound tool-name sanitization: wire names replace invalid characters with underscores (`file.read` → `file_read`) in both tool definitions and replayed history `tool_calls`, and response tool calls are mapped back to the registry names before execution. Other providers keep their existing wire format.

## Codex Plan

The Codex Plan path supports both GoHermit-managed device login and Hermes-style safe CLI import. Credential precedence is environment token, GoHermit credential store, then `CODEX_HOME/auth.json`. An expiring access token must refresh successfully before the access method is offered to Run. The Web catalog is discovered from the authenticated account's live Codex models endpoint; guessed or unsupported models are not offered. GoHermit never rewrites the Codex CLI file. Requests use the Codex Responses endpoint plus the `originator`, Codex-shaped user agent, and JWT-derived `ChatGPT-Account-ID` headers.

Codex streaming is assembled from `response.output_item.done` events because the terminal `response.completed` payload may omit output. Function names are mapped to API-safe identifiers and mapped back before local execution. Encrypted reasoning replay strips response item IDs and replaces reasoning summaries with an empty required array, preserving continuation without persisting private reasoning text.

Either log in from Web Provider Settings, or prepare the CLI fallback on the host:

```bash
codex login
docker compose up --build -d
```

Compose mounts `${HOME}/.codex` at `/codex` read-only and sets `CODEX_HOME=/codex`. `GOHERMIT_CODEX_ACCESS_TOKEN` is available for controlled non-CLI environments, but a maintained Codex login is preferred. Tokens are never returned by Web APIs or logged.

An opt-in live smoke test validates the account path against the real Codex backend with one bounded call: `GOHERMIT_LIVE_CODEX_SMOKE=1 go test ./internal/evals/ -run TestLiveCodexSmoke -count=1 -v`. It skips when credentials are missing, never runs in the default suite, and is the only path that can place a paid live call; CI runs it solely via the manual `live-smoke` workflow-dispatch job.

## Agent profiles

- `coding`: full workspace-scoped development tools.
- `review`: an enforced read-only registry containing only filesystem and Git inspection tools.
- `devops`: full workspace-scoped tools with an operations-focused system prompt.

Agent profile is independent from provider and model. Adding a model must not add new tool permissions.

The Responses provider sends `store=false`, requests `reasoning.encrypted_content`, and retains only encrypted continuation items. DeepSeek `reasoning_content` is encrypted before checkpointing and replayed only by its provider adapter.

## Upstream design source

The catalog and Codex account logic are adapted from Hermes Agent under its MIT license:

- Provider catalog: <https://github.com/NousResearch/hermes-agent/blob/main/hermes_cli/provider_catalog.py>
- Display-only provider groups: <https://github.com/NousResearch/hermes-agent/blob/main/hermes_cli/models.py>
- Codex provider profile: <https://github.com/NousResearch/hermes-agent/tree/main/plugins/model-providers/openai-codex>
- Codex credential/model handling: <https://github.com/NousResearch/hermes-agent/blob/main/hermes_cli/auth.py>
- Hermes license: <https://github.com/NousResearch/hermes-agent/blob/main/LICENSE>

Protocol references:

- OpenAI Responses and function calling: <https://developers.openai.com/api/docs/guides/function-calling>
- OpenAI Codex with ChatGPT plans: <https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan>
- DeepSeek API: <https://api-docs.deepseek.com/>
- Qwen OpenAI-compatible API: <https://help.aliyun.com/en/model-studio/compatibility-of-openai-with-dashscope>
