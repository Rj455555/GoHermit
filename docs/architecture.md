# Architecture

## System boundary

GoHermit is a foreground CLI or local Web process. Both presentation surfaces reuse the same runtime assembly and structured events. The process owns configuration, model HTTP calls, workspace-scoped built-in tools, plugin child processes, and `.gohermit` session data. The workspace and configured plugins are trusted inputs; model output, tool arguments, provider responses, browser input, plugin stdout, and checkpoint files are untrusted and validated at their boundary.

```text
CLI renderer
    │ events
Agent loop ── Context manager
    ├── Provider ── HTTPS ── OpenAI-compatible API
    ├── Tool executor ── Registry ── Built-in tools ── Workspace
    │                            └── Plugin tools ── stdio JSON-RPC child
    └── Session store ── atomic JSON / batched JSONL ── .gohermit
```

Dependencies point inward: `cmd` depends on `internal/app`; app assembles domain packages; agent depends on provider/tool/session abstractions; infrastructure packages implement them. Agent never imports the CLI.

## Agent loop

1. Create or load a versioned Session and its queued/interrupted Run.
2. Rebuild layered context before every model call from system rules, `AGENTS.md`, project memory, Session summary, active Run state, goal, and recent bounded messages.
3. Start a turn only while total deadline and `max_turns` allow it.
4. Call the provider with a per-request deadline and finite retry policy.
5. Emit stream deltas without persisting token chunks.
6. If the assistant returns tool calls, execute each through the registry and append structured results, including errors, to model context.
7. Checkpoint after tool completion and every configured number of turns.
8. A response with no tool calls enters verification. Read-only work may complete; mutations require `git diff --check`, and non-document changes require a successful test after the last mutation. Verification may return work to the model up to three times.

## Model call flow

Provider-neutral messages and tool definitions are converted at the HTTP boundary. A Hermes-style catalog keeps canonical provider slug, display company group, `auth_type`, model list, and Agent profile separate. Provider slugs resolve into either Chat Completions or Responses implementations. `openai-codex` imports Codex CLI OAuth and targets the subscription backend; `openai-api` uses an API key and the public endpoint. Both protocols never log requests or Authorization headers, classify HTTP errors, parse JSON/SSE, and propagate cancellation. Responses retains only provider-encrypted reasoning continuation items; the Chat adapter encrypts DeepSeek `reasoning_content` before checkpointing.

## Web boundary

`hermit-web` embeds a Codex-style single workbench with a task sidebar, conversation/execution canvas, composer, and Settings drawer. Same-origin Session APIs create fixed provider/model/Agent selections, append user-message Runs, and replay persisted events by sequence before continuing live SSE. Workspace, endpoints, and credentials remain server-side. One workspace permits one active Run; history and settings remain readable. The surface is only for loopback or SSH-tunneled access.

## Tool call flow

The registry rejects duplicate names. The executor resolves the tool, applies a deadline, converts panics into process-level failures rather than hiding them, normalizes errors, and truncates returned data with `truncated`, `original_size`, and `returned_size`. Built-ins independently validate strict JSON arguments and workspace paths. Permission-required and blocked shell decisions are returned as data and emitted as events.

## Session flow

A Session is a durable conversation; each user message creates a Run with its own status and verification state. Visible user/assistant messages and sequenced events are append-only JSONL, while bounded recovery state is atomically replaced in `session.json`. Schema v1 migrates explicitly to v2. Workspace identity mismatch fails closed; external file/Git changes trigger reconciliation instead of discarding the Session. Started-but-unfinished tools become uncertain and are never blindly replayed.

## Cancellation and errors

One parent context covers the task. Model calls, tools, Git checks, tests, and plugins derive bounded child contexts. Cancellation moves the session to `cancelled`, saves a final summary, emits `task_cancelled`, and exits with code 130. Ordinary runtime errors move it to `failed`. Tool errors remain inside the loop so the model may repair or choose another action.

## Plugin boundary

Plugins are configured child processes, not Go `.so` modules. Stdout is reserved for one JSON-RPC object per line; stderr is bounded diagnostic output. The supervisor multiplexes request IDs, limits message size/concurrency, detects invalid JSON and abnormal exit, forwards cancellation, and kills a process that cannot shut down before its deadline. A plugin crash cannot crash Agent Core.
