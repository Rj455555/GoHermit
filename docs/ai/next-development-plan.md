# Next development plan

This file starts after the `0.3.0-dev` Personal Agent Team milestone. Session/Run, Owner Profile, Mission/WorkItem/Handoff, the default six-stage team workflow, stable Worker recovery, one-writer policy, Verifier gate, and team Web UI are implemented; do not plan them again.

## P0: live team evaluation and repair policy

1. Add an explicit opt-in live smoke test that runs one small Team Mission with Codex; keep paid calls outside default tests.
2. Build checked-in deterministic repository fixtures that score handoff quality, edits, review findings, verification, model calls, tokens, recovery, and final owner summary.
3. Replace the fixed single repair pass with a bounded review/repair loop driven by structured issue severity, capped by Mission budget.
4. Use provider token counters consistently, including failed and summary calls, and surface per-role usage in the UI.

Acceptance: quality changes are measured; a failed Verifier never reaches Lead; raw prompts, private reasoning, and unbounded output remain absent from storage.

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
