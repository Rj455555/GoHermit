# ADR 0010: durable adaptive Run control

## Status

Accepted for `0.5.0-dev`.

## Context

The v0.4 Web layer both translated Team events into Plan changes and published events that could still be waiting for a later checkpoint. Plans used fixed generic titles, Team topology was fixed, and an independent verification failure ended the Mission instead of entering a bounded repair cycle. The owner also needed a Cursor-like review-first Plan mode without granting tool or side-effect permission.

## Decision

1. Persistent events and their Session checkpoint use a prepared `commit.json` journal. Recovery applies the checkpoint and event batch idempotently; presentation delivery occurs only after the commit succeeds.
2. Hidden Worker activity uses a detached durable parent-event commit. Raw tool arguments remain excluded.
3. `internal/runcontrol` owns Team-to-Plan, interruption, and cancellation transitions independently from HTTP or UI code.
4. Plans keep stable execution IDs but use task-specific titles. Team Plans may track concurrent read-only WorkItems with `allow_parallel`; the one-writer Mission invariant remains authoritative.
5. `plan_mode=review` leaves a new Run queued until explicit Plan approval. This approval authorizes starting the already selected Agent only; it does not approve permission-required tools, commits, pushes, deploys, or other side effects.
6. Team topology is selected deterministically from task intent. Mutation topology includes independent preflight and a repair/verify pair. Failed verification may requeue that pair for at most three total verification attempts and remains subject to Mission call/token/time budgets.

## Consequences

- SSE consumers cannot observe a persistent lifecycle event that disappears after process restart.
- Run/Plan transitions are reusable by future CLI or desktop presentations.
- Parallel Plan steps are legal only for mapped concurrent WorkItems; workspace mutation remains single-writer.
- Review-first provides owner control without expanding the tool security boundary.
- Deterministic intent classification is intentionally conservative. Model-proposed substeps and per-role models require separate validated contracts and evals.
