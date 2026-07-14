# Local Web and Docker debugging

`hermit-web` is a local single-user console with Dashboard, Run Agent, and Provider Settings pages. Settings accepts provider credentials over the loopback-only origin; secrets are stored server-side and never returned to the browser. Run selects a company group, provider/access slug, model, and Agent profile, then streams structured Agent Core events over one POST response.

## Start on the same machine

```bash
docker compose up --build -d
open http://127.0.0.1:8787
```

Compose defaults to `configs/codex.toml` and `./sandbox`. The config supplies startup defaults; the Web picker applies a validated per-run selection. Select another workspace without editing committed files:

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

For Codex Plan, open Provider Settings and choose **Login Codex**. GoHermit starts the OpenAI device-code flow, polls it server-side, and saves the resulting tokens in the dedicated `gohermit-data` volume. Existing Codex CLI login remains a fallback: Compose mounts `${HOME}/.codex` read-only and never modifies it.

For API providers, paste the key in Provider Settings. A configured environment variable remains supported and takes effect when no Web-managed key exists. Only access methods with usable credentials appear in Run Agent.

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

## Security boundaries

- Web-managed credentials stay in `/data/auth.json` with mode `0600`; Compose persists `/data` in the `gohermit-data` volume.
- Model keys and OAuth tokens are never returned by `/api/info` or any settings response.
- The workspace and config path are fixed when the server starts; browser selections can only reference server-defined catalog entries.
- Only one task may run at a time.
- Request size and task length are bounded; disconnecting cancels the run.
- Browser POSTs are same-origin checked and responses set a restrictive content security policy.
- The container drops Linux capabilities, enables `no-new-privileges`, mounts config read-only, and does not mount the Docker socket.
- Repository build/test code and configured plugins remain trusted code; Docker packaging is isolation-in-depth, not a hostile-code sandbox.

Useful commands:

```bash
docker compose ps
docker compose logs --tail=100 gohermit-web
docker compose down
```
