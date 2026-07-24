# Next development plan

This file starts after the `0.5.0-dev` adaptive Plan/Team milestone. Durable event commits, task-specific titles, review-first approval, intent-based Team topology, parallel read-only preflight, and bounded repair/reverify are implemented; do not plan them again.

## P0: eval-driven Plan refinement — DONE

1. ~~Turn `docs/ai/evals/v0.5.md` into checked-in deterministic repository fixtures and graders for Plan fidelity, Handoff quality, recovery, verification, and final owner summary.~~ Done: `internal/evals` plus per-package `eval_test.go` graders; see the grader mapping in `docs/ai/evals/v0.5.md`.
2. ~~Let Explorer propose bounded task-specific substeps through a strict schema. Every substep must map to a real WorkItem; completed IDs and revisions cannot be rewritten.~~ Done (A1, PR #5): `team.SubstepSpec` + strict `ValidateSubstepProposal` + atomic `Mission.AddSubsteps` + bounded `taskplan.AddSteps`; see `docs/ai/handoff-a1.md`.
3. ~~Add structured Reviewer issue severity and make repair scheduling depend on actionable findings instead of always running one initial repair pass.~~ Done (A2, PR #6): `Handoff.Findings` with blocking/advisory severity; repair runs only on a blocking finding (`WorkSkipped` otherwise); see `docs/ai/handoff-a2.md`.
4. ~~Record provider usage consistently for failed, retry, and summary calls and show per-role usage without exposing prompts.~~ Done (A3, PR #7): attempt counting in providers/Runner, failed-worker partial usage, `mission.usage_by_role`; see `docs/ai/handoff-a3.md`.
5. ~~Keep one opt-in Codex live smoke outside default tests; paid calls remain disabled by default.~~ Done (A4, PR #8): `GOHERMIT_LIVE_CODEX_SMOKE=1` + workflow-dispatch-only CI job; see `docs/ai/handoff-a4.md`.

Acceptance: deterministic evals pass three consecutive runs; Plan state never outruns execution facts; recovery never duplicates completed tools or WorkItems. — Met; see `docs/ai/handoff-a4.md` Phase A closeout.

## P1: personal Team templates and per-role models — DONE

1. ~~Add a local Team Template editor with a default provider/model and optional role overrides.~~ Done (B1+B5, PRs #10/#15): `internal/teamtemplate` owner-scoped store; per-role overrides drive real worker runtime selection (template editor UI remains future work; the schema/store/execution path is complete); see `docs/ai/handoff-b1.md` and `docs/ai/handoff-b5.md`.
2. ~~Validate provider capabilities and credentials for every selected role before Session creation.~~ Done (B2, PR #11): pre-creation per-role catalog/credential/capability validation fails synchronously with no partial state; see `docs/ai/handoff-b2.md`.
3. ~~Define cost ceilings, retry ownership, fallback audit events, and failure semantics before implementing provider fallback.~~ Done (B3, PR #12): `Budget.RoleLimits` + retry-ownership contract + `provider_fallback` audit event (contract only; no fallback switching); see `docs/ai/handoff-b3.md`.
4. ~~Keep templates in owner-scoped storage outside repositories; export/import must redact credentials.~~ Done (B1/B4, PRs #10/#13): owner-scoped store; export blanks secrets, import rejects them via the shared `owner.LooksSecret`; see `docs/ai/handoff-b4.md`.

Acceptance: unsupported or unconfigured role selections fail before a Run exists; one model failure cannot silently switch vendors. — Met; fallback switching intentionally unimplemented (contract layer only).

## P2: scoped tool and Operator approval — DONE (Operator role still disabled)

Plan review approval does not authorize side effects. Add a separate transport-neutral, scoped, expiring approval request/response contract for permission-required tools and future Operator work. — Done: ADR 0011 (C1, PR #16); `internal/approval` storage + lifecycle + Session schema v5 (C2, PR #20); expiry triggers wired into run transitions (C2b, PR #21); shell `ConfirmationRequired` calls produce real requests with a concurrency-safe rendezvous (C3, PR #22); workbench approval panel (PR #23). See `docs/ai/handoff-c1.md` … `docs/ai/handoff-c3.md` and `docs/ai/handoff-ui-approvals.md`.

Acceptance: unattended mode denies approval-required actions; approvals cannot broaden workspace, credential, shell, or network policy; restart preserves pending approval without replaying a completed call. — Met (unattended expiry denies; executor re-validates; pending survives restart, consumed never replays).

## Status note (2026-07-24)

- PR #27 (`fix/verifier-synthetic-check`) is merged as `a3e396e` (owner action after the 2026-07-24 audit was written): read-only Team Runs pass verification with `Checks == [] && Issues == []`; mutation runs still require at least one real passing Check. `team.HandoffChecksPassed` is the single definition shared with runcontrol.
- Historical drafts PR #2, PR #3, PR #4 are stale: their heads are ancestors of `main` and contain no unmerged product code. They are safe for the owner to close; closing is left to the owner (no GitHub write actions by the agent).

## P3: isolated writer worktrees

Draft an ADR before implementation. Add temporary Git worktrees only after merge ownership, conflict handling, cleanup, recovery, ignored files, submodules, and user changes have deterministic tests. Until then, keep one writer.

Status: ADR 0012 drafted (PR #24) but UNRESOLVED — its WIP self-commit recovery model conflicts with the no-auto-commit invariant (see gap analysis section 6.3). Owner decision required before any worktree implementation.

## P4: private background service

After scoped approval is complete, design an optional local daemon, task inbox, schedule, and notifications for the single owner. It must remain local by default and cannot silently commit, push, deploy, message, or publish.

## Loop Mode (v0.6, from docs/gohermit-gap-analysis-next-prd-2026-07-24.md)

In progress under that document's narrowed scope: PR28 docs calibration (this note), then Control Plane application services, Loop Domain/Store, Dry Run, Manual Invocation, and Verification Recipe. Worktree isolation, Loop UI, cron/daemon, Task Inbox/Artifacts, and any Publisher remain deferred.

## Explicitly deferred

- Public or multi-user hosting
- Organization accounts and cloud secret management
- Unbounded autonomous Agent creation or free-form Agent chat
- Automatic commit, push, deploy, messaging, or pull-request creation without scoped approval
- Telemetry or a remote control plane
