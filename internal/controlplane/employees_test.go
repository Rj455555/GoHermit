package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

func TestEmployeeCRUDDryRunAndSingleWorkspaceProjects(t *testing.T) {
	workspace := t.TempDir()
	workspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	store, err := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := modelauth.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-only-not-persisted")
	service := &Service{Workspace: workspace, employees: store, credentials: credentials}
	input := EmployeeInput{
		Employee: controlPlaneDraft("employee-a"),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-a", Label: "Current workspace", WorkspaceRealPath: workspace,
			ReadAllowed: true, MutationAllowed: true, AllowedToolCapabilities: []string{"read", "write"},
		}},
	}
	record, err := service.CreateEmployee(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListEmployees(context.Background(), employeestore.ListOptions{})
	if err != nil || len(page.Employees) != 1 {
		t.Fatalf("list = %#v, %v", page, err)
	}
	loaded, err := service.GetEmployee(context.Background(), record.Employee.ID)
	if err != nil || loaded.Employee.ID != record.Employee.ID {
		t.Fatalf("get = %#v, %v", loaded, err)
	}
	proposed := record.Employee
	proposed.Name = "Updated Employee"
	updated, err := service.UpdateEmployee(context.Background(), record.Employee.ID, EmployeeUpdateInput{
		ExpectedRevision: record.Employee.Revision, Employee: proposed, ProjectBindings: record.ProjectBindings,
	})
	if err != nil || updated.Employee.Revision != 2 {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	if _, err := service.UpdateEmployee(context.Background(), record.Employee.ID, EmployeeUpdateInput{
		ExpectedRevision: 1, Employee: proposed, ProjectBindings: record.ProjectBindings,
	}); serviceErrorKind(err) != KindConflict {
		t.Fatalf("stale update error = %v", err)
	}
	record = updated
	before, err := store.LoadRevision(record.Employee.ID, record.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	readiness, err := service.DryRunEmployee(context.Background(), record.Employee.ID)
	if err != nil || !readiness.Ready {
		t.Fatalf("dry run = %#v, %v", readiness, err)
	}
	after, err := store.LoadRevision(record.Employee.ID, record.Employee.Revision)
	if err != nil || before.Digest != after.Digest {
		t.Fatalf("dry run mutated employee: %v", err)
	}
	record, err = service.DisableEmployee(context.Background(), record.Employee.ID, record.Employee.Revision)
	if err != nil || record.Employee.State != employee.StateDisabled {
		t.Fatalf("disable = %#v, %v", record, err)
	}
	record, err = service.EnableEmployee(context.Background(), record.Employee.ID, record.Employee.Revision)
	if err != nil || record.Employee.State != employee.StateActive {
		t.Fatalf("enable = %#v, %v", record, err)
	}
	record, err = service.ArchiveEmployee(context.Background(), record.Employee.ID, record.Employee.Revision)
	if err != nil || record.Employee.State != employee.StateArchived {
		t.Fatalf("archive = %#v, %v", record, err)
	}
	if _, err := service.EnableEmployee(context.Background(), record.Employee.ID, record.Employee.Revision); serviceErrorKind(err) != KindConflict {
		t.Fatalf("archived transition error = %v", err)
	}
	activity, err := service.EmployeeActivity(context.Background(), record.Employee.ID, employeestore.ListOptions{})
	if err != nil || len(activity.Events) != 5 {
		t.Fatalf("activity = %#v, %v", activity, err)
	}
	projects, err := service.Projects(context.Background())
	if err != nil || len(projects) != 1 || projects[0].WorkspaceRealPath != workspace {
		t.Fatalf("projects = %#v, %v", projects, err)
	}
}

func TestEmployeeErrorMapping(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "employees")
	store, _ := employeestore.NewStore(root)
	service := &Service{Workspace: workspace, employees: store}
	if _, err := service.GetEmployee(context.Background(), "missing"); serviceErrorKind(err) != KindNotFound {
		t.Fatalf("not found error = %v", err)
	}
	if _, err := store.Create(controlPlaneDraft("employee-a"), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetEmployee(context.Background(), "employee-a"); serviceErrorKind(err) != KindInternal {
		t.Fatalf("corrupt store error = %v", err)
	}
}

func serviceErrorKind(err error) Kind {
	var serviceErr *Error
	if errors.As(err, &serviceErr) {
		return serviceErr.Kind
	}
	return Kind(-1)
}

func TestEmployeeRejectsAnotherWorkspace(t *testing.T) {
	workspace := t.TempDir()
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	service := &Service{Workspace: workspace, employees: store}
	input := EmployeeInput{
		Employee: controlPlaneDraft("employee-a"),
		ProjectBindings: []employee.ProjectBinding{{
			ID: "project-a", Label: "Other", WorkspaceRealPath: t.TempDir(), ReadAllowed: true,
		}},
	}
	if _, err := service.CreateEmployee(context.Background(), input); err == nil {
		t.Fatal("another workspace must be rejected")
	}
}

func controlPlaneDraft(id string) employee.Employee {
	return employee.Employee{
		ID: id, Name: "Test Employee", Avatar: employee.Avatar{Kind: employee.AvatarInitials},
		JobTitle: "Engineer", Charter: "Build bounded systems.",
		DefaultSelection:  employee.ModelSelection{Company: "deepseek", Access: "deepseek", Model: "deepseek-chat"},
		AgentProfile:      "coding",
		PermissionPolicy:  employee.PermissionPolicy{AllowedCapabilities: []string{"read", "write"}},
		BudgetPolicy:      employee.BudgetPolicy{MaxModelCalls: 8, MaxTokens: 100000, TimeoutSeconds: 3600},
		ConcurrencyPolicy: employee.ConcurrencyPolicy{MaxRunningTasks: 1},
		MemoryPolicy:      employee.MemoryPolicy{Promotion: employee.MemoryPromotionOwnerConfirmation},
	}
}
