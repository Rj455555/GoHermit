# PR29 handoff: Control Plane application services

## Goal

- Requested outcome: extract Run lifecycle (create/start/resume/cancel), Team run launch/completion, approval coordination, and durable event commit/publish (as a port) from `internal/web.Server` into a new `internal/controlplane` package — zero behavior change, web ends with only thin HTTP handlers and presentation, services callable without HTTP.
- Scope actually handled: exactly that extraction. `internal/web/server.go` 1,777 → 758 lines; new `internal/controlplane` (1,536 lines over 4 files) holds the moved logic; tests relocated mechanically.

## Completed

- Changes:
  - `internal/controlplane/` (new; no `net/http` import — verified by grep): `service.go` (Service struct, `Publisher` port, classified `Error{Kind,...}` mapping to 400/404/409/500/502 with byte-identical messages, selection/credential/catalog/owner helpers), `runs.go` (CreateSession/StartRun/ResumeRun/ApprovePlan/CancelRun, launchSessionRun, commitAndPublish(Many), run gate), `team.go` (runTeam, finishTeamCancelled, failLaunchedRun, team event/plan mapping, template role plan), `approvals.go` (approvalBroker verbatim, ListApprovals/DecideApproval, expiry event helper).
  - `internal/web/server.go`: thin transport only — routing, handlers, sameOrigin, security headers, static FS, SSE subscriber fan-out (implements `Publisher`), owner/settings/login/template endpoints as presentation over service methods.
  - Tests: service-internal tests moved to `internal/controlplane` (`testServer`→`newTestService`, same fields; a few in-test HTTP calls became direct service calls — `svc.ApprovePlan`/`svc.CancelRun`/`svc.CreateSession`/`svc.DecideApproval`); HTTP-level tests stayed in web. Assertions unchanged in substance.
- Files/packages: `internal/controlplane` (new), `internal/web`.
- Decisions or ADRs:
  - Package name `controlplane` (gap-analysis recommendation) with documented dependency direction: web/cli → controlplane → domain; domain never imports either.
  - Error classification via `Kind` constants instead of HTTP codes so the CLI/Loop dispatcher can map statuses without parsing messages.
  - Owner-profile CRUD, codex device-login flow, and team-template export/import stayed in web as thin presentation; their store access goes through service methods.
  - `/api/run`'s run gate is acquired by the handler via `TryAcquireRun` in the same order as before; SSE headers are written on first event — same wire result.

## Verification

- `gofmt -l .` clean; `go vet ./...` ok.
- `go test ./... -count=1`: all 24 packages ok.
- `go test -race ./... -count=1`: all packages ok (broker/rendezvous paths included).
- `go build ./cmd/hermit`, `go build ./cmd/hermit-web`: ok.
- Eval set (evals/team/taskplan/runcontrol/app/session/web/controlplane): 3 consecutive runs, no failures.
- `git diff --check` clean; no secrets.
- Verified by inspection: no `net/http` in controlplane; web handlers contain no state-transition logic; event payloads, durable-before-visible ordering, commit.json journal, and the C3 single-writer rendezvous unchanged.
- Skipped checks and reason: none.

## Repository state

- Branch: `agent/pr29-controlplane`, based on `origin/main` (`ae1d36d`).
- Commit/PR: `refactor: extract control plane application services from web`, PR to `main`.
- Working tree: untracked owner files (`.claude/`, `.cursor/`, `.gemini/`, `.mcp.json`, `docs/` reference files, `sandbox/.gohermit/`) left exactly as found.
- External state changed: none.

## Remaining work

- Known limitations: the service still constructs its own stores from workspace/configPath (a future CLI may want shared construction); `RunOnce` requires the caller to hold the run gate (documented).
- Next concrete step: owner review/merge, then PR30 (Loop Domain and Store).
- Required user input or authority: PR review/merge remains with the owner.
