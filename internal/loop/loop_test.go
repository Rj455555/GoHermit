package loop

import (
	"strings"
	"testing"
	"time"
)

func validDefinition() Definition {
	return Definition{
		ID:                "loop-1",
		SchemaVersion:     SchemaVersion,
		Name:              "nightly-review",
		Description:       "reviews the repo",
		WorkspaceIdentity: "github.com/acme/widget",
		Enabled:           true,
		TaskSource:        TaskSource{Type: TaskSourceFixedPrompt, Prompt: "review the latest changes"},
		AgentSelection:    AgentSelection{Company: "acme", Access: "api", Model: "model-x", Agent: "hermit"},
		TeamTemplateRef:   "default",
		PlanMode:          PlanAuto,
		VerificationRecipe: VerificationRecipe{
			Checks: []RecipeCheck{
				{ID: "vet", Command: []string{"go", "vet", "./..."}, Required: true, TimeoutSeconds: 120},
			},
			IndependentVerifier: true,
			MaxRepairAttempts:   2,
		},
		Budget:          Budget{MaxModelCalls: 10, MaxTokens: 100_000, TimeoutSeconds: 900},
		ApprovalPolicy:  ApprovalPolicy{RequireForMutation: true},
		WorkspacePolicy: WorkspacePolicy{ReadOnly: false, RequireCleanGit: true},
		OutputPolicy:    OutputPolicy{IncludeDiff: true, MaxReportBytes: 64 << 10},
		Revision:        1,
	}
}

func TestValidateDefinition(t *testing.T) {
	secret := "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	oversize := strings.Repeat("x", MaxTextBytes+1)

	mutate := map[string]func(*Definition){
		"missing id":               func(d *Definition) { d.ID = "" },
		"id too long":              func(d *Definition) { d.ID = strings.Repeat("i", MaxIDBytes+1) },
		"negative revision":        func(d *Definition) { d.Revision = -1 },
		"missing name":             func(d *Definition) { d.Name = "  " },
		"missing workspace":        func(d *Definition) { d.WorkspaceIdentity = "" },
		"bad plan mode":            func(d *Definition) { d.PlanMode = "yolo" },
		"empty plan mode":          func(d *Definition) { d.PlanMode = "" },
		"unknown task source":      func(d *Definition) { d.TaskSource.Type = "jira" },
		"empty task source type":   func(d *Definition) { d.TaskSource.Type = "" },
		"empty prompt":             func(d *Definition) { d.TaskSource.Prompt = " \x00 " },
		"oversize prompt":          func(d *Definition) { d.TaskSource.Prompt = oversize },
		"missing company":          func(d *Definition) { d.AgentSelection.Company = "" },
		"missing access":           func(d *Definition) { d.AgentSelection.Access = "" },
		"missing model":            func(d *Definition) { d.AgentSelection.Model = "" },
		"missing agent":            func(d *Definition) { d.AgentSelection.Agent = "" },
		"too many checks":          func(d *Definition) { d.VerificationRecipe.Checks = make([]RecipeCheck, MaxChecks+1) },
		"check missing id":         func(d *Definition) { d.VerificationRecipe.Checks[0].ID = "" },
		"check missing command":    func(d *Definition) { d.VerificationRecipe.Checks[0].Command = nil },
		"check empty command argv": func(d *Definition) { d.VerificationRecipe.Checks[0].Command = []string{" "} },
		"check too many args":      func(d *Definition) { d.VerificationRecipe.Checks[0].Command = make([]string, MaxCommandArgs+1) },
		"check timeout zero":       func(d *Definition) { d.VerificationRecipe.Checks[0].TimeoutSeconds = 0 },
		"check timeout too big":    func(d *Definition) { d.VerificationRecipe.Checks[0].TimeoutSeconds = MaxCheckTimeoutSeconds + 1 },
		"repair attempts negative": func(d *Definition) { d.VerificationRecipe.MaxRepairAttempts = -1 },
		"repair attempts too big":  func(d *Definition) { d.VerificationRecipe.MaxRepairAttempts = MaxRepairAttempts + 1 },
		"budget calls zero":        func(d *Definition) { d.Budget.MaxModelCalls = 0 },
		"budget calls too big":     func(d *Definition) { d.Budget.MaxModelCalls = MaxModelCalls + 1 },
		"budget tokens zero":       func(d *Definition) { d.Budget.MaxTokens = 0 },
		"budget tokens too big":    func(d *Definition) { d.Budget.MaxTokens = MaxTokens + 1 },
		"budget timeout zero":      func(d *Definition) { d.Budget.TimeoutSeconds = 0 },
		"budget timeout too big":   func(d *Definition) { d.Budget.TimeoutSeconds = MaxBudgetTimeoutSeconds + 1 },
		"report bytes zero":        func(d *Definition) { d.OutputPolicy.MaxReportBytes = 0 },
		"report bytes too big":     func(d *Definition) { d.OutputPolicy.MaxReportBytes = MaxReportBytes + 1 },
		"secret in name":           func(d *Definition) { d.Name = secret },
		"secret in description":    func(d *Definition) { d.Description = secret },
		"secret in prompt":         func(d *Definition) { d.TaskSource.Prompt = "run with " + secret },
		"secret in workspace":      func(d *Definition) { d.WorkspaceIdentity = secret },
		"secret in template ref":   func(d *Definition) { d.TeamTemplateRef = secret },
		"secret in selection":      func(d *Definition) { d.AgentSelection.Model = secret },
		"secret in check id":       func(d *Definition) { d.VerificationRecipe.Checks[0].ID = secret },
		"secret in check command":  func(d *Definition) { d.VerificationRecipe.Checks[0].Command[1] = secret },
		"oversize description":     func(d *Definition) { d.Description = oversize },
	}

	for name, fn := range mutate {
		t.Run(name, func(t *testing.T) {
			d := validDefinition()
			fn(&d)
			if err := ValidateDefinition(d); err == nil {
				t.Fatalf("ValidateDefinition(%s) succeeded, want error", name)
			}
		})
	}

	t.Run("valid", func(t *testing.T) {
		if err := ValidateDefinition(validDefinition()); err != nil {
			t.Fatalf("ValidateDefinition(valid) = %v", err)
		}
	})
	t.Run("valid without checks", func(t *testing.T) {
		d := validDefinition()
		d.VerificationRecipe.Checks = nil
		if err := ValidateDefinition(d); err != nil {
			t.Fatalf("ValidateDefinition(no checks) = %v", err)
		}
	})
	t.Run("review plan mode", func(t *testing.T) {
		d := validDefinition()
		d.PlanMode = PlanReview
		if err := ValidateDefinition(d); err != nil {
			t.Fatalf("ValidateDefinition(review) = %v", err)
		}
	})
}

func TestRedactDefinition(t *testing.T) {
	d := validDefinition()
	d.Name = "token ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	d.TaskSource.Prompt = "use api_key=deadbeef to sync"
	d.VerificationRecipe.Checks[0].Command[1] = "github_pat_123456"
	redacted := RedactDefinition(d)
	for _, field := range SecretFields(redacted) {
		if strings.Contains(field.Value, "ghp_") || strings.Contains(field.Value, "api_key=") || strings.Contains(field.Value, "github_pat_") {
			t.Fatalf("redacted %s still carries secret content", field.Label)
		}
	}
	// Clean fields survive untouched.
	if redacted.WorkspaceIdentity != d.WorkspaceIdentity || redacted.AgentSelection.Company != d.AgentSelection.Company {
		t.Fatal("redaction blanked clean fields")
	}
	// The source definition is not mutated.
	if !strings.Contains(d.Name, "ghp_") {
		t.Fatal("redaction mutated the source definition")
	}
}

func TestNewInvocation(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	def := validDefinition()
	def.Revision = 3

	inv, err := NewInvocation(def, TriggerManual, def.TaskSource.Prompt, now)
	if err != nil {
		t.Fatalf("NewInvocation = %v", err)
	}
	if inv.ID == "" || inv.LoopID != def.ID || inv.DefinitionRevision != 3 {
		t.Fatalf("unexpected invocation identity: %+v", inv)
	}
	if inv.Status != Prepared || !inv.CreatedAt.Equal(now) {
		t.Fatalf("unexpected initial state: %+v", inv)
	}

	cases := map[string]func() (Definition, string, string){
		"invalid definition": func() (Definition, string, string) {
			d := validDefinition()
			d.Name = ""
			return d, TriggerManual, "task"
		},
		"unknown trigger": func() (Definition, string, string) {
			return validDefinition(), "cron", "task"
		},
		"empty task snapshot": func() (Definition, string, string) {
			return validDefinition(), TriggerManual, "  "
		},
		"oversize task snapshot": func() (Definition, string, string) {
			return validDefinition(), TriggerManual, strings.Repeat("x", MaxTextBytes+1)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			d, trigger, task := build()
			if _, err := NewInvocation(d, trigger, task, now); err == nil {
				t.Fatalf("NewInvocation(%s) succeeded, want error", name)
			}
		})
	}
}

func TestSnapshotImmutability(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	def := validDefinition()
	def.Revision = 1

	inv, err := NewInvocation(def, TriggerManual, def.TaskSource.Prompt, now)
	if err != nil {
		t.Fatalf("NewInvocation = %v", err)
	}

	// Mutate the source definition in every mutable way; the embedded
	// snapshot must stay untouched.
	def.Name = "renamed"
	def.TaskSource.Prompt = "replaced prompt"
	def.VerificationRecipe.Checks[0].Command[1] = "mutated-arg"
	def.VerificationRecipe.Checks = append(def.VerificationRecipe.Checks, RecipeCheck{ID: "extra", Command: []string{"true"}, TimeoutSeconds: 1})
	def.Revision = 2

	snapshot := inv.DefinitionSnapshot
	if snapshot.Name != "nightly-review" || snapshot.TaskSource.Prompt != "review the latest changes" {
		t.Fatal("snapshot changed after source definition was edited")
	}
	if len(snapshot.VerificationRecipe.Checks) != 1 || snapshot.VerificationRecipe.Checks[0].Command[1] != "vet" {
		t.Fatal("snapshot recipe changed after source recipe was edited")
	}
	if snapshot.Revision != 1 || inv.DefinitionRevision != 1 {
		t.Fatal("snapshot revision changed after source revision bump")
	}

	// The next invocation sees the new revision.
	next, err := NewInvocation(def, TriggerManual, def.TaskSource.Prompt, now)
	if err != nil {
		t.Fatalf("NewInvocation(updated) = %v", err)
	}
	if next.DefinitionRevision != 2 || next.DefinitionSnapshot.Name != "renamed" {
		t.Fatal("next invocation did not pick up the updated definition")
	}
}

func mustInvocation(t *testing.T) Invocation {
	t.Helper()
	inv, err := NewInvocation(validDefinition(), TriggerManual, "do the thing", time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewInvocation = %v", err)
	}
	return inv
}

func TestInvocationHappyPath(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	inv := mustInvocation(t)

	if err := inv.Dispatch(); err != nil {
		t.Fatalf("Dispatch = %v", err)
	}
	if inv.Status != Dispatched {
		t.Fatalf("status = %s, want dispatched", inv.Status)
	}
	if err := inv.Attach("session-1", "run-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("Attach = %v", err)
	}
	if inv.Status != Attached || inv.SessionID != "session-1" || inv.RunID != "run-1" {
		t.Fatalf("unexpected attached state: %+v", inv)
	}
	if inv.StartedAt == nil || !inv.StartedAt.Equal(now.Add(time.Minute)) {
		t.Fatal("started_at not stamped on attach")
	}
	if err := inv.Complete(now.Add(2 * time.Minute)); err != nil {
		t.Fatalf("Complete = %v", err)
	}
	if inv.Status != Completed || inv.FinishedAt == nil {
		t.Fatalf("unexpected completed state: %+v", inv)
	}
}

// drive moves an invocation to the requested starting status.
func drive(t *testing.T, status Status) Invocation {
	t.Helper()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	inv := mustInvocation(t)
	switch status {
	case Prepared:
	case Dispatched:
		if err := inv.Dispatch(); err != nil {
			t.Fatal(err)
		}
	case Attached:
		if err := inv.Dispatch(); err != nil {
			t.Fatal(err)
		}
		if err := inv.Attach("s", "r", now); err != nil {
			t.Fatal(err)
		}
	case Completed:
		inv = drive(t, Attached)
		if err := inv.Complete(now); err != nil {
			t.Fatal(err)
		}
	case Skipped:
		if err := inv.Skip("not needed", now); err != nil {
			t.Fatal(err)
		}
	case Blocked:
		if err := inv.Block("budget", "over budget", now); err != nil {
			t.Fatal(err)
		}
	case Failed:
		inv = drive(t, Dispatched)
		if err := inv.Fail("crash", "run crashed", now); err != nil {
			t.Fatal(err)
		}
	case Cancelled:
		if err := inv.Cancel("owner stopped it", now); err != nil {
			t.Fatal(err)
		}
	}
	if inv.Status != status {
		t.Fatalf("drive(%s) landed on %s", status, inv.Status)
	}
	return inv
}

func TestInvocationIllegalTransitions(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	// Every transition applied from every state it must reject.
	cases := []struct {
		name  string
		from  Status
		apply func(*Invocation) error
	}{
		{"dispatch from dispatched", Dispatched, func(i *Invocation) error { return i.Dispatch() }},
		{"dispatch from attached", Attached, func(i *Invocation) error { return i.Dispatch() }},
		{"no prepared->attached", Prepared, func(i *Invocation) error { return i.Attach("s", "r", now) }},
		{"attach from attached", Attached, func(i *Invocation) error { return i.Attach("s", "r", now) }},
		{"no prepared->completed", Prepared, func(i *Invocation) error { return i.Complete(now) }},
		{"no dispatched->completed", Dispatched, func(i *Invocation) error { return i.Complete(now) }},
		{"skip from dispatched", Dispatched, func(i *Invocation) error { return i.Skip("x", now) }},
		{"skip from attached", Attached, func(i *Invocation) error { return i.Skip("x", now) }},
		{"block from dispatched", Dispatched, func(i *Invocation) error { return i.Block("c", "x", now) }},
		{"block from attached", Attached, func(i *Invocation) error { return i.Block("c", "x", now) }},
		{"fail from prepared", Prepared, func(i *Invocation) error { return i.Fail("c", "x", now) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := drive(t, tc.from)
			if err := tc.apply(&inv); err == nil {
				t.Fatalf("%s succeeded, want error", tc.name)
			}
			if inv.Status != tc.from {
				t.Fatalf("rejected transition still moved %s -> %s", tc.from, inv.Status)
			}
		})
	}
}

func TestInvocationTerminalStatesAreFinal(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	terminals := []Status{Completed, Skipped, Blocked, Failed, Cancelled}
	moves := map[string]func(*Invocation) error{
		"dispatch": func(i *Invocation) error { return i.Dispatch() },
		"attach":   func(i *Invocation) error { return i.Attach("s", "r", now) },
		"complete": func(i *Invocation) error { return i.Complete(now) },
		"skip":     func(i *Invocation) error { return i.Skip("x", now) },
		"block":    func(i *Invocation) error { return i.Block("c", "x", now) },
		"fail":     func(i *Invocation) error { return i.Fail("c", "x", now) },
		"cancel":   func(i *Invocation) error { return i.Cancel("x", now) },
	}
	for _, terminal := range terminals {
		for move, apply := range moves {
			t.Run(string(terminal)+"/"+move, func(t *testing.T) {
				inv := drive(t, terminal)
				if err := apply(&inv); err == nil {
					t.Fatalf("%s from terminal %s succeeded, want error", move, terminal)
				}
				if inv.Status != terminal {
					t.Fatalf("terminal %s moved to %s", terminal, inv.Status)
				}
			})
		}
	}
}

func TestInvocationTransitionArguments(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	inv := drive(t, Dispatched)
	if err := inv.Attach("", "run-1", now); err == nil {
		t.Fatal("Attach without session id succeeded, want error")
	}
	if err := inv.Attach("session-1", " ", now); err == nil {
		t.Fatal("Attach without run id succeeded, want error")
	}

	inv = mustInvocation(t)
	if err := inv.Block("", "summary", now); err == nil {
		t.Fatal("Block without failure code succeeded, want error")
	}

	inv = drive(t, Dispatched)
	if err := inv.Fail(" ", "summary", now); err == nil {
		t.Fatal("Fail without failure code succeeded, want error")
	}
	if err := inv.Fail("crash", "boom", now); err != nil {
		t.Fatalf("Fail = %v", err)
	}
	if inv.FailureCode != "crash" || inv.FailureSummary != "boom" || inv.FinishedAt == nil {
		t.Fatalf("failure detail not recorded: %+v", inv)
	}
}

func TestValidateInvocation(t *testing.T) {
	valid := mustInvocation(t)
	if err := ValidateInvocation(valid); err != nil {
		t.Fatalf("ValidateInvocation(valid) = %v", err)
	}

	cases := map[string]func(*Invocation){
		"missing id":        func(i *Invocation) { i.ID = "" },
		"missing loop id":   func(i *Invocation) { i.LoopID = "" },
		"unknown status":    func(i *Invocation) { i.Status = "running" },
		"unknown trigger":   func(i *Invocation) { i.Trigger = "cron" },
		"empty task":        func(i *Invocation) { i.TaskSnapshot = "" },
		"revision mismatch": func(i *Invocation) { i.DefinitionRevision = 99 },
		"invalid snapshot":  func(i *Invocation) { i.DefinitionSnapshot.Name = "" },
		"secret summary":    func(i *Invocation) { i.FailureSummary = "ghp_abcdefghijklmnopqrstuvwxyz0123456789" },
		"blocked no code": func(i *Invocation) {
			i.Status = Blocked
			i.FailureCode = ""
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			inv := mustInvocation(t)
			fn(&inv)
			if err := ValidateInvocation(inv); err == nil {
				t.Fatalf("ValidateInvocation(%s) succeeded, want error", name)
			}
		})
	}
}
