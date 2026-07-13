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

Legacy `codex`, `openai`, `openai-chat`, and `openai-compatible` TOML presets remain valid for CLI compatibility. In the Web catalog, OpenAI Codex Plan and OpenAI API are deliberately separate providers.

## Codex Plan

The Codex Plan path mirrors Hermes's safe import rule: GoHermit reads `CODEX_HOME/auth.json`, requires a valid access token, refreshes an expiring access token in memory when a refresh token exists, and never rewrites the Codex CLI file. Requests use the Codex Responses endpoint plus the `originator`, Codex-shaped user agent, and JWT-derived `ChatGPT-Account-ID` headers.

On the host:

```bash
codex login
docker compose up --build -d
```

Compose mounts `${HOME}/.codex` at `/codex` read-only and sets `CODEX_HOME=/codex`. `GOHERMIT_CODEX_ACCESS_TOKEN` is available for controlled non-CLI environments, but a maintained Codex login is preferred. Tokens are never returned by Web APIs or logged.

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
