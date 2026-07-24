# PR30 handoff: Loop Domain and Store

## Goal

- Requested outcome: new `internal/loop` (Definition/Invocation types + validation, pure domain) and `internal/loopstore` (owner-scoped, versioned persistence following the atomic-write/journal discipline) with import/export reusing the Team Template pattern. No wiring into controlplane/web/CLI (PR31+).
- Scope actually handled: both packages plus their unit tests. Nothing else.

## Completed

- Changes:
  - `internal/loop/definition.go`: `Definition` with the exact P2 field list (id, schema_version, name, description, workspace_identity, enabled, task_source, agent_selection, team_template_ref, plan_mode, verification_recipe, budget, approval_policy, workspace_policy, output_policy, created_at, updated_at, revision). `ValidateDefinition` bounds every field; `task_source` is fail-closed `fixed_prompt` only; command arrays are `[]string` by construction; a single `SecretFields` list drives validation, export redaction, and import rejection via `owner.LooksSecret`.
  - `internal/loop/invocation.go`: `Invocation` with the exact P4 field list (id, loop_id, definition_revision, definition_snapshot, trigger, task_snapshot, session_id, run_id, status, timestamps, failure_code/summary). State machine `prepared → dispatched → attached → completed` with terminal `skipped/blocked/failed/cancelled`; `NewInvocation` deep-copies the definition snapshot.
  - `internal/loopstore/`: owner-scoped `Store` (explicit path → `GOHERMIT_LOOP_STORE` → user config dir `gohermit/loops/`; never inside a workspace). Definitions in one bounded `definitions.json` (revision owned by the store: insert=1, update=old+1; 0600 atomic writes; corrupt/unknown-version/unknown-field fail closed, never wiped). Invocations as per-id atomic JSON files with sorted, traversal-safe list/get. `ExportDefinition`/`ImportDefinition` mirroring teamtemplate (redacted export, `ErrImportSecret` + strict decode + validate on import).
- Files/packages: `internal/loop`, `internal/loopstore` only.
- Decisions or ADRs:
  - `ApprovalPolicy{RequireForMutation}`, `WorkspacePolicy{ReadOnly, RequireCleanGit}`, `OutputPolicy{IncludeDiff, MaxReportBytes}` kept minimal and documented (PR32/33 give them teeth).
  - Trigger restricted to `"manual"`, fail-closed and extensible (cron/event triggers are v0.8).
  - Invocation carries no own schema_version (the P4 list omits it; the embedded snapshot carries the definition's).

## Verification

- Unit tests: 43-case validation table (all bounds + 8 secret locations); state machine (happy path, 11 illegal moves, all terminal×all moves final); snapshot immutability (edit source after NewInvocation → snapshot untouched, next invocation sees new revision); store round-trip/revision bump/0600/corrupt-file/strict decode/path resolution; export redaction; import secret rejection at all 8 locations; concurrent save/get (41 serialized revisions).
- Gates (actual): `gofmt -l .` clean, `go vet ./...` ok, `go test ./... -count=1` all 24 packages ok, `go test -race ./internal/loop/ ./internal/loopstore/` ok, `go build ./cmd/hermit` and `./cmd/hermit-web` ok, `git diff --check` clean.
- Skipped checks and reason: none.

## Repository state

- Branch: `agent/pr30-loop-domain`, based on `origin/main` (includes PR29's controlplane).
- Commit/PR: `feat: add loop domain and owner-scoped loop store`, PR to `main`.
- Working tree: untracked owner files left exactly as found.
- External state changed: none.

## Remaining work

- Next concrete step: owner review/merge, then PR31 (Dry Run over the new controlplane service + CLI).
- Required user input or authority: PR review/merge remains with the owner.
