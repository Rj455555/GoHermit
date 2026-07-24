# PR31 handoff: Loop Dry Run

## Goal

- Requested outcome: Dry Run for `fixed_prompt` Loop Definitions, exposed via the controlplane application service and a CLI command, that never calls a model, never creates a Session/Run/Approval, and never touches the workspace filesystem.
- Scope actually handled: report type in `internal/loop`, `Service.DryRunLoop` (+ ListLoops/GetLoop) in `internal/controlplane`, `hermit loop dry-run`/`hermit loop list` in `cmd/hermit`, and tests including the counter-based no-creation proof.

## Completed

- Changes:
  - `internal/loop/dryrun.go`: `DryRunReport` (definition revision/validity, workspace identity+match, git clean, task prompt, agent/team template, per-role availability, write scope, checks, budget, requires-approval, ready verdict + bounded reasons) and validation. Verdict is conservative: any doubt → not ready.
  - `internal/controlplane/loops.go`: `DryRunLoop` — loads the definition via the service's new injectable `loopStore` seam; reuses the existing template/credential resolution (shared `loadTeamTemplate` extracted in team.go; `accessStatus` per role, NO provider construction, NO runtime build); workspace match = cleaned absolute path equality (most conservative); git cleanliness via `session.GitState` (read-only porcelain); `RequiresApproval = !read_only && require_for_mutation`. Explicit no-creation contract comment.
  - `cmd/hermit/loop.go`: `hermit loop dry-run <id>` (exit 0 ready / 1 not-ready-or-error / 2 usage) and `hermit loop list`; constructs `controlplane.New` directly with a nil publisher — the PR29 boundary exercised without HTTP.
  - Tests: ready/team/not-ready cases with the exact reasons; `TestDryRunLoopCreatesNothing` (counting build stub asserts 0 provider constructions, workspace tree hash unchanged, zero sessions, zero approval waiters); CLI exit-code/output/untouched-workspace tests; report validation tests.
- Files/packages: `internal/loop`, `internal/controlplane`, `cmd/hermit`.
- Decisions or ADRs:
  - CLI lives in `cmd/hermit`, NOT `internal/app`: controlplane imports `internal/app` (runtime assembly), so `app.CLI` cannot import controlplane without an import cycle. Deviation from the original file list, forced by the existing dependency direction and documented in the file header.
  - Capability checks (B2) remain creation-time; dry run reports availability only.
  - `"not-a-repository"` counts as dirty (fail closed).

## Verification

- Gates (actual): `gofmt -l .` clean, `go vet ./...` ok, `go test ./... -count=1` all 26 packages ok, `go test -race ./... -count=1` ok, `go build ./cmd/hermit` and `./cmd/hermit-web` ok, `git diff --check` clean. Manual smoke of the real binary against a temp git workspace printed the full READY report (exit 0) and left the workspace untouched.
- Failure-path requirement: dry run creates no Session/Run/Approval and calls no model — proven by call counts and store assertions, not output shape (see above).
- Skipped checks and reason: none.

## Repository state

- Branch: `agent/pr31-loop-dry-run`, based on `origin/main` (includes PR30).
- Commit/PR: `feat: add loop dry run via controlplane and cli`, PR to `main`.
- Working tree: untracked owner files left exactly as found.
- External state changed: none.

## Remaining work

- Known limitations: `RoleAvailability.Detail` reuses the existing Chinese `accessStatus` strings; `loop run` deliberately absent (PR32).
- Next concrete step: owner review/merge, then PR32 (Manual Invocation).
- Required user input or authority: PR review/merge remains with the owner.
