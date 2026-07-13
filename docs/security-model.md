# Security model

## Trust boundaries

The user-selected workspace and configuration are trusted. Model output, tool arguments, HTTP responses, plugin stdout, and stored checkpoints are untrusted. Configured plugin executables are explicitly trusted code with operating-system privileges and are outside the built-in workspace sandbox.

## Workspace and paths

Built-in file operations resolve the workspace to its real path. They reject absolute paths, Windows drive paths, `..`, writes under `.git` or `.gohermit`, credential-like names, and symlinks whose real target leaves the workspace. Nonexistent write paths validate their nearest existing ancestor before directories are created.

## Shell

The shell tool accepts only a narrow grammar without pipes, redirects, substitution, control operators, or absolute/traversal paths. Safe commands are limited to selected Go build/inspection and Git read operations plus `pwd`. Unknown commands return `confirmation_required`; destructive patterns return `blocked`. Non-interactive mode never auto-confirms. When network is disabled, Go subprocesses receive `GOPROXY=off`.

This policy reduces risk but is not an OS sandbox. Build/test tools can execute repository-controlled code. Run GoHermit only on workspaces whose code you are willing to execute.

## Sensitive information

Provider keys are read from configuration or the named environment variable and are used only to set the request Authorization header. Model requests, headers, and keys are not logged. Redaction covers Authorization, API keys, cookies, tokens, passwords, secrets, and common private-key blocks. Credential-like workspace files are denied by built-ins.

## Plugins

Plugins may access anything allowed to their OS process. GoHermit limits messages, time, concurrency, stdout protocol, and lifecycle, but does not sandbox filesystem or network access. Keep plugin entries disabled until reviewed; use an OS sandbox for hostile plugins.

## Non-interactive permissions

GoHermit v0.1 never commits, pushes, opens PRs, deploys, changes system configuration, writes outside the workspace, or confirms dangerous commands. Such operations require a future explicit interactive permission flow and remain out of scope now.
