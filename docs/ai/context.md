# AI context: read this second

This is the compact handoff for coding agents. Read `AGENTS.md` first, then this file. Do not load every document by default.

## Product in one paragraph

GoHermit `0.2.0-dev` is a foreground, local-first, single-agent coding runtime. The CLI and local Web console share one runtime assembly. The Hermes-derived catalog keeps provider slug, display company group, authentication type, model, and Agent profile separate. The console has Dashboard, Run, and Provider Settings pages. Codex Plan supports device-code login plus Codex CLI import; direct OpenAI, DeepSeek, and Qwen paths use server-side API keys. Only access methods with usable credentials appear on Run. The agent executes controlled built-in or stdio JSON-RPC tools and atomically checkpoints sessions under `.gohermit/`.

## Shortest useful reading path

1. Always read `AGENTS.md` and this file.
2. Read the target package and its `_test.go` files.
3. Read only the matching topic document:
   - agent/model flow: `docs/architecture.md`
   - context: `docs/context-management.md`
   - session/storage: `docs/session-storage.md`
   - plugin changes: `docs/plugin-protocol.md`
   - filesystem/shell/credentials: `docs/security-model.md`
4. Read a specific ADR only when changing that decision boundary.
5. Read `docs/ai/next-development-plan.md` for planned work.

## Code map

| Change | Start here | Also inspect |
|---|---|---|
| CLI flag/output | `internal/app/app.go` | `internal/app/app_test.go`, `cmd/hermit/main.go` |
| turns/stopping/tool loop | `internal/agent/agent.go` | agent tests, event/session contracts |
| provider/streaming/retry | `internal/model` | HTTP fixture tests |
| provider catalog/grouping | `internal/config` | `internal/auth`, provider docs, config tests |
| credentials/device login | `internal/auth` | `docs/ai/console-credentials.md`, Compose data volume |
| local Web/SSE | `internal/web`, `cmd/hermit-web` | Web tests, `docs/web-debug.md` |
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

## Current verified state

- Required commands pass: normal tests, race tests, vet, and CLI build.
- Linux amd64 and Windows amd64 cross-builds pass from macOS arm64.
- Python and Node echo plugin lifecycles are exercised by tests.
- Chat Completions and Responses HTTP behavior are tested with local servers, including reasoning continuation; no paid API call is part of the test suite.
- Docker Compose binds the Web surface to host loopback, mounts Codex CLI auth read-only, and persists Web-managed credentials in a dedicated data volume.
- `/api/info` returns secret-free status and separates the full Settings catalog from the credential-filtered Run catalog.
- The only third-party Go module is `github.com/BurntSushi/toml` for strict TOML decoding.

## Known boundaries

- Shell/test execution and configured plugins are not OS sandboxes; repository code and plugins must be trusted.
- Plugin streaming events are deferred beyond protocol v1.
- Schema version 1 rejects unknown versions; there is no migration framework yet.
- Permission-required events are non-interactive in v0.1.0.
- Codex device login uses OpenAI's device flow and stores tokens only in the server-side credential store. Revocation is detected when a refresh is required; there is no proactive remote token introspection.
- The Web surface is single-user and unauthenticated; public exposure is unsupported.

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
