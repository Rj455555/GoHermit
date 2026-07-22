# C1 handoff: approval contract ADR (docs only)

## Goal

- Requested outcome: design — document only, no code — a transport-neutral, scoped, expiring approval request/response contract for (a) permission-required tool calls and (b) the future Operator role, as Phase C's first step (upstream P2).
- Scope actually handled: `docs/adr/0011-scoped-expiring-approval-contract.md`. Number 0011 was the next free ADR number (latest was 0010).

## Decision summary (the questions the ADR had to answer)

- **Relationship to `plan_mode=review`**: two separate authorization layers. Plan approval authorizes *starting the Agent*; call approval authorizes *one specific side-effecting call*. Neither implies the other; the new contract does not replace or weaken plan approval.
- **Scope**: an approval binds exactly one call — tool name, exact workspace-relative resource path(s), argument digest, identity (session/run/mission/work-item/role), and a policy fingerprint (workspace root + policy config). Any change requires a new request; approvals never broaden workspace/credential/shell/network policy, and the executor re-validates at execution time.
- **One-shot vs reusable, expiry**: one-shot only (reusable approvals excluded); 15-minute TTL, lazily enforced, plus immediate invalidation on Run termination, Plan revision change, or policy fingerprint change.
- **Unattended default**: deny. No auto-approval in non-interactive contexts; denial (explicit/expired/unattended) returns a structured tool result so the Run can continue.
- **Restart recovery, no replay**: pending requests persist in the existing Session checkpoint/journal (no second persistence mechanism) and reload as pending; a call executes only after its approval is durably committed; a crash between approval and execution leaves the call `uncertain` and un-executed (new request required); consumed approvals never re-execute.

## Verification

- Documentation-only task: no code changed, no test verification applies. The ADR follows the Status/Context/Decision/Consequences structure of the existing ADRs and was written against ADR 0008/0009/0010 (re-read for this task).

## Repository state

- Branch: `agent/opc-c1`, based on `origin/main` (developed in a separate git worktree to parallelize with B5).
- Commit/PR: `docs: add scoped expiring approval contract ADR`, PR to `main`.
- Working tree: main worktree untouched; `compose.yaml` local binding and `sandbox/.gohermit/` unaffected.

## Remaining work

- Next concrete step: owner ratifies the ADR (PR review). Only after the ADR ships as Accepted does C2 (storage + request/decision lifecycle implementation) begin.
- Required user input or authority: PR review/merge remains with the owner.
