package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/session"
)

type memoryPolicyTaskFixture struct {
	service   *Service
	employees *employeestore.Store
	sessions  *session.Store
	employee  string
	project   string
	facts     []employeememory.Fact
}

func newMemoryPolicyTaskFixture(t *testing.T, values ...string) *memoryPolicyTaskFixture {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	employees, err := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewStore(workspace, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	draft := controlPlaneDraft("employee-a")
	draft.SkillBindings = nil
	draft.PermissionPolicy = employee.PermissionPolicy{AllowedCapabilities: []string{"read"}}
	draft.MemoryPolicy = employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation,
		MaxContextFacts: employee.MaxMemoryContextFacts, MaxContextBytes: employee.MaxMemoryContextBytes,
	}
	record, err := employees.Create(draft, []employee.ProjectBinding{{
		ID: "project-a", Label: "Current workspace", WorkspaceRealPath: workspace,
		ReadAllowed: true, AllowedToolCapabilities: []string{"read"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &memoryPolicyTaskFixture{
		service:   &Service{Workspace: workspace, employees: employees, store: sessions},
		employees: employees, sessions: sessions, employee: record.Employee.ID, project: "project-a",
	}
	for index, value := range values {
		now := time.Date(2026, 8, 24, 10, index, 0, 0, time.UTC)
		candidate, candidateErr := employeememory.NewCandidate(employeememory.Candidate{
			ID: fmt.Sprintf("candidate-%d", index), EmployeeID: fixture.employee,
			Category: "preference", Value: value,
			Provenance: []employeememory.Provenance{{SourceType: "owner", SourceID: fmt.Sprintf("owner-%d", index), VerifiedAt: now}},
		}, now)
		if candidateErr != nil {
			t.Fatal(candidateErr)
		}
		if err := employees.AddMemoryCandidate(fixture.employee, candidate); err != nil {
			t.Fatal(err)
		}
		fact, acceptErr := employees.AcceptMemoryCandidate(fixture.employee, candidate.ID)
		if acceptErr != nil {
			t.Fatal(acceptErr)
		}
		fixture.facts = append(fixture.facts, fact)
	}
	return fixture
}

func (f *memoryPolicyTaskFixture) input(factIDs ...string) EmployeeTaskCreateInput {
	return EmployeeTaskCreateInput{
		Prompt: "Use selected memory.", MemoryFactIDs: append([]string{}, factIDs...), ProjectBindingID: f.project,
		Policy: employee.TaskPolicy{
			AllowedCapabilities: []string{"read"},
			Budget:              employee.BudgetPolicy{MaxModelCalls: 1, MaxTokens: 1_000, TimeoutSeconds: 60},
		},
	}
}

func updateEmployeeMemoryPolicy(t *testing.T, store *employeestore.Store, employeeID string, policy employee.MemoryPolicy) {
	t.Helper()
	record, err := store.Get(employeeID)
	if err != nil {
		t.Fatal(err)
	}
	proposed := record.Employee
	proposed.MemoryPolicy = policy
	if _, err := store.Update(employeeID, record.Employee.Revision, proposed, record.ProjectBindings); err != nil {
		t.Fatal(err)
	}
}

func memoryFactIDs(facts []employeememory.Fact) []string {
	result := make([]string, 0, len(facts))
	for _, fact := range facts {
		result = append(result, fact.ID)
	}
	return result
}

func addAcceptedMemoryFact(t *testing.T, fixture *memoryPolicyTaskFixture, id, category, value string, at time.Time) employeememory.Fact {
	t.Helper()
	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: id + "-candidate", EmployeeID: fixture.employee, Category: category, Value: value,
		Provenance: []employeememory.Provenance{{SourceType: "owner", SourceID: id, VerifiedAt: at}},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.employees.AddMemoryCandidate(fixture.employee, candidate); err != nil {
		t.Fatal(err)
	}
	fact, err := fixture.employees.AcceptMemoryCandidate(fixture.employee, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.facts = append(fixture.facts, fact)
	return fact
}

func renderedMemoryPayloadBytes(t *testing.T, fact employeememory.Fact) int {
	t.Helper()
	provenance, err := json.Marshal(fact.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(
		"[source:employee-memory:%s/%s@%s]\n# Private Employee Memory: %s\n\nProvenance: %s\n\n%s",
		fact.EmployeeID, fact.ID, fact.Digest, fact.Category, provenance, fact.Value,
	)
	return len([]byte(payload))
}

func assertNoTaskOrSessionSideEffects(t *testing.T, fixture *memoryPolicyTaskFixture) {
	t.Helper()
	page, err := fixture.employees.ListTasks(fixture.employee, employeestore.TaskListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := fixture.sessions.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 0 || len(ids) != 0 {
		t.Fatalf("rejected creation produced side effects: tasks=%#v sessions=%#v", page.Tasks, ids)
	}
}

func TestCreateEmployeeTaskEnforcesMemoryFactCountBoundaries(t *testing.T) {
	t.Run("equal limit", func(t *testing.T) {
		fixture := newMemoryPolicyTaskFixture(t, "one", "two")
		updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
			Promotion: employee.MemoryPromotionOwnerConfirmation, MaxContextFacts: 2, MaxContextBytes: employee.MaxMemoryContextBytes,
		})
		if _, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, fixture.input(memoryFactIDs(fixture.facts)...)); err != nil {
			t.Fatalf("selection at limit failed: %v", err)
		}
	})

	t.Run("over limit before side effects", func(t *testing.T) {
		fixture := newMemoryPolicyTaskFixture(t, "one", "two")
		updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
			Promotion: employee.MemoryPromotionOwnerConfirmation, MaxContextFacts: 1, MaxContextBytes: employee.MaxMemoryContextBytes,
		})
		if _, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, fixture.input(memoryFactIDs(fixture.facts)...)); serviceErrorKind(err) != KindInvalid {
			t.Fatalf("selection over limit = %v, want invalid request", err)
		}
		assertNoTaskOrSessionSideEffects(t, fixture)
	})

	t.Run("zero permits only empty selection", func(t *testing.T) {
		empty := newMemoryPolicyTaskFixture(t)
		updateEmployeeMemoryPolicy(t, empty.employees, empty.employee, employee.MemoryPolicy{
			Promotion: employee.MemoryPromotionOwnerConfirmation, MaxContextFacts: 0, MaxContextBytes: employee.MaxMemoryContextBytes,
		})
		if _, err := empty.service.CreateEmployeeTask(context.Background(), empty.employee, empty.input()); err != nil {
			t.Fatalf("empty selection with zero limit failed: %v", err)
		}
		over := newMemoryPolicyTaskFixture(t, "one")
		updateEmployeeMemoryPolicy(t, over.employees, over.employee, employee.MemoryPolicy{
			Promotion: employee.MemoryPromotionOwnerConfirmation, MaxContextFacts: 0, MaxContextBytes: employee.MaxMemoryContextBytes,
		})
		if _, err := over.service.CreateEmployeeTask(context.Background(), over.employee, over.input(over.facts[0].ID)); serviceErrorKind(err) != KindInvalid {
			t.Fatalf("non-empty selection with zero limit = %v, want invalid request", err)
		}
		assertNoTaskOrSessionSideEffects(t, over)
	})
}

func TestCreateEmployeeTaskAutomaticallyRecallsAcceptedFacts(t *testing.T) {
	fixture := newMemoryPolicyTaskFixture(t, "Database migration checklist", "Unrelated runtime output")
	addAcceptedMemoryFact(t, fixture, "fact-verified-unrelated", "verified-run", "A completed release run", time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC))
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: true,
		MaxContextFacts: 12, MaxContextBytes: 16 << 10,
	})
	input := fixture.input()
	input.Prompt = "Please review the database migration checklist."
	created, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, input)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(created.MemoryFacts))
	for _, item := range created.MemoryFacts {
		ids = append(ids, item.FactID)
	}
	want := memoryFactIDs(fixture.facts[:1])
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("automatic recall ids = %#v, want matched facts only", ids)
	}
	digests := map[string]string{fixture.facts[0].ID: fixture.facts[0].Digest, fixture.facts[1].ID: fixture.facts[1].Digest}
	for _, item := range created.MemoryFacts {
		if item.Digest != digests[item.FactID] {
			t.Fatalf("automatic recall did not pin FactID + Digest: %#v", created.MemoryFacts)
		}
	}
}

func TestAutomaticRecallFillsRemainingBytesWithoutDisplacingManualFacts(t *testing.T) {
	fixture := newMemoryPolicyTaskFixture(t, "Manual memory", "migration", strings.Repeat("migration ", 200))
	manual := fixture.facts[0]
	small := fixture.facts[1]
	limit := renderedMemoryPayloadBytes(t, manual) + renderedMemoryPayloadBytes(t, small)
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: true,
		MaxContextFacts: 2, MaxContextBytes: limit,
	})
	input := fixture.input(manual.ID)
	input.Prompt = "migration"
	created, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.MemoryFacts) != 2 {
		t.Fatalf("selected %d Memory Facts, want manual plus one automatic: %#v", len(created.MemoryFacts), created.MemoryFacts)
	}
	ids := []string{created.MemoryFacts[0].FactID, created.MemoryFacts[1].FactID}
	want := []string{manual.ID, small.ID}
	sort.Strings(want)
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("manual/automatic ids = %#v, want %#v", ids, want)
	}
}

func TestAutomaticRecallDisabledPreservesManualOnlySelection(t *testing.T) {
	fixture := newMemoryPolicyTaskFixture(t, "database migration", "another database migration")
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: false,
		MaxContextFacts: 12, MaxContextBytes: 16 << 10,
	})
	input := fixture.input(fixture.facts[0].ID)
	input.Prompt = "database migration"
	created, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, input)
	if err != nil {
		t.Fatal(err)
	}
	if got := memoryFactIDsFromSnapshots(created.MemoryFacts); !reflect.DeepEqual(got, []string{fixture.facts[0].ID}) {
		t.Fatalf("automatic recall disabled selected = %#v", got)
	}
}

func TestAutomaticRecallStopsAtTwelveFacts(t *testing.T) {
	values := make([]string, 13)
	for index := range values {
		values[index] = fmt.Sprintf("migration fact %02d", index)
	}
	fixture := newMemoryPolicyTaskFixture(t, values...)
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: true,
		MaxContextFacts: 12, MaxContextBytes: 16 << 10,
	})
	input := fixture.input()
	input.Prompt = "migration"
	created, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.MemoryFacts) != 12 {
		t.Fatalf("automatic recall selected %d Facts, want 12: %#v", len(created.MemoryFacts), created.MemoryFacts)
	}
	seen := make(map[string]struct{}, len(created.MemoryFacts))
	for _, item := range created.MemoryFacts {
		if _, duplicate := seen[item.FactID]; duplicate {
			t.Fatalf("automatic recall selected duplicate Fact %q", item.FactID)
		}
		seen[item.FactID] = struct{}{}
	}
}

func TestAutomaticRecallAcceptsExactByteBoundaryAndSkipsOverlargeFact(t *testing.T) {
	exact := newMemoryPolicyTaskFixture(t, "migration")
	limit := renderedMemoryPayloadBytes(t, exact.facts[0])
	updateEmployeeMemoryPolicy(t, exact.employees, exact.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: true,
		MaxContextFacts: 1, MaxContextBytes: limit,
	})
	input := exact.input()
	input.Prompt = "migration"
	created, err := exact.service.CreateEmployeeTask(context.Background(), exact.employee, input)
	if err != nil {
		t.Fatal(err)
	}
	if got := memoryFactIDsFromSnapshots(created.MemoryFacts); !reflect.DeepEqual(got, []string{exact.facts[0].ID}) {
		t.Fatalf("exact automatic byte boundary selected = %#v", got)
	}

	over := newMemoryPolicyTaskFixture(t, "migration")
	updateEmployeeMemoryPolicy(t, over.employees, over.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: true,
		MaxContextFacts: 1, MaxContextBytes: limit - 1,
	})
	input = over.input()
	input.Prompt = "migration"
	created, err = over.service.CreateEmployeeTask(context.Background(), over.employee, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.MemoryFacts) != 0 {
		t.Fatalf("overlarge automatic Fact was selected: %#v", created.MemoryFacts)
	}
}

func memoryFactIDsFromSnapshots(items []employee.TaskMemoryFactSnapshot) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.FactID)
	}
	return result
}

func TestCreateEmployeeTaskEnforcesMemoryPayloadByteBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		delta     int
		wantError bool
	}{
		{name: "equal limit", delta: 0},
		{name: "over limit", delta: -1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMemoryPolicyTaskFixture(t, "UTF-8 记忆")
			limit := renderedMemoryPayloadBytes(t, fixture.facts[0]) + test.delta
			updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
				Promotion: employee.MemoryPromotionOwnerConfirmation, MaxContextFacts: 1, MaxContextBytes: limit,
			})
			_, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, fixture.input(fixture.facts[0].ID))
			if !test.wantError && err != nil {
				t.Fatalf("payload at byte limit failed: %v", err)
			}
			if test.wantError {
				if serviceErrorKind(err) != KindInvalid {
					t.Fatalf("payload over byte limit = %v, want invalid request", err)
				}
				assertNoTaskOrSessionSideEffects(t, fixture)
			}
		})
	}
}

func TestPrepareEmployeeTaskRechecksTightenedMemoryPolicyBeforeSideEffects(t *testing.T) {
	fixture := newMemoryPolicyTaskFixture(t, "policy tightens after creation")
	task, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, fixture.input(fixture.facts[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	limit := renderedMemoryPayloadBytes(t, fixture.facts[0]) - 1
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		Promotion: employee.MemoryPromotionOwnerConfirmation, MaxContextFacts: 1, MaxContextBytes: limit,
	})
	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), task.ID); serviceErrorKind(err) != KindConflict {
		t.Fatalf("Prepare after policy tightening = %v, want conflict", err)
	}
	ids, err := fixture.sessions.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("failed Prepare created Sessions: %#v", ids)
	}
	if _, err := fixture.employees.LoadDispatch(task.ID); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, employeestore.ErrNotFound) {
		t.Fatalf("failed Prepare created Dispatch Journal: %v", err)
	}
}

func TestPrepareEmployeeTaskRejectsPolicyChangeAfterInitialMemoryCheck(t *testing.T) {
	fixture := newPhase6Fixture(t)
	hookCalled := false
	fixture.service.prepareStageHook = func(stage string) error {
		if stage != "memory_gate_checked" {
			return nil
		}
		hookCalled = true
		record, err := fixture.employees.Get("employee-a")
		if err != nil {
			return err
		}
		proposed := record.Employee
		proposed.MemoryPolicy.MaxContextFacts = 0
		_, err = fixture.employees.Update(
			record.Employee.ID, record.Employee.Revision, proposed, record.ProjectBindings,
		)
		return err
	}

	if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); serviceErrorKind(err) != KindConflict {
		t.Fatalf("Prepare after atomic-point Policy change = %v, want conflict", err)
	}
	if !hookCalled {
		t.Fatal("memory gate hook was not reached after initial validation")
	}
	assertNoPreparedDispatchOrSession(t, fixture)
}

func TestPrepareEmployeeTaskRejectsFactChangeAfterInitialMemoryCheck(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*phase6Fixture) error
	}{
		{name: "edited", mutate: func(f *phase6Fixture) error {
			_, err := f.employees.EditMemory("employee-a", f.memoryID, "Changed after the initial read.")
			return err
		}},
		{name: "forgotten", mutate: func(f *phase6Fixture) error {
			return f.employees.ForgetMemory("employee-a", f.memoryID)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPhase6Fixture(t)
			before, err := fixture.employees.Get("employee-a")
			if err != nil {
				t.Fatal(err)
			}
			hookCalled := false
			fixture.service.prepareStageHook = func(stage string) error {
				if stage != "memory_gate_checked" {
					return nil
				}
				hookCalled = true
				return test.mutate(fixture)
			}

			if _, err := fixture.service.PrepareEmployeeTask(context.Background(), fixture.taskID); serviceErrorKind(err) != KindConflict {
				t.Fatalf("Prepare after atomic-point Fact change = %v, want conflict", err)
			}
			if !hookCalled {
				t.Fatal("memory gate hook was not reached after initial validation")
			}
			after, err := fixture.employees.Get("employee-a")
			if err != nil {
				t.Fatal(err)
			}
			if after.Employee.Revision != before.Employee.Revision {
				t.Fatalf("Fact mutation unexpectedly changed Employee revision: before=%d after=%d", before.Employee.Revision, after.Employee.Revision)
			}
			assertNoPreparedDispatchOrSession(t, fixture)
		})
	}
}

func assertNoPreparedDispatchOrSession(t *testing.T, fixture *phase6Fixture) {
	t.Helper()
	ids, err := fixture.sessions.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("failed atomic Memory gate created Sessions: %#v", ids)
	}
	if _, err := fixture.employees.LoadDispatch(fixture.taskID); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, employeestore.ErrNotFound) {
		t.Fatalf("failed atomic Memory gate created Dispatch Journal: %v", err)
	}
}
