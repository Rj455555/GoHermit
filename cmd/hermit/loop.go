package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/loop"
)

// runLoop implements `hermit loop <dry-run|list|run|history|cancel>`. It
// lives in cmd rather than internal/app because the dependency direction is
// cli → controlplane → app: internal/app cannot import controlplane without
// a cycle. The command constructs the controlplane Service directly — no
// HTTP server, no runtime — proving the application-service boundary works
// from the CLI.
//
// Exit codes: 0 when the report is ready, the listing succeeded, the
// invocation completed, or the cancellation was accepted; 1 otherwise; 2 for
// usage errors.
func runLoop(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "hermit: loop requires a subcommand: dry-run, list, run, history, or cancel")
		return 2
	}
	switch args[0] {
	case "dry-run":
		return loopDryRun(ctx, stdout, stderr, args[1:])
	case "list":
		return loopList(ctx, stdout, stderr, args[1:])
	case "run":
		return loopRun(ctx, stdout, stderr, args[1:])
	case "history":
		return loopHistory(ctx, stdout, stderr, args[1:])
	case "cancel":
		return loopCancel(ctx, stdout, stderr, args[1:])
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

// loopRun starts one manual invocation and follows it to a terminal state.
// The run itself executes asynchronously through the Session/Run machinery;
// this command only polls the invocation's outer status, bounded by the
// snapshot's budget timeout at a one-second interval.
func loopRun(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs, workspace, configPath := loopFlagSet("loop run", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "hermit: loop run requires exactly one loop id")
		return 2
	}
	service, err := loopService(*workspace, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	invocation, err := service.StartLoopInvocation(ctx, fs.Arg(0))
	if err != nil {
		if invocation.ID != "" {
			printInvocationStatus(stdout, invocation)
		}
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Invocation %s (loop %s, revision %d)\n", invocation.ID, invocation.LoopID, invocation.DefinitionRevision)
	printInvocationStatus(stdout, invocation)
	last := invocation.Status
	if last.Terminal() {
		return loopRunOutcome(stdout, invocation)
	}
	timeout := time.Duration(invocation.DefinitionSnapshot.Budget.TimeoutSeconds) * time.Second
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(stderr, "hermit:", ctx.Err())
			return 1
		case <-ticker.C:
		}
		current, err := service.GetInvocation(ctx, invocation.ID)
		if err != nil {
			fmt.Fprintln(stderr, "hermit:", err)
			return 1
		}
		if current.Status != last {
			printInvocationStatus(stdout, current)
			last = current.Status
		}
		if current.Status.Terminal() {
			return loopRunOutcome(stdout, current)
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(stdout, "Budget timeout %ds elapsed; invocation still %s — session %s run %s\n",
				invocation.DefinitionSnapshot.Budget.TimeoutSeconds, current.Status, current.SessionID, current.RunID)
			return 1
		}
	}
}

// loopRunOutcome prints the terminal outcome; exit 0 only when completed.
func loopRunOutcome(stdout io.Writer, invocation loop.Invocation) int {
	if invocation.Status == loop.Completed {
		fmt.Fprintf(stdout, "Outcome: completed — session %s run %s\n", invocation.SessionID, invocation.RunID)
		return 0
	}
	detail := invocation.FailureSummary
	if invocation.FailureCode != "" {
		detail = invocation.FailureCode + ": " + detail
	}
	fmt.Fprintf(stdout, "Outcome: %s (%s)\n", invocation.Status, detail)
	return 1
}

func printInvocationStatus(w io.Writer, invocation loop.Invocation) {
	switch {
	case invocation.Status == loop.Blocked || invocation.Status == loop.Failed:
		fmt.Fprintf(w, "Status: %s (%s): %s\n", invocation.Status, invocation.FailureCode, invocation.FailureSummary)
	case invocation.SessionID != "":
		fmt.Fprintf(w, "Status: %s — session %s run %s\n", invocation.Status, invocation.SessionID, invocation.RunID)
	default:
		fmt.Fprintf(w, "Status: %s\n", invocation.Status)
	}
}

// loopHistory lists the invocations of one loop with status, revision,
// binding, and timestamps.
func loopHistory(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs, workspace, configPath := loopFlagSet("loop history", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "hermit: loop history requires exactly one loop id")
		return 2
	}
	service, err := loopService(*workspace, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	invocations, err := service.ListInvocations(ctx, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	if len(invocations) == 0 {
		fmt.Fprintf(stdout, "No invocations for loop %s.\n", fs.Arg(0))
		return 0
	}
	for _, invocation := range invocations {
		fmt.Fprintf(stdout, "%s\t%s\trevision %d\tsession %s\trun %s\tcreated %s\tstarted %s\tfinished %s\n",
			invocation.ID, invocation.Status, invocation.DefinitionRevision,
			orDash(invocation.SessionID), orDash(invocation.RunID),
			invocation.CreatedAt.Format(time.RFC3339), invocationTime(invocation.StartedAt), invocationTime(invocation.FinishedAt))
	}
	return 0
}

func invocationTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func orDash(id string) string {
	if id == "" {
		return "-"
	}
	return id
}

// loopCancel cancels one invocation by id.
func loopCancel(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	fs, workspace, configPath := loopFlagSet("loop cancel", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "hermit: loop cancel requires exactly one invocation id")
		return 2
	}
	service, err := loopService(*workspace, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	invocation, err := service.CancelLoopInvocation(ctx, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "hermit:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Invocation %s: %s\n", invocation.ID, invocation.Status)
	if !invocation.Status.Terminal() {
		fmt.Fprintf(stdout, "Cancellation requested — session %s run %s\n", invocation.SessionID, invocation.RunID)
	}
	return 0
}
