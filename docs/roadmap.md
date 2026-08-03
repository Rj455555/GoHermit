# Roadmap

## Completed development milestones

- **v0.1 core:** bounded single-Agent loop, tools, policy, checkpoints, plugins.
- **v0.2 local workbench:** provider/access catalog, persistent Sessions/Runs,
  SSE, cancellation/recovery, project memory, local Web and Docker.
- **v0.3 Personal Agent Team:** bounded Mission/WorkItems/Handoffs, one writer,
  independent verification, Owner Profile.
- **v0.4 Live Plan:** durable public checklist, recovery, and real lifecycle
  progress.
- **v0.5 approval and recovery hardening:** commit journals, review-first Plan,
  scoped approval, adaptive topology, repair/reverify, deterministic evals.
- **v0.6 Loop Workbench:** owner-scoped revisioned Loops, zero-side-effect Dry
  Run, manual Invocation, Verification Recipe, Session-backed Timeline.
- **v0.7 Electronic Employees:** owner-scoped Employee revisions,
  Skill/Knowledge/Memory/Project context, explicit Task Prepare/Start,
  Employees/Tasks Web UI, bounded Artifacts/Candidates, and optional Team Role
  assignment. Phase 10 performs final eval/Docker/docs closeout; version remains
  `0.7.0-dev` and is not a formal release.

## v0.8 candidate themes

- First-class owner-scoped Weixin channel with QR login, account isolation,
  cursor-backed polling, explicit-Start Employee Task inbox, bounded outbox,
  and Settings bindings.

- Cross-process owner Employee Store semantics for one instance per Workspace.
- Explicit Artifact/report export and publish approval workflows.
- Evidence-backed cross-Employee read-only concurrency.
- Worktree isolation only after ADR 0012 is resolved.
- Better Memory contradiction/review UX without automatic promotion.
- A separately threat-modeled authenticated deployment mode.

## Deferred

Vector/embedding memory, browser automation, marketplace, public/hosted UI,
accounts, collaboration, cloud sync, telemetry, analytics, schedulers, daemons,
automatic unapproved Git/publish/deploy, parallel Workspace writers, Kubernetes
SDK integration, Go `.so` plugins, and a general workflow engine remain
deferred. Each requires separate evidence and Owner authorization.
