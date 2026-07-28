package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

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
