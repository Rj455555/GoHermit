# A1 handoff: Explorer-proposed bounded substeps

## Goal

- Requested outcome: let the Explorer role propose bounded, task-specific substeps through a strict schema; every substep maps one-to-one to a real WorkItem; completed step ids and revisions can never be rewritten or referenced as pending; the 16-step Plan bound from ADR 0009 holds.
- Scope actually handled: strict proposal validation and atomic materialization in `internal/team`, bounded Plan extension in `internal/taskplan`, event-driven Plan application in `internal/runcontrol`, Explorer model I/O in `internal/app/team_worker.go`, prompt documentation, fixture graders, and unit tests.

## Completed

- Changes:
  - `internal/team/team.go`: `SubstepSpec`, `MaxProposedSubsteps = 8`, `Handoff.Substeps` (bounded in `validateHandoff`), `ValidateSubstepProposal` (safe unused ids — completed ids never reused; read-only roles only; dependencies must reference runnable work or peers, never completed/failed/cancelled items; acyclic; budget-aware), `Mission.AddSubsteps` (atomic, applies specs in dependency order via `orderSubsteps`, rewires queued Leads to depend on the new items, preserves history).
  - `internal/team/coordinator.go`: `SubstepsAccepted`/`SubstepsRejected` events; after an Explorer WorkItem completes with a proposal, the coordinator accepts (bounded `{id,title}` JSON message) or rejects (clipped reason; mission continues on the existing topology) and checkpoints either way.
  - `internal/taskplan/plan.go`: `Plan.AddSteps` — active plans only, total ≤ `MaxSteps` (16), rejects duplicate ids including completed history, appends Pending steps, exactly one revision bump.
  - `internal/runcontrol/controller.go`: `SubstepsAccepted` decodes the bounded JSON and extends the Plan via `AddSteps` (malformed payloads leave the Plan untouched and never panic); `SubstepsRejected` never changes the Plan.
  - `internal/app/team_worker.go`: `parseWorkerHandoff` accepts an optional `substeps` key; the Explorer assignment prompt documents the optional schema.
  - `prompts/coding.md`: repo-facing documentation of the Explorer substep schema.
  - Evals: `substep_proposal_scripts` in `internal/evals/testdata/plan_fidelity.json` (valid accept with 1:1 Plan mapping, completed-dependency reject, unknown-id reject, completed-id collision reject, oversized reject) graded by `GradeSubstepProposalScript`.
  - Unit tests: `internal/team/substeps_test.go` (16-case reject table, running-dependency accept, out-of-order peers, atomicity, lead rewiring, coordinator end-to-end accept/reject), `internal/taskplan/plan_test.go`, `internal/runcontrol/controller_test.go`, `internal/app/team_worker_test.go`.
- Files outside the predicted list, with reasons:
  - `internal/runcontrol/controller.go` — the web sink applies Plan changes only through `ApplyTeamEvent`; there is no other Team-event→Plan path.
  - `internal/app/team_worker.go` — worker JSON parsing whitelists `summary/evidence/issues/next_steps`; without a `substeps` key no real Explorer model could emit a proposal.
- Decisions or ADRs:
  - Substeps are read-only only (Explorer/Reviewer/Verifier roles): a mutating substep could violate the one-writer lease against the generic build/repair items, and ADR 0010 defers model-proposed mutation to a separate contract.
  - Rejection is fail-safe, not fail-closed: an invalid proposal is dropped with an auditable `substeps_rejected` event and the Mission continues on its deterministic topology.
  - `AddSubsteps` topologically orders peer specs before `AddWork`, so out-of-order proposals cannot partially apply.
  - No new ADR; ADR 0009/0010 "substeps deferred" wording should be amended in a later docs pass.

## Verification

- Focused tests: `go test ./internal/team/ -count=1` ok (incl. new out-of-order peer dependency case).
- Full tests: `go test ./... -count=1` all packages ok.
- Race test: `go test -race ./internal/team/ ./internal/taskplan/ ./internal/runcontrol/ ./internal/app/ ./internal/evals/ -count=1` ok.
- Vet/build: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok; `git diff --check` clean; no secrets in the diff.
- Evals: the 7-package eval set (`evals team taskplan runcontrol app session web`) passed 3 consecutive runs.
- Skipped checks and reason: E2E/Compose not rerun (no web-asset or packaging change); CI reruns the full baseline on push.

## Acceptance mapping

1. Proposals referencing nonexistent or completed WorkItem ids are rejected before entering the Plan: `ValidateSubstepProposal` rejects them inside `AddSubsteps` before any `Plan.AddSteps` call; graded by the `substep_proposal` eval scenarios and the team reject table.
2. A valid proposal generates Plan steps one-to-one with WorkItems: each accepted spec becomes exactly one WorkItem and exactly one Plan step sharing its id; graded by the valid-accept eval scenario.

## Repository state

- Branch: `agent/opc-a1`, based on `origin/main` (`e788169`).
- Commit/PR: `feat: add explorer-proposed bounded substeps`, PR to `main`.
- Working tree: `compose.yaml` still carries only the owner's local `0.0.0.0` port binding (never commit); `sandbox/.gohermit/` untracked runtime data.
- External state changed: none.

## Harness state

- Session/Run IDs used for live verification: none (deterministic tests only; no paid calls).
- Last event sequence and terminal Run state: not applicable.
- Project memory updated: no.
- Recovery or workspace-reconciliation notes: new substeps persist through the existing Mission checkpoint and session schema v4; no new persistence mechanism.

## Remaining work

- Known limitations: substeps are read-only; mutation-expanding proposals remain deferred per ADR 0010. ADR 0009/0010 wording still says substeps are deferred and should be amended when Phase A lands.
- Next concrete step: owner review of the A1 PR, then A2 (Reviewer severity gating) on `agent/opc-a2`.
- Required user input or authority: PR review/merge remains with the owner.
