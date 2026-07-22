# B1 handoff: Team Template data model and owner-level storage

## Goal

- Requested outcome: new `internal/teamtemplate` package — Team Template schema (default provider/model + optional per-role overrides for Lead/Explorer/Builder/Reviewer/Verifier, Operator reserved) stored outside repositories, mirroring the `internal/owner` storage discipline and reusing its secret-pattern check.
- Scope actually handled: schema, validation, and the owner-level store with tests. Storage only — no role behavior or web endpoints.

## Completed

- Changes:
  - `internal/owner/profile.go`: exported the existing secret detector as `LooksSecret` (behavior identical) so teamtemplate reuses it — no second pattern list.
  - `internal/teamtemplate/template.go`: `RoleSelection{Company, Access, Model}` (mirrors `session.Selection` minus Agent; holds names, never keys), `Template{SchemaVersion, Name, Default, Roles, UpdatedAt}`, `Validate` (allowed override keys come from `team.Role*` constants; `operator` and unknown keys rejected; incomplete overrides rejected; text bounded; every field screened by `owner.LooksSecret`), and `Store` with mutex-guarded `Load`/`Save`.
  - Store path resolution mirrors owner: explicit path → `GOHERMIT_TEAM_TEMPLATE_STORE` → `os.UserConfigDir()/gohermit/team-template.json`, always absolute; resolution never consults the working directory. Load: missing file → empty template, 256KB cap, `DisallowUnknownFields`, unknown schema version fails closed. Save: validate → marshal → cap → `storage.AtomicWrite` mode 0600.
  - `internal/teamtemplate/template_test.go`: 8 tests (25 cases) — round-trip lossless, path precedence, fallback location, workspace exclusion, validation table, fail-closed loads, file mode, concurrent access.
- Files/packages: `internal/owner` (one export), `internal/teamtemplate` (new).
- Decisions or ADRs: `MaxTextBytes` 8KB (owner's value; fields are short provider/model names); a small `Store.Path()` accessor exists for path-resolution tests.

## Verification

- Focused tests: `go test ./internal/teamtemplate/ ./internal/owner/ -count=1` ok.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/teamtemplate/ ./internal/owner/ -count=1` ok (incl. concurrent load/save case).
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets.
- Evals: the 7-package eval set passed 3 consecutive runs (untouched by this change).
- Skipped checks and reason: E2E/Compose not rerun (no web/packaging change); CI reruns the baseline on the PR.

## Acceptance mapping

1. A written Template reads back losslessly: `TestRoundTrip` (default + 3 overrides, field-level equality across two loads).
2. The Template file never appears under any workspace/repository path: resolution uses only explicit path/env/user-config-dir; `TestEnvPathWinsOverFallbackAndStaysOutOfWorkspace` asserts a separate workspace dir stays empty.

## Repository state

- Branch: `agent/opc-b1`, based on `origin/main` after Phase A (`8b1c058`; A5 merged as `78d6c18` while this was in flight — disjoint files).
- Commit/PR: `feat: add team template storage`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: the template is not yet consulted anywhere; B2 wires validation into Session creation, B4 adds redacted export/import.
- Next concrete step: owner review/merge, then B2 on `agent/opc-b2`.
- Required user input or authority: PR review/merge remains with the owner.
