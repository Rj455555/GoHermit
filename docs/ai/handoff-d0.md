# AI handoff: D0 — ADR 0012 (isolated writer worktrees)

Written 2026-07-22 by Claude, standing in as author while Kimi Code's weekly quota was
exhausted. Reviewer/author overlap for this one task — the owner should read the ADR
themselves before treating it as accepted; this is not a self-certified decision.

## Goal and completed scope

- Drafted `docs/adr/0012-isolated-writer-worktrees.md`, answering all seven questions the D0
  task required (merge ownership/approval, conflict handling, cleanup, recovery across
  restart, .gitignore/submodules, pre-existing owner uncommitted changes, test strategy) with
  no "TBD" left in the document.
- Grounded every decision in the actual current code, not just the prior ADRs:
  - `internal/team/team.go:455-458` (single-writer check) and
    `internal/team/coordinator.go:185-204` (`selectBatch`'s one-writer-per-batch admission) are
    the two places D1 must reinterpret as "one writer per `WorktreeID`" rather than "one writer
    globally" — both cited by line in the ADR.
  - `internal/session/session.go:727` (`GitState`, the existing `git status --porcelain`
    fingerprint) is the mechanism reused for detecting the owner's pre-existing uncommitted
    changes before a parallel-writer Mission starts.
  - Merge-back reuses `internal/approval` (ADR 0011) exactly as-is — no changes to that package
    are anticipated; a merge action is just another scoped, one-shot, owner-approved side
    effect.

## Key decisions (read the ADR itself for the full reasoning)

1. Parallel-writer mode is opt-in per Mission, off by default — every existing single-writer
   Mission is unaffected.
2. One writer per worktree (new optional `WorkItem.WorktreeID`), not one writer globally.
3. Worktree checkout is disposable; the branch is the durable record — cleanup can be
   unconditional (`git worktree remove --force`) because commits survive independently.
4. A dirty main workspace (via existing `GitState`) fails a parallel-writer Mission closed
   rather than guessing how to reconcile the owner's uncommitted edits.
5. Conflicts are detected via a `--no-commit --no-ff` dry run, aborted, and surfaced as a
   blocked Mission state — no agent auto-resolves a real conflict in this ADR's scope.
6. Every merge-back requires explicit owner approval through the existing ADR 0011 contract,
   with no fast-forward/trivial-diff exception.
7. Recovery persists `WorktreeID`/path/branch on the WorkItem itself, checkpointed the same way
   as everything else; restart reuses the worktree if present, recreates it from the branch if
   the checkout was lost, and only re-does work from the last Handoff if both are gone.

## Verification completed

- This is a documentation-only change. `go build ./...`, `go vet ./...` pass (unaffected, run
  as a sanity check, not because the ADR touches Go code).
- No code was written for D1+; nothing here is meant to compile or run yet.

## Pending / next steps

- **The owner must read and accept this ADR before D1 (worktree lifecycle utilities) starts** —
  same gate this project has applied to every ADR so far (0008 before the team orchestration
  code, 0011 before C2).
- Once accepted, D1-D5 follow the existing Phase D task breakdown (worktree lifecycle →
  scheduler support for concurrent mutation WorkItems → reviewed merge-back step → recovery for
  parallel-writer Missions → conflict/cleanup/submodule test suite). Kimi Code resumes there
  once its quota resets.
- This ADR was written by the reviewer, not the usual implementer — extra scrutiny on the
  Decision section specifically is warranted precisely because the author and reviewer are the
  same person for this one document.

## Repository state

- Branch: `agent/opc-d0`, based on `origin/main` after the UI approval panel (`beeabbb`).
- No implementation branch exists yet for D1.
