# Next development plan

GoHermit `0.7.0-dev` Electronic Employees is implemented through Phase 9.
Phase 10 is eval, Docker persistence, documentation, and version closeout only.
Do not reimplement Employee, Task, Prepare/Start, Web UI, or Team mapping.

## Frozen v0.7 boundary

- One owner, one foreground service, one startup-configured Workspace.
- Manual Task creation and explicit Start; no queue daemon or scheduler.
- Existing Session/Run/Plan/Approval/Verification/Event/Tool/recovery kernel.
- One running Task per Employee and one Workspace mutation writer.
- Local configured-root Skills and Knowledge only.
- Owner-confirmed Employee Memory; no automatic promotion.
- No automatic commit, push, PR, merge, deploy, or external message.

## v0.8 candidates requiring separate Owner gates

1. **Shared owner Employee Store across per-Workspace instances.** Specify
   cross-process revision coordination, file locks, crash ownership, and
   ProjectBinding identity in a new ADR before code.
2. **Artifact/report workflows.** Add explicit export/publish approvals and
   destinations without treating a verified Artifact as permission to send,
   commit, or deploy it.
3. **Read-only concurrency.** Prove tool classification and Workspace
   observations are race-free before allowing Tasks from different Employees
   to overlap.
4. **Worktree isolation.** Resolve ADR 0012's cleanup, conflict, ignored-file,
   submodule, and no-auto-commit questions first.
5. **Memory quality.** Improve Candidate review, provenance explanation, and
   contradiction handling; automatic promotion remains out of scope.
6. **Optional authentication for non-loopback deployments.** Public hosting,
   accounts, tenancy, remote secrets, and telemetry require a distinct threat
   model and are not incremental UI work.

## Explicitly deferred

- Multi-user/cloud control plane or organization tenancy.
- Cron, daemon, unattended queue, auto-start-next, or notifications.
- Remote Skill install/update, executable Skill content, or dependency install.
- Remote Knowledge crawl, embeddings, or vector database.
- Free-form Employee-to-Employee chat or private Memory sharing.
- Model/vendor fallback without an explicit audited contract.
- Parallel Workspace mutation writers.
- Automatic Git/publish/deploy/message actions.
- Avatar uploads, filesystem paths, or remote URLs.

Every candidate must begin with a plan/ADR, deterministic acceptance tests, and
an explicit Owner gate. Version `0.7.0-dev` remains unreleased until separately
authorized.
