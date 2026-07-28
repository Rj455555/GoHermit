package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/skill"
)

func TestPrepareEmployeeTaskCreatesOneStableSessionWithoutExecution(t *testing.T) {
	fixture := newPhase6Fixture(t)
	marker := filepath.Join(fixture.workspace, "marker.txt")
	if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != EmployeeTaskPrepared || second != first || first.SessionID == "" {
		t.Fatalf("preparations = %#v / %#v", first, second)
	}
	ids, err := fixture.sessions.List()
	if err != nil || len(ids) != 1 || ids[0] != first.SessionID {
		t.Fatalf("Sessions = %#v, %v", ids, err)
	}
	prepared, err := fixture.sessions.Load(context.Background(), first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Runs) != 0 || prepared.ActiveRunID != "" ||
		prepared.EmployeeTaskID != fixture.taskID ||
		prepared.EmployeeTaskSnapshotDigest != fixture.taskDigest ||
		prepared.EmployeeContextSnapshot == nil ||
		prepared.EmployeeContextSnapshot.Digest != first.CompactSnapshotDigest {
		t.Fatalf("prepared Session = %#v", prepared)
	}
	if !reflect.DeepEqual(
		prepared.EmployeeContextSnapshot.EffectivePolicy.AllowedCapabilities,
		[]string{"read"},
	) || prepared.EmployeeContextSnapshot.EffectivePolicy.NetworkAllowed {
		t.Fatalf("effective policy intersection = %#v", prepared.EmployeeContextSnapshot.EffectivePolicy)
	}
	checkpoint, err := os.ReadFile(filepath.Join(
		fixture.workspace, ".gohermit", "sessions", first.SessionID, "session.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"phase-six-readiness-value", `"employee_snapshot"`, `"project_bindings"`,
		`"raw_tool_arguments"`,
	} {
		if strings.Contains(string(checkpoint), forbidden) {
			t.Fatalf("forbidden full/private content %q entered Session checkpoint", forbidden)
		}
	}
	journal, err := fixture.employees.LoadDispatch(fixture.taskID)
	if err != nil || journal.Stage != employeestore.DispatchSessionCreated ||
		journal.SessionID != first.SessionID || journal.TaskSnapshotDigest != fixture.taskDigest {
		t.Fatalf("dispatch = %#v, %v", journal, err)
	}
	task, err := fixture.employees.GetTask(fixture.taskID)
	if err != nil || task.SessionID != "" || task.RunID != "" || task.SnapshotDigest != fixture.taskDigest {
		t.Fatalf("Phase 5 immutable Task changed = %#v, %v", task, err)
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != "unchanged" || fixture.builds.Load() != 0 || fixture.service.Active() {
		t.Fatalf("execution side effect: marker=%q builds=%d active=%t", raw, fixture.builds.Load(), fixture.service.Active())
	}
}

func TestPrepareEmployeeTaskReconcilesEveryJournalCrashPoint(t *testing.T) {
	for _, stage := range []string{"journal_written", "session_saved"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newPhase6Fixture(t)
			failed := false
			fixture.service.prepareStageHook = func(current string) error {
				if current == stage && !failed {
					failed = true
					return errors.New("simulated crash")
				}
				return nil
			}
			if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
				t.Fatal("expected simulated crash")
			}
			fixture.service.prepareStageHook = nil
			reopenedEmployees, _ := employeestore.NewStore(fixture.employeeRoot)
			reopenedSessions, _ := session.NewStore(fixture.workspace, ".gohermit")
			restarted := &Service{
				Workspace: fixture.service.Workspace, ConfigPath: fixture.service.ConfigPath,
				employees: reopenedEmployees, store: reopenedSessions,
				skills: fixture.service.skills, credentials: fixture.credentials,
				build: fixture.service.build,
			}
			result, err := restarted.PrepareEmployeeTask(context.Background(), fixture.taskID)
			if err != nil {
				t.Fatal(err)
			}
			ids, _ := fixture.sessions.List()
			if len(ids) != 1 || ids[0] != result.SessionID {
				t.Fatalf("reconciled Sessions = %#v", ids)
			}
			loaded, _ := fixture.sessions.Load(context.Background(), result.SessionID)
			if len(loaded.Runs) != 0 {
				t.Fatalf("reconciliation created a Run: %#v", loaded.Runs)
			}
		})
	}
}

func TestPrepareEmployeeTaskConcurrentRetriesUseOneSession(t *testing.T) {
	fixture := newPhase6Fixture(t)
	const attempts = 12
	results := make(chan EmployeeTaskPreparation, attempts)
	failures := make(chan error, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	sessionID := ""
	for result := range results {
		if sessionID == "" {
			sessionID = result.SessionID
		}
		if result.SessionID != sessionID {
			t.Fatalf("concurrent preparation IDs differ: %q / %q", sessionID, result.SessionID)
		}
	}
	ids, _ := fixture.sessions.List()
	if len(ids) != 1 || ids[0] != sessionID {
		t.Fatalf("concurrent preparation Sessions = %#v", ids)
	}
}

func TestPrepareEmployeeTaskReadinessDriftFailsBeforeJournalOrSession(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*phase6Fixture)
	}{
		{"employee disabled", func(f *phase6Fixture) {
			record, _ := f.employees.Get("employee-a")
			_, _ = f.employees.Disable("employee-a", record.Employee.Revision)
		}},
		{"workspace mismatch", func(f *phase6Fixture) {
			f.service.Workspace = t.TempDir()
		}},
		{"missing credential", func(f *phase6Fixture) {
			_ = f.credentials.Delete("deepseek")
		}},
		{"Knowledge deleted", func(f *phase6Fixture) {
			_ = f.employees.DeleteKnowledge("employee-a", f.sourceID)
		}},
		{"Knowledge refreshed", func(f *phase6Fixture) {
			catalog, _ := knowledge.NewCatalog("")
			source, index, _ := catalog.Index(knowledge.Source{
				ID: f.sourceID, EmployeeID: "employee-a", Kind: knowledge.KindManualText,
				Title: "Handbook", ManualText: "The current content changed.",
			})
			_, _ = f.employees.SaveKnowledge("employee-a", source, index)
		}},
		{"Memory edited", func(f *phase6Fixture) {
			_, _ = f.employees.EditMemory("employee-a", f.memoryID, "A different accepted fact.")
		}},
		{"Memory forgotten", func(f *phase6Fixture) {
			_ = f.employees.ForgetMemory("employee-a", f.memoryID)
		}},
		{"Skill changed", func(f *phase6Fixture) {
			_ = os.WriteFile(f.skillPath, []byte("# Changed instructions\n"), 0o600)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPhase6Fixture(t)
			test.mutate(fixture)
			if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
				t.Fatal("readiness drift must fail closed")
			}
			if ids, _ := fixture.sessions.List(); len(ids) != 0 {
				t.Fatalf("readiness failure created Session: %#v", ids)
			}
			if _, err := fixture.employees.LoadDispatch(fixture.taskID); !errors.Is(err, os.ErrNotExist) &&
				!errors.Is(err, employeestore.ErrNotFound) {
				t.Fatalf("readiness failure created/corrupted journal: %v", err)
			}
			if fixture.builds.Load() != 0 {
				t.Fatalf("readiness failure built runtime %d times", fixture.builds.Load())
			}
		})
	}
}

func TestPrepareEmployeeTaskUsesLiveCodexModelCatalog(t *testing.T) {
	t.Run("pinned model missing from live catalog", func(t *testing.T) {
		fixture := newPhase6FixtureWithSelection(t, employee.ModelSelection{
			Company: "openai", Access: "openai-codex", Model: "gpt-5.3-codex",
		})
		fixture.service.codexModels = []config.ModelOption{{
			ID: "account-other-model", Label: "Other", Provider: "openai-codex",
		}}
		fixture.service.codexModelsAt = time.Now()

		if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
			t.Fatal("model absent from live Codex catalog must fail readiness")
		} else if serviceErr, ok := err.(*Error); !ok || serviceErr.Kind != KindConflict {
			t.Fatalf("error = %#v, want conflict", err)
		}
		assertNoPreparationWrites(t, fixture)
	})

	t.Run("live-only pinned model can prepare", func(t *testing.T) {
		fixture := newPhase6FixtureWithSelection(t, employee.ModelSelection{
			Company: "openai", Access: "openai-codex", Model: "account-live-only-model",
		})
		fixture.service.codexModels = []config.ModelOption{{
			ID: "account-live-only-model", Label: "Live only", Provider: "openai-codex",
		}}
		fixture.service.codexModelsAt = time.Now()

		prepared, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.State != EmployeeTaskPrepared {
			t.Fatalf("preparation = %#v", prepared)
		}
		if fixture.builds.Load() != 0 {
			t.Fatalf("live model readiness built runtime %d times", fixture.builds.Load())
		}
	})
}

func TestPrepareEmployeeTaskNonCodexKeepsStaticReadiness(t *testing.T) {
	fixture := newPhase6Fixture(t)
	fixture.service.codexModels = []config.ModelOption{{
		ID: "irrelevant-codex-model", Label: "Irrelevant", Provider: "openai-codex",
	}}
	fixture.service.codexModelsAt = time.Now()
	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareEmployeeTaskRejectsUnsafeSessionTargetBeforeDispatch(t *testing.T) {
	fixture := newPhase6Fixture(t)
	task, err := fixture.employees.GetTask(fixture.taskID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := canonicalWorkspace(fixture.workspace)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := stableEmployeeSessionID(task, workspace)
	outside := t.TempDir()
	marker := filepath.Join(outside, "marker.txt")
	if err = os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionsDir := filepath.Join(fixture.workspace, ".gohermit", "sessions")
	if err = os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(sessionsDir, sessionID)); err != nil {
		t.Fatal(err)
	}

	if _, err = fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("unsafe Session target must fail readiness")
	}
	assertNoPreparationWrites(t, fixture)
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "unchanged" {
		t.Fatalf("external target changed: %q, %v", raw, err)
	}
}

func TestPrepareEmployeeTaskRejectsUnavailableSessionStoreBeforeDispatch(t *testing.T) {
	fixture := newPhase6Fixture(t)
	fixture.service.store = nil
	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("unavailable Session Store must fail readiness")
	}
	assertNoPreparationWrites(t, fixture)
}

func assertNoPreparationWrites(t *testing.T, fixture *phase6Fixture) {
	t.Helper()
	if _, err := fixture.employees.LoadDispatch(fixture.taskID); !errors.Is(err, os.ErrNotExist) &&
		!errors.Is(err, employeestore.ErrNotFound) {
		t.Fatalf("readiness failure left dispatch journal: %v", err)
	}
	if fixture.builds.Load() != 0 || fixture.service.Active() {
		t.Fatalf("execution side effect: builds=%d active=%t", fixture.builds.Load(), fixture.service.Active())
	}
}

func TestPrepareEmployeeTaskRejectsMismatchedExistingSession(t *testing.T) {
	fixture := newPhase6Fixture(t)
	result, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.workspace, ".gohermit", "sessions", result.SessionID, "session.json")
	raw, _ := os.ReadFile(path)
	var document map[string]any
	_ = json.Unmarshal(raw, &document)
	document["employee_task_snapshot_digest"] = strings.Repeat("0", 64)
	raw, _ = json.Marshal(document)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("mismatched Session must fail closed")
	}
	if fixture.builds.Load() != 0 {
		t.Fatal("mismatch invoked runtime build")
	}
}

func TestPrepareEmployeeTaskRejectsServiceConfigDriftAgainstExistingSession(t *testing.T) {
	fixture := newPhase6Fixture(t)
	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(fixture.workspace, "hermit.toml"),
		[]byte("[agent]\nmax_turns = 51\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); err == nil {
		t.Fatal("service configuration drift must not reuse a mismatched prepared Session")
	}
	if ids, _ := fixture.sessions.List(); len(ids) != 1 {
		t.Fatalf("config drift created another Session: %#v", ids)
	}
	if fixture.builds.Load() != 0 {
		t.Fatal("config drift invoked runtime build")
	}
}

type phase6Fixture struct {
	service      *Service
	employees    *employeestore.Store
	sessions     *session.Store
	credentials  *modelauth.Store
	workspace    string
	taskID       string
	taskDigest   string
	sourceID     string
	memoryID     string
	skillPath    string
	employeeRoot string
	builds       atomic.Int32
}

func newPhase6Fixture(t *testing.T) *phase6Fixture {
	return newPhase6FixtureWithSelection(t, employee.ModelSelection{
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat",
	})
}

func newPhase6FixtureWithSelection(t *testing.T, selection employee.ModelSelection) *phase6Fixture {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skillRoot := t.TempDir()
	digest := writeControlPlaneNativeUpperDigest(t, skillRoot, "review", "1.0.0")
	catalog, err := skill.NewCatalog(skillRoot)
	if err != nil {
		t.Fatal(err)
	}
	employeeRoot := filepath.Join(t.TempDir(), "employees")
	employees, err := employeestore.NewStore(employeeRoot)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewStore(workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := modelauth.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if selection.Access == "openai-codex" {
		t.Setenv("GOHERMIT_CODEX_ACCESS_TOKEN", "phase-six-codex-readiness-token")
	} else {
		if err := credentials.SetAPIKey("deepseek", "phase-six-readiness-value"); err != nil {
			t.Fatal(err)
		}
	}
	fixture := &phase6Fixture{
		employees: employees, sessions: sessions, credentials: credentials, workspace: workspace,
		skillPath:    filepath.Join(skillRoot, "review", "1.0.0", "SKILL.md"),
		employeeRoot: employeeRoot,
	}
	fixture.service = &Service{
		Workspace: workspace, employees: employees, store: sessions, skills: catalog,
		credentials: credentials,
		build: func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
			fixture.builds.Add(1)
			return nil, errors.New("runtime build must not be called")
		},
	}
	draft := controlPlaneDraft("employee-a")
	draft.DefaultSelection = selection
	draft.PermissionPolicy = employee.PermissionPolicy{
		AllowedCapabilities: []string{"read", "write"}, NetworkAllowed: false,
	}
	draft.SkillBindings = []employee.SkillBinding{{
		SkillID: "review", Version: "1.0.0", Digest: digest,
		Configuration: json.RawMessage(`{}`), Enabled: true,
	}}
	record, err := employees.Create(draft, []employee.ProjectBinding{{
		ID: "project-a", Label: "Current workspace", WorkspaceRealPath: workspace,
		ReadAllowed: true, MutationAllowed: true,
		AllowedToolCapabilities: []string{"read", "write"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	knowledgeCatalog, _ := knowledge.NewCatalog("")
	source, index, err := knowledgeCatalog.Index(knowledge.Source{
		ID: "handbook", EmployeeID: record.Employee.ID, Kind: knowledge.KindManualText,
		Title: "Handbook", ManualText: "Review changes carefully.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := employees.SaveKnowledge(record.Employee.ID, source, index); err != nil {
		t.Fatal(err)
	}
	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: "candidate-a", EmployeeID: record.Employee.ID, Category: "preference",
		Value: "Prefer bounded changes.",
		Provenance: []employeememory.Provenance{{
			SourceType: "owner", SourceID: "owner-note",
			VerifiedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		}},
	}, time.Date(2026, 7, 28, 12, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := employees.AddMemoryCandidate(record.Employee.ID, candidate); err != nil {
		t.Fatal(err)
	}
	fact, err := employees.AcceptMemoryCandidate(record.Employee.ID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	citation := index.Documents[0].Citations[0]
	task, err := fixture.service.CreateEmployeeTask(context.Background(), record.Employee.ID, EmployeeTaskCreateInput{
		Prompt: "Review the current workspace.",
		Skills: []EmployeeTaskSkillSelection{{SkillID: "review", Version: "1.0.0"}},
		Knowledge: []EmployeeTaskKnowledgeSelection{{
			SourceID: source.ID, CitationIDs: []string{citation.ID},
		}},
		MemoryFactIDs: []string{fact.ID}, ProjectBindingID: "project-a",
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget:              employee.BudgetPolicy{MaxModelCalls: 2, MaxTokens: 20_000, TimeoutSeconds: 900},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.taskID, fixture.taskDigest = task.ID, task.SnapshotDigest
	fixture.sourceID, fixture.memoryID = source.ID, fact.ID
	return fixture
}

func TestEmployeeTaskControlPlanePinsSelectionsAndNeverExecutes(t *testing.T) {
	ctx := context.Background()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(workspace, "marker.txt")
	if err := os.WriteFile(markerPath, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	if err != nil {
		t.Fatal(err)
	}
	var builds atomic.Int32
	service := &Service{
		Workspace: workspace,
		employees: store,
		build: func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error) {
			builds.Add(1)
			return nil, nil
		},
	}
	draft := controlPlaneDraft("employee-a")
	draft.SkillBindings = []employee.SkillBinding{{
		SkillID: "review", Version: "1.0.0", Digest: strings.Repeat("a", 64),
		Configuration: []byte(`{"mode":"safe"}`), Enabled: true,
	}}
	record, err := service.CreateEmployee(ctx, EmployeeInput{
		Employee: draft,
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-a", Label: "Current workspace", WorkspaceRealPath: workspace,
			ReadAllowed: true, MutationAllowed: true, AllowedToolCapabilities: []string{"read", "write"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	catalog, _ := knowledge.NewCatalog("")
	source, index, err := catalog.Index(knowledge.Source{
		ID: "handbook", EmployeeID: record.Employee.ID, Kind: knowledge.KindManualText,
		Title: "Handbook", ManualText: "Review changes carefully.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveKnowledge(record.Employee.ID, source, index); err != nil {
		t.Fatal(err)
	}
	citation := index.Documents[0].Citations[0]

	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: "candidate-a", EmployeeID: record.Employee.ID, Category: "preference",
		Value: "Prefer bounded changes.",
		Provenance: []employeememory.Provenance{{
			SourceType: "owner", SourceID: "owner-note",
			VerifiedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		}},
	}, time.Date(2026, 7, 28, 12, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddMemoryCandidate(record.Employee.ID, candidate); err != nil {
		t.Fatal(err)
	}
	fact, err := store.AcceptMemoryCandidate(record.Employee.ID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	candidatesBefore, _ := store.MemoryCandidates(record.Employee.ID)

	created, err := service.CreateEmployeeTask(ctx, record.Employee.ID, EmployeeTaskCreateInput{
		Prompt: "Review the current workspace.",
		Skills: []EmployeeTaskSkillSelection{{SkillID: "review", Version: "1.0.0"}},
		Knowledge: []EmployeeTaskKnowledgeSelection{{
			SourceID: source.ID, CitationIDs: []string{citation.ID},
		}},
		MemoryFactIDs:    []string{fact.ID},
		ProjectBindingID: record.ProjectBindings[0].ID,
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget: employee.BudgetPolicy{
				MaxModelCalls: 2, MaxTokens: 20_000, TimeoutSeconds: 900,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.State != employee.TaskQueued || created.SessionID != "" || created.RunID != "" {
		t.Fatalf("created Task view = %#v", created)
	}
	full, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Skills) != 1 || full.Skills[0].Digest != draft.SkillBindings[0].Digest ||
		len(full.Knowledge) != 1 || full.Knowledge[0].SourceDigest != source.Digest ||
		len(full.MemoryFacts) != 1 || full.MemoryFacts[0].Digest != fact.Digest ||
		full.ProjectBinding.ID != "project-a" {
		t.Fatalf("pinned Task = %#v", full)
	}
	if created.EmployeeSnapshot.Digest != full.EmployeeSnapshot.Digest ||
		created.ProjectBinding.WorkspaceFingerprint != full.ProjectBinding.WorkspaceFingerprint {
		t.Fatalf("bounded Task projection lost immutable metadata: %#v", created)
	}

	page, err := service.ListEmployeeTasks(ctx, record.Employee.ID, employeestore.TaskListOptions{})
	if err != nil || len(page.Tasks) != 1 {
		t.Fatalf("list = %#v, %v", page, err)
	}
	loaded, err := service.GetEmployeeTask(ctx, created.ID)
	if err != nil || loaded.SnapshotDigest != created.SnapshotDigest {
		t.Fatalf("get = %#v, %v", loaded, err)
	}
	cancelled, err := service.CancelEmployeeTask(ctx, created.ID)
	if err != nil || cancelled.State != employee.TaskCancelled {
		t.Fatalf("cancel = %#v, %v", cancelled, err)
	}

	marker, err := os.ReadFile(markerPath)
	if err != nil || string(marker) != "unchanged" {
		t.Fatalf("workspace changed: %q, %v", marker, err)
	}
	candidatesAfter, err := store.MemoryCandidates(record.Employee.ID)
	if err != nil || !reflect.DeepEqual(candidatesAfter, candidatesBefore) {
		t.Fatalf("Task operation generated a Memory Candidate: %#v, %v", candidatesAfter, err)
	}
	knowledgeAfter, err := store.Knowledge(record.Employee.ID)
	if err != nil || knowledgeAfter.Sources[0].Digest != source.Digest {
		t.Fatalf("Task operation refreshed Knowledge: %#v, %v", knowledgeAfter, err)
	}
	if builds.Load() != 0 || service.Active() || service.store != nil {
		t.Fatalf("Task operation touched runtime: builds=%d active=%t store=%v", builds.Load(), service.Active(), service.store)
	}
}

func TestEmployeeTaskControlPlaneRejectsSelectionDriftAndLifecycle(t *testing.T) {
	ctx := context.Background()
	workspace, _ := filepath.EvalSymlinks(t.TempDir())
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	service := &Service{Workspace: workspace, employees: store}
	record, err := service.CreateEmployee(ctx, EmployeeInput{
		Employee: controlPlaneDraft("employee-a"),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-a", Label: "Current", WorkspaceRealPath: workspace,
			ReadAllowed: true, AllowedToolCapabilities: []string{"read"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := EmployeeTaskCreateInput{
		Prompt: "Inspect the workspace.", ProjectBindingID: "project-a",
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget: employee.BudgetPolicy{
				MaxModelCalls: 1, MaxTokens: 1000, TimeoutSeconds: 60,
			},
		},
	}
	invalid := base
	invalid.MemoryFactIDs = []string{"missing"}
	if _, err := service.CreateEmployeeTask(ctx, "employee-a", invalid); serviceErrorKind(err) != KindInvalid {
		t.Fatalf("missing selection error = %v", err)
	}
	record, err = service.DisableEmployee(ctx, "employee-a", record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEmployeeTask(ctx, "employee-a", base); serviceErrorKind(err) != KindConflict {
		t.Fatalf("disabled create error = %v", err)
	}
	if _, err := service.GetEmployeeTask(ctx, "../outside"); serviceErrorKind(err) != KindInvalid {
		t.Fatalf("invalid Task id error = %v", err)
	}
	if _, err := service.GetEmployeeTask(ctx, "task-missing"); serviceErrorKind(err) != KindNotFound {
		t.Fatalf("missing Task error = %v", err)
	}
}
