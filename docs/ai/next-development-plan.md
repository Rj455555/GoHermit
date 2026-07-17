# Next development plan

This file starts after the `0.5.0-dev` adaptive Plan/Team milestone. Durable event commits, task-specific titles, review-first approval, intent-based Team topology, parallel read-only preflight, and bounded repair/reverify are implemented; do not plan them again.

## P0: eval-driven Plan refinement

1. Turn `docs/ai/evals/v0.5.md` into checked-in deterministic repository fixtures and graders for Plan fidelity, Handoff quality, recovery, verification, and final owner summary.
2. Let Explorer propose bounded task-specific substeps through a strict schema. Every substep must map to a real WorkItem; completed IDs and revisions cannot be rewritten.
3. Add structured Reviewer issue severity and make repair scheduling depend on actionable findings instead of always running one initial repair pass.
4. Record provider usage consistently for failed, retry, and summary calls and show per-role usage without exposing prompts.
5. Keep one opt-in Codex live smoke outside default tests; paid calls remain disabled by default.

Acceptance: deterministic evals pass three consecutive runs; Plan state never outruns execution facts; recovery never duplicates completed tools or WorkItems.

## P1: personal Team templates and per-role models

1. Add a local Team Template editor with a default provider/model and optional role overrides.
2. Validate provider capabilities and credentials for every selected role before Session creation.
3. Define cost ceilings, retry ownership, fallback audit events, and failure semantics before implementing provider fallback.
4. Keep templates in owner-scoped storage outside repositories; export/import must redact credentials.

Acceptance: unsupported or unconfigured role selections fail before a Run exists; one model failure cannot silently switch vendors.

## P2: scoped tool and Operator approval

Plan review approval does not authorize side effects. Add a separate transport-neutral, scoped, expiring approval request/response contract for permission-required tools and future Operator work.

Acceptance: unattended mode denies approval-required actions; approvals cannot broaden workspace, credential, shell, or network policy; restart preserves pending approval without replaying a completed call.

## P3: isolated writer worktrees

Draft an ADR before implementation. Add temporary Git worktrees only after merge ownership, conflict handling, cleanup, recovery, ignored files, submodules, and user changes have deterministic tests. Until then, keep one writer.

## P4: private background service

After scoped approval is complete, design an optional local daemon, task inbox, schedule, and notifications for the single owner. It must remain local by default and cannot silently commit, push, deploy, message, or publish.

## Explicitly deferred

- Public or multi-user hosting
- Organization accounts and cloud secret management
- Unbounded autonomous Agent creation or free-form Agent chat
- Automatic commit, push, deploy, messaging, or pull-request creation without scoped approval
- Telemetry or a remote control plane
