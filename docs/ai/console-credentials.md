# Console and credentials: AI quick reference

Read this file only when changing the Web console, provider availability, or authentication.

## User flow

`Dashboard -> Provider Settings -> Agent Session`

- Settings displays the full company/access catalog.
- Run receives `available_companies`, which contains only supported access methods whose credentials are currently usable.
- The picker remains `company -> access/billing path -> model -> Agent profile`.
- Dashboard summarizes connected access methods and current run state.

## Code map

| Responsibility | File |
|---|---|
| catalog and selection validation | `internal/config/config.go` |
| encrypted-channel credential resolution and Codex headers | `internal/auth/codex.go` |
| live account model discovery | `internal/auth/models.go` |
| mode-0600 atomic credential file | `internal/auth/store.go` |
| Codex device-code state machine | `internal/auth/device.go` |
| status/filter/settings/run APIs | `internal/web/server.go` |
| Dashboard/Run/Settings SPA | `internal/web/assets/` |
| persistent container data | `compose.yaml`, `Dockerfile` |

## Credential precedence

- API key: GoHermit credential store, then the provider's environment variable.
- Codex: `GOHERMIT_CODEX_ACCESS_TOKEN`, then GoHermit device login, then read-only `CODEX_HOME/auth.json`.
- An expiring Codex token is refreshed before it is marked configured. A failed refresh means unavailable.
- Codex models come from the authenticated account's live `/backend-api/codex/models` catalog and are cached for five minutes. Do not restore guessed Web model lists.
- The selected secret is placed in `RuntimeOptions.APIKey` for that run and is excluded from JSON serialization.

## HTTP surface

- `GET /api/info`: full catalog, filtered runnable catalog, secret-free auth status.
- `PUT /api/settings/providers/{provider}/api-key`: save one API key.
- `DELETE /api/settings/providers/{provider}/credentials`: remove locally stored credentials.
- `POST /api/settings/providers/openai-codex/login`: begin device login.
- `GET /api/settings/logins/{id}`: poll secret-free login state.
- `POST /api/run`: validate catalog selection, resolve credentials server-side, stream events.

All mutating endpoints enforce same-origin requests. Never add secrets to `/api/info`, logs, events, DOM persistence, localStorage, sessionStorage, or repository files.

Codex Responses streaming must collect output from `response.output_item.done`, not only `response.completed.response.output`. Tool names are mapped to wire-safe names and back. With `store=false`, replay encrypted reasoning without its response item `id`, include an empty `summary` array, and never persist reasoning summary text.
