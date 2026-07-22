# B4 handoff: Team Template export/import credential redaction

## Goal

- Requested outcome: exporting a Team Template must yield a file with zero credential content even if the source contains secrets; importing a file containing anything matching the existing secret-pattern rules must be rejected, never silently accepted.
- Scope actually handled: `Export`/`Import` in `internal/teamtemplate`, the two Web endpoints, and tests.

## Completed

- Changes:
  - `internal/teamtemplate/template.go`: `Export(t)` — marshals a redacted COPY (every string field screened with `owner.LooksSecret`; matches blanked to `""`, document structure preserved so the owner sees which fields to refill; clean templates export byte-identical to a plain marshal; input never mutated). `Import(data)` — 256KB cap → strict decode (`DisallowUnknownFields`) → schema-version check → secret screen rejecting with `ErrImportSecret` (error names the field location, never the value) → existing `Validate`; returns without saving.
  - `internal/web/server.go`: `GET /api/team-template/export` (attachment download of the redacted document) and `POST /api/team-template/import` (body capped via `http.MaxBytesReader`; `Import` then `Store.Save`, which revalidates and secret-screens again; 400 on poisoned/invalid input with no secret echoed; 500 bounded on store errors). Routes follow the existing registration, method, and same-origin conventions.
  - Tests: `internal/teamtemplate/template_test.go` (round-trip lossless, per-location secret rejection via `errors.Is(ErrImportSecret)`, redacted export only fails for missing required fields, malformed files), `internal/web/server_test.go` (export body equality, poisoned import → 400 with the store untouched, clean round-trip through both endpoints, cross-origin → 403, wrong method → 405).
- Files/packages: `internal/teamtemplate`, `internal/web`.
- Decisions or ADRs:
  - Blanking over entry-dropping: preserves document structure for refill.
  - The secret screen runs BEFORE generic validation on import so a poisoned file is refused explicitly (`ErrImportSecret`), distinct from ordinary validation errors.
  - Single detection source: `owner.LooksSecret` only; no second pattern list.

## Verification

- Focused tests: export redaction (secret markers planted in Name/Default/Roles entry vanish from output), import rejection at every field location, endpoint round-trip, no-partial-overwrite assertion.
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/teamtemplate/ ./internal/web/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; test-only secret fixtures confirmed absent from production code paths.
- Evals: the 7-package eval set passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns on the PR.

## Acceptance mapping

1. Zero-credential export even from a poisoned source: `TestExportRedactsSecretFields` (markers planted in all field classes are blanked; source unchanged).
2. Poisoned import rejected, never silently accepted: `TestImportRejectsSecretMarkers` (all 7 locations) and `TestTeamTemplateImportRejectsPoisonedBody` (400, previous store content intact).

## Repository state

- Branch: `agent/opc-b4`, based on `origin/main` after B3 (`116c954`).
- Commit/PR: `feat: redact credentials in team template export and import`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Remaining work

- Known limitations: export/import covers the template file only; there is no UI for these endpoints yet (API-level), and RoleLimits (B3) are not yet part of the template schema.
- Next concrete step: owner review/merge closes Phase B; P2 (scoped tool/Operator approval) or a template editor UI follows per `docs/ai/next-development-plan.md`.
- Required user input or authority: PR review/merge remains with the owner.
