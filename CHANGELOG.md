# Changelog

## 0.2.0-dev

- Added provider presets inspired by Hermes: Codex/OpenAI Responses, DeepSeek, Qwen, OpenAI Chat Completions, and custom OpenAI-compatible endpoints.
- Split provider selection into Hermes-style company groups, provider/auth slugs, models, and Agent profiles; added Codex CLI account import for Codex Plan.
- Added Responses API streaming and function calls with `store=false`, preserving only encrypted reasoning continuation between tool turns.
- Added DeepSeek `reasoning_content` replay for thinking-mode tool-call compatibility, encrypted before session checkpointing.
- Added a loopback-only local Web debugger, server-sent runtime events, Docker image, and Compose configuration.
- Replaced the single debug form with Dashboard, Run Agent, and Provider Settings pages.
- Added server-side API key storage, Codex device-code login, strict credential availability checks, and a credential-filtered run catalog.
- Added account-scoped Codex model discovery and Codex-compatible streaming tool-call/continuation handling.
- Added persistent multi-turn Sessions with one bounded Run per user message, visible local history, sequenced SSE replay, cancellation, and interrupted-run recovery.
- Rebuilt model context before every call, added safe automatic summary fallback and versioned verified project memory under `.gohermit/memory/`.
- Added mutation-aware completion verification so code changes cannot complete without post-mutation tests.
- Replaced the one-shot Agent page with a Session list, conversation transcript, activity timeline, and stop/resume controls.

## 0.1.0 - 2026-07-13

- Added a bounded single-agent coding loop with cancellation, model retry, tool errors, and structured events.
- Added human and JSON CLI modes with run, resume, status, context, clean, and config validation commands.
- Added an OpenAI-compatible Chat Completions provider with streaming and incremental tool calls.
- Added workspace-scoped filesystem, patch, shell, Git, and test tools.
- Added approximate context budgets, structured summaries, atomic JSON checkpoints, JSONL events, retention, log redaction, and rotation.
- Added stdio JSON-RPC 2.0 plugin supervision and Python/Node echo examples.
- Added security, architecture, testing, and ADR documentation.
