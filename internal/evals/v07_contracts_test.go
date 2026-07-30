package evals

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
	"github.com/Rj455555/GoHermit/internal/session"
)

func TestV07ReleaseVersion(t *testing.T) {
	if app.Version != "0.7.0-dev" {
		t.Fatalf("version = %q, want 0.7.0-dev", app.Version)
	}
}

func TestV07IdentitySnapshotProjectAndSkillPolicy(t *testing.T) {
	now := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	workspace := t.TempDir()
	value, binding := v07Employee(t, "employee-eval", "project-eval", workspace, now)
	snapshot, err := employee.NewRevisionSnapshot(value, []employee.ProjectBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	value.Charter = "mutable current record"
	binding.Label = "mutable current binding"
	if snapshot.Employee.Charter == value.Charter || snapshot.ProjectBindings[0].Label == binding.Label {
		t.Fatal("revision snapshot aliases mutable Employee or ProjectBinding state")
	}
	if err := employee.ValidateRevisionSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.ProjectBindings[0].MatchesCanonicalWorkspace(workspace) {
		t.Fatal("ProjectBinding does not retain the exact canonical workspace")
	}

	base := employee.CapabilityIntersection{
		Global: []string{"read", "write", "execute"}, AgentToolPolicy: "full",
		Employee: []string{"read", "write"}, Project: []string{"read", "write"},
		Task: []string{"read", "write"}, GlobalNetwork: true, EmployeeNetwork: true,
		ProjectNetwork: true, TaskNetwork: true,
	}
	withoutSkill, err := employee.ResolveEffectivePolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	base.EnabledSkillGrants = []employee.SkillCapabilityGrant{{Enabled: true, Requested: []string{"read"}}}
	withSkill, err := employee.ResolveEffectivePolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(withoutSkill.AllowedCapabilities, []string{"read", "write"}) ||
		!reflect.DeepEqual(withSkill.AllowedCapabilities, []string{"read"}) {
		t.Fatalf("Skill widened or changed the base incorrectly: base=%#v skill=%#v", withoutSkill, withSkill)
	}
	base.EnabledSkillGrants = []employee.SkillCapabilityGrant{{Enabled: true, InstructionOnly: true}}
	adapter, err := employee.ResolveEffectivePolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.AllowedCapabilities) != 0 || adapter.NetworkAllowed {
		t.Fatalf("instruction-only SKILL.md Adapter widened policy: %#v", adapter)
	}
}

func TestV07KnowledgeEmployeeMemoryAndProjectMemoryLayering(t *testing.T) {
	root := t.TempDir()
	store, err := employeestore.NewStore(filepath.Join(root, "employees"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	employeeA, bindingA := v07Employee(t, "employee-a", "project-a", filepath.Join(root, "workspace"), now)
	employeeB, bindingB := v07Employee(t, "employee-b", "project-b", filepath.Join(root, "workspace"), now)
	if _, err = store.Create(employeeA, []employee.ProjectBinding{bindingA}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Create(employeeB, []employee.ProjectBinding{bindingB}); err != nil {
		t.Fatal(err)
	}
	catalog, err := knowledge.NewCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	source, index, err := catalog.Index(knowledge.Source{
		ID: "knowledge-a", EmployeeID: employeeA.ID, Kind: knowledge.KindManualText,
		Title: "Employee A handbook", ManualText: "Only Employee A may receive this cited context.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveKnowledge(employeeA.ID, source, index); err != nil {
		t.Fatal(err)
	}
	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: "candidate-a", EmployeeID: employeeA.ID, Category: "preference",
		Value: "Employee A prefers deterministic verification.",
		Provenance: []employeememory.Provenance{{
			SourceType: "run", SourceID: "run-source-a", SourceTaskID: "task-source-a",
			SourceSessionID: "session-source-a", SourceRunID: "run-source-a", VerifiedAt: now,
		}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AddMemoryCandidate(employeeA.ID, candidate); err != nil {
		t.Fatal(err)
	}
	fact, err := store.AcceptMemoryCandidate(employeeA.ID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state, loadErr := store.Knowledge(employeeB.ID); loadErr != nil || len(state.Sources) != 0 {
		t.Fatalf("Employee B observed Employee A Knowledge: %#v, %v", state, loadErr)
	}
	if facts, loadErr := store.Memory(employeeB.ID); loadErr != nil || len(facts) != 0 {
		t.Fatalf("Employee B observed Employee A Memory: %#v, %v", facts, loadErr)
	}

	projectSession, err := session.New("Project Memory eval", filepath.Join(root, "workspace"), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	projectSession.CompletedSteps = []string{"Project-wide verified convention."}
	if err = contextmgr.UpdateProjectMemory(filepath.Join(root, "workspace"), projectSession, session.Run{ID: "run-project-memory"}); err != nil {
		t.Fatal(err)
	}
	projectMemory, err := contextmgr.LoadProjectMemory(filepath.Join(root, "workspace"))
	if err != nil || len(projectMemory.Decisions) != 1 || strings.Contains(projectMemory.Decisions[0].Value, fact.Value) {
		t.Fatalf("Project Memory mixed with Employee Memory: %#v, %v", projectMemory, err)
	}
	if err = store.ForgetMemory(employeeA.ID, fact.ID); err != nil {
		t.Fatal(err)
	}
	if facts, loadErr := store.Memory(employeeA.ID); loadErr != nil || len(facts) != 0 {
		t.Fatalf("forgotten Employee Memory still loads: %#v, %v", facts, loadErr)
	}
}

func TestDockerPersistenceFixture(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("GOHERMIT_EVAL_DATA_ROOT"))
	if root == "" {
		root = t.TempDir()
	}
	dataRoot := filepath.Join(root, "data")
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 29, 1, 0, 0, 0, time.UTC)
	employeeValue, binding := v07Employee(t, "employee-docker", "project-docker", "/workspace", now)
	employeeStore, err := employeestore.NewStore(filepath.Join(dataRoot, "employees"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := employeeStore.Create(employeeValue, []employee.ProjectBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := knowledge.NewCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	source, index, err := catalog.Index(knowledge.Source{
		ID: "knowledge-docker", EmployeeID: record.Employee.ID, Kind: knowledge.KindManualText,
		Title: "Persistent handbook", ManualText: "Container rebuilds preserve deterministic Knowledge.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = employeeStore.SaveKnowledge(record.Employee.ID, source, index); err != nil {
		t.Fatal(err)
	}
	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: "candidate-docker", EmployeeID: record.Employee.ID, Category: "preference",
		Value: "Retain this owner-accepted bounded Memory across rebuilds.",
		Provenance: []employeememory.Provenance{{
			SourceType: "run", SourceID: "run-docker", SourceTaskID: "task-source-docker",
			SourceSessionID: "session-source-docker", SourceRunID: "run-docker", VerifiedAt: now,
		}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = employeeStore.AddMemoryCandidate(record.Employee.ID, candidate); err != nil {
		t.Fatal(err)
	}
	fact, err := employeeStore.AcceptMemoryCandidate(record.Employee.ID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := employee.NewRevisionSnapshot(record.Employee, record.ProjectBindings)
	if err != nil {
		t.Fatal(err)
	}
	citation := index.Documents[0].Citations[0]
	task, err := employeeStore.CreateTask(record.Employee.ID, employee.EmployeeTask{
		EmployeeID: record.Employee.ID, EmployeeRevision: record.Employee.Revision,
		Prompt:           "Prove that the persistent v0.7 stores survive a container rebuild.",
		EmployeeSnapshot: revision,
		Knowledge: []employee.TaskKnowledgeSnapshot{{
			SourceID: source.ID, SourceDigest: source.Digest,
			Citations: []employee.TaskCitationReference{{
				CitationID: citation.ID, Path: citation.Path, Digest: citation.Digest,
				StartLine: citation.StartLine, EndLine: citation.EndLine,
			}},
		}},
		MemoryFacts:    []employee.TaskMemoryFactSnapshot{{FactID: fact.ID, Digest: fact.Digest}},
		ProjectBinding: record.ProjectBindings[0],
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget:              employee.BudgetPolicy{MaxModelCalls: 2, MaxTokens: 20_000, TimeoutSeconds: 600},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != employee.TaskQueued || task.SessionID != "" || task.RunID != "" {
		t.Fatalf("fixture Task executed implicitly: %#v", task)
	}

	loopStore, err := loopstore.NewStore(filepath.Join(dataRoot, "loops"))
	if err != nil {
		t.Fatal(err)
	}
	if err = loopStore.SaveDefinition(v07LoopDefinition()); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := session.NewStore(workspace, ".gohermit/sessions")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.New("Persistent Session", workspace, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = sessionStore.Save(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	verifyV07PersistenceFixture(t, dataRoot, workspace, record.Employee.ID, task.ID, sess.ID)
}

func TestV07RegressionCoverageManifest(t *testing.T) {
	required := map[string][]string{
		"internal/employee/snapshot_test.go": {
			"TestRevisionSnapshotIsCompleteImmutableAndDigestVerified",
			"TestRevisionSnapshotRequiresExactEmployeeProjectBindings",
		},
		"internal/employee/policy_phase3_test.go": {
			"TestEffectivePolicyIncludesAgentProfileAndSkillsOnlyNarrow",
			"TestAgentProfileToolPolicyNarrows",
		},
		"internal/contextmgr/employee_context_test.go": {
			"TestEmployeeKnowledgeAndMemoryLayersAreOrderedAndIndependentlyBounded",
			"TestLegacyBuildRunContractIsUnchanged",
		},
		"internal/controlplane/employee_tasks_test.go": {
			"TestPrepareEmployeeTaskCreatesOneStableSessionWithoutExecution",
			"TestPrepareEmployeeTaskReadinessDriftFailsBeforeJournalOrSession",
		},
		"internal/controlplane/employee_execution_test.go": {
			"TestEmployeeTaskConcurrentStartCreatesAndStartsOneStableRun",
			"TestEmployeeTaskStartReconcilesBindingCrashPoints",
			"TestEmployeeTaskInterruptedResumeUsesOriginalRun",
		},
		"internal/agent/recovery_no_replay_test.go": {
			"TestInterruptedRunDoesNotReplayCompletedToolCall",
			"TestRecoveryConsumesEachFrontierCompletionOnce",
		},
		"internal/agent/agent_test.go": {
			"TestMutationRequiresSuccessfulTestBeforeCompletion",
		},
		"internal/session/session_test.go": {
			"TestSaveLoadAndExternalChange",
			"TestSchemaV1MigrationAndVisibleHistory",
		},
		"internal/controlplane/team_employees_test.go": {
			"TestTeamEmployeePreflightPinsAssignmentAndRestoresWithoutMutableEmployee",
		},
		"internal/app/team_worker_test.go": {
			"TestTeamWorkerEmployeeContextsAreIsolatedAndHandoffStaysPublic",
			"TestTeamWorkerPrivateMemoryEchoPersistsOnlyInHiddenSession",
		},
		"internal/controlplane/hidden_sessions_test.go": {
			"TestHiddenSessionControlPlaneAccessFailsClosedWithoutSideEffects",
		},
		"internal/teamtemplate/template_test.go": {
			"TestSchemaV1MigratesWithoutInventingEmployeeAssignment",
			"TestSchemaV1RejectsV2OnlyEmployeeID",
		},
		"internal/web/server_test.go": {
			"TestPersistentSessionAPIAndEventReplay",
			"TestHiddenTeamWorkerSessionIsAbsentFromEveryPublicSessionAPI",
		},
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	for path, names := range required {
		t.Run(path, func(t *testing.T) {
			found := testFunctions(t, filepath.Join(root, filepath.FromSlash(path)))
			for _, name := range names {
				if !found[name] {
					t.Errorf("required v0.7 regression %s is missing", name)
				}
			}
		})
	}
	for path, fragments := range map[string][]string{
		"tests/e2e-react/phase4.spec.ts": {
			"nine-step Employee wizard persists exact configuration and uses real Dry Run",
			"queued Task requires explicit Prepare then Start and restores through history",
			"Loop Definition, Team, Dry Run, and Invocation use structured authoritative projections",
		},
		"tests/e2e-react/phase3.spec.ts": {
			"refresh resumes the Session high-water without creating Run-scoped EventSources",
		},
	} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(raw), fragment) {
				t.Errorf("%s no longer covers %q", path, fragment)
			}
		}
	}
}

func TestDockerBuildContextUsesExplicitAllowlist(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	dockerfileBytes, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(dockerfileBytes)
	for lineNumber, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "COPY") && fields[1] == "." {
			t.Fatalf("Dockerfile line %d broadly copies the repository: %s", lineNumber+1, line)
		}
	}
	for _, required := range []string{
		"COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./",
		"COPY web/package.json web/package.json",
		"COPY web web",
		"COPY go.mod go.sum ./",
		"COPY cmd cmd",
		"COPY internal internal",
		"COPY protocol protocol",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile is missing explicit build input %q", required)
		}
	}

	ignoreBytes, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	ignored := make(map[string]bool)
	for _, line := range strings.Split(string(ignoreBytes), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			ignored[line] = true
		}
	}
	for _, required := range []string{
		".claude",
		".codegraph",
		".cursor",
		".gemini",
		".mcp.json",
		".gohermit",
		"sandbox",
		"node_modules",
		"**/node_modules",
		"coverage",
		"**/coverage",
		"playwright-report",
		"test-results",
	} {
		if !ignored[required] {
			t.Errorf(".dockerignore is missing %q", required)
		}
	}
}

func v07Employee(t *testing.T, employeeID, projectID, workspace string, now time.Time) (employee.Employee, employee.ProjectBinding) {
	t.Helper()
	value, err := employee.Create(employee.Employee{
		ID: employeeID, Name: employeeID, Avatar: employee.Avatar{Kind: employee.AvatarInitials},
		JobTitle: "Verification Engineer", Charter: "Produce bounded, deterministic evidence.",
		Responsibilities:   []string{"Verify durable contracts"},
		BehaviorBoundaries: []string{"Never publish automatically"},
		DefaultSelection:   employee.ModelSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		AgentProfile:       "coding", ProjectBindingIDs: []string{projectID},
		PermissionPolicy:  employee.PermissionPolicy{AllowedCapabilities: []string{"read", "write"}},
		BudgetPolicy:      employee.BudgetPolicy{MaxModelCalls: 4, MaxTokens: 40_000, TimeoutSeconds: 900},
		ConcurrencyPolicy: employee.ConcurrencyPolicy{MaxRunningTasks: 1},
		MemoryPolicy: employee.MemoryPolicy{
			CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation,
			MaxContextFacts: 8, MaxContextBytes: 8 << 10,
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := employee.CreateProjectBinding(employee.ProjectBinding{
		ID: projectID, EmployeeID: employeeID, Label: "Current Service Workspace",
		WorkspaceRealPath: workspace, ReadAllowed: true, MutationAllowed: true,
		AllowedToolCapabilities: []string{"read", "write"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return value, binding
}

func v07LoopDefinition() loop.Definition {
	return loop.Definition{
		ID: "loop-docker", SchemaVersion: loop.SchemaVersion, Name: "Persistent Loop",
		WorkspaceIdentity: "gohermit-v0.7-eval", Enabled: true,
		TaskSource: loop.TaskSource{Type: loop.TaskSourceFixedPrompt, Prompt: "Verify persistent state."},
		AgentSelection: loop.AgentSelection{
			Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "coding",
		},
		PlanMode:        loop.PlanAuto,
		Budget:          loop.Budget{MaxModelCalls: 2, MaxTokens: 20_000, TimeoutSeconds: 600},
		WorkspacePolicy: loop.WorkspacePolicy{ReadOnly: true},
		OutputPolicy:    loop.OutputPolicy{MaxReportBytes: 16 << 10},
	}
}

func verifyV07PersistenceFixture(t *testing.T, dataRoot, workspace, employeeID, taskID, sessionID string) {
	t.Helper()
	employeeStore, err := employeestore.NewStore(filepath.Join(dataRoot, "employees"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = employeeStore.Get(employeeID); err != nil {
		t.Fatal(err)
	}
	if _, err = employeeStore.GetTask(taskID); err != nil {
		t.Fatal(err)
	}
	if state, loadErr := employeeStore.Knowledge(employeeID); loadErr != nil || len(state.Sources) != 1 {
		t.Fatalf("Knowledge reopen = %#v, %v", state, loadErr)
	}
	if facts, loadErr := employeeStore.Memory(employeeID); loadErr != nil || len(facts) != 1 {
		t.Fatalf("Memory reopen = %#v, %v", facts, loadErr)
	}
	loopStore, err := loopstore.NewStore(filepath.Join(dataRoot, "loops"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loopStore.GetDefinition("loop-docker"); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := session.NewStore(workspace, ".gohermit/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sessionStore.Load(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}
}

func testFunctions(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
			found[function.Name.Name] = true
		}
	}
	return found
}
