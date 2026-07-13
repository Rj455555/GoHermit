# GoHermit

GoHermit is a lightweight, local-first AI coding-agent runtime written in Go. It reads a workspace, calls an OpenAI-compatible model, executes bounded tools, runs tests, and persists auditable sessions that can be resumed after interruption.

GoHermit is not a hosted service, a multi-agent orchestrator, a workflow engine, or a port of Hermes/OpenClaw. Version 0.1.0 deliberately focuses on one reliable loop: inspect, edit, test, continue, checkpoint, and resume.

## Status

Version 0.1.0 is an experimental but end-to-end usable release. It includes the CLI, single-agent loop, OpenAI-compatible Chat Completions provider, built-in tools, context budgeting, file-based sessions, and stdio JSON-RPC plugins. It does not provide a TUI, web UI, background daemon, telemetry, automatic Git push, or deployment.

## Build and install

Go 1.24 or newer is required.

```bash
go test ./...
go build -o hermit ./cmd/hermit
install -m 0755 hermit "$HOME/.local/bin/hermit"
```

GoHermit has one third-party dependency: `github.com/BurntSushi/toml`, used for strict TOML decoding. The standard library has no TOML parser; replacing it with a private partial parser would be less interoperable and harder to maintain. The library is BSD-licensed, small, mature, and is only on the configuration-loading path.

## Quick start

```bash
cp hermit.example.toml hermit.toml
$EDITOR hermit.toml                     # set model.model
export OPENAI_API_KEY='...'
hermit config validate
hermit run --workspace /path/to/project "inspect the project and fix failing tests"
```

Sessions are written to `.gohermit/sessions/<session-id>/` in the workspace.

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

See [hermit.example.toml](hermit.example.toml). API keys may be supplied by `model.api_key`, but an environment variable named by `model.api_key_env` is strongly preferred. Keys, Authorization headers, cookies, passwords, common tokens, and private-key blocks are redacted from logs.

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
- [Next development plan](docs/ai/next-development-plan.md)
- [Architecture](docs/architecture.md)
- [Project structure](docs/project-structure.md)
- [Development rules](docs/development-rules.md)
- [Context management](docs/context-management.md)
- [Session storage](docs/session-storage.md)
- [Plugin protocol](docs/plugin-protocol.md)
- [Security model](docs/security-model.md)
- [Testing](docs/testing.md)
- [Roadmap](docs/roadmap.md)
