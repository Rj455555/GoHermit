# PR28 handoff: docs and plan calibration

## Goal

- Requested outcome: calibrate stale documentation — schema v4→v5 references, the read-only Verifier passing rule (PR #26/#27 semantics), P0–P2 completion status against actual code, and a dated status note (PR #27 merged; stale drafts PR #2/#3/#4). No code changes.
- Scope actually handled: `docs/architecture.md`, `docs/ai/context.md`, `docs/ai/harness.md`, `docs/ai/team.md`, `docs/ai/plan-mode.md`, `docs/ai/next-development-plan.md`. Historical records (old handoffs, ADR 0009, CHANGELOG entries) intentionally left as-is.

## Completed

- Changes:
  - schema v5 calibration: `architecture.md` (v1–v4 migrate to v5), `context.md` (v5 with explicit v1–v4 migrations), `harness.md` (v5 directory intro), `plan-mode.md` (three references), `team.md` (persistence section notes approval requests + v1–v4 migrations).
  - `team.md` Verifier rule now matches PR #26/#27: mutation Missions require at least one real passing Check; purely read-only Missions pass with `Checks == [] && Issues == []`; `team.HandoffChecksPassed` named as the single definition (verified against `internal/team/coordinator.go:424-470`).
  - `next-development-plan.md`: P0 items 2–5 (A1–A4, PRs #5–#8), P1 items 1–4 (B1–B5, PRs #10–#15), and P2 (ADR 0011 + C2/C2b/C3/UI, PRs #16/#20–#23) marked done with handoff references after verifying the code exists; P3 carries the ADR 0012 unresolved-conflict status; a dated 2026-07-24 note records PR #27 merged as `a3e396e` and PRs #2/#3/#4 as stale drafts safe for the owner to close (no GitHub write actions taken).
- Decisions or ADRs: none (docs only). `docs/session-storage.md` already carried the v5 paragraph from C2 — verified, no change needed.

## Verification

- Docs-only change; full gates run per protocol: `gofmt -l .` clean, `go vet ./...` ok, `go test ./... -count=1` all packages ok, `go test -race ./... -count=1` all packages ok, `go build ./cmd/hermit` and `./cmd/hermit-web` ok, `git diff --check` clean.
- Skipped checks and reason: none.

## Repository state

- Branch: `agent/pr28-docs-calibration`, based on `origin/main` (`a3e396e`, which includes PR #27).
- Commit/PR: `docs: calibrate schema v5, verifier rule, and plan status`, PR to `main`.
- Working tree: untracked owner files (`.claude/`, `.cursor/`, `.gemini/`, `.mcp.json`, new `docs/` reference files, `sandbox/.gohermit/`) left exactly as found.

## Remaining work

- Next concrete step: owner review/merge of this PR, then PR29 (Control Plane application services extraction).
- Required user input or authority: PR review/merge remains with the owner.
