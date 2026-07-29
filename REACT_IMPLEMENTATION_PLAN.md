# GoHermit React Workbench / i18n Migration Plan

> Gate document only. No product implementation is authorized until the Owner approves this plan.

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
- Environment note: Go exists at `/opt/homebrew/bin/go` (`go` is not on the SSH `PATH`); `node`, `npm`, and `pnpm` are not currently available through SSH. If Phase 1 is approved, its first preflight is the bounded Homebrew Node 22 + Corepack + pnpm 11.9.0 procedure in Section 4.5. This Gate does not install or change that toolchain.

This Gate creates only `REACT_IMPLEMENTATION_PLAN.md`. It does not change Go, HTML, CSS, JavaScript, tests, CI, Docker, documentation, generated assets, schemas, or persisted data.

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
3. Accept it only if it is a finite safe nonnegative integer and does not exceed a frontier the server can validate for that Session; otherwise remove/reset it to the safe value `0`. No Run frontier is consulted.
4. Open the shared Session EventSource with `after=<savedSequence>` when the validated value exists; otherwise use `after=0`.
5. Advance and persist the high-water only from valid monotonic events belonging to that Session.
6. Native `Last-Event-ID` remains supported, and an explicit reconnect uses the current Session high-water.
7. A normal refresh with a valid saved sequence does not unconditionally replay from `after=0`.
8. SSE events provide incremental display hints and trigger authoritative projection refreshes; they never become a second Session/Run/Task/Loop state store.

CodeGraph confirms that the current public Session DTO does not expose a separate event-frontier field; the Store’s sequence map is internal. Before implementing the hook, Phase 3 must prove a server-verifiable frontier mechanism within the existing Session/SSE contract. It must not invent a Run-owned sequence, silently trust an ahead-of-server value, or change an API/domain schema without a new Owner gate. If the current contract cannot verify an overshoot safely, Phase 3 stops and reports that blocker. When the server can identify a saved value as invalid or beyond its Session frontier, the client removes it and reconnects from `0`; this recovery behavior must be bounded and tested.

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

### 4.5 Phase 1 Node/pnpm preflight (plan only; not executed in this Gate)

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
- a validated high-water resumes with `after=<sessionSequence>`; a normal refresh does not unconditionally use `after=0`;
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
| Mac mini cannot currently run frontend commands over SSH | after Phase 1 approval use only `/opt/homebrew/bin/brew` for Node 22 and Corepack for pnpm 11.9.0, explicitly set non-interactive PATH, and stop on failure |
| Dependency expansion | use only the dependency table above; any addition requires evidence and Owner approval |

## 11. Approval gate

No Phase 1 implementation has started. This revision changes the Gate document only.

`STATUS: WAITING_FOR_REACT_IMPLEMENTATION_PLAN_REAPPROVAL`
