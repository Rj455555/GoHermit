package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/loop"
)

// runLoop implements `hermit loop <dry-run|list>`. It lives in cmd rather
// than internal/app because the dependency direction is cli → controlplane →
// app: internal/app cannot import controlplane without a cycle. The command
// constructs the controlplane Service directly — no HTTP server, no runtime
// — proving the application-service boundary works from the CLI. The
// publisher is nil: a dry run commits no events.
//
// Exit codes: 0 when the report is ready (or the listing succeeded), 1 when
// the report is not ready or the call failed, 2 for usage errors.
func runLoop(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "hermit: loop requires a subcommand: dry-run or list")
		return 2
	}
	switch args[0] {
	case "dry-run":
		return loopDryRun(ctx, stdout, stderr, args[1:])
	case "list":
		return loopList(ctx, stdout, stderr, args[1:])
	default:
		fmt.Fprintln(stderr, "hermit: unknown loop subcommand: "+args[0])
		return 2
	}
}

func loopFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cwd, _ := os.Getwd()
	workspace := fs.String("workspace", cwd, "workspace directory")
	configPath := fs.String("config", "", "configuration file")
	return fs, workspace, configPath
}

func loopService(workspace, configPath string) (*controlplane.Service, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	service, err := controlplane.New(abs, configPath, nil)
	if err != nil {
		return nil, fmt.Errorf("start control plane: %w", err)
	}
	return service, nil
}

func loopDryRun(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs, workspace, configPath := loopFlagSet("loop dry-run", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "hermit: loop dry-run requires exactly one loop id")
		return 2
	}
	service, err := loopService(*workspace, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	report, err := service.DryRunLoop(ctx, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	printDryRunReport(stdout, report)
	if !report.Ready {
		return 1
	}
	return 0
}

func loopList(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs, workspace, configPath := loopFlagSet("loop list", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	service, err := loopService(*workspace, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	definitions, err := service.ListLoops()
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	if len(definitions) == 0 {
		fmt.Fprintln(stdout, "No loop definitions.")
		return 0
	}
	for _, definition := range definitions {
		enabled := "enabled"
		if !definition.Enabled {
			enabled = "disabled"
		}
		fmt.Fprintf(stdout, "%s\trevision %d\t%s\t%s\n", definition.ID, definition.Revision, definition.Name, enabled)
	}
	return 0
}

func printDryRunReport(w io.Writer, report loop.DryRunReport) {
	fmt.Fprintf(w, "Loop dry run: %s\n", report.LoopID)
	valid := "valid"
	if !report.DefinitionValid {
		valid = "INVALID"
	}
	fmt.Fprintf(w, "Definition: revision %d (%s)\n", report.DefinitionRevision, valid)
	match := "matches this workspace"
	if !report.WorkspaceMatches {
		match = "DOES NOT match this workspace"
	}
	git := "clean"
	if !report.GitClean {
		git = "dirty or not a repository"
	}
	fmt.Fprintf(w, "Workspace: %s (%s); git: %s\n", report.WorkspaceIdentity, match, git)
	fmt.Fprintf(w, "Task: %s\n", report.TaskPrompt)
	fmt.Fprintf(w, "Agent: %s/%s model=%s agent=%s\n", report.Agent.Company, report.Agent.Access, report.Agent.Model, report.Agent.Agent)
	if report.TeamTemplateRef != "" {
		fmt.Fprintf(w, "Team template: %s\n", report.TeamTemplateRef)
	}
	if len(report.Roles) > 0 {
		fmt.Fprintln(w, "Roles:")
		for _, role := range report.Roles {
			mark := "✓"
			if !role.CredentialConfigured {
				mark = "✗"
			}
			name := role.Role
			if name == "" {
				name = "agent"
			}
			fmt.Fprintf(w, "  %s %-9s %s/%s model=%s — %s\n", mark, name, role.Company, role.Access, role.Model, role.Detail)
		}
	}
	fmt.Fprintf(w, "Write scope: %s\n", report.WriteScope)
	if len(report.Checks) > 0 {
		fmt.Fprintln(w, "Checks:")
		for _, check := range report.Checks {
			required := "optional"
			if check.Required {
				required = "required"
			}
			fmt.Fprintf(w, "  - %s: %s (%s, timeout %ds)\n", check.ID, strings.Join(check.Command, " "), required, check.TimeoutSeconds)
		}
	}
	fmt.Fprintf(w, "Budget: %d model calls, %d tokens, %ds timeout\n", report.Budget.MaxModelCalls, report.Budget.MaxTokens, report.Budget.TimeoutSeconds)
	approval := "not required"
	if report.RequiresApproval {
		approval = "required before mutation"
	}
	fmt.Fprintf(w, "Approval: %s\n", approval)
	if report.Ready {
		fmt.Fprintln(w, "Verdict: READY")
		return
	}
	fmt.Fprintln(w, "Verdict: NOT READY")
	for _, reason := range report.Reasons {
		fmt.Fprintf(w, "  - %s\n", reason)
	}
}
