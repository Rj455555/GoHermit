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

## Task Board workbench

The Task Board is one shared implementation in
`web/src/features/tasks/board/` (`TaskBoardGrid`, `TaskBoardCard`,
`useTaskBoard`). Both `/tasks?view=board` and the Dashboard render it through
thin wrappers; there is no read-only dashboard fork and no second board state
machine. The whole card is the primary activation target: a Task with an
authoritative `session_id` deep-links to `/agent/sessions/{sessionID}`, a Task
without one opens `/tasks/{taskID}`, and a Note opens its own detail modal —
Notes never create Sessions or Runs. Column moves go through
`PUT /api/task-board/cards/{id}` (Notes move `column_id`/`rank` only).
Dragging is pointer-based (6px threshold, `elementFromPoint` column
hit-testing, 300ms post-drag click suppression) so real mouse gestures and
plain clicks coexist on the same card. Dropping a Task onto `in_progress` is
state-gated: `queued`/`prepared` open the explicit Start confirmation,
`interrupted` opens it for Resume, and every other state is rejected with a
localized toast and no mutation. The confirmation modal loads the
authoritative Task first (spinner + disabled confirm while loading; load
failure closes the modal, toasts the real error, and refetches the board).
Any mutation failure refetches the authoritative board projection. On
desktop (≥1024px) the navigation Sider auto-collapses to its 68px rail below
1280px and the grid fits all visible business columns with `minmax(0,1fr)`
tracks and no horizontal scroll; below 1024px the board container scrolls
horizontally with scroll snap while the page body never overflows.

## Employee Loop workbench

The primary Loops surface is contract-first: cards summarize When / Does / You
get, and the short create path asks only for Employee, name, goal, and optional
daily time. The detail page separates the generated `LOOP.md` contract,
bounded runtime state, and Invocation logs. Model, Team, policy, budget, and
verification controls remain under Advanced settings. Employee details expose
only Loops whose authoritative `employee_id` matches the current Employee.

The browser never schedules or executes work. It reads
`/api/loops/{id}/runtime` and existing Invocation/Session/SSE projections; the
Go control plane remains the single owner of dispatch and recovery.

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

Employee mutations have an additional DTO boundary: the UI reads
`project_count` as a server-owned projection but the create/update wrappers
remove it before strict Employee requests. Do not send list/detail-only
projection fields back through mutations.

Employee creation opens with a name-only quick path. It immediately persists a
valid Employee using the current ready model/Agent, current Workspace, empty
Skill/Knowledge selections, disabled Memory, pending role text, and a
conservative read-only/no-network policy. It does not run Dry Run, create a
Task, or start execution. The detail page is the progressive configuration
surface for finishing the role, model, Skills, Knowledge, Memory, project
policy, and budget. The full nine-step workflow remains available behind an
explicit advanced-setup action.

The advanced Employee wizard can generate a local, deterministic role draft
from a short Owner brief and one of the developer, researcher, operations, or
writer presets. It fills identity, charter, responsibilities, boundaries, and
a conservative permission ceiling, then lets the Owner jump to the policy
review.
Employee display names and job titles remain Unicode. The separate Store ID is
bounded ASCII because it participates in fail-closed storage paths; when an
Owner enters a non-path-safe value, the identity step previews and generates a
safe ID before any Employee or ProjectBinding mutation. The create boundary
repeats this normalization defensively so a skipped optional step cannot cause
a late server rejection.
This helper does not invoke a model, persist a hidden draft, bind Skills, add
Knowledge, or create an Employee until the ordinary reviewed mutation runs.

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

- Employee detail/create responses use the strict Go DTO rather than the list
  summary projection. The decoder accepts only Go's canonical omission of
  empty optional slices and derives `project_count` from `project_bindings`;
  the deterministic browser fixture must preserve that response shape.
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
