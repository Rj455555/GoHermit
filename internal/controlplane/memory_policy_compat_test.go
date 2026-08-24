package controlplane

import (
	"context"
	"testing"

	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestAutomaticRecallCompatibilityFlagDoesNotSelectMemory(t *testing.T) {
	fixture := newMemoryPolicyTaskFixture(t, "matching preference")
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		CandidateGeneration: true,
		Promotion:           employee.MemoryPromotionOwnerConfirmation,
		AutomaticRecall:     true,
		MaxContextFacts:     1,
		MaxContextBytes:     employee.MaxMemoryContextBytes,
	})

	task, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, fixture.input())
	if err != nil {
		t.Fatal(err)
	}
	if len(task.MemoryFacts) != 0 {
		t.Fatalf("automatic_recall compatibility flag selected Facts: %#v", task.MemoryFacts)
	}
}
