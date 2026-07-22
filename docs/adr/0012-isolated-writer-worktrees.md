# ADR 0012: Isolated writer worktrees for parallel Builders

## Status

Proposed for `0.6.0-dev`. Written by Claude (reviewer role) while Kimi Code's implementation
quota was exhausted, standing in as author for this design step only; implementation (D1+)
does not start until the owner has read and accepted this ADR, same gate as every prior ADR
in this series.

## Context

ADR 0008 gives one workspace a single writer lease: `Mission.Start` refuses to run a second
`MutatesWorkspace` WorkItem while one is `WorkRunning` (`internal/team/team.go:455-458`), and
`selectBatch` (`internal/team/coordinator.go:185-204`) only ever admits one mutating WorkItem
per scheduling batch. This is why today's Personal Agent Team can have several Explorers or
Reviewers running concurrently but only ever one Builder at a time.

The OPC ("one owner, standing team") goal this fork is built toward specifically requires
genuine parallel implementation work — multiple Builders on independent parts of a Mission
making real filesystem changes at the same time. That requires more than one writable
workspace, which requires git worktrees, which requires deciding merge ownership, conflict
handling, cleanup, recovery, and interaction with the owner's own uncommitted work before any
code is written — the same discipline ADR 0011 applied to the approval contract.

Phase C already ships a scoped, expiring, owner-approved side-effect contract
(`internal/approval`, ADR 0011). This ADR reuses it rather than inventing a second approval
mechanism for merges.

## Decision

### 1. Parallel-writer mode is opt-in per Mission, off by default

A Mission runs in today's single-writer mode unless it is explicitly created with
`ParallelWriters: true` (or equivalent budget/template flag, exact plumbing left to D1).
Every existing single-writer Mission, test, and invariant is unaffected. This mirrors how
Operator started disabled-by-default (ADR 0008) — a new capability this consequential ships
opt-in until real usage has exercised it.

### 2. One writer per worktree, not one writer globally

`WorkItem` gains an optional `WorktreeID` field. The single-writer check in
`Mission.Start` (`team.go:455-458`) and the admission logic in `selectBatch`
(`coordinator.go:185-204`) are both reinterpreted: a mutating WorkItem conflicts only with
another *running* mutating WorkItem that shares the same `WorktreeID` (empty `WorktreeID`
keeps today's exact global behavior for non-parallel Missions). Two Builders with different
`WorktreeID`s may both be `WorkRunning` at once. This is additive to the existing struct and
scheduler — it does not replace them.

### 3. Worktree lifecycle: disposable checkout, durable branch

Each parallel Builder gets its own `git worktree` checked out from the Mission's integration
branch (the branch the Mission started on), on its own throwaway branch
(`gohermit/<mission-id>/<work-item-id>`). The Builder commits its own work-in-progress to that
branch as it goes — the branch, not the worktree directory, is the durable record. This means:

- The worktree checkout itself is disposable. Removing it (`git worktree remove --force`)
  never loses committed work, because the branch and its commits survive independently in the
  repository's object database.
- Cleanup of an abandoned worktree (crashed process, cancelled Mission, failed WorkItem) is
  therefore safe to run unconditionally: on Coordinator/daemon start, cross-reference
  `git worktree list` against WorkItems that are not `running`/`queued` in any loaded Mission,
  and `git worktree remove --force` anything unaccounted for. The branch is left alone —
  an owner or a later WorkItem can always inspect it with `git log gohermit/<mission>/<item>`.
- Submodules are not automatically initialized in a new worktree by plain `git worktree add`.
  Worktree creation runs `git submodule update --init --recursive` immediately after `add` when
  the repository has a `.gitmodules` file, so a Builder never silently works against a
  half-populated submodule tree. `.gitignore` needs no special handling — it is repository
  content and applies identically in every worktree.
- GoHermit's own bookkeeping (`.gohermit/` session storage, `session.json`, checkpoints) is
  never duplicated per worktree. Only the source tree being edited is parallelized; Mission and
  Session state remain the single, centrally-checkpointed source of truth they are today.

### 4. Pre-existing uncommitted owner changes: refuse, don't guess

`session.GitState` (`internal/session/session.go:727`) already fingerprints
`git status --porcelain` for the main workspace. Before starting any `ParallelWriters` Mission,
the Coordinator checks this fingerprint: if the main workspace has uncommitted or untracked
changes, the Mission fails to start with an explicit error asking the owner to commit or stash
first. A new worktree checks out a clean ref and cannot see the main workspace's uncommitted
files at all (this is standard git worktree behavior, not something to work around) — so rather
than silently ignoring the owner's in-progress edits, or inventing a stash/reapply mechanism
that could conflict with what a parallel Builder produces, this ADR takes the same conservative
position ADR 0010 took for intent classification: fail closed, require a clean starting point,
revisit only if this proves too restrictive in practice. Single-writer Missions are completely
unaffected by this check.

### 5. Conflict handling: detect, abort, surface — never auto-resolve

When a Builder's worktree WorkItem completes and clears review/verification, merging it back
into the Mission's integration branch is itself a mutating action on the shared workspace and
is itself a WorkItem (`RoleBuilder`-owned or a new lightweight merge role — exact typing left to
D1), scheduled like any other mutating WorkItem (rule 2 applies: it needs the integration
branch's own `WorktreeID`, i.e. the main workspace, as its writer scope).

The merge attempt itself uses `git merge --no-commit --no-ff` as a dry run:

- Clean merge → proceed to rule 6 (owner approval) before committing.
- Conflict → `git merge --abort` immediately, the merge WorkItem fails (not auto-resolved by any
  agent in this ADR's scope), and the Mission surfaces a blocked state for the owner. Automatic
  conflict resolution by an agent is explicitly deferred — it needs its own evidence and design,
  the same way ADR 0008 deferred parallel writing itself until this ADR existed.

### 6. Every merge-back requires explicit owner approval — no auto-merge exception

A worktree merge-back, clean or not, is a side-effecting call under ADR 0011's existing
contract: it creates a scoped `approval.Request` (`tool` identifying the merge action,
`resource_paths` the files the dry-run merge actually touches, same 15-minute TTL, same
one-shot Consume, same plan-revision/policy-fingerprint invalidation). There is no
fast-forward/trivial-diff exception that skips approval — consistency and auditability are
prioritized over convenience for this first version, matching the project's standing "no
automatic commit/push/deploy without scoped approval" invariant (ADR 0008, roadmap "Deferred").
This can be revisited in a later ADR once real usage shows the friction is worth trading away.

### 7. Recovery across restart

`WorkItem` records its `WorktreeID`, worktree path, and branch name — checkpointed the same way
every other WorkItem field is (no second persistence mechanism). On restart, for every
`running` worktree-bound WorkItem: if the worktree directory still exists, resume through the
existing Runner interrupted/resume path exactly as single-writer Missions do today, scoped to
that worktree. If the directory is gone but the branch survives, recreate the worktree from the
branch (no work lost) and resume. Only if both are gone does the WorkItem restart from its last
Handoff, same as any other lost-worktree failure today.

### Test strategy (binding on D1+, not implemented by this ADR)

- Worktree lifecycle: real `git worktree` operations against temporary repositories (this
  codebase already exercises real `git init`/`git diff` in tests, e.g.
  `internal/agent/agent_test.go`'s `TestMutationRequiresSuccessfulTestBeforeCompletion` — no
  mocking git).
- Conflict detection: construct two worktree branches with a genuine textual conflict, assert
  `git merge --abort` leaves the integration branch untouched and the Mission surfaces blocked,
  not partially merged.
- Cleanup: create a worktree, remove its WorkItem's record without calling the cleanup path,
  restart the Coordinator, assert the checkout is pruned but `git branch` still shows the
  commits.
- Recovery: kill the process mid-Builder-write in a worktree, restart, assert resume reuses the
  existing worktree/branch and does not duplicate committed work (mirrors the existing
  interrupted/resume tests for the single-writer path).
- Submodule/.gitignore: a small fixture repository with a real `.gitmodules` entry, assert a
  new worktree has the submodule initialized before any Builder WorkItem starts.
- Pre-existing uncommitted changes: dirty the main workspace, assert a `ParallelWriters` Mission
  fails to start with an explicit error, and that a non-parallel Mission is unaffected.

## Consequences

- `WorkItem` gains optional `WorktreeID`/path/branch fields; existing single-writer Missions
  (empty `WorktreeID`) are byte-for-byte unaffected in behavior.
- A new merge-back WorkItem type/role enters the scheduler, itself gated by the existing
  approval contract — no new side-effect path is created outside ADR 0011's contract.
- Automatic conflict resolution and any stash/reapply handling of the owner's uncommitted work
  are explicitly deferred; both fail closed for now.
- `internal/approval`'s scope model (tool + resource paths + digest) extends naturally to a
  merge action; no changes to `internal/approval` itself are anticipated.
- This is the highest-risk ADR in the series so far (real concurrent filesystem writers); the
  opt-in default and fail-closed choices throughout are deliberate, not placeholders to revisit
  immediately.
