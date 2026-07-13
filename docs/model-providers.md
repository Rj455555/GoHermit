# Model providers

GoHermit keeps Agent Core provider-neutral. A small preset registry resolves a provider name into an API protocol, default base URL, model, and API-key environment variable. Explicit configuration always wins. This adapts the useful catalog pattern in Hermes without importing Hermes runtime code or its OAuth implementation.

| Provider | Protocol | Default model | API key | Default base URL |
|---|---|---|---|---|
| `codex` | OpenAI Responses | `gpt-5.3-codex` | `OPENAI_API_KEY` | `https://api.openai.com/v1` |
| `openai` | OpenAI Responses | `gpt-5.6` | `OPENAI_API_KEY` | `https://api.openai.com/v1` |
| `deepseek` | OpenAI-compatible Chat Completions | `deepseek-v4-pro` | `DEEPSEEK_API_KEY` | `https://api.deepseek.com` |
| `qwen` | OpenAI-compatible Chat Completions | `qwen3.7-plus` | `DASHSCOPE_API_KEY` | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| `openai-chat` | Chat Completions | explicit or current OpenAI model | `OPENAI_API_KEY` | `https://api.openai.com/v1` |
| `openai-compatible` | Chat Completions | required | configurable | configurable |

`codex` is an API preset, not ChatGPT/Codex OAuth. It uses an OpenAI API key and the Responses API. Its default is the current Codex-specific API model; `openai` remains the general-purpose flagship preset.

The Responses provider sends `store=false`, requests `reasoning.encrypted_content`, converts neutral tools to Responses function tools, and retains only encrypted reasoning continuation items in the neutral message's provider envelope. Agent Core never interprets, renders, or logs private reasoning.

DeepSeek V4 may return `reasoning_content` while thinking. The Chat Completions adapter preserves and replays that field on later tool-call turns. Before checkpointing, it encrypts the field with AES-GCM using a domain-separated key derived from the configured API key; readable reasoning is excluded from session JSON. Changing the API key invalidates an interrupted reasoning continuation, but does not affect completed sessions.

Qwen accounts are region and workspace specific. The preset uses the still-supported Beijing compatibility endpoint; production configurations should explicitly set the workspace-specific `base_url` shown by Alibaba Cloud Model Studio.

Examples live under `configs/`. A custom Qwen-like endpoint uses the original compatibility shape:

```toml
[model]
provider = "openai-compatible"
base_url = "https://example.com/compatible-mode/v1"
model = "your-model"
api_key_env = "YOUR_API_KEY"
```

References:

- OpenAI Responses and function calling: <https://developers.openai.com/api/docs/guides/function-calling>
- OpenAI GPT-5.3-Codex: <https://developers.openai.com/api/docs/models/gpt-5.3-codex>
- OpenAI model catalog: <https://developers.openai.com/api/docs/models>
- DeepSeek API: <https://api-docs.deepseek.com/>
- Qwen OpenAI-compatible Chat API: <https://help.aliyun.com/en/model-studio/compatibility-of-openai-with-dashscope>
- Hermes provider catalog: <https://github.com/NousResearch/hermes-agent/blob/main/website/docs/integrations/providers.md>
