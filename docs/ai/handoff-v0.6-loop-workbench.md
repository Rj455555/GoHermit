# GoHermit v0.6 Loop Workbench handoff

## Read first

The next coding agent must read, in order:

1. `AGENTS.md`
2. `docs/ai/context.md`
3. `docs/ai/next-development-plan.md`
4. this handoff

The current product version is `0.6.0-dev`. PR #28–#33 are complete:
documentation calibration, `internal/controlplane`, Loop Domain/Store, Dry Run,
Manual Invocation, and Verification Recipe are implemented and must not be
recreated.

## Delivered in this milestone

- Added the Loop Definition and Invocation Web resource handlers in
  `internal/web/loops.go`. They only parse HTTP, enforce same-origin/body
  limits/strict JSON, map status codes, serialize JSON, and call
  `internal/controlplane`.
- Added Control Plane create/update operations. `internal/loopstore` remains
  the owner of Definition timestamps and monotonically increasing revisions.
- Added Dashboard / Agent / Loops / Settings navigation and the Codex-style
  Loop Workbench.
- Added ordinary form controls for identity, fixed task Prompt, configured
  company/access/model, Agent/Team, Team Template, Plan mode, workspace and
  approval policy, argv-only verification checks, budget, repairs, and output.
- Added a zero-side-effect Dry Run Review and disabled start whenever
  readiness is false.
- Added manual Invocation start, bounded history, cancellation, and a
  restorable Timeline assembled from the bound Session/Run, Plan, Team,
  bounded tool summaries, Approval records, Verification evidence, and final
  owner summary.
- Reused the existing Session SSE endpoint with persisted sequence
  continuation. No Loop event bus, runtime, Run state machine, Verifier,
  Approval store, or resume state machine was added.
- Added `examples/loops/document-maintenance.json`, a credential-free,
  read-only template that never commits, pushes, opens a PR, merges, or
  deploys.
- Added Go HTTP/control-plane failure-path tests and Fake Provider Playwright
  coverage. Default tests never call a paid model.

## Data and security boundaries

- Definitions and Invocations remain owner-scoped and outside target
  repositories.
- A Definition update creates a new revision. Existing Invocation snapshots
  do not change.
- Every Invocation creates at most one independent Session/Run and relies on
  existing recovery and cancellation.
- Mutation Definitions require a real required verification check in the Web
  form; runtime acceptance still fails closed on missing independent evidence.
- Verification commands are argv arrays; no shell command string is composed.
- Loop bodies are capped at 256 KiB, reject unknown fields, and screen
  credential markers before persistence.
- Browser responses do not contain API keys, OAuth tokens, system prompts,
  private reasoning, or unbounded raw tool output.
- Compose remains loopback-only. Existing Owner, credential, Team Template,
  Session, and Loop data stay in the persistent `/data` volume.
- GoHermit product behavior still never commits, pushes, opens PRs, merges, or
  deploys automatically.

## Verification

Final verification and Docker/CI results are recorded in the Draft PR and the
final development-agent handoff. At implementation checkpoint:

- Control Plane, Loop Store, and Web target-package Go tests pass on the Mac.
- Playwright passes all 7 tests, including existing Approval/Review Plan tests
  and the new Loop Workbench suite.
- Frontend JavaScript syntax checks pass for `app.js`, `loops.js`, and the
  static E2E server.

## Remaining product work

- Worktree Foundation is postponed. ADR 0012 remains unresolved because its
  recovery commit proposal conflicts with the no-auto-commit invariant.
- v0.7 is Task Inbox and Shared Artifacts. The first slice remains manual and
  foreground.
- Cron, a background daemon, notifications, Publisher actions, public hosting,
  multi-user auth, and automatic commit/push/PR/deploy remain deferred.
- Do not commit the user's untracked large PRD drafts, AI/ECC configuration,
  `.codegraph/`, or `sandbox/.gohermit/`.
