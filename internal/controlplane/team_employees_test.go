package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/skill"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

func TestTeamEmployeePreflightPinsAssignmentAndRestoresWithoutMutableEmployee(t *testing.T) {
	fixture := newPhase6Fixture(t)
	templateStore, err := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = templateStore.Save(teamtemplate.Template{
		Name: "employee-team",
		Default: teamtemplate.RoleSelection{
			Company: "deepseek", Access: "deepseek", Model: "deepseek-chat",
		},
		Roles: map[string]teamtemplate.RoleSelection{
			string(team.RoleExplorer): {EmployeeID: "employee-a"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.service.teamTemplates = templateStore
	selection := config.RuntimeSelection{
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team",
	}
	plan, err := fixture.service.resolveTeamRolePlan(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.overrides[string(team.RoleExplorer)].Selection.Model; got != "deepseek-chat" {
		t.Fatalf("Employee default model = %q", got)
	}
	parent, err := session.NewConversation("Team", fixture.workspace, "digest", session.Selection{
		Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := parent.NewRun("inspect the documentation")
	if err != nil {
		t.Fatal(err)
	}
	parent.Mission, err = team.AdaptiveMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.service.materializeTeamEmployeeAssignments(context.Background(), parent, plan); err != nil {
		t.Fatal(err)
	}
	assignment, ok := parent.Mission.EmployeeAssignments["explore"]
	if !ok || assignment.EmployeeID != "employee-a" || assignment.EmployeeRevision < 1 {
		t.Fatalf("assignment=%+v", assignment)
	}
	childID := parent.Mission.WorkItems[0].ExecutionSessionID
	child, err := fixture.sessions.Load(context.Background(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.TeamEmployeeContextSnapshot == nil ||
		len(child.TeamEmployeeContextSnapshot.Memory) != 1 ||
		child.TeamEmployeeContextSnapshot.Digest != assignment.ContextDigest {
		t.Fatalf("hidden child did not pin isolated Employee context: %+v", child)
	}
	if _, _, err = fixture.service.GetSession(context.Background(), childID); err == nil {
		t.Fatal("hidden Team Worker Session was exposed through the public Control Plane")
	} else {
		var serviceErr *Error
		if !errors.As(err, &serviceErr) || serviceErr.Kind != KindNotFound {
			t.Fatalf("hidden Session error = %v, want KindNotFound", err)
		}
	}

	record, err := fixture.employees.Get("employee-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.employees.Disable("employee-a", record.Employee.Revision); err != nil {
		t.Fatal(err)
	}
	if err = fixture.employees.ForgetMemory("employee-a", fixture.memoryID); err != nil {
		t.Fatal(err)
	}
	restored, err := fixture.service.restoreTeamEmployeePlan(context.Background(), parent)
	if err != nil {
		t.Fatalf("recovery reread mutable Employee state: %v", err)
	}
	if restored.workItems["explore"].EmployeeContext.Digest != assignment.ContextDigest {
		t.Fatal("recovery replaced the immutable assignment snapshot")
	}
	if len(restored.workItems["explore"].EmployeeContext.Memory) != 1 {
		t.Fatal("recovery reread mutable Memory instead of the pinned hidden snapshot")
	}
	if err = parent.Mission.AddWork(team.WorkItem{
		ID: "follow-up", Title: "Follow up", Goal: "inspect more", Role: team.RoleExplorer,
	}); err != nil {
		t.Fatal(err)
	}
	followUp := &parent.Mission.WorkItems[len(parent.Mission.WorkItems)-1]
	if err = fixture.service.materializeTeamEmployeeWorkItem(context.Background(), parent, restored, followUp); err != nil {
		t.Fatal(err)
	}
	if pinned := parent.Mission.EmployeeAssignments["follow-up"]; pinned.EmployeeID != "employee-a" ||
		len(restored.workItems["follow-up"].EmployeeContext.Memory) != 1 {
		t.Fatal("dynamic WorkItem did not inherit the original immutable role assignment")
	}
}

func TestTeamEmployeeModelOverrideAndPreflightFailureHasZeroSessionSideEffects(t *testing.T) {
	fixture := newPhase6Fixture(t)
	templateStore, _ := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err := templateStore.Save(teamtemplate.Template{
		Name: "employee-team",
		Default: teamtemplate.RoleSelection{
			Company: "deepseek", Access: "deepseek", Model: "deepseek-chat",
		},
		Roles: map[string]teamtemplate.RoleSelection{
			string(team.RoleExplorer): {
				EmployeeID: "employee-a", Company: "deepseek",
				Access: "deepseek", Model: "deepseek-reasoner",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.service.teamTemplates = templateStore
	selection := config.RuntimeSelection{
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team",
	}
	plan, err := fixture.service.resolveTeamRolePlan(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.overrides[string(team.RoleExplorer)].Selection.Model; got != "deepseek-reasoner" {
		t.Fatalf("mission override lost: %q", got)
	}
	record, _ := fixture.employees.Get("employee-a")
	if _, err = fixture.employees.Disable("employee-a", record.Employee.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.service.resolveTeamRolePlan(context.Background(), selection); err == nil {
		t.Fatal("disabled Employee must fail Team preflight")
	}
	summaries, err := fixture.sessions.ListSummaries(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 || fixture.builds.Load() != 0 {
		t.Fatalf("preflight created execution side effects: sessions=%d builds=%d", len(summaries), fixture.builds.Load())
	}
}

func TestTeamEmployeeStaleSkillDigestFailsClosed(t *testing.T) {
	fixture := newPhase6Fixture(t)
	templateStore, _ := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err := templateStore.Save(teamtemplate.Template{
		Name: "employee-team",
		Default: teamtemplate.RoleSelection{
			Company: "deepseek", Access: "deepseek", Model: "deepseek-chat",
		},
		Roles: map[string]teamtemplate.RoleSelection{
			string(team.RoleExplorer): {EmployeeID: "employee-a"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.service.teamTemplates = templateStore
	if err := os.WriteFile(fixture.skillPath, []byte("# changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Reopen the configured catalog so the on-disk digest mismatch is checked
	// instead of using the already loaded immutable item.
	root := filepath.Dir(filepath.Dir(filepath.Dir(fixture.skillPath)))
	catalog, err := skill.NewCatalog(root)
	if err == nil {
		fixture.service.skills = catalog
	}
	selection := config.RuntimeSelection{
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team",
	}
	if _, err = fixture.service.resolveTeamRolePlan(context.Background(), selection); err == nil {
		t.Fatal("stale Skill digest must fail Team preflight")
	}
}

func TestTeamEmployeeMissingDisabledOrArchivedFailsBeforeExecutionState(t *testing.T) {
	tests := []struct {
		name       string
		employeeID string
		mutate     func(*phase6Fixture)
	}{
		{name: "missing", employeeID: "missing-employee", mutate: func(*phase6Fixture) {}},
		{name: "disabled", employeeID: "employee-a", mutate: func(f *phase6Fixture) {
			record, _ := f.employees.Get("employee-a")
			_, _ = f.employees.Disable("employee-a", record.Employee.Revision)
		}},
		{name: "archived", employeeID: "employee-a", mutate: func(f *phase6Fixture) {
			record, _ := f.employees.Get("employee-a")
			_, _ = f.employees.Archive("employee-a", record.Employee.Revision)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPhase6Fixture(t)
			test.mutate(fixture)
			templateStore, _ := teamtemplate.NewStore(filepath.Join(t.TempDir(), "team-template.json"))
			if err := templateStore.Save(teamtemplate.Template{
				Name: "employee-team",
				Default: teamtemplate.RoleSelection{
					Company: "deepseek", Access: "deepseek", Model: "deepseek-chat",
				},
				Roles: map[string]teamtemplate.RoleSelection{
					string(team.RoleExplorer): {EmployeeID: test.employeeID},
				},
			}); err != nil {
				t.Fatal(err)
			}
			fixture.service.teamTemplates = templateStore
			selection := config.RuntimeSelection{
				Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "team",
			}
			if _, err := fixture.service.resolveTeamRolePlan(context.Background(), selection); err == nil {
				t.Fatalf("%s Employee must fail Team preflight", test.name)
			}
			summaries, err := fixture.sessions.ListSummaries(context.Background(), 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(summaries) != 0 || fixture.builds.Load() != 0 {
				t.Fatalf("failure created execution side effects: sessions=%d builds=%d", len(summaries), fixture.builds.Load())
			}
		})
	}
}
