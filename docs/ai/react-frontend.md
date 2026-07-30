# React frontend architecture

GoHermit has one Web frontend: a React + TypeScript application built by Vite
and embedded into the Go Web binary. This document is the entry point for Web
architecture, routing, localization, state ownership, API/SSE boundaries,
building, testing, and debugging.

The visual source of truth is `web/DESIGN.md`. It defines the Workbench palette,
layout density, navigation dimensions, form behavior, responsive rules, and banned
generic dashboard patterns. Read it before changing shared CSS or introducing a new
resource page.

## Source and serving layout

```text
web/                              React source, tests, and Vite configuration
tests/e2e-react/                  deterministic browser fixture and parity tests
tests/e2e-docker/                 browser acceptance against the Go container
internal/web/assets/dist/         committed content-hashed production build
internal/web/server.go            API routes and fail-closed React asset handler
```

`//go:embed assets/dist` is the only static embed tree. `Server.New` reads the
embedded `index.html` and serves content-hashed `/assets/**` files with their
real content types and immutable caching. There is no legacy static root,
preview URL, `/dist/**` alias, CDN, external font, or second frontend.

## Routes and fallback ownership

Go returns `index.html` only for these declared shapes:

- `/dashboard`
- `/employees`, `/employees/{employeeID}`
- `/tasks`, `/tasks/{taskID}`
- `/agent`, `/agent/sessions/{sessionID}`
- `/loops`, `/loops/{loopID}`
- `/loops/{loopID}/invocations/{invocationID}`
- `/settings`

`GET /` redirects to `/dashboard`. A resource missing inside a legal detail
shape is rendered by React as a localized resource Not Found. Unknown top-level
paths, illegal shapes, extra segments, extension-bearing misses, `/dist/**`,
unknown `/api/*`, traversal, encoded separators, encoded traversal, controls,
and malformed paths are Go/API 404 responses and never React HTML.

React Router owns in-app navigation, direct-load restoration, refresh, and
back/forward behavior. Path and query parameters are the navigation truth.

## Localization and literal content

Bundled `zh-CN` and `en-US` resources live in `web/src/i18n`. Chinese is the
default; switching language updates the document language, title, visible
labels, status metadata, and accessible names immediately. Only a validated
locale preference is persisted.

UI metadata such as known roles, states, event types, and statuses is
localized. User messages, model output, Employee content, Tool names and
summaries, paths, code, JSON, IDs, Plan content, and WorkItem content remain
byte-for-byte authoritative and are not translated.

## State ownership

- The URL owns selected resources, tabs, filters, and history restoration.
- React Context owns only shell preferences, toast, dialog, and mobile-drawer
  presentation state.
- `localStorage` is limited to validated locale/navigation preferences and the
  Session SSE high-water key `gohermit.ui.sseSequence.{sessionId}`.
- API responses own Employee, Task, Session, Run, Plan, Tool, Verification,
  Approval, Loop, Invocation, and readiness projections.
- Mutations bind to current server IDs/revisions and refresh authoritative
  projections. The browser does not implement a second execution state machine.

Delayed resource requests carry an AbortSignal and/or owner request epoch.
Results, navigation, and Toast details are discarded after their owning route
changes.

## Session SSE registry

The EventSource registry key is the Session ID. One Session has at most one
underlying `/api/sessions/{sessionID}/events` connection, shared by Agent, Task,
and Loop consumers. Subscribers may provide a Run ID filter; filtering changes
only the subscriber projection, not the connection or sequence ownership.

The durable sequence high-water is Session-scoped. Initial loading:

1. reads the authoritative Session projection, including
   `next_event_sequence`;
2. accepts the stored high-water only when it is a safe nonnegative integer no
   greater than that frontier;
3. connects with `after=<high-water>`, or safely uses `after=0` after a missing,
   corrupt, negative, fractional, unsafe, or frontier-ahead value;
4. uses SSE only for bounded incremental presentation and authoritative
   projection refresh.

StrictMode cleanup/remount uses deferred final disposal so it cannot create two
persistent connections. The last subscriber closes the source. Explicit fatal
reconnect preserves the Session high-water, subscribers, and Run filters while
creating one replacement source. `model_delta` never enters Activity; each
subscriber has an independent 256 KiB streaming buffer and terminal events
refresh the authoritative Session.

## API trust boundary

`web/src/api` accepts only same-origin relative `/api/**` paths. It forwards
cancellation, performs fatal UTF-8 decoding, validates DTOs and enums, bounds
collections, and exposes sanitized error categories rather than raw response
bodies. Default JSON responses are capped at 1 MiB. Exact Session detail reads
are capped at 16 MiB, with Session record arrays independently bounded.

Session list decoding intentionally accepts an entirely empty legacy
`selection` object because older stored Sessions predate fixed provider/model
selection. Partially populated selections remain corrupt and fail closed.
Dashboard history and Agent Session history are supporting projections: their
load failures must not hide authoritative `/api/info` readiness or the current
Workspace.

Mutations are not automatically retried. Conflict, offline, and generic
failures use localized sanitized feedback. Credentials and API keys are
transient inputs sent to the existing server endpoints; they are never stored
in browser state or storage.

## Build and deterministic artifacts

From the repository root:

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
pnpm test:e2e
```

Vite empties `internal/web/assets/dist`, emits content-hashed JS/CSS, disables
production sourcemaps, and emits no timestamp, absolute workspace path, or
random build ID. The generated distribution is committed so ordinary Go builds
remain self-contained; CI rebuilds it and fails on byte drift.

The Dockerfile builds the frontend with Node 22/Corepack/pnpm 11.9.0, copies
only its generated distribution into the Go build stage, and copies only the
two stripped Go binaries into a minimal Alpine Git runtime. The runtime
contains Git and CA certificates required by GoHermit, but no Go, Node, npm,
pnpm, Corepack, cache, `node_modules`, package manifest, lockfile, or frontend
source.

The Docker build context is deny-by-default. `.dockerignore` re-includes only
the frontend manifests and `web/` source plus `go.mod`, `go.sum`, `cmd/`,
`internal/`, and `protocol/`. The Go stage copies those roots explicitly; it
never uses a repository-wide `COPY`. Private workspace tool state such as
`.claude/`, `.codegraph/`, `.cursor/`, `.gemini/`, `.mcp.json`, `.gohermit/`,
and `sandbox/` is excluded before the context is sent to the builder.

## Testing and debugging

- `pnpm test` and `pnpm test:coverage`: Vitest and Testing Library.
- `pnpm test:e2e`: the complete deterministic React browser suite.
- `pnpm test:e2e:docker`: browser acceptance against an already running
  acceptance container.
- `go test ./internal/web -count=1`: embedded assets, declared routes, API
  isolation, aliases, content types, and encoded-path security.
- `bash internal/evals/docker_acceptance.sh`: isolated Compose project and port,
  image-content inspection, browser checks, health, no-cache rebuild, and
  byte-identical persistent Employee/Task/Knowledge/Memory/Session/Loop data.

For source-level browser development, run the Vite workspace directly:

```bash
pnpm --filter @gohermit/web dev
```

For the real embedded boundary, build and start the Go server:

```bash
pnpm build
go run ./cmd/hermit-web -listen 127.0.0.1:8787 \
  -workspace ./sandbox -config ./configs/codex.toml
```

For an isolated container acceptance run, use
`GOHERMIT_ACCEPTANCE_PORT=<unused-port> bash internal/evals/docker_acceptance.sh`.
The acceptance project is uniquely named and never replaces the normal Compose
project or its persistent volume.
