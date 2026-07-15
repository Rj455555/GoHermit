# Security model

## Trust boundaries

The user-selected workspace and configuration are trusted. Model output, tool arguments, HTTP responses, plugin stdout, and stored checkpoints are untrusted. Configured plugin executables are explicitly trusted code with operating-system privileges and are outside the built-in workspace sandbox.

## Workspace and paths

Built-in file operations resolve the workspace to its real path. They reject absolute paths, Windows drive paths, `..`, writes under `.git` or `.gohermit`, credential-like names, and symlinks whose real target leaves the workspace. Nonexistent write paths validate their nearest existing ancestor before directories are created.

## Shell

The shell tool accepts only a narrow grammar without pipes, redirects, substitution, control operators, or absolute/traversal paths. Safe commands are limited to selected Go build/inspection and Git read operations plus `pwd`. Unknown commands return `confirmation_required`; destructive patterns return `blocked`. Non-interactive mode never auto-confirms. When network is disabled, Go subprocesses receive `GOPROXY=off`.

This policy reduces risk but is not an OS sandbox. Build/test tools can execute repository-controlled code. Run GoHermit only on workspaces whose code you are willing to execute.

## Sensitive information

Provider keys are read from configuration or the named environment variable and are used only to set the request Authorization header. Codex Plan credentials are imported from the read-only Codex CLI auth file, refreshed only in memory, and never copied into the workspace. Model requests, headers, keys, and OAuth tokens are not logged. Redaction covers Authorization, API keys, cookies, tokens, passwords, secrets, and common private-key blocks. Credential-like workspace files are denied by built-ins.

The Web API reports only authentication type, configured/not-configured state, and non-secret setup guidance. It never accepts or returns a key or OAuth token. Responses API requests set `store=false` and checkpoint only server-encrypted reasoning continuation items. DeepSeek reasoning needed for a later tool turn is AES-GCM encrypted with a key derived from the configured API key; readable reasoning is never serialized, rendered, or written to event logs.

## Local Web and Docker

The Web surface has no user authentication and must remain bound to host loopback or accessed through an SSH tunnel. Same-origin checks, request limits, a restrictive content security policy, one active run, fixed server-side workspace/config, dropped container capabilities, and no Docker socket reduce its local attack surface. They do not make it suitable for public exposure or hostile repositories.

Owner Profile data is separate from credentials and workspace/project memory. It is bounded, atomically mode-0600 written outside the repository, rejects common credential/token patterns, and enters model context only as a compact explicit profile plus confirmed facts. The owner can view, edit, and forget facts through same-origin APIs.

Multi-agent execution does not expand the workspace boundary. Roles receive fixed tool policies; Explorer, Reviewer, and Lead are read-only, Verifier adds only the bounded test runner, and one Mission writer lease serializes Builder/Operator mutation. Restricted roles receive only plugin tools declared `read` and non-mutating. Agents persist structured Handoffs instead of free-form conversations. Operator remains disabled by default and does not authorize commit, push, deploy, or external messaging.

## Plugins

Plugins may access anything allowed to their OS process. GoHermit limits messages, time, concurrency, stdout protocol, and lifecycle, but does not sandbox filesystem or network access. Keep plugin entries disabled until reviewed; use an OS sandbox for hostile plugins.

## Non-interactive permissions

GoHermit v0.1 never commits, pushes, opens PRs, deploys, changes system configuration, writes outside the workspace, or confirms dangerous commands. Such operations require a future explicit interactive permission flow and remain out of scope now.
