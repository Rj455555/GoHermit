# Local Web and Docker debugging

`hermit-web` is a local single-owner, Codex-style workbench: a compact tool rail, persistent task list, conversation/execution canvas, pinned composer, and a Settings drawer. Every Run shows a collapsible Cursor-style Live Plan with checkboxes, current phase, bounded detail, and completion progress. Settings manages the Owner Profile plus provider credentials; both remain server-side. A Session fixes company, access, model, and single Agent or Personal Agent Team selection; each user message creates a Run. The browser reloads the selected Plan and resumes structured SSE events with `after=<sequence>` or `Last-Event-ID`.

Session endpoints and recovery behavior are summarized in `docs/ai/harness.md`. The legacy one-shot `POST /api/run` remains for compatibility, but the Web UI uses `/api/sessions`.

## Start on the same machine

```bash
docker compose up --build -d
open http://127.0.0.1:8787
```

Compose defaults to `configs/codex.toml` and `./sandbox`. The config supplies startup defaults; the Web picker applies a validated per-Session selection. Select another workspace without editing committed files:

```bash
GOHERMIT_CONFIG=./configs/deepseek.toml \
GOHERMIT_WORKSPACE=/absolute/path/to/project \
DEEPSEEK_API_KEY='...' \
docker compose up --build -d
```

On macOS Docker Desktop, the default container identity is UID 501/GID 20 so mounted developer workspaces remain writable. Override `GOHERMIT_UID` and `GOHERMIT_GID` on other hosts.

## Codex Plan versus API

OpenAI expands to two provider rows, matching Hermes:

- `openai-codex`: ChatGPT/Codex subscription login and the Codex backend.
- `openai-api`: direct API billing through `OPENAI_API_KEY`.

For Codex Plan, open the Settings drawer and choose **Login Codex**. GoHermit starts the OpenAI device-code flow, polls it server-side, and saves the resulting tokens in the dedicated `gohermit-data` volume. Existing Codex CLI login remains a fallback: Compose mounts `${HOME}/.codex` read-only and never modifies it.

For API providers, paste the key in Settings. A configured environment variable remains supported and takes effect when no Web-managed key exists. Only access methods with usable credentials appear in the new-task composer.

Alibaba likewise exposes standard DashScope API and Alibaba Coding Plan as separate provider rows because their keys and endpoints differ.

If Docker Hub is unavailable through a configured registry mirror, point the build at an equivalent trusted mirror without changing Docker's global settings:

```bash
GOHERMIT_GO_IMAGE=docker.m.daocloud.io/library/golang:1.26-bookworm \
docker compose up --build -d
```

## Access the Mac mini remotely

The published port is intentionally `127.0.0.1` on the Mac mini. From another computer, create an SSH tunnel:

```bash
ssh -N -L 8787:127.0.0.1:8787 macmini
```

Then open <http://127.0.0.1:8787> locally. Do not change the Compose bind to `0.0.0.0` unless an authenticated reverse proxy and network policy are added first.

On Windows, `scripts/connect-web.ps1` keeps the SSH tunnel alive and retries after the Mac mini wakes or reconnects:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\connect-web.ps1
```

The Web heartbeat reports **服务离线** and disables sending while the tunnel is unavailable. When health recovers it reloads provider status, Sessions, and the selected conversation automatically.

## Security boundaries

- Web-managed credentials stay in `/data/auth.json` with mode `0600`; Compose persists `/data` in the `gohermit-data` volume.
- Owner Profile data stays in `/data/owner.json`, is editable/forgettable, rejects credential patterns, and is never placed in the workspace.
- Model keys and OAuth tokens are never returned by `/api/info` or any settings response.
- The workspace and config path are fixed when the server starts; browser selections can only reference server-defined catalog entries.
- Only one task may run at a time.
- Request size and task length are bounded; Runs continue across an SSE reconnect and stop only through their bounded runtime or the explicit cancel endpoint.
- Browser POSTs are same-origin checked and responses set a restrictive content security policy.
- The container drops Linux capabilities, enables `no-new-privileges`, mounts config read-only, and does not mount the Docker socket.
- Repository build/test code and configured plugins remain trusted code; Docker packaging is isolation-in-depth, not a hostile-code sandbox.

Useful commands:

```bash
docker compose ps
docker compose logs --tail=100 gohermit-web
docker compose down
```
