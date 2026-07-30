# GoHermit agent entrypoint

This file stays at the repository root because coding agents discover `AGENTS.md` automatically while walking the workspace. Keep it short: detailed AI-only material belongs in `docs/ai/`.

## Read order

1. Read `docs/ai/context.md`.
2. For Electronic Employees, Tasks, or Team assignments, read
   `docs/ai/employees.md`.
3. For the React Workbench, routing, API/SSE state, or Web build, read
   `docs/ai/react-frontend.md`.
4. For Employee-owned recurring work, read `docs/ai/employee-loops.md`.
5. Read `docs/ai/handoff-v0.7.md` for the shipped baseline and accepted risks.
6. Read the target package and its `_test.go` files.
7. Open only the topic document selected by the map in `docs/ai/context.md`.
8. For planned work, read `docs/ai/next-development-plan.md`.

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
- Employee Loops may schedule EmployeeTasks, but must not introduce a second
  execution state machine, Tool lifecycle, Verifier, recovery journal, or SSE
  stream. Mutable state/logs stay outside generated `LOOP.md`.
- Hidden Team Worker Sessions are internal recovery evidence. Every public
  Session/Run/Plan/Approval/message/event entrypoint must return not found
  before reading or mutating them.
- The React Workbench is the only Web frontend. Keep navigation in the URL,
  execution truth in server projections, and one Session-keyed SSE connection;
  do not add browser-side execution state or a second static root.

## Required verification

```bash
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm test
pnpm test:coverage
pnpm build
git diff --exit-code -- internal/web/assets/dist
pnpm test:e2e
test -z "$(gofmt -l $(git ls-files '*.go'))"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
docker compose config
bash internal/evals/docker_acceptance.sh
```

Before handoff, review the diff and secrets, update affected documentation, use `docs/ai/handoff-template.md`, and report every skipped check or incomplete feature.

## Local deployment completion

Every accepted product update must finish by rebuilding and replacing the
Mac mini's local Docker Compose service. Verify the new container identity,
healthy status, `/api/health`, `/api/info`, and the real Workbench on port
`8787`. A commit, push, or green CI run alone is not a completed local
handoff. Preserve mounted data and never use global Docker prune commands.
