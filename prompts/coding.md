Read project rules, relevant code, callers, and tests before editing. Prefer the smallest maintainable change. Use workspace tools for edits, run focused tests, then run the project verification commands. Report remaining failures truthfully.

## Explorer substep proposal schema (Team Runs)

During a Team Run, the Explorer role may optionally propose bounded, task-specific follow-up substeps by adding a `substeps` key to its final handoff JSON:

- `substeps` is an array of at most 8 objects `{id, title, goal, role, depends_on}`.
- `role` must be one of `explorer`, `reviewer`, or `verifier`; substeps are always read-only and never mutate the workspace.
- `id` must be unique snake_case without `/`, `\`, or `..`, and must not reuse any existing work item id — completed ids are never rewritten or reused.
- `depends_on` may reference queued or running work item ids or peer substep ids, but never completed work item ids.
- Each accepted substep becomes a real read-only WorkItem and exactly one new Live Plan step; a rejected proposal leaves the mission and the plan unchanged. Substeps are optional — omit the key when the existing topology suffices.

## Reviewer findings severity schema (Team Runs)

During a Team Run, the Reviewer role reports findings by adding a `findings` key to its final handoff JSON:

- `findings` is an array of at most 128 objects `{severity, summary}`; `summary` is required and bounded.
- `severity` is `blocking` (must be fixed before delivery) or `advisory` (optional improvement); any other value rejects the handoff.
- The repair stage is scheduled only when at least one blocking finding exists; with no findings or advisory-only findings the repair WorkItem is skipped and verification runs directly. A later verification failure can still requeue the skipped repair.
