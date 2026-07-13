# Context management

## Layers

Context is ordered as: system policy, workspace `AGENTS.md`, `.gohermit/memory/project.md`, recovered structured summary, current goal, and recent messages/tool results. The manager removes exact duplicate messages before budgeting.

## Budget

The v0.1 estimator uses approximately four UTF-8 bytes per token plus fixed per-message/tool-call overhead. `reserve_output_tokens` is removed first. The compression threshold marks a session for structured summarization; the hard usable limit triggers oldest-layer/message removal and final content clipping. Provider token usage may differ, so configuration should retain margin.

## Truncation and deduplication

Tool output is first bounded at execution. Context construction then deduplicates messages and removes the oldest non-system layers until under budget. A final oversized message is clipped from the front, keeping the most recent portion. The system safety layer is retained.

## Compression

The structured summary contains only the current goal, completed work, modified files, commands, test results, confirmed decisions, current problems, remaining work, and recovery information. It does not contain chain-of-thought or secret material. v0.1 creates this summary deterministically from session facts; a future model-assisted compressor must preserve the same schema and safety rules.

## Recovery

Resume loads the summary and recent bounded messages, validates checkpoint/workspace/file state, and rebuilds current project rules and memory from disk. Full historical dialogue is intentionally unnecessary.
