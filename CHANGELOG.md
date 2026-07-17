# Changelog

## 0.5.0-dev

- Added prepared commit journals so Session checkpoints and persistent event batches recover idempotently after crashes and are durable before SSE delivery.
- Added a presentation-neutral Run/Plan controller and consistent resumable timeout versus terminal cancellation semantics.
- Added task-specific Plan titles plus `auto` and review-first Plan modes; review Runs stay queued until explicit owner approval and can be cancelled before execution.
- Added adaptive Personal Agent Team topologies with parallel read-only evidence gathering, a single writer, and Plan steps derived from real Mission WorkItems.
- Added a bounded repair/reverify loop that preserves prior Handoffs and retries failed independent verification up to three attempts within the Mission budget.
- Added durable detached Worker activity relay without storing raw tool arguments.
- Added Playwright coverage for review Plan creation, refresh recovery, approval, and execution-state controls, plus Linux Go/race, Web E2E, cross-build, and Docker CI jobs.

## 0.4.0-dev

- Added a durable Cursor-style Live Plan to every Run, with bounded checkbox steps, one current step, progress, details, and terminal completed/failed/cancelled states.
- Added schema v4 with an explicit v3 migration and validation on both checkpoint save and load.
- Added sequenced `plan_created` and `plan_updated` SSE events containing bounded public Plan snapshots; refresh and reconnect recover the same revision.
- Mapped single-Agent analysis/execution/verification/report phases and Team WorkItems to the shared Plan contract without persisting private model reasoning.
- Added a collapsible workbench checklist with live current-step text, completion bar, failure/cancellation states, and persisted reload behavior.

## 0.3.0-dev

- Added the single-owner Personal Agent Team: Lead, Explorer, Builder, Reviewer, repair Builder, and Verifier execute a bounded dependency workflow.
- Added Mission, WorkItem, Agent, Handoff, model-budget, and execution-session state with schema v2-to-v3 migration.
- Added stable hidden Worker Sessions so interrupted team work resumes without replaying completed model or tool work.
- Added parallel read-only work with a single workspace-writer lease and an independent verification gate before Lead completion.
- Added an explicit Owner Profile and confirmed personal memory outside repositories, with Web APIs to view, edit, and forget facts and secret-pattern rejection.
- Added owner context to every Worker while preserving project memory as a separate workspace-scoped layer.
- Added honest offline/reconnecting Web states, a five-second health heartbeat, automatic Session reload after recovery, and a reconnecting Windows SSH tunnel script.
- Added a team activity panel, per-role status cards, usage budget display, and personal settings to the Codex-style Web workbench.
- Restricted read-only team roles to plugin tools declared read-only and non-mutating; team runs now fail closed on unsupported CLI and legacy one-shot entry points.
- Separated terminal owner cancellation from resumable interruption and ensured failed parallel batches cannot leave phantom running WorkItems.

## 0.2.0-dev

- Added provider presets inspired by Hermes: Codex/OpenAI Responses, DeepSeek, Qwen, OpenAI Chat Completions, and custom OpenAI-compatible endpoints.
- Split provider selection into Hermes-style company groups, provider/auth slugs, models, and Agent profiles; added Codex CLI account import for Codex Plan.
- Added Responses API streaming and function calls with `store=false`, preserving only encrypted reasoning continuation between tool turns.
- Added DeepSeek `reasoning_content` replay for thinking-mode tool-call compatibility, encrypted before session checkpointing.
- Added a loopback-only local Web debugger, server-sent runtime events, Docker image, and Compose configuration.
- Replaced the single debug form with a Codex-style task sidebar, conversation workbench, pinned composer, and Settings drawer.
- Added server-side API key storage, Codex device-code login, strict credential availability checks, and a credential-filtered run catalog.
- Added account-scoped Codex model discovery and Codex-compatible streaming tool-call/continuation handling.
- Added persistent multi-turn Sessions with one bounded Run per user message, visible local history, sequenced SSE replay, cancellation, and interrupted-run recovery.
- Rebuilt model context before every call, added safe automatic summary fallback and versioned verified project memory under `.gohermit/memory/`.
- Added mutation-aware completion verification so code changes cannot complete without post-mutation tests.
- Replaced one-shot execution with persistent Sessions, a conversation transcript, collapsible activity, stop/resume controls, and refresh recovery.

## 0.1.0 - 2026-07-13

- Added a bounded single-agent coding loop with cancellation, model retry, tool errors, and structured events.
- Added human and JSON CLI modes with run, resume, status, context, clean, and config validation commands.
- Added an OpenAI-compatible Chat Completions provider with streaming and incremental tool calls.
- Added workspace-scoped filesystem, patch, shell, Git, and test tools.
- Added approximate context budgets, structured summaries, atomic JSON checkpoints, JSONL events, retention, log redaction, and rotation.
- Added stdio JSON-RPC 2.0 plugin supervision and Python/Node echo examples.
- Added security, architecture, testing, and ADR documentation.
