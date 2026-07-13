# Changelog

## 0.2.0-dev

- Added provider presets inspired by Hermes: Codex/OpenAI Responses, DeepSeek, Qwen, OpenAI Chat Completions, and custom OpenAI-compatible endpoints.
- Added Responses API streaming and function calls with `store=false`, preserving only encrypted reasoning continuation between tool turns.
- Added DeepSeek `reasoning_content` replay for thinking-mode tool-call compatibility, encrypted before session checkpointing.
- Added a loopback-only local Web debugger, server-sent runtime events, Docker image, and Compose configuration.

## 0.1.0 - 2026-07-13

- Added a bounded single-agent coding loop with cancellation, model retry, tool errors, and structured events.
- Added human and JSON CLI modes with run, resume, status, context, clean, and config validation commands.
- Added an OpenAI-compatible Chat Completions provider with streaming and incremental tool calls.
- Added workspace-scoped filesystem, patch, shell, Git, and test tools.
- Added approximate context budgets, structured summaries, atomic JSON checkpoints, JSONL events, retention, log redaction, and rotation.
- Added stdio JSON-RPC 2.0 plugin supervision and Python/Node echo examples.
- Added security, architecture, testing, and ADR documentation.
