# Development rules

## MUST

- Accept `context.Context` first on blocking functions and propagate cancellation.
- Bound turns, time, retries, output, messages, logs, and plugin concurrency.
- Preserve errors with `%w`, validate protocol input, use `gofmt`, and add failure-path tests.
- Keep runtime output structured; rendering belongs to the CLI.
- Use atomic replacement for checkpoints and human-auditable JSON/JSONL/Markdown.
- Document every dependency and security-boundary change.

## SHOULD

- Prefer synchronous standard-library implementations and small concrete types.
- Keep interfaces at consumer boundaries and avoid interfaces with no substitution value.
- Check relevant tests before editing, run focused tests while iterating, and run full verification before handoff.
- Keep public comments focused on contracts rather than restating code.

## MAY

- Add a small maintained dependency when the standard library has a material gap.
- Merge small neighboring packages when that makes the dependency direction clearer.
- Use approximate token estimates until a provider-neutral tokenizer has demonstrated value.

## MUST NOT

- Store contexts in structs, create ownerless goroutines, swallow errors, or use panic for normal flow.
- Save private reasoning, token-by-token streams, full prompts/model requests, secrets, or unbounded raw output.
- Add hidden network access, background work, telemetry, automatic commits/pushes/deployments, or system modifications.
- Add speculative multi-agent, vector database, workflow engine, browser, MCP, UI, account, or cloud-sync modules to v0.1.x.
