package controlplane

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
)

// scriptedTeamWorker returns a fixed handoff per role; the verifier handoff
// carries the configured issues and no checks, simulating a read-only
// mission's independent cross-check.
type scriptedTeamWorker struct {
	verifierIssues []string
}

func (w scriptedTeamWorker) Execute(_ context.Context, assignment team.Assignment) (team.Result, error) {
	handoff := team.Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "completed " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == team.RoleVerifier {
		handoff.Issues = w.verifierIssues
	}
	return team.Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

// readOnlyRecipeDefinition is a read-only docs-style loop with zero checks
// but an independent verifier: the recipe is present, so acceptance
// evaluation applies.
func readOnlyRecipeDefinition(workspace string) loop.Definition {
	definition := readOnlyInvocationDefinition(workspace)
	definition.ID = "loop-ro-recipe"
	definition.AgentSelection.Agent = "team"
	definition.VerificationRecipe = loop.VerificationRecipe{IndependentVerifier: true, MaxRepairAttempts: 2}
	return definition
}

// mutationRecipeDefinition is a team-based mutation loop with a clean-git
// workspace policy and the given verification recipe.
func mutationRecipeDefinition(workspace string, recipe loop.VerificationRecipe) loop.Definition {
	definition := loopTestDefinition(workspace)
	definition.ID = "loop-mut"
	definition.Name = "mutation-recipe"
	definition.TaskSource.Prompt = "implement the requested change"
	definition.AgentSelection.Agent = "team"
	definition.VerificationRecipe = recipe
	return definition
}

// TestLoopInvocationReadOnlyZeroChecks proves failure-path 2: a read-only
// invocation with zero checks completes only when the verifier handoff
// reports Issues == []; a verifier returning issues blocks the invocation —
// it is never completed.
func TestLoopInvocationReadOnlyZeroChecks(t *testing.T) {
	start := func(t *testing.T, issues []string) (*Service, loop.Invocation) {
		svc := newInvocationService(t)
		injectLoopStore(t, svc, readOnlyRecipeDefinition(svc.Workspace))
		svc.teamWorker = scriptedTeamWorker{verifierIssues: issues}
		provider := &countingLoopProvider{fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
			return stopLoopResponse("")
		}}
		var builds int64
		fakeLoopBuild(svc, provider, &builds)
		invocation, err := svc.StartLoopInvocation(context.Background(), "loop-ro-recipe")
		if err != nil {
			t.Fatalf("start err = %v", err)
		}
		waitForRun(t, svc)
		invocation, err = svc.GetInvocation(context.Background(), invocation.ID)
		if err != nil {
			t.Fatal(err)
		}
		return svc, invocation
	}

	t.Run("empty issues completes", func(t *testing.T) {
		svc, invocation := start(t, nil)
		if invocation.Status != loop.Completed || invocation.FinishedAt == nil {
			t.Fatalf("invocation=%+v", invocation)
		}
		sess, err := svc.store.Load(context.Background(), invocation.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if sess.Mission == nil || sess.Mission.Status != team.Completed {
			t.Fatalf("mission=%+v", sess.Mission)
		}
	})

	t.Run("verifier issues block the invocation", func(t *testing.T) {
		svc, invocation := start(t, []string{"stale claim in the summary"})
		if invocation.Status == loop.Completed {
			t.Fatalf("invocation with verifier issues completed: %+v", invocation)
		}
		if invocation.Status != loop.Blocked || invocation.FailureCode != failVerification {
			t.Fatalf("invocation=%+v", invocation)
		}
		sess, err := svc.store.Load(context.Background(), invocation.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(sess.Runs) != 1 || sess.Runs[0].Status != session.RunFailed {
			t.Fatalf("runs=%+v", sess.Runs)
		}
		if sess.Mission == nil || !strings.Contains(sess.Mission.Error, team.VerificationFailureMessage) {
			t.Fatalf("mission=%+v", sess.Mission)
		}
	})
}

// TestLoopInvocationMutationPassingCheckCompletes is the integration proof: a
// mutation invocation whose required recipe check passes runs the check
// deterministically through the verifier, the evidence (exit code, duration,
// bounded output) reaches the verifier handoff and the session TestResults,
// the run completes, and the invocation completes.
func TestLoopInvocationMutationPassingCheckCompletes(t *testing.T) {
	svc := newInvocationService(t)
	initGitRepo(t, svc.Workspace)
	definition := mutationRecipeDefinition(svc.Workspace, loop.VerificationRecipe{
		Checks:              []loop.RecipeCheck{{ID: "go-version", Command: []string{"go", "version"}, Required: true, TimeoutSeconds: 60}},
		IndependentVerifier: true,
		MaxRepairAttempts:   2,
	})
	injectLoopStore(t, svc, definition)
	var mu sync.Mutex
	var prompts []string
	provider := &countingLoopProvider{fn: func(_ int, request model.GenerateRequest) (model.GenerateResponse, error) {
		mu.Lock()
		prompts = append(prompts, request.Messages[len(request.Messages)-1].Content)
		mu.Unlock()
		return stopLoopResponse("")
	}}
	var builds int64
	fakeLoopBuild(svc, provider, &builds)

	invocation, err := svc.StartLoopInvocation(context.Background(), "loop-mut")
	if err != nil {
		t.Fatalf("start err = %v", err)
	}
	waitForRun(t, svc)
	invocation, err = svc.GetInvocation(context.Background(), invocation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Status != loop.Completed || invocation.FailureCode != "" {
		t.Fatalf("invocation=%+v", invocation)
	}
	sess, err := svc.store.Load(context.Background(), invocation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Runs) != 1 || sess.Runs[0].Status != session.RunCompleted {
		t.Fatalf("runs=%+v", sess.Runs)
	}
	// The session carries the invocation snapshot's recipe.
	if sess.VerificationRecipe == nil || len(sess.VerificationRecipe.Checks) != 1 || sess.VerificationRecipe.Checks[0].ID != "go-version" {
		t.Fatalf("session recipe=%+v", sess.VerificationRecipe)
	}
	// The verifier handoff carries the deterministic check with exit code
	// and duration evidence.
	var verifier *team.Handoff
	for i := len(sess.Mission.Handoffs) - 1; i >= 0; i-- {
		if sess.Mission.Handoffs[i].Role == team.RoleVerifier {
			verifier = &sess.Mission.Handoffs[i]
			break
		}
	}
	if verifier == nil || len(verifier.Checks) != 1 {
		t.Fatalf("verifier handoff=%+v", verifier)
	}
	check := verifier.Checks[0]
	if check.Command != "go version" || !check.Passed || check.ExitCode != 0 || !strings.Contains(check.Summary, "go version") {
		t.Fatalf("check=%+v", check)
	}
	// The same evidence is visible on the session's run timeline.
	found := false
	for _, result := range sess.TestResults {
		if result.Command == "go version" && result.Passed && result.ExitCode == 0 && result.RunID == invocation.RunID {
			found = true
		}
	}
	if !found {
		t.Fatalf("test results=%+v", sess.TestResults)
	}
	// The verifier's assignment declared the recipe check.
	mu.Lock()
	defer mu.Unlock()
	declared := false
	for _, prompt := range prompts {
		if strings.Contains(prompt, "Declared deterministic verification checks") && strings.Contains(prompt, "go-version: go version (required=true)") {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("no verifier prompt declared the recipe checks: %d prompts", len(prompts))
	}
}

// TestLoopInvocationMutationFailingCheckBlockedAfterBoundedRepairs proves
// failure-path 3: a mutation invocation with a failing required check runs
// the bounded repair loop (capped by the recipe's max_repair_attempts) and
// the invocation is blocked — never completed, never ready for review.
func TestLoopInvocationMutationFailingCheckBlockedAfterBoundedRepairs(t *testing.T) {
	svc := newInvocationService(t)
	initGitRepo(t, svc.Workspace)
	// go vet ./... fails fast in the temp workspace (no go.mod).
	definition := mutationRecipeDefinition(svc.Workspace, loop.VerificationRecipe{
		Checks:              []loop.RecipeCheck{{ID: "vet", Command: []string{"go", "vet", "./..."}, Required: true, TimeoutSeconds: 60}},
		IndependentVerifier: true,
		MaxRepairAttempts:   2,
	})
	injectLoopStore(t, svc, definition)
	provider := &countingLoopProvider{fn: func(int, model.GenerateRequest) (model.GenerateResponse, error) {
		return stopLoopResponse("")
	}}
	var builds int64
	fakeLoopBuild(svc, provider, &builds)

	invocation, err := svc.StartLoopInvocation(context.Background(), "loop-mut")
	if err != nil {
		t.Fatalf("start err = %v", err)
	}
	waitForRun(t, svc)
	invocation, err = svc.GetInvocation(context.Background(), invocation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Status == loop.Completed {
		t.Fatalf("invocation with a failing required check completed: %+v", invocation)
	}
	if invocation.Status != loop.Blocked || invocation.FailureCode != failVerification {
		t.Fatalf("invocation=%+v", invocation)
	}
	sess, err := svc.store.Load(context.Background(), invocation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Runs) != 1 || sess.Runs[0].Status != session.RunFailed {
		t.Fatalf("runs=%+v", sess.Runs)
	}
	// The repair loop was bounded by the recipe: the verifier ran exactly
	// max_repair_attempts times and the mission failed verification.
	var verify *team.WorkItem
	for i := range sess.Mission.WorkItems {
		if sess.Mission.WorkItems[i].Role == team.RoleVerifier {
			verify = &sess.Mission.WorkItems[i]
			break
		}
	}
	if verify == nil || verify.Attempt != 2 {
		t.Fatalf("verify work item=%+v", verify)
	}
	if sess.Mission.Status != team.Failed || !strings.Contains(sess.Mission.Error, team.VerificationFailureMessage) {
		t.Fatalf("mission=%+v", sess.Mission)
	}
	// The failing check's evidence is recorded on the verifier handoffs.
	evidence := 0
	for _, handoff := range sess.Mission.Handoffs {
		if handoff.Role != team.RoleVerifier {
			continue
		}
		for _, check := range handoff.Checks {
			if check.Command == "go vet ./..." && !check.Passed && check.ExitCode != 0 {
				evidence++
			}
		}
	}
	if evidence == 0 {
		t.Fatalf("no failing check evidence: %+v", sess.Mission.Handoffs)
	}
}
