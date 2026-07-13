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

1. Create or load a versioned session.
2. Build layered context from system rules, `AGENTS.md`, project memory, goal, summary, and recent bounded messages.
3. Start a turn only while total deadline and `max_turns` allow it.
4. Call the provider with a per-request deadline and finite retry policy.
5. Emit stream deltas without persisting token chunks.
6. If the assistant returns tool calls, execute each through the registry and append structured results, including errors, to model context.
7. Checkpoint after tool completion and every configured number of turns.
8. A response with no tool calls is the explicit success condition. Context cancellation, total timeout, provider failure, checkpoint failure, or maximum turns are explicit non-success conditions.

## Model call flow

Provider-neutral messages and tool definitions are converted at the HTTP boundary. Presets resolve configuration into either Chat Completions or Responses protocol implementations. Both never log requests or Authorization headers, classify HTTP errors, retry only rate-limit/availability failures, parse JSON/SSE, and propagate cancellation through `http.NewRequestWithContext`. Responses retains only provider-encrypted reasoning continuation items; the Chat adapter AES-GCM encrypts DeepSeek `reasoning_content` before checkpointing and decrypts it only when replaying a tool turn.

## Web boundary

`hermit-web` embeds static assets and exposes health, non-secret provider metadata, and one same-origin SSE task endpoint. Workspace, config, endpoint, and credentials are fixed server-side. It permits one active run and is designed only for loopback or SSH-tunneled access.

## Tool call flow

The registry rejects duplicate names. The executor resolves the tool, applies a deadline, converts panics into process-level failures rather than hiding them, normalizes errors, and truncates returned data with `truncated`, `original_size`, and `returned_size`. Built-ins independently validate strict JSON arguments and workspace paths. Permission-required and blocked shell decisions are returned as data and emitted as events.

## Session flow

Session state is kept in memory during a turn. Events are buffered and appended in batches. A checkpoint serializes a language-neutral schema, writes a temporary file in the destination directory, flushes it, and atomically renames it. Resume checks schema version, workspace identity, and saved hashes of files changed by the agent. Complete conversation history is not required for recovery.

## Cancellation and errors

One parent context covers the task. Model calls, tools, Git checks, tests, and plugins derive bounded child contexts. Cancellation moves the session to `cancelled`, saves a final summary, emits `task_cancelled`, and exits with code 130. Ordinary runtime errors move it to `failed`. Tool errors remain inside the loop so the model may repair or choose another action.

## Plugin boundary

Plugins are configured child processes, not Go `.so` modules. Stdout is reserved for one JSON-RPC object per line; stderr is bounded diagnostic output. The supervisor multiplexes request IDs, limits message size/concurrency, detects invalid JSON and abnormal exit, forwards cancellation, and kills a process that cannot shut down before its deadline. A plugin crash cannot crash Agent Core.
