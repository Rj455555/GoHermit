# Local Web and Docker debugging

`hermit-web` is a local single-user debugging surface. It displays the resolved non-secret model configuration and streams structured Agent Core events over one POST response. It does not accept API keys, base URLs, workspaces, or shell approvals from the browser.

## Start on the same machine

```bash
export OPENAI_API_KEY='...'
docker compose up --build -d
open http://127.0.0.1:8787
```

Compose defaults to `configs/codex.toml` and `./sandbox`. Select another provider or workspace without editing committed files:

```bash
GOHERMIT_CONFIG=./configs/deepseek.toml \
GOHERMIT_WORKSPACE=/absolute/path/to/project \
DEEPSEEK_API_KEY='...' \
docker compose up --build -d
```

On macOS Docker Desktop, the default container identity is UID 501/GID 20 so mounted developer workspaces remain writable. Override `GOHERMIT_UID` and `GOHERMIT_GID` on other hosts.

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

- Model keys stay in container environment variables and are never returned by `/api/info`.
- The workspace and config path are fixed when the server starts.
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
