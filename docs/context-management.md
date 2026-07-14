# Context management

## Layers

Context is rebuilt before every model call and ordered as: system policy, workspace `AGENTS.md`, `.gohermit/memory/project.md`, recovered Session summary, active Run state, current goal, and recent messages/tool results. The manager removes exact duplicates before budgeting.

## Budget

The v0.1 estimator uses approximately four UTF-8 bytes per token plus fixed per-message/tool-call overhead. `reserve_output_tokens` is removed first. The compression threshold marks a session for structured summarization; the hard usable limit triggers oldest-layer/message removal and final content clipping. Provider token usage may differ, so configuration should retain margin.

## Truncation and deduplication

Tool output is first bounded at execution. Context construction then deduplicates messages and removes the oldest non-system layers until under budget. A final oversized message is clipped from the front, keeping the most recent portion. The system safety layer is retained.

## Compression

At the compression threshold, the current provider receives a bounded, no-tools request for JSON containing the fixed structured-summary headings. Invalid output or provider failure falls back to a deterministic summary. Neither path contains chain-of-thought, secrets, system/provider request bodies, or raw unbounded output.

Verified Runs merge bounded facts into versioned `project.json` and generate the compact `project.md` view. Each fact records its source Run. Memory updates reject redacted secret-bearing facts and never copy the full conversation.

## Recovery

Resume loads the summary and recent bounded messages, validates workspace identity, reconciles external Git/file changes, and rebuilds current rules and memory from disk. The visible transcript is available for UI history, but only the bounded recent window and summary enter model context.
