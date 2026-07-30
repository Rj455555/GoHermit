# GoHermit

GoHermit is a lightweight, local-first AI coding-agent runtime written in Go. It reads a workspace, calls an OpenAI-compatible model, executes bounded tools, runs tests, and persists auditable sessions that can be resumed after interruption.

GoHermit is not a hosted service, a general-purpose multi-agent platform, an unbounded workflow engine, or a port of Hermes/OpenClaw. Its provider catalog adapts Hermes's canonical-provider, display-group, and auth-type split while keeping a small provider-neutral Go core.

## Status

The current development version is `0.8.0-dev`. Electronic Employees add owner-scoped revisioned identities, pinned local Skill/Knowledge/Memory/Project context, a persistent Task Inbox with zero-execution Prepare and explicit Start, and optional Team Role assignments. Employee-owned Loops add durable Goal/Boundary/SOP contracts, manual or daily scheduling, and projected state/logs while every invocation still reuses the existing EmployeeTask, Session/Run, Live Plan, Approval, Verification, Tool, SSE, and recovery kernel. The service remains single-owner, loopback-only, one-Workspace, and free of telemetry or automatic Git/publish actions.

## Build and install

Go 1.24 or newer and Node 22 with Corepack are required. The repository pins
`pnpm@11.9.0` and has one workspace lockfile.

```bash
corepack enable
corepack prepare pnpm@11.9.0 --activate
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm test
pnpm test:coverage
pnpm build
git diff --exit-code -- internal/web/assets/dist
pnpm exec playwright install chromium
pnpm test:e2e
go test ./...
go build -o hermit ./cmd/hermit
go build -o hermit-web ./cmd/hermit-web
install -m 0755 hermit "$HOME/.local/bin/hermit"
```

The Go runtime has one third-party Go module dependency:
`github.com/BurntSushi/toml`, used for strict TOML decoding. The standard
library has no TOML parser; replacing it with a private partial parser would be
less interoperable and harder to maintain. The library is BSD-licensed, small,
mature, and is only on the configuration-loading path. Frontend build and test
dependencies are pinned by the single `pnpm-lock.yaml` and do not ship in the
runtime image.

## Quick start

```bash
cp configs/codex.toml hermit.toml       # or deepseek.toml / qwen.toml
export OPENAI_API_KEY='...'
hermit config validate
hermit run --workspace /path/to/project "inspect the project and fix failing tests"
```

Sessions are written to `.gohermit/sessions/<session-id>/` in the workspace. The Web console keeps one Session across follow-up messages; every message creates a bounded, verified Run. Team Runs add a Mission and hidden role-specific execution Sessions. Owner preferences use `GOHERMIT_OWNER_STORE` outside the workspace (Compose uses `/data/owner.json`).

Each new Run also owns a public execution Plan. Single Agents track analysis, execution, verification, and reporting; Team Runs track the six real role WorkItems. Plan snapshots contain public status and bounded summaries only, never private reasoning or full model prompts.

## Commands

```text
hermit run [--workspace PATH] [--config FILE] [--output human|json] "TASK"
hermit resume [--workspace PATH] [--config FILE] [--output human|json] SESSION-ID
hermit status [--workspace PATH] [--output human|json] SESSION-ID
hermit context [--workspace PATH] SESSION-ID
hermit clean [--workspace PATH] --older-than 7d
hermit config validate [--config hermit.toml] [--output human|json]
hermit version
```

JSON mode emits one JSON object per line for runtime events, making it safe to consume from scripts. Exit codes are stable: `0` success, `1` runtime failure, `2` usage error, `3` configuration error, and `130` cancellation/timeout.

## Configuration

See [hermit.example.toml](hermit.example.toml) and [model provider documentation](docs/model-providers.md). API keys may be supplied by `model.api_key`, but an environment variable named by `model.api_key_env` is strongly preferred. Keys, Authorization headers, cookies, passwords, common tokens, and private-key blocks are redacted from logs.

## Local Web debug

```bash
export OPENAI_API_KEY='...'
docker compose up --build -d
open http://127.0.0.1:8787
```

The Compose port is published only on loopback. The page selects company, access path, model, and either a single Agent or Personal Agent Team when creating a Session, then supports continued conversation, team activity, event replay, cancellation, and interrupted-run recovery. API keys remain server-side. Codex Plan imports an existing Codex CLI login from the host's `${HOME}/.codex` read-only mount. See [local Web and Docker guide](docs/web-debug.md).

The Web console is a single embedded React application. Go serves only declared
SPA route shapes and content-hashed assets; unknown API paths, malformed paths,
traversal attempts, and missing assets fail closed. See the
[React frontend architecture and debugging guide](docs/ai/react-frontend.md).

Configured plugins are opt-in:

```toml
[[plugins.process]]
name = "python-echo"
command = "python3"
args = ["examples/plugins/python-echo/plugin.py"]
enabled = true
```

Discovered tools are exposed as `plugin.python-echo.echo`.

## Security

All built-in file operations are constrained to the resolved workspace. Absolute paths, `..` traversal, Windows drive paths, symlink escapes, credential-like files, `.git` writes, and `.gohermit` writes are rejected. Shell execution uses a narrow read/build allowlist; other commands return `permission_required` or `blocked` and are not run in non-interactive mode.

External plugins are separate processes and form an explicit trust boundary. Read-only team roles only receive plugin tools declared `read` and non-mutating, but the plugin process itself still has its operating-system privileges. Only configure plugins you trust.

## Documentation

- [AI documentation and low-token reading index](docs/ai/README.md)
- [AI context and code map](docs/ai/context.md)
- [React frontend architecture and debugging](docs/ai/react-frontend.md)
- [Agent Harness quick reference](docs/ai/harness.md)
- [Personal Agent Team quick reference](docs/ai/team.md)
- [Live Plan quick reference](docs/ai/plan-mode.md)
- [Current v0.5 implementation handoff](docs/ai/handoff-v0.5.md)
- [Next development plan](docs/ai/next-development-plan.md)
- [Architecture](docs/architecture.md)
- [Project structure](docs/project-structure.md)
- [Development rules](docs/development-rules.md)
- [Context management](docs/context-management.md)
- [Session storage](docs/session-storage.md)
- [Plugin protocol](docs/plugin-protocol.md)
- [Model providers](docs/model-providers.md)
- [Local Web and Docker debugging](docs/web-debug.md)
- [Security model](docs/security-model.md)
- [Testing](docs/testing.md)
- [Roadmap](docs/roadmap.md)
