package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
)

// setupLoopCLI builds a temp git workspace with one stored loop definition
// and every owner store redirected into temp dirs, mirroring the
// controlplane test harness from the transport side.
func setupLoopCLI(t *testing.T, mutate func(*loop.Definition)) (workspace string) {
	t.Helper()
	workspace = t.TempDir()
	stores := t.TempDir()
	t.Setenv("GOHERMIT_AUTH_STORE", filepath.Join(stores, "credentials.json"))
	t.Setenv("GOHERMIT_OWNER_STORE", filepath.Join(stores, "owner.json"))
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", filepath.Join(stores, "team-template.json"))
	t.Setenv("GOHERMIT_LOOP_STORE", filepath.Join(stores, "loops"))
	t.Setenv("CODEX_HOME", filepath.Join(stores, "missing-codex"))
	t.Setenv("DEEPSEEK_API_KEY", "test-secret")
	if err := os.WriteFile(filepath.Join(workspace, "hermit.toml"), []byte("[model]\nprovider = \"codex\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-q", "-m", "init")

	definition := loop.Definition{
		ID:                "loop-1",
		Name:              "nightly-review",
		WorkspaceIdentity: workspace,
		Enabled:           true,
		TaskSource:        loop.TaskSource{Type: loop.TaskSourceFixedPrompt, Prompt: "review the latest changes"},
		AgentSelection:    loop.AgentSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "coding"},
		PlanMode:          loop.PlanAuto,
		VerificationRecipe: loop.VerificationRecipe{
			Checks: []loop.RecipeCheck{{ID: "vet", Command: []string{"go", "vet", "./..."}, Required: true, TimeoutSeconds: 120}},
		},
		Budget:          loop.Budget{MaxModelCalls: 10, MaxTokens: 100_000, TimeoutSeconds: 900},
		ApprovalPolicy:  loop.ApprovalPolicy{RequireForMutation: true},
		WorkspacePolicy: loop.WorkspacePolicy{ReadOnly: false, RequireCleanGit: true},
		OutputPolicy:    loop.OutputPolicy{IncludeDiff: true, MaxReportBytes: 64 << 10},
	}
	mutate(&definition)
	store, err := loopstore.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(definition); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func runLoopCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runLoop(context.Background(), &stdout, &stderr, args)
	return code, stdout.String(), stderr.String()
}

func TestLoopDryRunReady(t *testing.T) {
	workspace := setupLoopCLI(t, func(*loop.Definition) {})
	code, stdout, stderr := runLoopCLI(t, "dry-run", "--workspace", workspace, "loop-1")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Loop dry run: loop-1",
		"Definition: revision 1 (valid)",
		"git: clean",
		"Task: review the latest changes",
		"✓ agent",
		"Write scope: workspace-writable",
		"- vet: go vet ./... (required, timeout 120s)",
		"Budget: 10 model calls, 100000 tokens, 900s timeout",
		"Approval: required before mutation",
		"Verdict: READY",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestLoopDryRunNotReadyExitsOne(t *testing.T) {
	workspace := setupLoopCLI(t, func(d *loop.Definition) { d.WorkspaceIdentity = "/somewhere/else" })
	code, stdout, _ := runLoopCLI(t, "dry-run", "--workspace", workspace, "loop-1")
	if code != 1 {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "Verdict: NOT READY") || !strings.Contains(stdout, "does not match") {
		t.Fatalf("stdout=%s", stdout)
	}
}

func TestLoopDryRunUnknownLoopExitsOne(t *testing.T) {
	workspace := setupLoopCLI(t, func(*loop.Definition) {})
	code, _, stderr := runLoopCLI(t, "dry-run", "--workspace", workspace, "missing")
	if code != 1 || !strings.Contains(stderr, "not found") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestLoopDryRunUsageExitsTwo(t *testing.T) {
	if code, _, _ := runLoopCLI(t, "dry-run"); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if code, _, _ := runLoopCLI(t, "bogus"); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestLoopList(t *testing.T) {
	workspace := setupLoopCLI(t, func(*loop.Definition) {})
	code, stdout, stderr := runLoopCLI(t, "list", "--workspace", workspace)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "loop-1") || !strings.Contains(stdout, "nightly-review") || !strings.Contains(stdout, "enabled") {
		t.Fatalf("stdout=%s", stdout)
	}
}

func TestLoopDryRunLeavesWorkspaceUntouched(t *testing.T) {
	workspace := setupLoopCLI(t, func(*loop.Definition) {})
	code, stdout, _ := runLoopCLI(t, "dry-run", "--workspace", workspace, "loop-1")
	if code != 0 {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".gohermit")); !os.IsNotExist(err) {
		t.Fatalf("dry run created workspace state: %v", err)
	}
	cmd := exec.Command("git", "status", "--porcelain=v1")
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, out)
	}
	if len(out) != 0 {
		t.Fatalf("dry run dirtied the git tree: %s", out)
	}
}
