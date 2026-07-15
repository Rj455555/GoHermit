# GoHermit agent entrypoint

This file stays at the repository root because coding agents discover `AGENTS.md` automatically while walking the workspace. Keep it short: detailed AI-only material belongs in `docs/ai/`.

## Read order

1. Read `docs/ai/README.md` and `docs/ai/context.md`.
2. Read the target package and its `_test.go` files.
3. Open only the topic document selected by the map in `docs/ai/context.md`.
4. For planned work, read `docs/ai/next-development-plan.md`.
5. For Owner Profile or multi-agent work, read `docs/ai/team.md`.
6. For Live Plan, checklist, progress events, or Plan recovery work, read `docs/ai/plan-mode.md`.

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

## Required verification

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
```

Before handoff, review the diff and secrets, update affected documentation, use `docs/ai/handoff-template.md`, and report every skipped check or incomplete feature.
