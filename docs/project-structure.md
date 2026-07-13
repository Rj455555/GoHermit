# Project structure

| Path | May contain | Must not contain |
|---|---|---|
| `cmd/hermit` | process setup, signals, exit | domain logic, provider/tool implementations |
| `internal/app` | flags, dependency assembly, event rendering | agent decisions, unrestricted file access |
| `internal/agent` | turns, stop policy, tool/model loop | terminal printing, vendor wire types |
| `internal/model` | neutral types, provider implementation | workspace or CLI behavior |
| `internal/contextmgr` | layers, budgets, summaries | network/storage side effects beyond reading declared inputs |
| `internal/tool` | interface, registry, executor | built-in-specific policy |
| `internal/tool/builtin` | controlled workspace tools | session internals or unrestricted shell |
| `internal/session` | session schema and lifecycle | model/vendor payloads |
| `internal/storage` | atomic files, redaction, log rotation | task semantics |
| `internal/policy` | permission classification | command execution |
| `internal/plugin` | process supervision and tool adapters | core filesystem/Git/session implementations |
| `protocol/plugin/v1` | stable JSON wire structures | process management |
| `prompts` | reviewed prompt source | secrets or generated runtime state |
| `examples/plugins` | zero-dependency fixtures | production-only dependencies |
| `docs/adr` | accepted durable decisions | transient implementation notes |

Package dependencies must follow domain ownership and remain acyclic. Interfaces belong to the consumer that needs substitution. Do not create a new package for a few unrelated helpers; create one only when it owns a coherent domain rule, has multiple files/consumers, or isolates a real external boundary.

The unusual package name `contextmgr` avoids collision with the Go standard-library `context` package while preserving the context-management boundary.
