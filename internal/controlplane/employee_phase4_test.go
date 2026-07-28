package controlplane

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employeememory"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/knowledge"
)

func TestEmployeeKnowledgeControlPlaneIndexesSearchesAndIsolates(t *testing.T) {
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	_, _ = store.Create(controlPlaneDraft("employee-a"), nil)
	_, _ = store.Create(controlPlaneDraft("employee-b"), nil)
	catalog, _ := knowledge.NewCatalog("")
	service := &Service{Workspace: t.TempDir(), employees: store, knowledge: catalog}
	result, err := service.AddEmployeeKnowledge(context.Background(), "employee-a", knowledge.Source{
		ID: "guide", Kind: knowledge.KindManualText, Title: "Guide", ManualText: "Deterministic citations are stable.",
	})
	if err != nil || len(result.Sources) != 1 || len(result.Indexes) != 1 {
		t.Fatalf("add = %#v, %v", result, err)
	}
	result, err = service.EmployeeKnowledge(context.Background(), "employee-a", "deterministic", 10)
	if err != nil || len(result.Results) != 1 {
		t.Fatalf("search = %#v, %v", result, err)
	}
	other, err := service.EmployeeKnowledge(context.Background(), "employee-b", "deterministic", 10)
	if err != nil || len(other.Results) != 0 || len(other.Sources) != 0 {
		t.Fatalf("cross Employee Knowledge = %#v, %v", other, err)
	}
	if err := service.DeleteEmployeeKnowledge(context.Background(), "employee-a", "guide"); err != nil {
		t.Fatal(err)
	}
}

func TestEmployeeMemoryControlPlaneRequiresExplicitOwnerAcceptance(t *testing.T) {
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	_, _ = store.Create(controlPlaneDraft("employee-a"), nil)
	service := &Service{Workspace: t.TempDir(), employees: store}
	now := time.Now().UTC()
	candidate, err := employeememory.NewCandidate(employeememory.Candidate{
		ID: "candidate-a", EmployeeID: "employee-a", Category: "fact", Value: "Owner verified fact.",
		Provenance: []employeememory.Provenance{{SourceType: "owner", SourceID: "owner-note", VerifiedAt: now}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddMemoryCandidate("employee-a", candidate); err != nil {
		t.Fatal(err)
	}
	before, err := service.EmployeeMemory(context.Background(), "employee-a")
	if err != nil || len(before.Facts) != 0 {
		t.Fatalf("Candidate promoted automatically: %#v, %v", before, err)
	}
	fact, err := service.AcceptEmployeeMemoryCandidate(context.Background(), "employee-a", candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := service.EditEmployeeMemory(context.Background(), "employee-a", fact.ID, "Owner edited fact.")
	if err != nil || !edited.OwnerEdited {
		t.Fatalf("edit = %#v, %v", edited, err)
	}
	if err := service.ForgetEmployeeMemory(context.Background(), "employee-a", fact.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := service.EmployeeMemory(context.Background(), "employee-a")
	if len(after.Facts) != 0 {
		t.Fatal("Forgotten fact remains visible")
	}
	if _, err := service.AcceptEmployeeMemoryCandidate(context.Background(), "employee-a", "missing"); !errors.As(err, new(*Error)) {
		t.Fatalf("missing Candidate was not classified: %v", err)
	}
}
