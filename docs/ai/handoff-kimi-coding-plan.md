# Kimi Code (kimi-coding-plan) handoff

## Goal

- Requested outcome: add the Kimi Code coding plan as a model provider and fix the HTTP 400 function-name validation failures it exposed.
- Scope actually handled: provider catalog, TOML/compose/docs wiring, outbound tool-name sanitization scoped to the preset, a context-manager dedupe fix for consecutive tool-call turns, unit tests, and live smoke verification against the real Kimi endpoint.

## Completed

- Changes:
  - New provider slug `kimi-coding-plan`: Chat Completions protocol, base URL `https://api.kimi.com/coding/v1`, default model `kimi-for-coding`, API key env `KIMI_API_KEY`; Web catalog company `kimi` ("Moonshot AI / Kimi") with access "Kimi Code 编程套餐" and models `kimi-for-coding`, `kimi-for-coding-highspeed`, `k3`.
  - `ModelPreset.SanitizeToolNames` capability flag, set only for `kimi-coding-plan`; threaded through `app.NewProvider` into `model.OpenAIConfig`.
  - Outbound tool-name sanitization in `internal/model/openai.go`: names outside the strict shape (letter first, then letters/digits/`_`/`-`) are rewritten (`file.read` → `file_read`, `plugin.echo.ping` → `plugin_echo_ping`) in both tool definitions and replayed history `tool_calls`; a per-request bidirectional mapper restores registry names from response tool calls, with suffix disambiguation on collisions and pass-through for unknown names.
  - `contextmgr.dedupe` now keys on role, content, `ToolCallID`, and full tool-call identity. Previously all assistant tool-call messages share empty content, so the second consecutive tool-call turn was dropped and its tool result orphaned; strict APIs then fail with `tool_call_id ... is not found`. This bug predates Kimi and affected every provider; lenient APIs never validated the linkage.
  - Preset config `configs/kimi-coding-plan.toml`, `KIMI_API_KEY` passthrough in `compose.yaml`, preset comment in `hermit.example.toml`, and the Kimi section in `docs/model-providers.md`.
- Files/packages: `internal/config`, `internal/model`, `internal/app`, `internal/contextmgr`, `configs/`, `docs/`, `compose.yaml`, `hermit.example.toml`.
- Decisions or ADRs:
  - Sanitization is preset-scoped (conservative) rather than client-wide: DeepSeek/DashScope wire behavior stays byte-identical; history and definitions share one serializer, so per-request mapping consistency holds either way.
  - Wire names are stable within a request and restored before registry execution, so executor, storage, and checkpoints keep the original dotted names.

## Verification

- Focused tests: new `internal/model` tests (definition + history sanitization, streaming fragment restore, underscore names unchanged, unmapped fallback, disabled-flag pass-through, mapper collisions) and `internal/contextmgr` regression test (consecutive tool-call turns survive dedupe).
- Full tests: `go test ./...` all packages ok on the Mac mini.
- Race test: `go test -race ./internal/model/ ./internal/agent/ ./internal/contextmgr/` ok.
- Vet/build/cross-build: `gofmt -l` clean, `go vet ./...` ok, `go build ./cmd/hermit` and `./cmd/hermit-web` ok, `docker compose config --quiet` and `docker compose up -d --build` ok.
- Live smoke (real Kimi endpoint, UI-stored `KIMI_API_KEY` in the container): simple Chinese reply completed; multi-turn tool use completed (`filesystem.list`, `shell.execute`, allowlist denial handled, final answer correctly listed `.gitkeep`); three-run history with multiple tool-call rounds replayed without HTTP 400.
- Skipped checks and reason: none; unlike the v0.5 Windows-host milestone, the full Mac baseline (race + Docker acceptance) ran green here.

## Repository state

- Branch: `agent/adaptive-plan-v0.5`.
- Commit/PR: `feat: add kimi-coding-plan model provider`, `fix: sanitize tool names and keep tool-call turns for strict APIs`, `docs: record kimi-coding-plan handoff`.
- Working tree: `compose.yaml` intentionally still modified with only the owner's local `0.0.0.0` port binding (excluded from commits per the loopback-only security model); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none beyond the local Docker container rebuild used for smoke verification.

## Harness state

- Session/Run IDs used for live verification: `20260721T163225Z-fcb609ab4a77bd5c165d212a` (three completed runs) plus one empty session from an early script-reply parsing miss; left in place for the owner to delete.
- Last event sequence and terminal Run state: all three runs `completed`.
- Project memory updated: no.
- Recovery or workspace-reconciliation notes: none.

## Remaining work

- Known limitations: `kimi-for-coding-highspeed` and `k3` were not individually smoke-tested; Kimi may stream `reasoning_content`, which is replayed in plaintext within a process and encrypted at rest as with DeepSeek; model-side quirks (an occasional off-topic short reply) are not protocol failures.
- Next concrete step: owner review of the two commits, then push so CI reruns the Linux baseline.
- Required user input or authority: push/PR and any rotation of the stored `KIMI_API_KEY` remain with the owner.
