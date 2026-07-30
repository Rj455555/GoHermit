# Employee loops

Employee Loops are durable recurring jobs owned by an Electronic Employee.
They borrow the useful contract/state/log split from LoopAny while preserving
GoHermit's existing execution kernel.

## Ownership and execution

```text
Employee 1 ──owns──> Loop 1..N
Loop ──dispatches──> EmployeeTask ──Prepare──> Session ──Start──> Run
Loop state <──projection── Invocation / Session / Run
```

Employee is the long-lived Agent identity. A Loop is one repeatable job fo
that Employee. Each invocation creates an ordinary immutable EmployeeTask, so
the run receives the Employee revision, enabled Skills, ready Knowledge
citations, accepted private Memory Facts, ProjectBinding, policy, and budget.
There is no second runtime, tool lifecycle, verifier, recovery engine, or SSE
journal.

## Durable files

`GOHERMIT_LOOP_STORE` contains three separate kinds of data:

- the existing versioned Definition and Invocation journal;
- `contracts/{loop-id}/LOOP.md`, generated from the immutable contract fields;
- `states/{loop-id}.json`, a bounded rebuildable scheduling/status projection.

`LOOP.md` contains Goal, Boundaries, SOP, Definition of Done, Stop Conditions,
Employee identity, revision, and trigger. Mutable counters and run logs neve
enter the contract. Invocation records remain the run log and retain thei
Session/Run/EmployeeTask references.

## Schedule and at-most-once dispatch

The initial scheduler supports `manual` and one daily `HH:MM + IANA timezone`
trigger. One service instance is the single scheduler/writer for its configured
Workspace. It advances and persists `next_run_at` before dispatching through
the EmployeeTask path, preventing the same occurrence from being launched
twice after refresh or restart. Multiple service instances sharing the store
remain unsupported.

The scheduler never bypasses readiness: every invocation revalidates Employee,
provider/model credentials, Workspace/ProjectBinding, Skills, Knowledge,
Memory, and the effective permission intersection before a Run can exist.

## API and UI

- `GET /api/loops` returns full Definitions for contract cards.
- `GET /api/loops/{id}/contract.md` returns the generated `LOOP.md`.
- `GET /api/loops/{id}/runtime` returns the bounded state projection.
- Existing Definition, Dry Run, Invocation, cancellation, Session and SSE
  endpoints remain authoritative.

The React Workbench presents When / Does / You get cards, a short create path,
and a Contract / State / Logs detail view. Provider, Team, policy, budget, and
verification configuration remains available under Advanced settings.
Employee details include a Loops tab filtered by `employee_id`.

## Code map

- Domain and rendering: `internal/loop/contract.go`
- Contract/state persistence: `internal/loopstore/runtime.go`, `store.go`
- EmployeeTask dispatch and scheduler: `internal/controlplane/employee_loops.go`
- HTTP projection: `internal/web/loops.go`
- React UI: `web/src/features/loops/LoopsPage.tsx`
