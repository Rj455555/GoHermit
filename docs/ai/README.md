# AI documentation

This directory contains documentation written specifically for coding agents. It is separated from general technical documentation so an agent can load a small, predictable context instead of reading the whole repository.

`AGENTS.md` remains at the repository root only as the automatically discovered bootstrap. Detailed AI context lives here.

## Minimum context

For most tasks, read only:

1. `/AGENTS.md`
2. `context.md`
3. the target package and its tests
4. one relevant technical document selected by `context.md`

This is the preferred low-token path. Do not preload every ADR or topic document.

## Files

- `context.md`: current product boundary, code map, invariants, verified state, and known limitations.
- `next-development-plan.md`: ordered v0.1.1 milestones with acceptance criteria.
- `handoff-template.md`: compact factual format for ending an AI development session.

## General technical documentation

The parent `docs/` directory remains the source of truth for architecture, security, context, sessions, plugins, testing, project structure, roadmap, and ADRs. Those documents are useful to humans and AI, so they are not duplicated here.

## Maintenance rule

Update AI documentation only when its facts change. Prefer links and compact mappings over copied prose. A handoff records facts, decisions, commands, and remaining work—never private reasoning, secrets, full prompts, or raw unbounded output.
