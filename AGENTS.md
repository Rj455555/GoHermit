# GoHermit agent entrypoint

This file stays at the repository root because coding agents discover `AGENTS.md` automatically while walking the workspace. Keep it short: detailed AI-only material belongs in `docs/ai/`.

## Read order

1. Read `docs/ai/context.md`.
2. For Electronic Employees, Tasks, or Team assignments, read
   `docs/ai/employees.md`.
3. Read `docs/ai/handoff-v0.7.md` for the shipped baseline and accepted risks.
4. Read the target package and its `_test.go` files.
5. Open only the topic document selected by the map in `docs/ai/context.md`.
6. For planned work, read `docs/ai/next-development-plan.md`.

Do not load all documentation by default.

## Non-negotiable rules

- Keep Agent Core presentation-free; it emits structured events.
- Keep every loop, request, tool, process, output, log, and checkpoint bounded and cancellable.
- Never weaken workspace, traversal, symlink, shell, credential, or plugin safety checks.
- Never persist secrets, private reasoning, stream chunks, full prompts/requests, or unbounded output.
- Prefer synchronous standard-library code; document every new dependency and protocol change.
- Preserve `%w` error chains, strong internal types, and failure-path tests.
- Multi-agent work must follow ADR 0008: bounded structured Handoffs, one workspace writer, explicit budgets, and single-owner/local-only operation. Do not add a public daemon, telemetry, auto-push, or speculative frameworks.
- Live Plan work must follow ADR 0009: public bounded phase state only, one current step, monotonic revisions, and no private reasoning or fabricated progress.
- Do not rewrite unrelated changes or bypass tool policy through shell commands.
- EmployeeTask execution must reuse Session/Run truth and Session SSE. Prepare
  creates no Run; only explicit Start may execute.
- Hidden Team Worker Sessions are internal recovery evidence. Every public
  Session/Run/Plan/Approval/message/event entrypoint must return not found
  before reading or mutating them.

## Required verification

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
pnpm test:e2e
docker compose config
```

Before handoff, review the diff and secrets, update affected documentation, use `docs/ai/handoff-template.md`, and report every skipped check or incomplete feature.
