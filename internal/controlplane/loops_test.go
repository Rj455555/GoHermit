package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

func loopTestDefinition(workspace string) loop.Definition {
	return loop.Definition{
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
}

func injectLoopStore(t *testing.T, svc *Service, definitions ...loop.Definition) {
	t.Helper()
	store, err := loopstore.NewStore(filepath.Join(t.TempDir(), "loops"))
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if err := store.SaveDefinition(definition); err != nil {
			t.Fatal(err)
		}
	}
	svc.loopStore = store
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-q", "-m", "init")
}

func TestDryRunLoopReady(t *testing.T) {
	svc := newTestService(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, svc.Workspace)
	injectLoopStore(t, svc, loopTestDefinition(svc.Workspace))

	report, err := svc.DryRunLoop(context.Background(), "loop-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("report not ready: %v", report.Reasons)
	}
	if !report.DefinitionValid || report.DefinitionRevision != 1 {
		t.Fatalf("definition=%+v", report)
	}
	if !report.WorkspaceMatches || !report.GitClean {
		t.Fatalf("workspace/git: %+v", report)
	}
	if report.TaskPrompt != "review the latest changes" {
		t.Fatalf("task=%q", report.TaskPrompt)
	}
	if len(report.Roles) != 1 || !report.Roles[0].CredentialConfigured {
		t.Fatalf("roles=%+v", report.Roles)
	}
	if !strings.Contains(report.WriteScope, "workspace-writable") || !strings.Contains(report.WriteScope, "clean git") {
		t.Fatalf("write scope=%q", report.WriteScope)
	}
	if len(report.Checks) != 1 || report.Checks[0].ID != "vet" {
		t.Fatalf("checks=%+v", report.Checks)
	}
	if report.Budget.MaxModelCalls != 10 || report.Budget.MaxTokens != 100_000 || report.Budget.TimeoutSeconds != 900 {
		t.Fatalf("budget=%+v", report.Budget)
	}
	if !report.RequiresApproval {
		t.Fatal("mutating loop with require_for_mutation must require approval")
	}
	if err := loop.ValidateDryRunReport(report); err != nil {
		t.Fatalf("report fails domain validation: %v", err)
	}
}

func TestDryRunLoopTeamReady(t *testing.T) {
	svc := newTestService(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, svc.Workspace)
	definition := loopTestDefinition(svc.Workspace)
	definition.AgentSelection.Agent = "team"
	definition.TeamTemplateRef = "default"
	injectLoopStore(t, svc, definition)
	injectTeamTemplate(t, svc, &teamtemplate.Template{
		Name:    "default",
		Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
	})

	report, err := svc.DryRunLoop(context.Background(), "loop-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("team report not ready: %v", report.Reasons)
	}
	if len(report.Roles) != 5 {
		t.Fatalf("roles=%+v", report.Roles)
	}
	for _, role := range report.Roles {
		if role.Role == "" || !role.CredentialConfigured {
			t.Fatalf("role unavailable: %+v", role)
		}
	}
}

func TestDryRunLoopNotReadyReasons(t *testing.T) {
	setup := func(t *testing.T, mutate func(*loop.Definition)) (*Service, loop.DryRunReport) {
		svc := newTestService(t)
		t.Setenv("DEEPSEEK_API_KEY", "")
		if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, svc.Workspace)
		definition := loopTestDefinition(svc.Workspace)
		mutate(&definition)
		injectLoopStore(t, svc, definition)
		report, err := svc.DryRunLoop(context.Background(), "loop-1")
		if err != nil {
			t.Fatal(err)
		}
		return svc, report
	}
	hasReason := func(report loop.DryRunReport, want string) bool {
		for _, reason := range report.Reasons {
			if strings.Contains(reason, want) {
				return true
			}
		}
		return false
	}

	t.Run("disabled definition", func(t *testing.T) {
		_, report := setup(t, func(d *loop.Definition) { d.Enabled = false })
		if report.Ready || !hasReason(report, "disabled") {
			t.Fatalf("report=%+v", report)
		}
	})
	t.Run("workspace identity mismatch", func(t *testing.T) {
		_, report := setup(t, func(d *loop.Definition) { d.WorkspaceIdentity = "/somewhere/else" })
		if report.Ready || report.WorkspaceMatches || !hasReason(report, "does not match") {
			t.Fatalf("report=%+v", report)
		}
	})
	t.Run("dirty git with require_clean_git", func(t *testing.T) {
		svc, report := setup(t, func(d *loop.Definition) {})
		if err := os.WriteFile(filepath.Join(svc.Workspace, "dirty.txt"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		report, err := svc.DryRunLoop(context.Background(), "loop-1")
		if err != nil {
			t.Fatal(err)
		}
		if report.Ready || report.GitClean || !hasReason(report, "dirty") {
			t.Fatalf("report=%+v", report)
		}
	})
	t.Run("missing credential", func(t *testing.T) {
		svc := newTestService(t)
		t.Setenv("DEEPSEEK_API_KEY", "")
		initGitRepo(t, svc.Workspace)
		injectLoopStore(t, svc, loopTestDefinition(svc.Workspace))
		report, err := svc.DryRunLoop(context.Background(), "loop-1")
		if err != nil {
			t.Fatal(err)
		}
		if report.Ready || len(report.Roles) != 1 || report.Roles[0].CredentialConfigured {
			t.Fatalf("report=%+v", report)
		}
		if !hasReason(report, "credential missing") {
			t.Fatalf("reasons=%v", report.Reasons)
		}
	})
	t.Run("team template load failure", func(t *testing.T) {
		svc := newTestService(t)
		t.Setenv("DEEPSEEK_API_KEY", "")
		if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, svc.Workspace)
		definition := loopTestDefinition(svc.Workspace)
		definition.AgentSelection.Agent = "team"
		injectLoopStore(t, svc, definition)
		svc.teamTemplatesErr = errors.New("boom")
		report, err := svc.DryRunLoop(context.Background(), "loop-1")
		if err != nil {
			t.Fatal(err)
		}
		if report.Ready || !hasReason(report, "team template load failure") {
			t.Fatalf("report=%+v", report)
		}
	})
	t.Run("team role missing credential", func(t *testing.T) {
		svc := newTestService(t)
		t.Setenv("DEEPSEEK_API_KEY", "")
		t.Setenv("DASHSCOPE_API_KEY", "")
		if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, svc.Workspace)
		definition := loopTestDefinition(svc.Workspace)
		definition.AgentSelection.Agent = "team"
		injectLoopStore(t, svc, definition)
		injectTeamTemplate(t, svc, &teamtemplate.Template{
			Name:    "mixed",
			Default: teamtemplate.RoleSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
			Roles: map[string]teamtemplate.RoleSelection{
				"builder": {Company: "alibaba", Access: "alibaba", Model: "qwen3.7-plus"},
			},
		})
		report, err := svc.DryRunLoop(context.Background(), "loop-1")
		if err != nil {
			t.Fatal(err)
		}
		if report.Ready || !hasReason(report, "builder") || !hasReason(report, "credential missing") {
			t.Fatalf("report=%+v reasons=%v", report, report.Reasons)
		}
	})
}

// TestDryRunLoopCreatesNothing is the mandatory failure-path proof: a dry
// run must be pure inspection. It asserts with counters and hashes, not
// vibes — no runtime build, no session, no approval waiter, and a byte-for-
// byte identical workspace tree (excluding git's internal bookkeeping).
func TestDryRunLoopCreatesNothing(t *testing.T) {
	svc := newTestService(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, svc.Workspace)
	injectLoopStore(t, svc, loopTestDefinition(svc.Workspace))

	buildCalls := 0
	svc.build = func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
		buildCalls++
		return nil, errors.New("dry run must never build a runtime")
	}
	treeHash := func() string {
		h := sha256.New()
		root := svc.Workspace
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			h.Write([]byte(rel))
			if entry.IsDir() {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(content)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	before := treeHash()

	report, err := svc.DryRunLoop(context.Background(), "loop-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("report not ready: %v", report.Reasons)
	}
	if buildCalls != 0 {
		t.Fatalf("dry run built a runtime %d times", buildCalls)
	}
	if after := treeHash(); after != before {
		t.Fatal("dry run changed the workspace file tree")
	}
	if _, statErr := os.Stat(filepath.Join(svc.Workspace, ".gohermit", "sessions")); !os.IsNotExist(statErr) {
		t.Fatalf("dry run left a sessions directory behind: %v", statErr)
	}
	ids, err := svc.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("dry run persisted sessions: %v", ids)
	}
	svc.approvals.mu.Lock()
	waiters := len(svc.approvals.waiters)
	svc.approvals.mu.Unlock()
	if waiters != 0 {
		t.Fatalf("dry run parked %d approval waiters", waiters)
	}
}

func TestDryRunLoopUnknownDefinition(t *testing.T) {
	svc := newTestService(t)
	injectLoopStore(t, svc)
	_, err := svc.DryRunLoop(context.Background(), "missing")
	var serviceErr *Error
	if !errors.As(err, &serviceErr) || serviceErr.Kind != KindNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestListAndGetLoops(t *testing.T) {
	svc := newTestService(t)
	first := loopTestDefinition(svc.Workspace)
	second := loopTestDefinition(svc.Workspace)
	second.ID = "loop-2"
	second.Name = "weekly-audit"
	injectLoopStore(t, svc, second, first)

	definitions, err := svc.ListLoops()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 2 || definitions[0].ID != "loop-1" || definitions[1].ID != "loop-2" {
		t.Fatalf("definitions=%+v", definitions)
	}
	definition, err := svc.GetLoop("loop-2")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Name != "weekly-audit" || definition.Revision != 1 {
		t.Fatalf("definition=%+v", definition)
	}
	var serviceErr *Error
	if _, err = svc.GetLoop("missing"); !errors.As(err, &serviceErr) || serviceErr.Kind != KindNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestImportLoop(t *testing.T) {
	svc := newTestService(t)
	injectLoopStore(t, svc)
	definition := loopTestDefinition(svc.Workspace)
	definition.SchemaVersion = loop.SchemaVersion
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}

	imported, err := svc.ImportLoop(raw)
	if err != nil {
		t.Fatalf("ImportLoop = %v", err)
	}
	if imported.ID != definition.ID || imported.Revision != 1 {
		t.Fatalf("imported=%+v", imported)
	}
	stored, err := svc.GetLoop(definition.ID)
	if err != nil || stored.Name != definition.Name {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}

	// Importing the same id again is an update: the store bumps the
	// revision rather than erroring or duplicating the entry.
	definition.Name = "renamed"
	raw, err = json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.ImportLoop(raw)
	if err != nil {
		t.Fatalf("ImportLoop (update) = %v", err)
	}
	if updated.Revision != 2 || updated.Name != "renamed" {
		t.Fatalf("updated=%+v", updated)
	}
	all, err := svc.ListLoops()
	if err != nil || len(all) != 1 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
}

func TestImportLoopRejectsSecret(t *testing.T) {
	svc := newTestService(t)
	injectLoopStore(t, svc)
	definition := loopTestDefinition(svc.Workspace)
	definition.SchemaVersion = loop.SchemaVersion
	definition.Description = "api_key=deadbeef00000000000000000000"
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	// classified() flattens the error to a message string (Error has no
	// Unwrap), so assert on loopstore.ErrImportSecret's wrapped text rather
	// than errors.Is.
	var serviceErr *Error
	if _, err := svc.ImportLoop(raw); !errors.As(err, &serviceErr) || serviceErr.Kind != KindInvalid ||
		!strings.Contains(serviceErr.Message, loopstore.ErrImportSecret.Error()) {
		t.Fatalf("err=%v, want KindInvalid wrapping ErrImportSecret", err)
	}
	if _, err := svc.GetLoop(definition.ID); err == nil {
		t.Fatal("secret-carrying import was persisted")
	}
}
