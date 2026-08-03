# Changelog

## 0.8.0-dev

- Added a first-class owner-scoped Weixin transport with bounded QR login,
  separate credentials, per-account cursor polling, idempotent inbox/outbox,
  explicit Employee bindings, and queued-only Task creation. Session/Run
  execution remains under the existing Owner Start control-plane boundary.

- Added Employee-owned recurring Loops with a bounded Goal/Boundaries/SOP
  contract, Definition of Done, Stop Conditions, manual or daily trigger, and
  generated `contracts/{loop-id}/LOOP.md`.
- Added a separate bounded runtime-state projection and an at-most-once
  single-process scheduler. Every invocation creates an ordinary EmployeeTask
  and reuses the existing Prepare, Session/Run, Tool, Verification, Approval,
  recovery, Memory Candidate, Artifact, and SSE paths.
- Rebuilt the Loops React surface around When / Does / You get cards, a short
  create flow, and Contract / State / Logs. Detailed model, Team, policy,
  budget, and verification controls remain in Advanced settings.
- Added an Employee Loops tab plus Chinese and English UI copy, strict typed
  runtime decoding, API/contract tests, scheduling/recovery regressions, and
  deterministic browser coverage.

## 0.7.0-dev

- Fixed successful Employee creation being reported as failed by aligning the
  React Employee-record decoder with Go's canonical `omitempty` response:
  absent empty slices now decode as empty collections and `project_count` is
  derived from the returned ProjectBindings.
- Added a name-only Employee quick-create path that persists immediately with
  the current ready model, current Workspace, no Skills, Knowledge, or Memory,
  and a conservative read-only/no-network policy. The full nine-step setup
  remains available as optional advanced configuration, and every field can
  be completed later from Employee details.
- Fixed Employee creation with non-ASCII display names by generating a bounded,
  path-safe system ID before persistence and showing the conversion in the
  identity step instead of failing late during server validation.
- Fixed React Employee create/update against the strict Go DTO by removing
  response-only `project_count`, added guided role-draft generation, and
  tightened select, checkbox, and radio presentation.
- Replaced the legacy HTML/JavaScript/CSS console with one embedded React,
  TypeScript, and Vite Workbench covering Dashboard, Employees, Tasks, Agent,
  Loops, and Settings with bundled zh-CN/en-US switching and fail-closed routes.
- Added a typed, size-bounded API trust boundary and one Session-keyed SSE
  registry with per-subscriber Run filtering, Session high-water recovery, and
  authoritative projection refresh instead of browser execution state.
- Finalized deterministic committed React assets, Node-to-Go multi-stage
  Docker builds, a Go-free/Node-free minimal runtime image, frontend CI gates,
  React browser coverage, and container rebuild persistence acceptance.
- Added first-class owner-scoped Electronic Employees with immutable revision
  snapshots, explicit lifecycle, ProjectBindings, policy/budget/concurrency,
  strict fail-closed persistence, and CRUD/Dry Run APIs.
- Added exact version/digest-pinned local Skills, deterministic cited local
  Knowledge, and private Employee Memory with verified Candidates and explicit
  Owner acceptance, edit, and Forget.
- Added a persistent Employee Task Inbox. Task creation queues only; Runtime
  Preparation creates one stable schema-v6 Session and no Run; explicit Start
  persists and starts at most one existing Run.
- Added Task status projection, cancellation/resume/restart reconciliation,
  canonical completed-Tool recovery, bounded verified Artifacts and Memory
  Candidates, and conservative Employee/Workspace concurrency gates.
- Added Employees and Tasks Web UI with real server readiness, exact Skill
  identity/configuration, current-Workspace Projects, explicit Start, existing
  Session/Run Timeline/Approval/Verification, and sequenced SSE recovery.
- Added optional Team Role-to-Employee assignment with strict TeamTemplate
  v1→v2 migration, immutable bounded assignment/context snapshots, model
  precedence, private per-Worker context, and fail-closed hidden Worker Session
  public access.
- Added deterministic cross-module v0.7 evals and Docker CI acceptance that
  builds, starts, health-checks, rebuilds, and byte-compares persistent
  Employee, Task, Knowledge, Memory, Session, and Loop data.
- Kept the service single-owner, loopback-only, single-Workspace, manually
  started, and free of automatic Git, PR, publish, deploy, scheduler, or paid
  model behavior in default tests.

## 0.6.0-dev

- Added the Codex-style Loop Workbench with Dashboard/Agent/Loops/Settings navigation, Definition create/import/edit forms, configured provider/model selection, argv verification editing, revision display, Dry Run Review, manual start, bounded history, cancellation, and refresh recovery.
- Added REST resources for Loop Definitions and Invocations. Web handlers remain thin same-origin, size-capped, strict-JSON transports over the same `internal/controlplane` services used by the CLI.
- Added a resumable Invocation Timeline built from the existing Session/Run, Live Plan, Team, tool summary, Scoped Approval, Verification evidence, repair/reverify, and SSE journal; no second runtime or event store was introduced.
- Added a credential-free, read-only documentation-maintenance Loop template and Fake Provider Playwright coverage for create/import persistence, revision updates, readiness failure, start, Timeline, refresh recovery, cancellation, history, and Dashboard/Agent/Settings regressions.
- Added Loop Mode foundation: an owner-scoped, versioned Loop Definition and Invocation domain (`internal/loop`, `internal/loopstore`) reusing the existing Session/Run kernel — no second Agent runtime, Run state machine, or Verifier framework.
- Added `hermit loop dry-run` and `hermit loop list`: a zero-side-effect readiness report (workspace/git match, task, per-role provider/credential status, write scope, checks, budget, approval requirement) that never calls a model, creates a Session, or touches the workspace.
- Added `hermit loop run`, `hermit loop history`, and `hermit loop cancel`: manual Loop Invocations that snapshot the Definition, bind to one independent Session/Run, and recover after a crash without duplicating Sessions, Runs, or replaying completed tool calls; a dirty workspace fails a mutating Invocation closed before any provider call.
- Added declarative Verification Recipes: bounded argv-only check commands screened by the same policy allowlist/deny table shell execution uses, feeding the existing independent Verifier and bounded repair loop — mutating Invocations require at least one real passing required check, read-only Invocations require `Issues == []`.
- Extracted `internal/controlplane` application services (Run lifecycle, Team execution, Approval coordination, durable event publish) out of `internal/web.Server`, which is now routing/presentation only; the CLI calls the same services directly with no HTTP hop.
- Calibrated documentation to Session schema v5 and the read-only Verifier passing rule (`team.HandoffChecksPassed`).

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
