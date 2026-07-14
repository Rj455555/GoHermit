# GoHermit

GoHermit is a lightweight, local-first AI coding-agent runtime written in Go. It reads a workspace, calls an OpenAI-compatible model, executes bounded tools, runs tests, and persists auditable sessions that can be resumed after interruption.

GoHermit is not a hosted service, a multi-agent orchestrator, a workflow engine, or a port of Hermes/OpenClaw. Its provider catalog adapts Hermes's canonical-provider, display-group, and auth-type split while keeping a small provider-neutral Go core.

## Status

The main branch is now `0.2.0-dev`. It adds OpenAI Codex Plan and direct API paths, DeepSeek and Qwen providers, selectable single-agent profiles, durable multi-turn Sessions, verified project memory, plus a local-only Web console and Docker packaging. It remains single-user and foreground; there is no daemon, telemetry, automatic Git push, or cloud deployment.

## Build and install

Go 1.24 or newer is required.

```bash
go test ./...
go build -o hermit ./cmd/hermit
go build -o hermit-web ./cmd/hermit-web
install -m 0755 hermit "$HOME/.local/bin/hermit"
```

GoHermit has one third-party dependency: `github.com/BurntSushi/toml`, used for strict TOML decoding. The standard library has no TOML parser; replacing it with a private partial parser would be less interoperable and harder to maintain. The library is BSD-licensed, small, mature, and is only on the configuration-loading path.

## Quick start

```bash
cp configs/codex.toml hermit.toml       # or deepseek.toml / qwen.toml
export OPENAI_API_KEY='...'
hermit config validate
hermit run --workspace /path/to/project "inspect the project and fix failing tests"
```

Sessions are written to `.gohermit/sessions/<session-id>/` in the workspace. The Web console keeps one Session across follow-up messages; every message creates a bounded, verified Run.

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

The Compose port is published only on loopback. The page selects company, access path, model, and Agent when creating a Session, then supports continued conversation, event replay, cancellation, and interrupted-run recovery. API keys remain server-side. Codex Plan imports an existing Codex CLI login from the host's `${HOME}/.codex` read-only mount. See [local Web and Docker guide](docs/web-debug.md).

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

External plugins are separate processes and form an explicit trust boundary. Only configure plugins you trust: the operating system, not GoHermit, ultimately controls what their process can access.

## Documentation

- [AI documentation and low-token reading index](docs/ai/README.md)
- [AI context and code map](docs/ai/context.md)
- [Agent Harness quick reference](docs/ai/harness.md)
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
