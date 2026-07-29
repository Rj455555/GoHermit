# GoHermit React Workbench / i18n Migration Plan

> The Owner reapproved this plan at baseline commit `c4074a2963e0b210804d08b2bfcab5b1e010762f` and authorized Phase 1 only. Phase 1 is now complete and awaiting its approval gate; Phases 2–5 remain unauthorized.

## 0. Gate, baseline, and audit evidence

- Audit date: 2026-07-29 (Asia/Shanghai).
- Repository: `/Users/yuanxin/Developer/GoHermit`.
- Remote: `https://github.com/Rj455555/GoHermit.git`.
- Expected baseline: `origin/main@e3bef0e6e531e6576035b163fa7a0eac3f73749c`.
- Fetched baseline: `origin/main@e3bef0e6e531e6576035b163fa7a0eac3f73749c`.
- Baseline decision: the expected and fetched SHAs match; no rebase onto an unknown baseline is needed.
- Working branch: `agent/react-workbench-i18n`, created from and currently at the fetched baseline.
- Existing tracked-worktree changes: none before this plan.
- Existing protected untracked files: `.claude/*`, `.cursor/mcp.json`, `.gemini/settings.json`, `.mcp.json`, `.codegraph/.gitignore`, multiple untracked files below `docs/`, and persistent `sandbox/.gohermit/**` records. They remain unmodified and unstaged. The post-switch untracked path-list SHA-256 was `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`.
- CodeGraph: a `.codegraph/` directory exists and the executable is available at `/Users/yuanxin/.local/bin/codegraph` (version 1.4.1 during this Gate). The earlier bare `codegraph` lookup failed because the non-interactive SSH `PATH` did not include `/Users/yuanxin/.local/bin`; it did not mean CodeGraph was absent. This revision re-audited Session SSE ownership and static routing with `/Users/yuanxin/.local/bin/codegraph explore "<question>"`. Future code audits must use that absolute path before grep/file reads. The index was not initialized, rebuilt, changed, or upgraded.
- Baseline verification: `/opt/homebrew/bin/go test ./internal/web -count=1` passed in 1.670s.
- Reapproval verification: the Markdown heading/fence/table/status structure check and `git diff --check` passed; `/opt/homebrew/bin/go test ./internal/web -count=1` passed again in 1.937s.
- Phase 1 toolchain evidence: `/opt/homebrew/bin/brew` installed Homebrew `node@22` once because it was absent. Non-interactive SSH used `PATH=/opt/homebrew/opt/node@22/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`. Node command `/opt/homebrew/opt/node@22/bin/node` resolves to `/opt/homebrew/Cellar/node@22/22.23.1/bin/node` (`v22.23.1`); Corepack command `/opt/homebrew/opt/node@22/bin/corepack` resolves to `/opt/homebrew/Cellar/node@22/22.23.1/lib/node_modules/corepack/dist/corepack.js` (`0.34.6`); pnpm command `/opt/homebrew/opt/node@22/bin/pnpm` resolves to `/opt/homebrew/Cellar/node@22/22.23.1/lib/node_modules/corepack/dist/pnpm.js` (`11.9.0`). No alternative installer or global npm install was used.

The original plan Gate changed only this document. The reapproved Phase 1 changes only the paths listed in its authorized-files section; no Phase 2–5 product migration, CI/Docker cutover, API/domain schema change, or persisted-data change is included.

## 1. Non-negotiable migration invariants

1. Session/Run/Plan/Approval/Verification/Event/Tool and Employee/EmployeeTask/Loop stores remain the only execution and persistence truth. React only renders their API projections.
2. The migration does not change backend domain semantics, API JSON field names, API enums, Session schema, Employee Store schema, Agent Runtime, hidden Worker Session access rules, same-origin rules, or request limits.
3. EmployeeTask creation remains queued-only; only explicit Start may create/bind a Run. Prepare never starts execution.
4. Hidden Worker Sessions continue to return generic not-found responses on every public Session, SSE, Run, Plan, Approval, message, and event path.
5. No credential, token, prompt, message, model output, raw tool arguments/output, private reasoning, or unbounded event payload is stored in browser persistence or logged by the frontend.
6. No analytics, telemetry, external fonts, remote images, CDN scripts, or external data sinks are introduced.
7. The existing frontend remains the served production UI until the React routes reach functional and E2E parity. It is removed after the cutover; two served frontends are never a steady-state outcome.
8. Every phase is separately implemented, verified, committed, pushed, reported, and stopped for Owner approval. Work on the following phase must not begin automatically.

## 2. Current frontend audit

### 2.1 Go static-resource and embed entry points

| Area | Current implementation | Migration consequence |
|---|---|---|
| Embed declaration | `internal/web/server.go`: `//go:embed assets/*` into `embed.FS` | React output remains below `internal/web/assets/`. Phase 1 first proves whether this existing pattern already recursively embeds `dist`; it must not change the declaration merely for form. |
| Embedded root | `fs.Sub(assets, "assets")` | Final root becomes `fs.Sub(assets, "assets/dist")`. |
| Static handler | `http.FileServer(http.FS(root))` | Replace with a bounded SPA handler after parity. |
| Static route | `mux.Handle("GET /", s.static)` after all explicit API routes | Unknown GET paths currently reach the file server and deep routes return 404. The new handler must explicitly reject `/api` and `/api/*` before any SPA fallback. |
| Security wrapper | `securityHeaders(mux)` | Preserve CSP (`default-src`, `script-src`, `style-src`, `connect-src`, `img-src`, `frame-ancestors`), Referrer-Policy, X-Content-Type-Options, and existing same-origin mutation checks. |
| Legacy assets | `index.html`, `styles.css`, `app.js`, `employees.js`, `tasks.js`, `loops.js` | Keep until React parity and cutover; remove in Phase 5. |
| Entrypoints | `cmd/hermit-web/main.go` → `internal/web.New(...)` → `Server.Handler()` | No Node server is added to runtime. |

At final cutover, `fs.Sub(assets, "assets/dist")` becomes the served root. The embed declaration changes only if a Phase 1 test proves the current `//go:embed assets/*` pattern does not recursively include `dist/index.html` and its hashed assets. Phase 1 does not change `Server.New()` away from the legacy `assets/` static root.

### 2.2 Current pages and interaction surfaces

The current browser UI is one DOM document. It has no URL router; `switchWorkbenchView` hides/shows sections and persists `gohermit.view`.

| Current surface | Current state/behavior | React destination |
|---|---|---|
| Dashboard | Loop/Invocation-derived readiness counters and recent invocation list | `/dashboard`, `DashboardPage` |
| Employees list | State filter, cards, saved selection | `/employees`, `EmployeesPage` |
| Employee detail | Overview, Settings, Skills, Knowledge/Citations, Memory Candidates/Facts, Projects, Tasks, Activity; readiness and lifecycle actions | `/employees/:employeeId`, `EmployeeDetailPage` |
| Employee wizard | 9 controlled-by-DOM steps: identity, model, charter, skills, knowledge, memory, project, policy/budget, review/readiness | Nested `EmployeeWizard` on `/employees` |
| Tasks inbox | Employee/project/state/time filters and Task creation | `/tasks`, `TasksPage` |
| Task detail | explicit Start/Cancel/Resume, Session-backed Plan, Tools, Verification, Timeline, Approvals, pinned context, Artifacts | `/tasks/:taskId`, `TaskDetailPage` |
| Agent | Session list, Session creation, messages, Run state, Plan, Team/Mission, Approvals, Activity, composer | `/agent` and `/agent/sessions/:sessionId`, `AgentPage` |
| Loops | Definition list/editor/import, Dry Run, Team role mapping, verification recipe, Invocation history/start/cancel, Session-backed timeline | `/loops`, plus restoration routes described below |
| Settings | Right-side drawer containing Owner Profile/Facts, provider credentials, and Codex login status | `/settings`, `SettingsPage`; narrow layouts may still use panels inside the page |
| Connectivity | `/api/health` heartbeat, offline banner, disabled mutations, reconnect reload | `ConnectivityProvider` + banner shared by `AppShell` |
| Toast/confirm | One DOM toast and ad-hoc `window.confirm`/`window.prompt` patterns | `ToastRegion`, `ConfirmDialog`, controlled forms |

The displayed `V0.6 · LOOP WORKBENCH` label in `index.html` is a product-version residue. React will use:

- `zh-CN`: `GOHERMIT · 工作流`
- `en-US`: `GOHERMIT · LOOP WORKBENCH`

The Agent session list will be labeled “会话”/“Sessions”, never “任务”/“Tasks”.

### 2.3 Current API inventory

All listed endpoints are same-origin and retain their current method, path, request JSON, response JSON, size limits, status mapping, and backend handler.

#### Service, catalog, Owner, and configuration

| Method | Path | Frontend use |
|---|---|---|
| GET | `/api/health` | heartbeat/readiness |
| GET | `/api/info` | version, workspace, provider/access/model/Agent catalog, configured selection, active state |
| GET | `/api/owner` | Owner Profile/Facts |
| PUT | `/api/owner` | save Owner Profile |
| PUT | `/api/owner/facts/{id}` | create/update confirmed Owner Fact |
| DELETE | `/api/owner/facts/{id}` | forget Owner Fact |
| GET | `/api/projects` | current startup-configured Workspace only |
| GET | `/api/skills` | bounded Skill catalog |
| PUT | `/api/settings/providers/{provider}/api-key` | save API key server-side |
| DELETE | `/api/settings/providers/{provider}/credentials` | delete server-side credentials |
| POST | `/api/settings/providers/openai-codex/login` | start device-code login |
| GET | `/api/settings/logins/{session}` | poll login status |

#### Employees, context, and lifecycle

| Method | Path |
|---|---|
| GET, POST | `/api/employees` |
| GET, PUT | `/api/employees/{id}` |
| POST | `/api/employees/{id}/dry-run` |
| POST | `/api/employees/{id}/disable` |
| POST | `/api/employees/{id}/enable` |
| POST | `/api/employees/{id}/archive` |
| GET | `/api/employees/{id}/activity` |
| GET, PUT | `/api/employees/{id}/skills` |
| GET, POST | `/api/employees/{id}/knowledge` |
| POST | `/api/employees/{id}/knowledge/{sourceID}/refresh` |
| DELETE | `/api/employees/{id}/knowledge/{sourceID}` |
| GET | `/api/employees/{id}/memory` |
| GET | `/api/employees/{id}/memory-candidates` |
| POST | `/api/employees/{id}/memory-candidates/{candidateID}/accept` |
| DELETE | `/api/employees/{id}/memory-candidates/{candidateID}` |
| PUT | `/api/employees/{id}/memory/{factID}` |
| DELETE | `/api/employees/{id}/memory/{factID}` |

Project bindings remain embedded in the Employee resource; no separate projects mutation endpoint currently exists.

#### Employee Tasks

| Method | Path |
|---|---|
| POST, GET | `/api/employees/{id}/tasks` |
| GET | `/api/employee-tasks/{taskID}` |
| POST | `/api/employee-tasks/{taskID}/start` |
| POST | `/api/employee-tasks/{taskID}/cancel` |
| POST | `/api/employee-tasks/{taskID}/resume` |

#### Sessions, Runs, Plans, and Approvals

| Method | Path |
|---|---|
| POST, GET | `/api/sessions` |
| GET | `/api/sessions/{id}` |
| POST | `/api/sessions/{id}/runs` |
| GET (SSE) | `/api/sessions/{id}/events?after={sequence}` |
| POST | `/api/sessions/{id}/runs/{run}/cancel` |
| POST | `/api/sessions/{id}/runs/{run}/resume` |
| POST | `/api/sessions/{id}/runs/{run}/approve` |
| GET | `/api/sessions/{id}/approvals` |
| POST | `/api/sessions/{id}/approvals/{requestID}/decide` |
| POST | `/api/run` (legacy compatibility only; React must not adopt it) |

#### Loops and Team Template

| Method | Path |
|---|---|
| GET, POST | `/api/loops` |
| POST | `/api/loops/import` |
| GET, PUT | `/api/loops/{id}` |
| POST | `/api/loops/{id}/dry-run` |
| GET, POST | `/api/loops/{id}/invocations` |
| GET | `/api/loop-invocations/{id}` |
| POST | `/api/loop-invocations/{id}/cancel` |
| GET | `/api/team-template/export` |
| POST | `/api/team-template/import` |

### 2.4 Current SSE inventory and contracts

There is exactly one public event stream:

```text
GET /api/sessions/{sessionID}/events?after={sequence}
```

Server behavior to preserve:

- Verifies the public Session before subscribing, so hidden Worker Sessions stay inaccessible.
- Uses the larger of `after` and a valid `Last-Event-ID`.
- Subscribes by Session ID, replays persisted events after the sequence, then streams live committed events.
- Emits SSE `id` from the monotonic event sequence, named `event` from the existing event type, and JSON `data` from the existing event DTO.
- Suppresses events whose nonzero sequence is not greater than the connection high-water mark.
- Sends a keepalive comment every 15 seconds and unregisters the subscriber on context cancellation.
- Does not persist `model_delta` stream chunks.

Current browser consumers:

| Consumer | Isolation/recovery today | Gap to close |
|---|---|---|
| Agent (`app.js`) | one EventSource per selected Session; in-memory `lastSequence`; closes on Session switch | sequence resets to zero on reload; connection logic is duplicated and not StrictMode-aware |
| Task (`tasks.js`) | key is Task + Session; filters mismatched Run IDs; persists sequence; caps in-memory events at 200; closes on navigation | React must move connection/sequence ownership to Session while retaining Run filtering per subscriber |
| Loop (`loops.js`) | key is Invocation; persists sequence; reloads bound Session/Invocation; caps runtime events at 100 | no explicit duplicate check or Run filter; logic is duplicated |

The React implementation will continue to use Session SSE. It will not add Task SSE, Loop SSE, a browser Run state machine, or a browser recovery store.

### 2.5 Current localStorage inventory

| Key | Current purpose | Final strategy |
|---|---|---|
| `gohermit.view` | active pseudo-page (`agent`, `dashboard`, `employees`, `employee-tasks`, `loops`) | replace with URL routes; read once only if needed during migration, then remove |
| `gohermit.session` | selected Agent Session | replace with `/agent/sessions/:sessionId`; do not use as business truth |
| `gohermit.employee` | selected Employee | replace with `/employees/:employeeId` |
| `gohermit.employee-task` | selected EmployeeTask | replace with `/tasks/:taskId` |
| `gohermit.loop` | selected Loop Definition | replace with `/loops/:loopId` |
| `gohermit.loop.invocation` | selected Invocation | replace with `/loops/:loopId/invocations/:invocationId` |
| `gohermit.task-sse.{taskId}.{sessionId}` | Task SSE high-water sequence | migrate to the shared sequence-key format after compatibility tests |
| `gohermit.loop.sequence.{invocationId}` | Loop SSE high-water sequence | migrate to the shared sequence-key format after compatibility tests |

New UI-only keys:

| Key | Type/default |
|---|---|
| `gohermit.ui.locale` | `'zh-CN' \| 'en-US'`; default `zh-CN` |
| `gohermit.ui.navigationCollapsed` | strict serialized boolean; default `false` |
| `gohermit.ui.sessionSidebarCollapsed` | strict serialized boolean; default `false` |
| `gohermit.ui.sseSequence.{sessionId}` | finite nonnegative safe integer only; Session-owned high-water marker, never event content |

Every localStorage read will treat the value as untrusted input, whitelist/parse it, and fall back safely. No API response, prompt, message, credentials, Tool data, or model output is persisted.

### 2.6 Existing test contracts

Current Playwright suites and behaviors must be retained, not deleted:

- `approvals.spec.ts`: pending approval details/countdown, correct decision body, cleared/empty states.
- `review-plan.spec.ts`: review-first Plan survives refresh and starts only after explicit approval.
- `employees.spec.ts`: complete pinned Skill identity, server readiness, Employee settings/tabs, Knowledge/Citations, Memory decisions, Projects, Tasks, lifecycle, invalid Skill configuration.
- `tasks.spec.ts`: queued-only creation, explicit Start/Cancel/Resume, Session execution truth, Plan/Tool/Verification/Approval, SSE after-sequence recovery, duplicate suppression, Task/Run isolation, close-on-navigation, native history after a stored sequence.
- `loops-workbench.spec.ts`: Team Role Employee mapping and model precedence, create/revise/import/Dry Run, blocked start, Invocation timeline restoration/cancellation, Dashboard/Agent/Settings navigation.

Existing stable `data-testid` values (navigation, Employee wizard/detail, Task controls/runtime, Loop controls, and generated resource rows) will be retained through the React rewrite. Accessible role/name assertions will either run with `en-US` explicitly selected or use locale-independent test IDs; the new default-Chinese tests will not weaken the existing English behavior coverage.

## 3. Target React architecture

### 3.1 Repository and package-manager structure

The repository already declares `packageManager: pnpm@11.9.0` and has exactly one `pnpm-lock.yaml`. The migration will keep pnpm as the sole package manager and will not add `package-lock.json`, `yarn.lock`, or a Bun lockfile.

```text
web/
  package.json
  tsconfig.json
  tsconfig.app.json
  vite.config.ts
  eslint.config.js
  index.html
  src/
    main.tsx
    App.tsx
    api/
      client.ts
      decoders.ts
      errors.ts
      types.ts
    components/
      ConfirmDialog/
      EmptyState/
      ErrorState/
      PageHeader/
      StatusBadge/
      ToastRegion/
    features/
      dashboard/
      employees/
      tasks/
      agent/
      loops/
      settings/
    hooks/
      useDocumentTitle.ts
      useMediaQuery.ts
      usePersistentUIState.ts
      useSessionEvents.ts
    i18n/
      index.ts
      locale.ts
      resources/
        en-US.ts
        zh-CN.ts
    layouts/
      AppShell.tsx
      NavigationRail.tsx
      SessionSidebar.tsx
    routes/
      AppRoutes.tsx
      RouteErrorBoundary.tsx
      routeConfig.ts
    state/
      UIStateProvider.tsx
      uiReducer.ts
    styles/
      global.css
      tokens.css
    types/
    utils/
      guards.ts
      redaction.ts
  tests/
    setup.ts
internal/web/assets/dist/
  index.html
  assets/<content-hashed-js-css-and-icons>
```

Root `package.json` remains the repository command entrypoint and delegates to the `web` package. A `pnpm-workspace.yaml` may be added so root and `web` share one lockfile and one deterministic install.

### 3.2 Component and feature boundaries

```text
StrictMode
└─ BrowserRouter
   └─ I18nextProvider
      └─ UIStateProvider (UI preferences only)
         └─ ToastProvider
            └─ AppShell
               ├─ NavigationRail
               │  └─ LanguageSwitcher
               ├─ ConnectivityBanner
               └─ RouteErrorBoundary
                  └─ AppRoutes
                     ├─ DashboardPage
                     ├─ EmployeesPage
                     │  ├─ EmployeeCard
                     │  └─ EmployeeWizard
                     ├─ EmployeeDetailPage
                     ├─ TasksPage
                     │  └─ TaskList
                     ├─ TaskDetailPage
                     │  ├─ TaskDetail
                     │  ├─ RunPlan
                     │  ├─ ToolCallPanel
                     │  ├─ VerificationPanel
                     │  └─ ApprovalPanel
                     ├─ AgentPage
                     │  ├─ SessionSidebar
                     │  ├─ SessionTimeline
                     │  └─ Run/Plan/Approval/Team panels
                     ├─ LoopsPage
                     │  └─ LoopWorkbench
                     └─ SettingsPage
                        └─ SettingsPanel
```

Pages compose data loading, feature actions, and layout. API parsing stays in `api/`; SSE connection ownership stays in `useSessionEvents`; shared UI state contains only locale/sidebar/toast/dialog/connectivity preferences. Large monolithic `App.tsx` and cross-feature mutable singleton state are prohibited.

### 3.3 Routes and refresh restoration

| Route | Selection source and refresh behavior |
|---|---|
| `/` | `<Navigate replace to="/dashboard" />` |
| `/dashboard` | reload dashboard projections from Loop/Invocation APIs |
| `/employees` | load list; no hidden selection |
| `/employees/:employeeId` | validate/decode route ID, fetch that Employee and active detail tab; tabs use a whitelisted `?tab=` query |
| `/tasks` | load Task inbox and URL query filters |
| `/tasks/:taskId` | fetch EmployeeTask, then its bound Session/Approval projection |
| `/agent` | new-Session/selection state; Session list remains server-derived |
| `/agent/sessions/:sessionId` | fetch Session/messages, approvals, and bind SSE to the route Session |
| `/loops` | load definitions without an implicit detail selection |
| `/loops/:loopId` | fetch Definition/history |
| `/loops/:loopId/invocations/:invocationId` | fetch Invocation and bound Session; restore timeline |
| `/settings` | fetch Owner/Profile and provider status |
| declared route with a missing resource ID | Go returns `index.html`; React renders a localized resource Not Found without selecting a replacement |
| unknown top-level path, invalid route shape, or extra path segment | Go returns HTTP 404; it does not send React HTML |

Browser back/forward changes the actual selection. URL path/query is the navigation truth; localStorage is limited to the three UI preferences and an SSE high-water number. On a route ID mismatch/not-found, the page renders a localized recoverable `ErrorState` without selecting a different resource behind the Owner’s back.

React Router may retain a catch-all Not Found component for client-side navigation and defense in depth, but the Go Handler promises `index.html` only for the declared route shapes above. It does not broadly fall back arbitrary non-API URLs to React.

### 3.4 State ownership

| State class | Owner |
|---|---|
| Route, selected Employee/Task/Session/Loop/Invocation, filters/tabs worth sharing | React Router path/search params |
| Locale, global navigation collapsed, Agent session sidebar collapsed, toast/dialog/connectivity presentation | React Context + Reducer; only the three approved preferences persist |
| Form drafts and validation | local controlled component state; never persisted automatically |
| Employee, Task, Session, Run, Loop, Plan, Approval, Verification, Tool, Artifact data | server APIs; reload after mutations/events |
| SSE connection, in-memory event dedupe, sequence high-water mark | shared `useSessionEvents` registry and persistence keyed only by Session; optional Run filters belong to individual subscribers |

No Redux, Zustand, TanStack Query, or persistence framework is justified. Abortable effects plus small feature hooks are sufficient for the current single-owner/local-first surface.

### 3.5 Typed API client and DTO validation

`web/src/api/types.ts` will mirror current JSON names and enums exactly. DTO families include Health/Info/Catalog, Owner/Facts, Project, Skill/Binding, Employee/Revision/Readiness/Activity/Knowledge/Citation/Memory, EmployeeTask/Artifact, Session/Run/Message/Plan/Tool/Verification/Mission, Approval, Loop Definition/Dry Run/Invocation, Team Template, and Codex Login.

`client.ts` will:

- accept same-origin relative paths only;
- support caller-provided `AbortSignal` and distinguish abort from network/API failure;
- parse response JSON initially as `unknown`;
- call an endpoint-specific decoder/type guard from `decoders.ts`;
- throw a typed, sanitized `ApiError` for non-2xx or invalid bodies;
- never print raw request/response bodies or sensitive fields;
- preserve current methods, headers, request fields, status behavior, and content types.

Hand-written bounded validation helpers are preferred to adding a runtime schema dependency. External strings, arrays, objects, enums, IDs, sequence values, and optional fields are narrowed from `unknown`. Display translations map existing enum values to i18n keys without altering DTO values.

### 3.6 Session SSE hook

`useSessionEvents(sessionId, options)` will subscribe to a module-local connection registry whose ownership exactly matches the server:

- EventSource registry key: `sessionId`;
- sequence storage key: `gohermit.ui.sseSequence.{sessionId}`;
- exactly one underlying EventSource per Session, regardless of how many Runs or Agent/Task/Loop consumers observe it;
- every subscriber may provide an optional Run ID filter; the filter changes only that subscriber's projection and never the connection, sequence, replay, or reconnect boundary;
- events advance the shared Session high-water before per-subscriber Run filtering, so an event ignored by one projection cannot cause replay or connection divergence;
- reference counting and deferred cleanup keep the connection alive while any subscriber remains;
- switching a consumer to another Session releases its old subscription; the underlying old Session connection closes only when its final subscriber exits;
- React StrictMode mount → cleanup → remount reuses/cancels deferred disposal and leaves one persistent connection with a correct subscriber count;
- known event types and decoded objects only; event `session_id`, when present, must match the registry Session;
- each subscriber receives only events accepted by its optional Run filter, including a documented choice for Session-scoped events with no Run ID;
- drop non-safe, negative, non-integer, out-of-order, or duplicate sequences;
- preserve existing authoritative projections on disconnect and expose recoverable connection status;
- cap in-memory event summaries; do not store event bodies;
- persist only the numeric Session high-water;
- refresh authoritative Session/Task/Invocation projections on checkpoint/terminal/approval/plan events instead of building a browser execution state machine.

Initial load and reconnect contract:

1. Fetch and decode `GET /api/sessions/{id}` first. Its Session, Runs, Messages, Plan, Tool, and Verification fields are the authoritative render projection.
2. Read `gohermit.ui.sseSequence.{sessionId}` as untrusted input.
3. Accept it only if it is a finite safe nonnegative integer and does not exceed the authoritative `session.next_event_sequence` returned by `GET /api/sessions/{id}`. If it exceeds that Session frontier, remove the storage key and recover with `after=0`. No Run frontier is consulted.
4. Open the shared Session EventSource with `after=<savedSequence>` when the validated value exists; otherwise use `after=0`.
5. Advance and persist the high-water only from valid monotonic events belonging to that Session.
6. Native `Last-Event-ID` remains supported, and an explicit reconnect uses the current Session high-water.
7. A normal refresh with a valid saved sequence does not unconditionally replay from `after=0`.
8. SSE events provide incremental display hints and trigger authoritative projection refreshes; they never become a second Session/Run/Task/Loop state store.

CodeGraph confirms that the public `Session` DTO already exposes `NextEventSequence` as `next_event_sequence`, `GET /api/sessions/{id}` returns that Session field, and the Store maintains it as the current committed Session event frontier during commit and recovery. Phase 3 therefore validates the local high-water directly against `session.next_event_sequence`: the value must be a safe nonnegative integer no greater than that Session frontier. An overshoot removes the local key and reconnects with `after=0`. This uses the existing API and Session schema unchanged and never introduces a Run-level frontier.

### 3.7 i18n

- Initialize i18next with `supportedLngs: ['zh-CN', 'en-US']`, `lng: persisted-valid-locale || 'zh-CN'`, and `fallbackLng: 'zh-CN'`.
- Do not add a browser language detector and do not inspect `navigator.language`.
- Store only a whitelisted locale in `gohermit.ui.locale`.
- Update `<html lang>`, document title, navigation, controls, labels, placeholders, empty/error states, toast/confirm content, statuses, tooltips, `title`, and `aria-label` immediately on language change.
- Keep UI copy in typed, feature-namespaced resource modules. Add a test that both locale trees have the same leaf keys.
- Translate status/enum labels only at render time.
- Never pass user input, Employee names, Task prompts, Session messages, model responses, Tool output, paths, code/JSON, or internal Provider/Model/Agent/Skill IDs through `t()`.

### 3.8 Codex-style two-level sidebars

Global `NavigationRail`:

- desktop (`>900px`): 184px expanded, 56px collapsed; first visit expanded;
- expanded: GoHermit mark plus localized icon labels; collapsed: icons plus tooltip/title/accessible name;
- contains Dashboard, Employees, Tasks, Agent, Loops, Settings, LanguageSwitcher, and a Chevron toggle;
- preference key `gohermit.ui.navigationCollapsed`;
- active item derives from the current route, not a second view state.

Agent `SessionSidebar`:

- rendered only inside Agent routes;
- desktop: 292px expanded, 0 collapsed;
- expanded header owns a collapse button; collapsed Agent header always retains the expand button;
- preference key `gohermit.ui.sessionSidebarCollapsed`;
- collapsing moves focus to the surviving expand control when needed;
- buttons expose localized `aria-label`, `title`, and `aria-expanded`; native `<button>` supplies Enter/Space behavior.

Responsive behavior (`<=900px`):

- the Session sidebar becomes a modal drawer with an overlay;
- Escape and overlay click close it;
- focus moves into the drawer, is trapped while open, and returns to the trigger;
- background content is inert/hidden from assistive technology while the drawer is open;
- body horizontal overflow is prevented;
- the desktop collapsed preference is not overwritten by temporary mobile drawer state;
- returning above 900px restores the saved desktop preference.

Widths animate for 180ms (inside the required 160–200ms range). `prefers-reduced-motion: reduce` removes nonessential transitions.

### 3.9 Errors, dialogs, forms, and accessibility

- A route-level Error Boundary localizes failure to one page and offers Retry and safe navigation; the shell/navigation remain usable.
- `ErrorState` handles fetch/validation/not-found errors; `EmptyState` handles valid empty resources.
- `ConfirmDialog` replaces impactful action confirmations (archive, forget, delete credentials/knowledge, cancel); it restores focus and supports Escape.
- `ToastRegion` uses an accessible live region and never displays raw sensitive payloads.
- Complex Employee/Task/Loop/Settings forms remain controlled, use explicit field validation, and set errors by field plus a summary. Browser implicit validation is not the only gate.
- All interactive elements use semantic buttons/links, visible focus, accessible names, and keyboard operation.

## 4. Build artifacts, Go embed, and production serving

### 4.1 Selected artifact strategy

The requested recommended strategy is adopted:

1. React source lives in `web/`.
2. Vite builds directly to `internal/web/assets/dist/` with `emptyOutDir: true`, a root base path, deterministic content hashes, and no external asset URLs.
3. `dist/` is committed so `go build ./cmd/hermit-web` works from a clean clone without Node.
4. CI runs a clean pnpm install and rebuild, then fails if `git diff --exit-code -- internal/web/assets/dist` reports drift.
5. A source change and its generated artifact are committed together.
6. Sourcemaps are disabled for production unless the Owner separately approves their exposure.
7. Production artifacts must not embed build timestamps, absolute workspace paths, host-specific paths, random build IDs, or other nondeterministic metadata. Two builds from identical source, lockfile, tool versions, and configuration must produce byte-identical `dist` content.

### 4.2 Go SPA handler and security

The final Go handler will:

- serve existing files from the embedded `assets/dist` subtree with correct content types;
- serve `index.html` only for the declared React route shapes: `/`, `/dashboard`, `/employees`, `/employees/{employeeId}`, `/tasks`, `/tasks/{taskId}`, `/agent`, `/agent/sessions/{sessionId}`, `/loops`, `/loops/{loopId}`, `/loops/{loopId}/invocations/{invocationId}`, and `/settings`;
- let React render a localized resource Not Found when a declared detail route shape is valid but its Employee, Task, Session, Loop, or Invocation does not exist;
- return Go HTTP 404 for unknown top-level paths, invalid route shapes, extra path segments, and missing extension-bearing assets;
- accept GET/HEAD only for static/SPA content;
- return not found (never HTML) for `/api`, `/api/`, and every unmatched `/api/*` path;
- reject traversal/canonicalization anomalies and never concatenate an unchecked URL path to a filesystem path;
- preserve explicit API route precedence and existing security headers;
- set `index.html` to no-cache and allow hashed assets to use immutable caching;
- include Go tests for every declared deep-link shape, missing resources inside valid shapes, unknown top-level paths, extra segments, HEAD, hashed assets, extension-bearing misses, malformed/encoded paths, traversal attempts, unknown API paths, and content types.

React Router retains a catch-all Not Found component for in-app navigation and defense in depth, but Go uses the explicit route-shape matcher above and makes no broad arbitrary-URL fallback promise.

### 4.3 Docker multi-stage build

Planned stages:

1. `web-deps`/`web-build`: Node 22 image, Corepack-enabled pnpm 11.9.0, copy only package manifests/lockfile first, `pnpm install --frozen-lockfile`, then copy `web/` and build Vite.
2. `go-build`: current trusted Go builder, download `go.mod`/`go.sum`, copy repository, overwrite `internal/web/assets/dist` with the artifact from `web-build`, then build `hermit` and `hermit-web` with current CGO/trimpath/ldflags behavior.
3. runtime: run only the Go binaries and existing runtime OS dependencies. It contains no Node executable, pnpm/npm, frontend dependency tree, npm cache, or frontend source dependencies.

Compose remains loopback-bound by default, keeps existing UID/GID, volumes, secrets, healthcheck, dropped capabilities, and entrypoint. No Node service or published development port is added.

### 4.4 Reproducible build order

```text
clean clone
→ Corepack/pnpm 11.9.0
→ pnpm install --frozen-lockfile
→ pnpm typecheck
→ pnpm lint
→ pnpm test
→ pnpm build
→ verify committed dist is unchanged
→ Go tests/vet/build/cross-build
→ Playwright
→ docker compose config
→ Docker build and browser acceptance
```

The request’s generic `npm ci`/`npm run ...` checklist is implemented with the repository’s existing pnpm policy (`pnpm install --frozen-lockfile` and `pnpm ...`). Adding a `package-lock.json` solely to run `npm ci` would violate the single-lockfile requirement.

### 4.5 Phase 1 Node/pnpm preflight

After explicit Phase 1 approval, use:

```text
Homebrew Node 22
Corepack
pnpm 11.9.0
```

Procedure and stop conditions:

1. Resolve `/opt/homebrew/bin/brew` and use it preferentially. If it is absent or unusable, stop and report; do not choose another installer.
2. Install/activate Homebrew Node 22 only through Homebrew. Do not use `curl | sh`.
3. Use Node’s Corepack to prepare/activate the repository-declared `pnpm@11.9.0`. Do not run `npm install -g pnpm`.
4. Do not create `package-lock.json` or any second lockfile.
5. Explicitly configure the non-interactive SSH `PATH` for the resolved Homebrew Node, Corepack, and pnpm binary directories rather than assuming login-shell initialization.
6. Record the resolved absolute paths and real versions for Node, Corepack, and pnpm before changing frontend manifests.
7. If any Homebrew installation or Corepack activation step fails, stop Phase 1 and report the failure. Do not fall back to nvm, fnm, Volta, a downloaded installer, or another package manager without a new Owner decision.

## 5. Dependency decision

Runtime dependencies:

| Dependency | Reason |
|---|---|
| `react`, `react-dom` | required component/runtime target |
| `react-router-dom` | BrowserRouter, deep links, route params, navigation and error routing |
| `i18next`, `react-i18next` | required locale resources and live React binding |
| `lucide-react` | locally bundled, accessible navigation/Chevron/status icons; no CDN |

Development dependencies:

| Dependency | Reason |
|---|---|
| `typescript` | strict DTO/component checking |
| `vite`, `@vitejs/plugin-react` | deterministic production bundle and local development |
| `@types/react`, `@types/react-dom` | React TypeScript declarations |
| `eslint`, `typescript-eslint`, `eslint-plugin-react-hooks`, `eslint-plugin-react-refresh` | TypeScript/React correctness and required lint command |
| `vitest`, `jsdom`, `@testing-library/react`, `@testing-library/user-event`, `@testing-library/jest-dom` | fast component/hook/a11y-oriented regression tests |
| existing `@playwright/test` | browser/E2E contract coverage |

No Redux, Zustand, TanStack Query, Tailwind, CSS-in-JS, UI framework, runtime schema framework, persistence framework, analytics SDK, font package, or animation framework is added. Exact versions will be resolved once with pnpm in Phase 1 and committed in the existing lockfile.

## 6. Phased migration, file boundaries, acceptance, and stop gates

### Phase 1 — React engineering, build chain, and Go embed preparation

Authorized files after approval:

- root `package.json`, `pnpm-lock.yaml`, new `pnpm-workspace.yaml`;
- new `web/package.json`, TypeScript/Vite/ESLint/Vitest config, minimal React entry/tests/styles;
- new committed `internal/web/assets/dist/**`;
- `internal/web/server_test.go` for direct embedded-FS assertions; `internal/web/server.go` only if a failing test proves the existing embed declaration must change;
- `.gitignore`/`.dockerignore` only as required for caches while explicitly retaining committed `dist`.

Implementation:

- complete the Section 4.5 Homebrew/Corepack/pnpm preflight and stop immediately on its stated failure conditions;
- establish strict TypeScript and all required scripts;
- build a minimal React bootstrap artifact;
- let Go Embed contain React `dist`, while `Server.New()` continues to use the legacy `assets/` subtree as its default static root;
- do not switch the production service entrypoint and do not add a hidden, preview, alternate, or otherwise user-accessible React URL;
- test the package-level embedded FS directly to prove `dist/index.html` and its referenced content-hashed JS/CSS assets are embedded;
- keep the formal `fs.Sub(assets, "assets/dist")` serving-root switch exclusively in Phase 4;
- retain the existing `//go:embed assets/*` declaration when its direct embedded-FS test proves recursive inclusion;
- make identical-source Vite builds byte-identical, with no timestamp, absolute path, host path, random build ID, or similar nondeterministic output.

Acceptance:

- one pnpm lockfile only;
- absolute Node/Corepack/pnpm paths and real versions are recorded; no alternative installer or global npm pnpm install was used;
- `pnpm install --frozen-lockfile`, typecheck, lint, unit test, build pass;
- two clean builds from identical source/tool inputs produce identical `dist` digests and rebuilt `dist` is clean;
- `go test ./internal/web -count=1`, existing Go suite/builds, and legacy Playwright remain green;
- embedded-FS tests prove React `dist/index.html` plus hashed assets are present;
- `Server.New()` and `GET /` still serve the legacy UI; no HTTP path exposes the React artifact;
- only Phase 1 files changed; protected untracked files remain untouched.

Stop: commit, push, report SHA/tests/artifacts/risks/Draft PR state, then wait.

### Phase 2 — App Shell, routes, i18n, and two-level sidebars

Authorized files:

- `web/src/main.tsx`, `App.tsx`, `layouts/**`, `routes/**`, `state/**`, `i18n/**`, shared `components/**`, `hooks/useMediaQuery.ts`, `hooks/usePersistentUIState.ts`, shell styles/tests;
- generated `internal/web/assets/dist/**`;
- React-focused Playwright specs/config/static test server only.

Implementation:

- BrowserRouter route table and root redirect;
- default Chinese and live en-US switch;
- UI Context + Reducer;
- NavigationRail and Agent SessionSidebar desktop/mobile behavior;
- route Error Boundary, toast/confirm primitives, document title/lang synchronization;
- localized placeholder pages for feature routes in the React artifact while legacy remains the Go-served default.

Acceptance:

- all target routes render directly under the React test server and survive refresh;
- locale and both sidebar preferences persist and validate corrupt storage safely;
- desktop/mobile widths, overlay/Escape/focus return, keyboard operation, no horizontal overflow, and reduced motion are tested;
- no untranslated raw i18n keys/`undefined`/white screen;
- StrictMode shell effects do not duplicate requests;
- legacy production E2E remains green.

Stop: commit, push, report, then wait.

### Phase 3 — Dashboard, Settings, and Agent migration

Authorized files:

- `web/src/api/**`;
- `web/src/features/dashboard/**`, `settings/**`, `agent/**`;
- `web/src/hooks/useSessionEvents.ts`, shared runtime panels and tests;
- affected Playwright specs/page objects and generated `dist/**`;
- no backend API/schema changes.

Implementation:

- typed/decoded API client;
- connectivity heartbeat;
- Dashboard projections;
- Owner/Profile/Facts, provider credentials and Codex login;
- Agent Session creation/list/detail/messages, Plan, Team/Mission, Approval, Tool/Activity, explicit Run actions;
- shared StrictMode-safe Session SSE hook.

Acceptance:

- API method/path/JSON fixtures match current Go contracts;
- settings never expose/log/persist credentials;
- Agent Session deep link and refresh restore server data and timeline;
- the EventSource registry and persisted sequence are keyed only by Session ID;
- one Session with two different Run-filtered subscribers creates exactly one underlying EventSource, while each subscriber receives only its selected Run projection;
- removing one subscriber does not close the shared Session connection while another subscriber remains; the final unsubscribe closes it;
- Session switching releases the old subscription and closes the old connection only when its reference count reaches zero;
- a validated safe nonnegative high-water no greater than `session.next_event_sequence` resumes with `after=<sessionSequence>`; an overshoot removes the key and safely uses `after=0`, while a normal refresh with a valid value does not unconditionally replay from zero;
- corrupt, negative, fractional, unsafe, or beyond-server-frontier sequence values are discarded and recover from a safe value;
- dedupe, disconnect preservation, and StrictMode mount/cleanup/remount connection counts pass without creating multiple EventSources for different Runs in the same Session;
- review-first Plan and Approval suites remain behaviorally equivalent;
- user/model/tool content is never translated.

Stop: commit, push, report, then wait.

### Phase 4 — Employees, Tasks, and Loops migration; React cutover

Authorized files:

- `web/src/features/employees/**`, `tasks/**`, `loops/**`, related components/tests/styles;
- existing Playwright suites updated without deletion, generated `dist/**`;
- `internal/web/server.go`, `server_test.go`, and test helpers for the final SPA handler/cutover.

Implementation:

- complete Employee list/wizard/detail/tabs/readiness/lifecycle;
- complete queued Task create/filter/detail/Start/Cancel/Resume and Session projections;
- complete Loop Definition/import/Dry Run/Team mapping/start/cancel/history/timeline;
- supplemental Loop/Invocation routes for refresh restoration;
- switch Go’s served root to the embedded React `dist`;
- keep legacy files embedded in the branch only as rollback material until Phase 5, but do not expose a second served URL.

Acceptance:

- all existing Employee/Task/Loop Playwright behavior and test IDs pass against React;
- archived Employees are read-only;
- impactful actions require confirmation;
- Task and Loop timelines use Session SSE only;
- root redirects to `/dashboard`; every declared React route shape returns `index.html` on direct load/refresh, and missing resources inside valid detail shapes render React’s localized resource Not Found;
- unknown top-level paths, invalid shapes, extra segments, extension-bearing asset misses, unknown `/api/*`, traversal, and encoded-path anomalies return Go/API 404 and never React HTML;
- browser back/forward restores selections;
- full React unit tests and Playwright pass, with critical SSE/navigation/mutation paths repeated 10 times;
- existing backend semantics and DTOs remain unchanged.

Stop: commit, push, report, then wait.

### Phase 5 — Legacy removal, full validation, docs, CI, and Docker acceptance

Authorized files:

- delete only after parity: `internal/web/assets/app.js`, `employees.js`, `tasks.js`, `loops.js`, legacy `index.html`, and legacy `styles.css`;
- remove transitional embed/cutover code and legacy static-server assumptions;
- `Dockerfile`, `.github/workflows/ci.yml`, package scripts/config;
- add `docs/ai/react-frontend.md`;
- update `AGENTS.md`, `README.md`, `docs/ai/context.md`, `CHANGELOG.md` without changing version/tag/release;
- generated `dist/**` and final tests.

Implementation:

- final Node → Go → runtime Docker stages;
- CI frontend install/typecheck/lint/unit/build/artifact-drift checks;
- final single React embed tree and documentation;
- no long-term duplicate frontend.

Acceptance:

- clean-clone reproducibility and committed artifact drift check;
- all commands in Section 7 pass or each environmental skip is explicitly reported;
- Docker final image has no Node/npm/pnpm executable, cache, `node_modules`, or frontend source dependency;
- Docker browser acceptance verifies deep links, API 404 isolation, default Chinese, and persistent data behavior;
- secret scan and diff review show no credentials/debug artifacts;
- only intended tracked files are committed; all protected untracked files remain untouched.

Stop: commit, push, report final Phase 5 evidence and remaining risks. Do not merge, tag, release, deploy, or replace the current Mac mini service.

## 7. Test and verification matrix

Frontend:

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm test
pnpm build
git diff --exit-code -- internal/web/assets/dist
pnpm test:e2e
pnpm exec playwright test --repeat-each=10 <critical-specs>
```

Go and packaging:

```bash
go test ./internal/web -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/hermit
go build ./cmd/hermit-web
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/hermit ./cmd/hermit-web
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/hermit ./cmd/hermit-web
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./cmd/hermit ./cmd/hermit-web
git diff --check
```

Docker/security:

```bash
docker compose config
docker build .
bash internal/evals/docker_acceptance.sh
# inspect the final image for node/npm/pnpm/node_modules/npm cache/frontend source
# run browser acceptance against the built container
# scan tracked diff and generated assets for credential-shaped content
```

Required new browser coverage:

1. all declared routes direct-load, refresh, back, and forward;
2. default `zh-CN` and `<html lang="zh-CN">`;
3. immediate en-US switch and refresh persistence;
4. dynamic statuses translated while user/Employee/Prompt/message/model/Tool/path/code/JSON/IDs remain byte-for-byte unchanged;
5. no raw i18n keys, `undefined`, or white screens;
6. NavigationRail and SessionSidebar independent persistence;
7. mobile drawer overlay/Escape/focus trap/focus return and desktop-preference restoration;
8. Employee create/detail/tabs/readiness/lifecycle/archived read-only;
9. Task create/filter/detail/Start/Cancel/Resume/Approval/Verification;
10. Session-owned SSE registry, after-sequence reconnect, dedupe, consumer Run filtering, disconnect preservation, reference-counted cleanup, and StrictMode;
11. Loop create/revise/import/Dry Run/Team mapping/start/cancel/refresh timeline;
12. Settings Owner/Facts/API-key deletion/Codex login status;
13. Error Boundary retry and shell survival;
14. every declared SPA route shape returns HTML; a missing resource inside a valid detail shape renders localized React Not Found; unknown top-level paths, invalid shapes, extra segments, unknown `/api/*`, traversal/encoded anomalies, and missing extension-bearing assets return Go/API 404.

Required SSE connection/sequence tests:

1. one Session with two subscribers filtering two different Run IDs creates one underlying EventSource;
2. each subscriber receives only its requested Run projection, while Session-scoped events follow the documented subscriber rule;
3. unsubscribing one consumer does not close the connection still used by another;
4. the final unsubscribe closes the underlying connection and a Session switch releases the old Session subscription;
5. high-water persistence and `after` continuation are keyed only by Session;
6. refresh loads the authoritative Session projection first and, with a valid saved sequence, does not unconditionally replay all events from `after=0`;
7. missing sequence safely uses `after=0`;
8. corrupt text, negative, fractional, unsafe, and beyond-server-frontier sequences are removed/reset and recover from a safe value;
9. StrictMode mount → cleanup → remount maintains correct reference and connection counts at every step;
10. two Runs in one Session never create two EventSources, and Run filter changes never reset the shared Session sequence.

## 8. Documentation deliverable

`docs/ai/react-frontend.md` will record:

- React entry and provider/component tree;
- route table and selection/restoration rules;
- typed API client and DTO decoder pattern;
- Session SSE hook/registry and sequence behavior;
- i18n namespace/term/status rules;
- both sidebar state machines and responsive focus behavior;
- every localStorage key and prohibited browser data;
- Vite output, committed artifact rule, Go embed and SPA/API routing boundary;
- Docker build chain;
- checklist for adding a route/page/component/translation/DTO;
- unit, Go, Playwright, stability, and Docker test entrypoints.

`AGENTS.md`, `README.md`, `docs/ai/context.md`, and `CHANGELOG.md` are updated only in Phase 5 so they describe the final, verified state. No version number, tag, release, or deployment is created.

## 9. Rollback plan

- Each phase is one bounded commit series with generated assets included and an Owner stop gate. Reverting the current phase returns to the previously verified phase.
- Phases 1–3 do not switch the Go-served default away from legacy, so rollback is a direct revert with no product/data migration.
- Phase 4’s React cutover and SPA handler are reverted together if acceptance fails; legacy assets are still present through that phase.
- Phase 5 deletes legacy assets only after parity. Rollback requires reverting Phase 5 plus the Phase 4 cutover as a pair, restoring both the legacy handler and its matching files.
- No API or persistence schema migration is planned, so rollback does not rewrite Employee, Task, Session, Run, Loop, Owner, credential, Team, or workspace data.
- Committed Vite artifacts make any source/artifact pair reproducible at its commit.
- No force push, automatic merge, tag, release, deploy, or Mac mini Docker replacement is part of rollback or implementation.

## 10. Known risks and controls

| Risk | Control |
|---|---|
| Legacy DOM behavior is broad and partly implicit | preserve existing Playwright suites/test IDs; migrate feature by feature; cut over only after parity |
| Default Chinese breaks English accessible-name assertions | make locale explicit for legacy English contract tests; add independent default-Chinese suites |
| Agent/Task/Loop or multiple Runs create duplicate SSE connections | key the registry and sequence only by Session, reference-count subscribers, apply Run filters after shared high-water advancement, and assert one underlying EventSource |
| StrictMode corrupts SSE reference counts | deferred final disposal with remount cancellation plus connection/reference-count tests at each StrictMode lifecycle step |
| Corrupt or ahead-of-server saved sequence loses incremental updates | decode as an untrusted safe nonnegative integer, validate against the Session frontier when server-verifiable, reset invalid/unverifiable values to `0`, and test every recovery case |
| Refresh unnecessarily replays the full Session journal | load the authoritative Session projection first and use `after=<validatedSavedSequence>`; use `after=0` only when no safe saved value exists |
| SPA fallback masks API/asset errors or traversal | return HTML only for declared route shapes; Go/API 404 unknown top-level paths, invalid shapes, extra segments, API misses, extension-bearing asset misses, traversal, and encoded anomalies |
| Generated assets drift | commit `dist`, rebuild in CI, fail on diff |
| Embedded React artifact becomes a hidden second UI before cutover | Phase 1 keeps `Server.New()` on legacy `assets/`, exposes no React URL, and tests `dist` through the embedded FS only; the serving-root switch is Phase 4 |
| Mac mini frontend commands depend on a non-login SSH environment | Phase 1 installed only Homebrew Node 22, activated pnpm 11.9.0 through Corepack, recorded real paths/versions, and explicitly sets the non-interactive PATH |
| A persisted collapsed Session sidebar steals focus on load, navigation, refresh, or breakpoint changes | arm focus transfer only inside the user's current-page expanded-to-collapsed action, consume the pending transfer before focusing, and test initialization, navigation, refresh, breakpoint, and StrictMode lifecycles |
| ConfirmDialog leaves background controls operable or leaks `inert`/scroll locking | isolate the App Shell while either the dialog or mobile drawer is open, keep the Toast live region outside that isolation, restore prior body overflow and trigger focus in effect cleanup, and test every close path |
| Dependency expansion | use only the dependency table above; any addition requires evidence and Owner approval |

## 11. Phase ledger and approval gate

| Phase | Authorization | State | Evidence |
|---|---|---|---|
| Plan Gate | Owner reapproval at `c4074a2963e0b210804d08b2bfcab5b1e010762f` | complete | SSE ownership, SPA fallback, CodeGraph, temporary Embed boundary, and toolchain plan approved |
| Phase 1 | `APPROVAL: REACT_PHASE_1_APPROVED` at `4f06a1e970923ba9c8b981c5fb151ef324f56e65` | complete; approved | minimal React/TypeScript/Vite workspace, committed deterministic Embed artifact, old UI boundary, and validations below |
| Phase 2 | `APPROVAL: REACT_PHASE_2_REAPPROVED` at `019c6e70df32b4116f92d5007515def6f53421a8` | complete; approved | BrowserRouter shell, zh-CN/en-US i18n, corrected action-scoped Session sidebar focus, isolated ConfirmDialog, shared feedback primitives, deterministic Embed artifact, and independent React E2E |
| Phase 3 | `AUTHORIZED_PHASE: PHASE_3_ONLY` at `019c6e70df32b4116f92d5007515def6f53421a8` | `SECOND_GATE_REVISION_COMPLETE_WAITING_FOR_OWNER` | typed API/DTO boundary, corrected Active Run mutation ownership, visible mutation failures, Session-owned SSE recovery, delta-free bounded Activity projection, consistent fatal recovery state, localized runtime metadata, bounded API/history/streaming data, deterministic Embed artifact, and Phase 3 browser coverage |
| Phase 4 | `APPROVAL: REACT_PHASE_3_SECOND_REAPPROVED` at `3579e8331c2df91ce668fe6a59d16b467b8a8660` | `SECOND_GATE_REVISION_COMPLETE_WAITING_FOR_OWNER` | server-validated wizard Skill bindings/readiness, exact Loop argv editing, Store-aligned Employee routes, complete Employee settings and repair actions, confirmed destructive mutations, complete URL-owned Task filters, owner-scoped request isolation, deterministic artifacts, and fail-closed embedded React serving-root cutover |
| Phase 5 | not authorized | `BLOCKED_BY_GATE` | no legacy deletion, Docker/CI migration, final documentation, release, merge, or deployment |

### 11.1 Phase 1 execution evidence

- Frontier evidence correction: CodeGraph shows public `Session.NextEventSequence` serializes as `next_event_sequence`, is returned by `GET /api/sessions/{id}`, and is maintained by Store commit/recovery as the committed Session event frontier. Phase 3 will validate the Session-owned local high-water as a safe nonnegative integer no greater than this field, remove an overshoot, and recover with `after=0`; no API field, Session schema, or Run frontier is added.
- Workspace: the root scripts delegate `typecheck`, `lint`, `test`, and `build` to the single `@gohermit/web` workspace while retaining the existing root Playwright E2E command. One `pnpm-lock.yaml` covers both importers; `pnpm-workspace.yaml` allows only the Vite-required `esbuild` install script.
- Resolved frontend stack: React/React DOM `19.2.8`, TypeScript `5.9.2`, Vite `7.3.6`, ESLint `9.39.5`, Vitest/coverage `3.2.7`, Testing Library React `16.3.2`, and the existing Playwright `1.61.1`. No router, i18n, product state library, UI framework, external font/CDN, telemetry, or remote resource was added.
- Bootstrap: `main.tsx` mounts `App` through `StrictMode` and `BootstrapErrorBoundary`; three Vitest tests cover the app marker, bounded render-error fallback, and real document-root mount. Coverage passed at 95% lines/statements, 87.5% branches, and 100% functions.
- Embed boundary: a failing test first proved that the pre-Phase-1 `FileServer` returned 301 for `/dist/index.html`. The minimal `server.go` wrapper now fails closed with 404 for `/dist` and `/dist/**`, delegates every other request to the unchanged legacy `assets/` static root, and keeps `//go:embed assets/*` unchanged. Direct embedded-FS tests prove `dist/index.html`, its content-hashed JS/CSS, missing-resource failure, and machine-path absence. `GET /` remains the legacy UI.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `44d61fdb285046e830b0534217a15e77efbdfc1944c2172ed420505501d307d6`, JS `3f18f959a336e87b95fe7585f88ffebd7d5b01179711edca2551ed5e187c1c1e`, and `index.html` `e3ea78822442835bfc386f5cadc6706dfabfcf5f541169d669f746e7e2cde6f8`. No sourcemap, timestamp, absolute machine path, or Build ID marker was found.
- Frontend verification: `pnpm install --frozen-lockfile`, `pnpm typecheck`, `pnpm lint`, `pnpm test`, `pnpm build`, and the existing Chromium `pnpm test:e2e` suite passed; legacy E2E remained 13/13 and no browser installation was needed.
- Go verification: `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. Build executables were moved outside the repository after verification.
- Hygiene: Markdown structure, `git diff --check`, the single-lockfile check, production-sourcemap check, and authorized-path review passed. Excluding the authorized Phase 1 additions, the protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`; those files remain unmodified and unstaged.

`STATUS: WAITING_FOR_REACT_PHASE_1_APPROVAL`

### 11.2 Phase 2 execution evidence

- Frontier evidence synchronization: the existing public `Session.NextEventSequence` / `next_event_sequence` field is the authoritative Session event frontier returned by `GET /api/sessions/{id}` and maintained by Store commit/recovery. Phase 3 will accept a local Session high-water only when it is a safe nonnegative integer no greater than that field; an overshoot removes `gohermit.ui.sseSequence.{sessionId}` and safely resumes with `after=0`. No API field, Session schema, or Run-level frontier is added.
- Dependencies: the only Phase 2 additions are `react-router-dom` `7.18.1` (with `react-router` `7.18.1`), `i18next` `26.3.6`, `react-i18next` `17.0.11`, and `lucide-react` `1.27.0`. The router is pinned to the latest version that passed the repository minimum-release-age policy; the final frozen install requires no policy exception. The repository retains one pnpm workspace and one `pnpm-lock.yaml`.
- Routes and i18n: BrowserRouter owns `/` → `/dashboard` plus Dashboard, Employees, Tasks, Agent/Session, Loops/Invocation, and Settings route shapes. Every Phase 2 route is a localized placeholder with URL-derived navigation selection; unknown React routes render localized Not Found. Bundled leaf-key-equivalent `zh-CN` and `en-US` resources default to Chinese without browser detection, validate untrusted locale storage strictly, and synchronize all shell copy, accessible names, `<html lang>`, and document title immediately.
- UI ownership: one Context + Reducer owns only `locale`, `navigationCollapsed`, `sessionSidebarCollapsed`, `mobileSessionDrawerOpen`, `toast`, and `dialog`. Only the first three approved preferences persist under their canonical keys. Strict boolean/locale parsing removes invalid values; route, API, user content, business state, toast, dialog, and mobile drawer state are never persisted.
- Shell behavior: the Lucide-based NavigationRail is `184px`/`56px` on desktop and compact on mobile. The Agent-only SessionSidebar is `292px`/`0`, preserves a focusable restore entry, and keeps its desktop preference independent from the mobile drawer. The mobile drawer uses overlay/Escape close, focus entry/trap/return, background `inert`, body scroll locking, and breakpoint-safe state reset. Reduced-motion and 200% zoom contracts are covered.
- Shared primitives: route-scoped Error Boundary, EmptyState, ErrorState, PageHeader, StatusBadge, ToastRegion, and ConfirmDialog are present. The route boundary leaves shell navigation operational and never renders/logs the caught object; toast announcements and dialog Escape/focus behavior are accessible, and no native confirm/prompt is used.
- React test boundary: `playwright.react.config.ts` starts an independent static server on `127.0.0.1:4174`, serves only the committed React `dist`, returns 404 for `/api` and `/api/**`, and shuts down with Playwright. It does not expose a Go route, business API, Session SSE, external font, CDN, telemetry, or remote resource.
- Frontend verification: the final frozen install, typecheck, zero-warning lint, 44/44 Vitest assertions, and build passed. Coverage is 99.37% statements/lines, 94.65% branches, and 96.29% functions. React Playwright passed 8/8, and the three critical shell cases passed 30/30 under `--repeat-each=10`; legacy Playwright remained 13/13.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `d8266ec659c74b48ab0e9e2555b633ef9f50804439e6b5c69339afa0673ade19`, JS `edcb482e3ee9665bdb1548750f0689233d471296fa4c91f3655f2e330b616ea9`, and `index.html` `f7ceda8be4612bd887ae3ff391119640f865f062184b29f058e112be4dc86bcd`. `git diff --exit-code -- internal/web/assets/dist` passed after the second build; no sourcemap, timestamp, absolute machine path, or Build ID marker was found.
- Go and serving-boundary verification: `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. Existing tests prove the new hashed React assets are embedded while `GET /` still serves the legacy UI and `/dist` plus `/dist/**` remain 404. No React preview route, Go static-root cutover, Docker update, or Phase 3 business integration occurred.
- Hygiene: authorized-path review, Markdown structure, `git diff --check`, credential-shaped scan, single-lockfile check, alternate-lockfile check, production-sourcemap check, and generated-artifact scan passed. Excluding authorized Phase 2 additions, the protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`; those files remain unmodified and unstaged.

`STATUS: WAITING_FOR_REACT_PHASE_2_APPROVAL`

### 11.3 Phase 2 Gate revision evidence

- Session sidebar focus contract: focus transfer is armed only by the user's current-page expanded-to-collapsed action and is consumed before the restore button is focused. A collapsed value loaded from storage, Agent route entry, browser refresh/remount, and mobile-to-desktop breakpoint restoration only reveal the restore button and never request focus. The pending ref is not timer-based and produces one focus transfer under React StrictMode.
- Dialog isolation contract: while ConfirmDialog is present, the App Shell is `inert`; keyboard and assistive-technology interaction therefore cannot reach its navigation or page controls. The Toast live region remains a sibling outside the isolated shell. Dialog setup locks body scrolling, and cleanup restores the previous body overflow value plus trigger focus for Escape, cancel, confirm, and overlay close. The shell isolation is the logical union of mobile drawer and dialog state, preventing either lifecycle from leaving residual `inert` or scroll locking.
- Regression coverage: Vitest passed 50/50 assertions, including stored collapse, cross-route navigation, refresh/remount, mobile-to-desktop restoration, active collapse, StrictMode focus count, shell isolation, Toast availability, focus return, and all dialog close paths. Coverage passed at 99.38% statements/lines, 94.70% branches, and 96.22% functions.
- Browser coverage: React Playwright passed 11/11. The critical Agent sidebar and real ConfirmDialog cases passed 40/40 with `--repeat-each=10`; legacy Playwright remained 13/13. The dialog harness is test-only, built into an operating-system temporary directory by the loopback React test server, removed on shutdown, and never exposed by Go or included in production `dist`.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `d8266ec659c74b48ab0e9e2555b633ef9f50804439e6b5c69339afa0673ade19`, JS `06a71cc7f11fd171cc8d15c5b07cd03c3444b589b4970cb9046f992b461bdd36`, and `index.html` `0b5ccaa60f5ffcf710637cdfbfa9e03cf59b3a30d7815425bcfae68579375b18`. `git diff --exit-code -- internal/web/assets/dist` passed after the second build.
- Validation: `pnpm install --frozen-lockfile`, typecheck, zero-warning lint, unit tests, coverage, production build, React E2E, repeated critical E2E, legacy E2E, `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, and `go vet ./...` passed. Existing Go tests continue to prove that `GET /` serves the legacy UI and `/dist` plus `/dist/**` return 404.
- Scope and hygiene: no business API, SSE, Session, Run, Go Server, Docker, CI workflow, Phase 3 page, or legacy frontend code changed. Credential-shaped data, absolute machine paths, production sourcemaps, timestamps, random Build IDs, and unexpected generated files were not introduced. The protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`, and those files remain unmodified and unstaged.

`STATUS: WAITING_FOR_REACT_PHASE_2_REAPPROVAL`

### 11.4 Phase 3 execution evidence

- Frontier evidence synchronization: the implementation uses the existing public `Session.NextEventSequence` / `next_event_sequence` returned by `GET /api/sessions/{id}` as the Session frontier. A saved `gohermit.ui.sseSequence.{sessionId}` is accepted only as a safe nonnegative integer no greater than that value; corrupt, negative, fractional, unsafe, and frontier-ahead values are removed and recover with `after=0`. No API field, Session schema, or Run-level frontier was added.
- Typed trust boundary: the API client accepts only same-origin relative `/api/**` paths, forwards cancellation, reads at most 1 MiB of JSON, rejects malformed content/status/DTOs with sanitized error codes, and never retries mutations. Runtime decoders fail closed on unknown enums, unsafe IDs/times/sequences, oversized collections, and invalid sequence-zero events. The endpoint table uses the current Session-scoped Go handlers and never calls legacy `/api/run`.
- Connectivity and projections: one StrictMode-cleaned 30-second `/api/health` heartbeat owns online/offline mutation gating and explicit recovery refresh. Dashboard renders authoritative workspace/readiness, bounded Loop invocation summaries, recent activity, and active Session hints. Settings covers Owner Profile/Facts, transient API-key entry, confirmed credential deletion, and terminal-aware Codex login polling without storing credentials; readiness refresh completes before the polling effect enters its terminal state, so effect cleanup cannot abort that authoritative reload. Agent uses URL-owned Session selection, readiness-filtered creation, guarded explicit Run mutations, authoritative Messages/Plan/Mission/Tools/Verification/Approvals, bounded approval countdowns, and untranslated text-node rendering. Employees, Tasks, and Loops remain Phase 4 placeholders.
- Session event ownership: the module registry is keyed only by Session ID, reference-counts subscribers, defers final cleanup across the StrictMode cleanup/remount microtask, supports optional per-subscriber Run filters, advances the Session high-water before projection filtering, drops duplicate/descending durable events, and resumes explicit reconnects from the current high-water. Sequence-zero `model_delta` chunks remain ephemeral, are capped at 256 KiB, never advance storage, and clear on terminal/checkpoint/reconnect refresh. Consecutive invalid events fail closed after a bounded threshold. The loopback fixture can actively terminate a Session stream; browser coverage proves native reconnect preserves the loaded projection, retains one underlying connection, and resumes with a Session-scoped `after` cursor.
- Frontend verification: frozen install, typecheck, zero-warning lint, 98/98 Vitest assertions, and production build passed. Coverage passed at 94.61% statements/lines, 81.69% branches, and 91.74% functions. React Playwright passed 19/19; the Phase 3 mutation/SSE and ConfirmDialog suites passed 90/90 under `--repeat-each=10`; legacy Playwright remained 13/13.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `c64e4ae018f0a04d7e433c68641e9bb6aa25f437e462d4eefd891421c860831e`, JS `aab128f1e0186bbcf668abaa629e3f5e332e663886917b6584f37577928a87a7`, and `index.html` `7a3a2856c49dba8b9ae5b0bb74415053c33aff7355cfafa0fbfa1a1d1255d317`. No production sourcemap, absolute machine path, timestamp marker, or random Build ID was found.
- Go and serving-boundary verification: `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. The build outputs were written outside the repository. Existing Go tests continue to prove that `GET /` serves the legacy UI and `/dist` plus `/dist/**` return 404; the React API/SSE fixture remains loopback E2E infrastructure and is not a Go product route.
- Scope: no backend, business API, Session/Run schema, Control Plane, Store, Agent Runtime, Dockerfile, CI workflow, production static root, legacy frontend, or Phase 4/5 product feature changed. No dependency or lockfile change was needed.

`STATUS: WAITING_FOR_REACT_PHASE_3_APPROVAL`

### 11.5 Phase 3 Gate revision evidence

- Session/Run ownership: `boundActiveRun` is resolved only through `session.active_run_id` and is the sole Run accepted by cancel, resume, and plan-approval mutations; `latestRun` is display-only. Any nonempty active Run ID disables the Composer. A queued Review Run exposes approve only when its unapproved Plan exists and also exposes cancel; running/verifying expose cancel, interrupted exposes resume, and terminal history exposes no mutation. Unit and browser tests cover queued approval/cancellation, Composer gating, terminal history, and duplicate pointer/keyboard submission guards.
- Message and mutation contracts: Run input is normalized exactly once and measured with `TextEncoder`; the maximum is the Go `StartRun` boundary of `16 << 10` UTF-8 bytes, with exact-boundary acceptance and one-byte-over rejection for ASCII, Chinese, and Emoji. Session creation, Run start/cancel/resume/approve, and Approval decisions now release in-flight guards in `finally` and surface only localized sanitized conflict/offline/general Toasts. Start failure preserves Composer text; Approval `409` refreshes authoritative state without exposing an error body, while other failures remain visible and retryable.
- Session SSE recovery: explicit reconnect now closes the old source, preserves the Session high-water and all subscribers/Run filters, clears connection fatal/invalid-event state, and creates one replacement EventSource. The 256 KiB model stream cap is subscriber-local: that subscriber stops accumulating and shows a localized truncation notice, the shared Session connection and other Run-filtered subscribers continue, and a terminal event refreshes the authoritative Session projection and clears transient state. Tests cover five-invalid-event fatal recovery, monotonic high-water, subscriber/filter retention, shared-connection isolation, and StrictMode connection counts.
- Localized dynamic metadata: zh-CN/en-US leaf-key-equivalent trees cover every allowed Message role, Mission WorkItem status, Tool status, Runtime event type, and Loop Invocation status. Unknown presentation values use a localized fallback. Language-switch tests prove metadata labels change while user/model text, Tool names and summaries, WorkItem/Plan content, paths, IDs, and other authoritative text remain byte-for-byte unchanged.
- API trust boundary: Session listing now requests the Go maximum `limit=100`. Default responses retain the 1 MiB ceiling; only exact `GET /api/sessions/{id}` responses receive a 16 MiB ceiling. Session-owned record collections are individually capped at 8,192 and remain protected by the total response ceiling. UTF-8 decoding is fatal and cancellation is best-effort without replacing the sanitized boundary error. Tests recover a valid Session response above 1 MiB, cancel a stream beyond 16 MiB, reject malformed UTF-8 as `invalid_response`, and accept legal Chinese split across chunk boundaries.
- Frontend verification: the frozen install, typecheck, zero-warning lint, 116/116 Vitest assertions, coverage, production build, and React Playwright passed. Coverage is 95.23% statements/lines, 83.37% branches, and 92.44% functions. React Playwright passed 22/22; the complete Phase 3 Agent/Settings/SSE suite passed 110/110 with `--repeat-each=10`; legacy Playwright remained 13/13.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `c64e4ae018f0a04d7e433c68641e9bb6aa25f437e462d4eefd891421c860831e`, JS `ac1075d03be7c4a82144acbdf40e81ad5240c547ae840752822a4868f779f17e`, and `index.html` `bcefb4773e730e74ce3f32b715e727fdc06dca284144efd6f3e07c206c59c86b`. A further build left the staged `internal/web/assets/dist` byte-identical.
- Go and scope verification: `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. Existing Web tests continue to prove `GET /` serves the legacy UI and `/dist` plus `/dist/**` return 404. No Go API, Store, Schema, Agent Runtime, Docker, CI, dependency, lockfile, React serving-root, Phase 4/5 feature, deployment, or Mac mini Docker service changed.
- Hygiene: authorized-path, Markdown structure, `git diff --check`, single-lockfile/alternate-lockfile, credential-shaped data, machine-path, production-sourcemap, timestamp/Build-ID, and generated-artifact checks passed. The protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`; protected files remain unmodified and unstaged.

`Phase 3: GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`

`STATUS: WAITING_FOR_REACT_PHASE_3_REAPPROVAL`

### 11.6 Phase 3 second Gate revision evidence

- Delta retention boundary: `model_delta` is handled before Activity projection and never enters the bounded Activity array. Activity retains only display-required, bounded metadata and excludes message text, raw payloads, errors, and other transient model content. The existing 256 KiB streaming cap remains subscriber-local; after truncation that subscriber ignores further deltas without closing or replacing the shared Session EventSource, while another Run-filtered subscriber continues receiving its events. Terminal/model-completed events refresh the authoritative Session projection and clear the temporary stream/truncation state.
- Delta regression coverage: unit and shared-registry integration tests send 100 maximum-size 32 KiB chunks and prove `streamingText` never exceeds 256 KiB, Activity contains no `model_delta` or chunk content, the shared source is neither closed nor replaced, and the other Run subscriber remains functional.
- Fatal recovery ownership: Hook fatal UI is now derived from the shared Connection status instead of a subscriber-local boolean. A subscriber that remounts during deferred disposal, or a second subscriber joining an already-fatal Connection, synchronously receives `fatal` and exposes the reconnect action. Explicit reconnect preserves the Session high-water, subscribers, and Run filters, closes the prior source, and creates exactly one shared replacement EventSource. StrictMode-style immediate unsubscribe/remount does not lose the recovery entry or create a duplicate connection.
- Frontend verification: frozen install, typecheck, zero-warning lint, 119/119 Vitest assertions, coverage, production build, and React Playwright passed. Coverage is 95.25% statements/lines, 83.68% branches, and 92.44% functions. React Playwright passed 22/22; the complete Phase 3 Agent/Settings/SSE suite passed 110/110 with `--repeat-each=10`; legacy Playwright remained 13/13.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `c64e4ae018f0a04d7e433c68641e9bb6aa25f437e462d4eefd891421c860831e`, JS `7568e0ebea07aeb47970a994d93c6b4b0990c882c8fee815acba6b21e772dedf`, and `index.html` `c5ce9b4b731aa6e7a14b734227f0f1e2212a6d85f7e774ac90949c32865de71c`. The artifact contains no production sourcemap, workspace absolute path, or source-map marker.
- Go and scope verification: `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. Legacy Web tests remain green. No Go API, Store, Schema, Agent Runtime, Docker, CI, dependency, lockfile, React serving-root, Phase 4/5 feature, deployment, or Mac mini Docker service changed.
- Hygiene: authorized-path, Markdown structure, `git diff --check`, single-lockfile/alternate-lockfile, credential-shaped data, machine-path, generated-artifact, and protected-file checks passed. The protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`; protected files remain unmodified and unstaged.

`Phase 3: SECOND_GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`

`STATUS: WAITING_FOR_REACT_PHASE_3_SECOND_REAPPROVAL`

### 11.7 Phase 4 execution evidence

- Employees: React now owns the bounded cursor list and URL state filter, a nine-step creation flow, server-backed catalog loading and Dry Run readiness, revision-bound update/lifecycle mutations, and the details, Skills, Knowledge/Citations, Memory, Projects, Tasks, and Activity sections. Skill identity is pinned as `skill_id + version + digest`; Adapter bindings are identified as zero-capability, digest drift is visible, and native configuration remains subject to the existing server validator. Archived Employees expose no mutation controls and are fully read-only. API keys, credentials, private Memory, and raw sensitive material are never written to browser storage.
- Employee Tasks: the global page performs at most four concurrent Employee queries and explicitly states the existing latest-100-per-Employee limit. URL-owned Employee/state/project filters and detail routes restore through refresh and browser history. Creation remains `queued`; Prepare only loads the authoritative context, while the separate Start action calls the existing backend start endpoint. Start, Cancel, and Resume are gated by the authoritative Task state. Session/Run, Plan, Tool, Verification, Approval, Artifact, and Activity projections refresh from the existing Session APIs and Session-owned SSE hook; no Task SSE or browser execution state machine was added.
- Loops: Definition create, strict JSON import, revision-preserving update, server Dry Run, Team template import/export, active Employee revision/readiness display, and Invocation start/cancel/history are available through the existing APIs. Editing invalidates a prior Dry Run, so Start requires readiness for the current Definition. The UI records the complete company/access/model Mission override rule and uses the Employee default otherwise. Supplemental Definition and Invocation routes direct-load, refresh, and traverse history; Invocation timelines reuse the Session SSE projection only.
- React serving boundary: `Server.New()` now serves the embedded `assets/dist` root while leaving the legacy assets embedded solely as rollback material. `/` returns a temporary redirect to `/dashboard`; only declared React route shapes return `index.html`, and content-hashed `/assets/**` files use their correct content types plus immutable caching. `/dist`, legacy asset aliases, unknown top-level paths, illegal/extra route shapes, extension-bearing misses, `/api`, `/api/`, unknown API paths, traversal, encoded traversal/slash/backslash, and malformed paths return a Go/API 404 without React HTML. Valid detail routes retain localized resource Not Found ownership in React.
- Frontend verification: frozen install, typecheck, zero-warning lint, 127/127 Vitest assertions, coverage, production build, and React Playwright passed. Coverage is 89.43% statements/lines, 81.34% branches, and 80.52% functions. React Playwright passed 26/26 and legacy Playwright remained 13/13. Employees/Tasks/Loops plus Phase 4 locale/literal preservation passed 40/40 under `--repeat-each=10`; every declared direct-load/refresh route passed 10/10; shared EventSource/high-water cases passed 20/20; and explicit SSE disconnect recovery passed 10/10.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `c64e4ae018f0a04d7e433c68641e9bb6aa25f437e462d4eefd891421c860831e`, JS `2ac0fd42a7abb62b9dc59f5eb82209e965fd9cb0d9359b45d6702f0bafb34e02`, and `index.html` `0b457e9c078151b742ad603bc2920519b23ce7a2cc27c1ab21a1506f40a465dd`. No production sourcemap, timestamp, absolute workspace path, or random Build ID was present.
- Go verification: the Phase 4 Web route/security matrix, `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. No business API, Store, Session/Run schema, Agent Runtime, dependency, lockfile, Dockerfile, CI workflow, deployment, or Mac mini Docker service changed.
- Hygiene: Markdown structure, `git diff --check`, authorized-path review, single-pnpm-lockfile and alternate-lockfile checks, credential-shaped scan, machine-path scan, production-sourcemap scan, generated-artifact review, and protected-file review passed. The pre-existing protected untracked paths remain unmodified and unstaged.

`Phase 4: COMPLETE_WAITING_FOR_OWNER`

`STATUS: WAITING_FOR_REACT_PHASE_4_APPROVAL`

### 11.8 Phase 4 Gate revision evidence

- Real DTO boundary: the frontend decoders and endpoint types now match the existing Go `EmployeeTask` snapshot, project, policy, artifact, and Loop recipe-check object shapes. Collections and response sizes remain bounded, DTO failures remain sanitized, and no Go API, DTO, Store, Schema, Session/Run model, or Agent Runtime was changed.
- Employees: the creation flow contains all nine approved steps—Identity, Model/Agent, Charter, Skills, Knowledge, Memory, Projects, Policy, and Review—and persists exact Skill identity/configuration, project policy, Knowledge input, and server Dry Run readiness before opening the record. The detail route renders structured Skills, Knowledge/Citations, Memory, Projects, Tasks, and Activity data; lifecycle and edits use expected revision, archived records remain fully read-only, and request epochs plus cancellation prevent stale Employee responses from crossing route changes.
- Tasks: creation requires explicit Employee and project selection, enforces the existing 16 KiB UTF-8 prompt boundary, remains queued, and separates Prepare from Start. Cancel is confirmed; Resume, Approval decisions, Plan, Tool, Verification, Artifact, Activity, and Session/Run evidence are bound to authoritative backend projections. Timelines reuse Session-owned SSE and retain offline/mutation guards without adding a Task execution state machine.
- Loops: Definition and Team editing use the complete structured schemas, including recipe-check objects, role-to-Employee mappings, Employee revision/readiness, Team default model selection, and complete Mission override triples. Strict import, Dry Run, revision-bound save, Start/Cancel, Invocation history/evidence, Approval decisions, and Session-SSE timelines use existing APIs. Employee and Loop detail loaders discard late A/B responses after route changes.
- Routes and serving boundary: Employee IDs containing dots and the full existing Loop/Invocation identifier range are accepted only in declared React route shapes. Go Web tests cover direct loads, refresh, hashed assets/content types, root redirect, localized resource Not Found ownership, and fail-closed API/asset/unknown/extra-segment/traversal/encoded-path cases. `/dist` and legacy assets are not an alternate HTTP UI.
- Fixtures and browser coverage: Phase 4 fixtures use nonempty real DTO projections for stale/native Skills, Knowledge/Citations, Memory, Task snapshots/artifacts, Team roles, recipe checks, and Invocation evidence. React Playwright passed 28/28 and legacy migration Playwright passed 13/13. The six Employee/Task/Loop/localization/request-isolation cases passed 60/60 under `--repeat-each=10`; the complete Phase 3 Session/SSE/Approval/16-KiB suite passed 110/110 under the same repetition.
- Frontend verification: frozen install, typecheck, zero-warning lint, 137/137 Vitest assertions, coverage, and production build passed. Coverage is 84.34% statements/lines, 80.25% branches, and 80.46% functions.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `c64e4ae018f0a04d7e433c68641e9bb6aa25f437e462d4eefd891421c860831e`, JS `e37c4ca680fc64d649ef3a7a633e5e219a224d0324bb22aa12943637a51c7a69`, and `index.html` `b32b477f7de7b1bd035c7f6651f46b3691e821041414eee8852788de07b42e7a`. No production sourcemap, timestamp, absolute workspace path, or random Build ID marker was present.
- Go and scope verification: `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. No dependency or lockfile, Dockerfile, CI workflow, legacy asset deletion, Phase 5 work, deployment, or Mac mini Docker service change occurred.
- Hygiene: Markdown structure, `git diff --check`, authorized-path review, single-pnpm-lockfile and alternate-lockfile checks, credential-shaped scan, machine-path scan, production-sourcemap scan, generated-artifact review, and protected-file review passed. The protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`; those paths remain unmodified and unstaged.

`Phase 4: GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`

`STATUS: WAITING_FOR_REACT_PHASE_4_REAPPROVAL`

### 11.9 Phase 4 second Gate revision evidence

- Employee creation readiness: the create mutation persists no unvalidated Skill bindings. The wizard then calls the existing `PUT /api/employees/{id}/skills` endpoint with the current expected revision, lets the server validate native configuration and Catalog identity, forces Adapter configuration to `{}`, and retains the returned Employee revision. Ready now requires the real Employee Dry Run, an exact all-`current` Skill projection, and all Knowledge Sources in `ready`; missing/drifted Skills, invalid configuration, and failed Knowledge remain blocked. A partially persisted Employee exposes retry and detail-repair actions without creating the ID twice.
- Loop verification argv: recipe commands are edited as a bounded array with one input per argument and no shell parsing, quoting, joining, or whitespace splitting. The round-trip case `["go", "test", "-run", "Test Name", "./..."]` remains element-for-element identical while unrelated Definition fields are edited and saved.
- Employee routes and repair surfaces: declared Employee routes accept the Store alphabet `A-Z`, `a-z`, `0-9`, `_`, `-`, and `.` at any position while exact dot segments, separators, controls, overlong IDs, encoded path anomalies, unknown top-level paths, extra segments, and missing assets still fail closed. Employee Details now edit every backend-supported Avatar, identity, charter, responsibility/boundary, company/access/model/agent, permission, budget, concurrency, and Memory policy field with expected revision. Skills render Catalog union persisted bindings, expose explicit missing/stale removal and digest-drift upgrade, never silently replace a digest, retain server-side native validation, and remain read-only when archived.
- Confirmed impact and Task filters: the existing ConfirmDialog now gates Knowledge deletion, Memory rejection/forgetting, Employee archive, Task cancellation, and Invocation cancellation; canceling the dialog sends no mutation. The Task list retains its latest-100-per-Employee and four-request concurrency boundaries while URL-owning Employee, Project, complete nine-state, and 24-hour/7-day/30-day time filters. Task creation consumes the authoritative Employee Skill projection and excludes missing or digest-drift bindings.
- Owner-scoped request isolation: Employee tab requests, Activity load-more, Knowledge/Memory refreshes, and Skill saves are tied to Employee ID, AbortSignal, and request epoch. Task and Loop mutations likewise discard completion, projection, navigation, and Toast effects after their owner route changes. Tests delay Employee A Activity, Task mutation, and Loop Definition mutation while navigating to another owner and prove no stale state crosses the route boundary.
- Frontend and browser verification: `pnpm install --frozen-lockfile`, typecheck, zero-warning lint, 147/147 Vitest assertions, coverage, and production build passed. Coverage is 85.09% statements/lines, 81.33% branches, and 80.58% functions. React Playwright passed 28/28; the complete Phase 3, Phase 4, and declared-route suites passed 200/200 under `--repeat-each=10`; legacy migration Playwright remained 13/13.
- Deterministic build: two clean builds produced the same three-file set and byte-identical SHA-256 manifest: CSS `c64e4ae018f0a04d7e433c68641e9bb6aa25f437e462d4eefd891421c860831e`, JS `d1b1dff94f85353871d7433640c8fc834d96532e01c2691c241a82b3ac91c9d1`, and `index.html` `a11715bb2bdfe162fb633505286a2bb636ea87767980f25b2e8b0b0d37997d50`. No production sourcemap, timestamp, absolute workspace path, or random Build ID marker was present.
- Go and security verification: the SPA/API/asset/traversal matrix, `go test ./internal/web -count=1`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./cmd/hermit`, and `go build ./cmd/hermit-web` passed. No backend business API, DTO, Store, Schema, Runtime, dependency, lockfile, Docker, CI, Phase 5, deployment, or Mac mini Docker service changed.
- Hygiene: Markdown structure, `git diff --check`, authorized-path review, single-pnpm-lockfile and alternate-lockfile checks, credential-shaped scan, machine-path scan, production-sourcemap scan, generated-artifact review, and protected-file review passed. The protected untracked path-list SHA-256 remains `530153626b098db04ebade3e1ff76660b58d7d6a0243b4b060b986f7a533b223`; those paths remain unmodified and unstaged.

`Phase 4: SECOND_GATE_REVISION_COMPLETE_WAITING_FOR_OWNER`

`Phase 5: BLOCKED_BY_GATE`

`STATUS: WAITING_FOR_REACT_PHASE_4_SECOND_REAPPROVAL`
