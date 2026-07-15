# AI context: read this second

This is the compact handoff for coding agents. Read `AGENTS.md` first, then this file. Do not load every document by default.

## Product in one paragraph

GoHermit `0.4.0-dev` is a foreground, local-first, single-owner coding harness. A durable Session contains multiple user-message Runs, visible history, bounded context, verified project memory, crash recovery, and a public Live Plan whose checkbox phases update from real execution facts. A Run may use the original single Agent or a Personal Agent Team whose Mission delegates bounded WorkItems through structured Handoffs. Explicit Owner Profile data lives outside repositories and enters every Worker context.

## Shortest useful reading path

1. Always read `AGENTS.md` and this file.
2. Read the target package and its `_test.go` files.
3. For Session/Run, context, memory, recovery, or Web conversation work, read `docs/ai/harness.md`.
4. For Owner Profile, Mission, WorkItem, role policy, Handoff, or team UI, read `docs/ai/team.md`.
5. For Live Plan/checklist/progress/recovery work, read `docs/ai/plan-mode.md`.
6. Read only the matching topic document:
   - agent/model flow: `docs/architecture.md`
   - context: `docs/context-management.md`
   - session/storage: `docs/session-storage.md`
   - plugin changes: `docs/plugin-protocol.md`
   - filesystem/shell/credentials: `docs/security-model.md`
7. Read a specific ADR only when changing that decision boundary.
8. Read `docs/ai/next-development-plan.md` for planned work.

## Code map

| Change | Start here | Also inspect |
|---|---|---|
| CLI flag/output | `internal/app/app.go` | `internal/app/app_test.go`, `cmd/hermit/main.go` |
| turns/stopping/tool loop | `internal/agent/agent.go` | agent tests, event/session contracts |
| provider/streaming/retry | `internal/model` | HTTP fixture tests |
| provider catalog/grouping | `internal/config` | `internal/auth`, provider docs, config tests |
| credentials/device login | `internal/auth` | `docs/ai/console-credentials.md`, Compose data volume |
| local Web/SSE | `internal/web`, `cmd/hermit-web` | Web tests, `docs/web-debug.md` |
| team orchestration | `internal/team`, `internal/app/team_worker.go` | `docs/ai/team.md`, ADR 0008 |
| Live Plan/checklist | `internal/taskplan`, Plan updates in agent/Web | `docs/ai/plan-mode.md`, ADR 0009 |
| owner preferences/memory | `internal/owner` | owner APIs and context manager |
| Docker packaging | `Dockerfile`, `compose.yaml` | Web/Docker guide |
| tool registry/timeouts | `internal/tool/tool.go` | executor tests |
| filesystem/shell/Git/test | `internal/tool/builtin` | policy and security tests |
| prompt budget/summary | `internal/contextmgr` | context tests and compression prompt |
| checkpoint/resume/clean | `internal/session` | storage package and session tests |
| redaction/rotation/atomic file | `internal/storage` | storage tests |
| shell classification | `internal/policy` | policy tests |
| plugin process/protocol | `internal/plugin`, `protocol/plugin/v1` | both echo examples and plugin tests |
| configuration | `internal/config` | `hermit.example.toml`, config tests |

## Invariants that must survive every change

- Agent Core emits events; it never prints to a terminal.
- Model/vendor JSON does not enter Agent Core behavior; opaque provider continuation data stays in the message envelope and is interpreted only by its provider.
- Every loop is bounded by turns and total time; every external call is cancellable and timed.
- Tool errors are returned to the model as structured results unless the task itself cannot continue.
- Built-in filesystem access stays inside the real workspace and cannot read `.git`, `.gohermit`, credential-like files, or symlink escapes.
- Shell is an allowlist, not a general terminal. Non-interactive permission requests are never auto-approved.
- Stream chunks, full prompts/requests, secrets, private reasoning, and unbounded outputs are not persisted.
- Checkpoints are versioned JSON, atomically replaced, and resumable without full conversation history.
- Plugin stdout is protocol-only, message/concurrency sizes are bounded, and crashes cannot crash the core.
- GoHermit never commits, pushes, opens PRs, changes system settings, or emits telemetry by itself. Docker packaging is an operator-started local debug surface.
- Team agents communicate durably only through bounded Handoffs; read-only agents may run concurrently, but one workspace has one writer.
- A Team Run cannot reach Lead completion without independent post-repair Verifier evidence.
- Live Plan revisions come only from real lifecycle transitions; at most one step is current, and no Plan contains private reasoning or fabricated progress.

## Current verified state

- The v0.2 Harness baseline passed normal tests, race tests, vet, CLI/Web builds, and cross-builds on the Mac development host before v0.3 work began.
- The v0.3 Personal Agent Team and Owner Profile milestone passed normal/race tests, vet, native/cross-builds, Compose validation, Docker acceptance, and recovery tests; its frozen record is `docs/ai/handoff-v0.3.md`.
- For v0.4, Plan state-machine, schema migration, single-Agent lifecycle, Team lifecycle, verifier-failure, cancellation/interruption, SSE ordering, and persistence tests pass.
- The v0.4 tree passes Mac formatting inspection, frontend JavaScript syntax, `go test ./...`, `go test -race ./...`, vet, native CLI/Web builds, and Linux amd64 plus Windows amd64 CLI/Web cross-builds.
- Compose validation, Docker rebuild, container health, `/api/health`, `/api/info`, and static Live Plan asset acceptance pass. The local workbench reports `0.4.0-dev`; exact repository/deployment state is in `docs/ai/handoff-v0.4.md`.
- Python and Node echo plugin lifecycles are covered by tests; restricted roles additionally filter plugin tools by declared permission and mutation flag.
- Chat Completions and Responses HTTP behavior are tested with local servers, including reasoning continuation; no paid API call is part of the test suite.
- Docker Compose binds the Web surface to host loopback, mounts Codex CLI auth read-only, and persists Web-managed credentials in a dedicated data volume.
- `/api/info` returns secret-free status and separates the full Settings catalog from the credential-filtered Session catalog.
- Codex Run models are discovered from the authenticated account; Responses streaming reconstructs text and tool calls from output-item events and safely replays encrypted continuation.
- Harness tests cover Session/Run verification, schema migration, event replay, recovery reconciliation, project-memory redaction, and `Last-Event-ID` SSE continuation. The Mac-hosted v0.3 workbench passes health/API/static acceptance; an interactive live-model Mission remains opt-in.
- Team tests cover dependency order, parallel readers, one writer, structured Handoffs, Verifier gating, parent Session completion, and completed Worker replay protection.
- The only third-party Go module is `github.com/BurntSushi/toml` for strict TOML decoding.

## Known boundaries

- Shell/test execution and configured plugins are not OS sandboxes; repository code and plugins must be trusted.
- Plugin streaming events are deferred beyond protocol v1.
- Session storage is schema v4 with explicit v1, v2, and v3 migrations; unknown versions still fail closed.
- Permission-required events are non-interactive in v0.1.0.
- Codex device login uses OpenAI's device flow and stores tokens only in the server-side credential store. Revocation is detected when a refresh is required; there is no proactive remote token introspection.
- The Web surface is single-user and unauthenticated; public exposure is unsupported.
- Provider/model/Agent selection is fixed for a Session; create a new Session to switch.
- Interactive approvals and multiple workspaces remain deferred.
- Team templates currently use one provider/model selection for all roles; per-role model overrides and isolated parallel worktrees remain deferred.
- Team orchestration currently starts and resumes through the Web Session/Run API. The CLI and legacy `/api/run` reject `team` instead of silently running a single Lead loop.

## Verification

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
docker compose config
```

When handing off, update this file only if the product boundary, code map, invariants, verified state, or known boundaries changed. Keep it compact.
