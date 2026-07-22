# ADR 0011: Scoped, expiring approval contract for side-effecting calls

## Status

Accepted for `0.6.0-dev` (Phase C contract; implementation begins with C2).

## Context

ADR 0010 introduced `plan_mode=review`: the owner approves a Run's Live Plan before the selected Agent starts. That approval authorizes *starting the Agent* — it explicitly does not authorize any permission-required tool, commit, push, deploy, or other side effect. Today, permission-required tool calls surface as non-interactive events and are never auto-approved; there is no contract by which an owner can grant a specific side-effecting call, and no safe path for the future Operator role (disabled by default per ADR 0008).

Phase C needs a transport-neutral, scoped, expiring approval request/response contract that covers (a) existing permission-required tool calls, which must become asynchronously approvable instead of synchronously blocked, and (b) future Operator operations, without widening any existing security boundary.

## Decision

### Two authorization layers, kept separate

There are exactly two layers, and neither implies the other:

1. **Plan approval** (ADR 0010, unchanged): authorizes starting the already-selected Agent for one Run.
2. **Call approval** (this contract): authorizes one specific side-effecting call.

The new contract does not replace, weaken, or subsume plan approval. An approved Plan grants no call approvals; an approved call grants no Plan rights. A Run in `plan_mode=review` still cannot start without Plan approval even if call approvals exist, and an approved call is void if its Run was never Plan-approved when required.

### The approval request

An approval request is a durable, bounded record containing:

- `request_id` — unique, generated at request time.
- Identity: `session_id`, `run_id`, and, for Team Runs, `mission_id`, `work_item_id`, and `role`.
- Scope: the exact tool name, the exact workspace-relative resource path(s) it will touch (already constrained by the workspace containment invariant), and a bounded, redacted summary of the arguments plus a digest of the canonical argument payload. Raw secrets are never stored; argument rendering follows the existing no-raw-tool-arguments persistence rule.
- Policy fingerprint: the workspace root and the policy configuration under which the call was requested. An approval is valid only under that fingerprint.
- `created_at`, `expires_at`, and `status`: `pending` / `approved` / `denied` / `expired` / `consumed`.

### Scope semantics

Approval binds exactly one call: the named tool, the named resource path(s), and the argument digest. Any change to tool, path, or arguments requires a new request. An approval never broadens workspace, credential, shell, or network policy — the executor re-validates the call against current policy at execution time, and a policy change invalidates the approval.

### One-shot and expiry

- Every approval is **one-shot**: approving a request permits exactly one execution of the exact call. On execution the request becomes `consumed` and can never authorize another call. Reusable or blanket approvals are deliberately excluded from this contract.
- Every request **expires**: `expires_at = created_at + 15 minutes`. Expiry is evaluated lazily at decision time and at execution time; an expired pending request becomes `expired` and the call is treated as denied. Expiry also fires on: Run termination, Plan revision change, or policy fingerprint change — each immediately invalidates all pending requests of that scope.

### Unattended default: deny

When no owner decision arrives — including fully unattended operation — pending requests are denied by default. Non-interactive contexts never auto-approve, matching the existing invariant. Denial (explicit, expired, or unattended) is returned to the model as a structured tool result, so the Run can continue without the side effect instead of failing blindly.

### Durability, recovery, and no replay

- Approval requests and decisions persist in the existing Session checkpoint/journal (schema-versioned; no second persistence mechanism), and decisions emit sequenced runtime events.
- On restart, `pending` requests reload as `pending` (still subject to expiry); the owner may decide them after recovery.
- A call executes only after its approval is durably committed. If the process crashes between approval and execution, recovery marks the call `uncertain` and does **not** re-execute it — the existing completed-tool replay guard prevents replay of completed calls, and an approved-but-unexecuted call requires a new request. A `consumed` approval is never re-executed under any recovery path.

### Transport neutrality

The contract is defined over durable records and runtime events, not HTTP: the Web surface (and any future presentation) renders a pending request and submits a decision through the same Session/Run event machinery. The Operator role remains disabled by default; enabling it later requires only role policy, not contract changes.

## Consequences

- Permission-required tools gain an asynchronous approval path; unattended behavior stays deny-by-default.
- No auto-approval, auto-commit, auto-push, deploy, or PR path is created; approvals cannot broaden any existing boundary.
- Session schema gains versioned approval records with an explicit migration (like v1–v4 before it).
- Recovery semantics stay deterministic: pending survives restart, consumed/executed calls never replay.
- C2 implements storage and the request/decision lifecycle; tool-specific request production (shell, filesystem, future Operator) lands in later, separately reviewed tasks.
