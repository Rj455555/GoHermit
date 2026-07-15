# Next development plan

This file starts after the `0.4.0-dev` Live Plan milestone. Session/Run, Owner Profile, the default Personal Agent Team, stable Worker recovery, one-writer policy, Verifier gate, and durable owner-facing checkbox Plan are implemented; do not plan them again.

## P0: task-specific Plan refinement and team evaluation

1. Build checked-in deterministic repository fixtures that score Plan fidelity, Handoff quality, edits, review findings, verification, recovery, and final owner summary.
2. Let Explorer propose bounded task-specific substeps only through a validated structure that maps every substep to one or more real WorkItems; preserve completed IDs/revisions during refinement.
3. Replace the fixed single repair pass with a bounded review/repair loop driven by structured issue severity and reflected honestly in the Plan.
4. Use provider token counters consistently, including failed and summary calls, and surface per-role usage in the UI.
5. Keep one explicit opt-in live smoke that runs a small Codex Team Mission; paid calls stay outside default tests.

Acceptance: a Plan never claims completion before its mapped execution facts; refinement cannot rewrite completed history; a failed Verifier never reaches Lead; raw prompts, private reasoning, and unbounded output remain absent from storage.

## P1: per-role model policy

1. Add a Team Template editor that chooses one default model plus optional role overrides.
2. Add provider capability flags and reject unsupported role/model combinations at Session creation.
3. Specify cost, retry, fallback, and audit semantics before adding ordered provider fallback.

Acceptance: selection remains server-validated and credential-filtered; Agent Core and team domain remain vendor-neutral.

## P2: interactive approval and Operator

1. Define a transport-neutral, scoped, expiring approval request/response contract.
2. Enable Operator only for explicitly approved deploy, commit, push, or external side effects.
3. Add crash/restart tests around pending approval and preserve completed-tool replay protection.

Acceptance: unattended mode denies approval-required work; approval cannot bypass workspace, credential, or shell policy.

## P3: isolated writer worktrees

Draft an ADR before implementation. Add temporary Git worktrees only after merge ownership, conflict handling, cleanup, recovery, ignored files, submodules, and user changes have deterministic tests. Until then, keep one writer.

## P4: personal background service

After approval semantics are complete, design an optional local daemon, task inbox, schedule, and notifications. It must remain single-owner and local by default, with explicit action scopes and no silent deployment or messaging.

## Explicitly deferred

- Public or multi-user hosting
- Organization accounts and cloud secret management
- Unbounded autonomous agent creation or free-form agent chat
- Automatic commit, push, deploy, or pull-request creation without approval
- Telemetry or remote control plane
