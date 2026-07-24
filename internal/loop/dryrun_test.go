package loop

import (
	"strings"
	"testing"
)

func validDryRunReport() DryRunReport {
	report := DryRunReport{
		LoopID:             "loop-1",
		DefinitionRevision: 1,
		DefinitionValid:    true,
		WorkspaceIdentity:  "/workspaces/widget",
		WorkspaceMatches:   true,
		GitClean:           true,
		TaskPrompt:         "review the latest changes",
		Agent:              AgentSelection{Company: "acme", Access: "api", Model: "model-x", Agent: "hermit"},
		Roles:              []RoleAvailability{{Company: "acme", Access: "api", Model: "model-x", CredentialConfigured: true, Detail: "configured"}},
		WriteScope:         "workspace-writable (requires a clean git workspace)",
		Checks:             []RecipeCheck{{ID: "vet", Command: []string{"go", "vet", "./..."}, Required: true, TimeoutSeconds: 120}},
		Budget:             Budget{MaxModelCalls: 10, MaxTokens: 100_000, TimeoutSeconds: 900},
		RequiresApproval:   true,
		Ready:              true,
	}
	return report
}

func TestValidateDryRunReport(t *testing.T) {
	oversize := strings.Repeat("x", MaxTextBytes+1)
	mutate := map[string]func(*DryRunReport){
		"missing loop id":      func(r *DryRunReport) { r.LoopID = "" },
		"loop id too long":     func(r *DryRunReport) { r.LoopID = strings.Repeat("i", MaxIDBytes+1) },
		"negative revision":    func(r *DryRunReport) { r.DefinitionRevision = -1 },
		"oversize workspace":   func(r *DryRunReport) { r.WorkspaceIdentity = oversize },
		"oversize prompt":      func(r *DryRunReport) { r.TaskPrompt = oversize },
		"oversize write scope": func(r *DryRunReport) { r.WriteScope = oversize },
		"oversize template":    func(r *DryRunReport) { r.TeamTemplateRef = oversize },
		"too many roles":       func(r *DryRunReport) { r.Roles = make([]RoleAvailability, MaxDryRunRoles+1) },
		"oversize role detail": func(r *DryRunReport) { r.Roles[0].Detail = oversize },
		"oversize role name":   func(r *DryRunReport) { r.Roles[0].Role = strings.Repeat("r", MaxIDBytes+1) },
		"too many checks":      func(r *DryRunReport) { r.Checks = make([]RecipeCheck, MaxChecks+1) },
		"too many reasons":     func(r *DryRunReport) { r.Ready = false; r.Reasons = make([]string, MaxDryRunReasons+1) },
		"empty reason":         func(r *DryRunReport) { r.Ready = false; r.Reasons = []string{"  "} },
		"oversize reason":      func(r *DryRunReport) { r.Ready = false; r.Reasons = []string{oversize} },
		"ready with reasons":   func(r *DryRunReport) { r.Reasons = []string{"dirty git"} },
	}
	for name, fn := range mutate {
		t.Run(name, func(t *testing.T) {
			report := validDryRunReport()
			fn(&report)
			if err := ValidateDryRunReport(report); err == nil {
				t.Fatalf("expected validation failure for %s", name)
			}
		})
	}
	t.Run("valid ready report", func(t *testing.T) {
		if err := ValidateDryRunReport(validDryRunReport()); err != nil {
			t.Fatalf("valid report rejected: %v", err)
		}
	})
	t.Run("valid not-ready report", func(t *testing.T) {
		report := validDryRunReport()
		report.Refuse("definition is disabled")
		if err := ValidateDryRunReport(report); err != nil {
			t.Fatalf("not-ready report rejected: %v", err)
		}
	})
}

func TestDryRunReportRefuseBoundsReasonsAndClearsReady(t *testing.T) {
	report := validDryRunReport()
	for i := 0; i < MaxDryRunReasons+4; i++ {
		report.Refuse("reason")
	}
	if report.Ready {
		t.Fatal("refused report must not be ready")
	}
	if len(report.Reasons) != MaxDryRunReasons {
		t.Fatalf("reasons=%d, want bounded at %d", len(report.Reasons), MaxDryRunReasons)
	}
	if err := ValidateDryRunReport(report); err != nil {
		t.Fatalf("bounded refused report rejected: %v", err)
	}
}
