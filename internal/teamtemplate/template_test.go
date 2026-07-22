package teamtemplate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Rj455555/GoHermit/internal/team"
)

func validTemplate() Template {
	return Template{
		Name:    "core",
		Default: RoleSelection{Company: "anthropic", Access: "api", Model: "claude-sonnet-4"},
		Roles: map[string]RoleSelection{
			string(team.RoleLead):     {Company: "openai", Access: "api", Model: "gpt-5"},
			string(team.RoleExplorer): {Company: "anthropic", Access: "cli", Model: "claude-opus-4"},
			string(team.RoleBuilder):  {Company: "anthropic", Access: "api", Model: "claude-opus-4"},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	want := validTemplate()
	if err := store.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, SchemaVersion)
	}
	if got.Name != want.Name {
		t.Fatalf("name = %q, want %q", got.Name, want.Name)
	}
	if got.Default != want.Default {
		t.Fatalf("default = %+v, want %+v", got.Default, want.Default)
	}
	if !reflect.DeepEqual(got.Roles, want.Roles) {
		t.Fatalf("roles = %+v, want %+v", got.Roles, want.Roles)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("updated_at not stamped on save")
	}
	again, err := store.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("reload differs: %+v vs %+v", got, again)
	}
}

func TestExplicitPathWins(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit.json")
	envPath := filepath.Join(t.TempDir(), "env.json")
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", envPath)
	store, err := NewStore(explicit)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if store.Path() != explicit {
		t.Fatalf("path = %q, want %q", store.Path(), explicit)
	}
	if err := store.Save(validTemplate()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(explicit); err != nil {
		t.Fatalf("explicit file missing: %v", err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("env path should be untouched, stat err = %v", err)
	}
}

func TestEnvPathWinsOverFallbackAndStaysOutOfWorkspace(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "env", "team-template.json")
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", envPath)
	store, err := NewStore("")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if store.Path() != envPath {
		t.Fatalf("path = %q, want %q", store.Path(), envPath)
	}
	workspace := t.TempDir()
	if err := store.Save(validTemplate()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Fatalf("env file missing: %v", err)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace must stay empty, found %d entries", len(entries))
	}
}

func TestFallbackUnderUserConfigDir(t *testing.T) {
	t.Setenv("GOHERMIT_TEAM_TEMPLATE_STORE", "")
	store, err := NewStore("")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	want := filepath.Join(root, "gohermit", "team-template.json")
	if store.Path() != want {
		t.Fatalf("path = %q, want %q", store.Path(), want)
	}
}

func TestValidate(t *testing.T) {
	full := RoleSelection{Company: "anthropic", Access: "api", Model: "claude-sonnet-4"}
	cases := []struct {
		name   string
		mutate func(*Template)
	}{
		{"missing name", func(t *Template) { t.Name = "  " }},
		{"operator override", func(t *Template) { t.Roles[string(team.RoleOperator)] = full }},
		{"unknown role override", func(t *Template) { t.Roles["intern"] = full }},
		{"incomplete override", func(t *Template) {
			t.Roles[string(team.RoleLead)] = RoleSelection{Company: "anthropic", Access: "api"}
		}},
		{"missing default company", func(t *Template) { t.Default.Company = "" }},
		{"missing default access", func(t *Template) { t.Default.Access = "" }},
		{"missing default model", func(t *Template) { t.Default.Model = "" }},
		{"oversized text", func(t *Template) { t.Default.Model = strings.Repeat("m", MaxTextBytes+1) }},
		{"secret value", func(t *Template) { t.Default.Access = "api_key=abc123" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			template := validTemplate()
			tc.mutate(&template)
			if err := Validate(template); err == nil {
				t.Fatal("Validate succeeded, want error")
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		if err := Validate(validTemplate()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

func TestSaveRejectsInvalidWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-template.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	bad := validTemplate()
	bad.Roles[string(team.RoleOperator)] = RoleSelection{Company: "anthropic", Access: "api", Model: "claude-sonnet-4"}
	if err := store.Save(bad); err == nil {
		t.Fatal("save succeeded, want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid save must not create the file, stat err = %v", err)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, Template{SchemaVersion: SchemaVersion}) {
		t.Fatalf("load = %+v, want empty template at schema %d", got, SchemaVersion)
	}
}

func TestLoadFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"unknown schema version", `{"schema_version": 99}`},
		{"unknown field", `{"schema_version": 1, "bogus": true}`},
		{"corrupt json", `{not json`},
		{"oversized file", strings.Repeat(" ", 257<<10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "team-template.json")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			store, err := NewStore(path)
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			if _, err := store.Load(); err == nil {
				t.Fatal("load succeeded, want error")
			}
		})
	}
}

func TestSaveFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team-template.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Save(validTemplate()); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestConcurrentAccess(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Save(validTemplate()); err != nil {
		t.Fatalf("save: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Load(); err != nil {
				t.Errorf("load: %v", err)
			}
			if err := store.Save(validTemplate()); err != nil {
				t.Errorf("save: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestEmptyReportsMissingDefaultSelection(t *testing.T) {
	if !(Template{}).Empty() {
		t.Fatal("zero template must be empty")
	}
	if !(Template{SchemaVersion: SchemaVersion}).Empty() {
		t.Fatal("template from a missing file must be empty")
	}
	partial := Template{Default: RoleSelection{Company: "openai", Access: "api"}}
	if !partial.Empty() {
		t.Fatal("default without a model must be empty")
	}
	if validTemplate().Empty() {
		t.Fatal("fully populated template must not be empty")
	}
}

func TestSelectionForRolePrefersOverride(t *testing.T) {
	template := validTemplate()
	if got := template.SelectionForRole(string(team.RoleLead)); got != template.Roles[string(team.RoleLead)] {
		t.Fatalf("lead selection = %+v, want override %+v", got, template.Roles[string(team.RoleLead)])
	}
	if got := template.SelectionForRole(string(team.RoleVerifier)); got != template.Default {
		t.Fatalf("verifier selection = %+v, want default %+v", got, template.Default)
	}
}

func TestEffectiveSelectionsCoversEveryValidatableRole(t *testing.T) {
	template := validTemplate()
	selections := EffectiveSelections(template)
	wantRoles := []string{
		string(team.RoleLead), string(team.RoleExplorer), string(team.RoleBuilder),
		string(team.RoleReviewer), string(team.RoleVerifier),
	}
	if len(selections) != len(wantRoles) {
		t.Fatalf("selections = %+v", selections)
	}
	for _, role := range wantRoles {
		selection, ok := selections[role]
		if !ok {
			t.Fatalf("role %q missing from %+v", role, selections)
		}
		if selection != template.SelectionForRole(role) {
			t.Fatalf("role %q selection = %+v, want %+v", role, selection, template.SelectionForRole(role))
		}
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	want := validTemplate()
	want.SchemaVersion = SchemaVersion
	raw, err := Export(want)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	plain, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != string(plain) {
		t.Fatalf("clean export must equal a plain marshal:\n%s\nvs\n%s", raw, plain)
	}
	got, err := Import(raw)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestExportRedactsSecretFields(t *testing.T) {
	template := validTemplate()
	template.Name = "core api_key=abc123"
	template.Default.Access = "sk-proj-xyz"
	template.Roles[string(team.RoleLead)] = RoleSelection{Company: "openai", Access: "api", Model: "ghp_tok123"}
	raw, err := Export(template)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := string(raw)
	for _, marker := range []string{"api_key=abc123", "sk-proj-xyz", "ghp_tok123"} {
		if strings.Contains(body, marker) {
			t.Fatalf("export leaked %q:\n%s", marker, body)
		}
	}
	var redacted Template
	if err := json.Unmarshal(raw, &redacted); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	// Redaction blanks the field but keeps the role entry's structure.
	if redacted.Name != "" || redacted.Default.Access != "" || redacted.Roles[string(team.RoleLead)].Model != "" {
		t.Fatalf("redacted = %+v", redacted)
	}
	if redacted.Default.Company != template.Default.Company || len(redacted.Roles) != len(template.Roles) {
		t.Fatalf("redaction must keep clean fields and entries: %+v", redacted)
	}
	// The tampered source template is untouched.
	if template.Name != "core api_key=abc123" {
		t.Fatalf("export mutated its input: %+v", template)
	}
}

func TestImportRejectsSecretMarkers(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Template)
	}{
		{"name", func(t *Template) { t.Name = "core api_key=abc123" }},
		{"default company", func(t *Template) { t.Default.Company = "ghp_tok123" }},
		{"default access", func(t *Template) { t.Default.Access = "sk-proj-xyz" }},
		{"default model", func(t *Template) { t.Default.Model = "password=hunter2" }},
		{"role company", func(t *Template) {
			t.Roles[string(team.RoleLead)] = RoleSelection{Company: "github_pat_abc", Access: "api", Model: "gpt-5"}
		}},
		{"role access", func(t *Template) {
			t.Roles[string(team.RoleLead)] = RoleSelection{Company: "openai", Access: "access_token=xyz", Model: "gpt-5"}
		}},
		{"role model", func(t *Template) {
			t.Roles[string(team.RoleLead)] = RoleSelection{Company: "openai", Access: "api", Model: "refresh_token=xyz"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			template := validTemplate()
			tc.mutate(&template)
			template.SchemaVersion = SchemaVersion
			raw, err := json.Marshal(template)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if _, err := Import(raw); !errors.Is(err, ErrImportSecret) {
				t.Fatalf("import err = %v, want ErrImportSecret", err)
			}
		})
	}
}

func TestImportRejectsMalformedFiles(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"corrupt json", `{not json`},
		{"unknown field", `{"schema_version": 1, "bogus": true}`},
		{"unknown schema version", `{"schema_version": 99}`},
		{"oversized file", strings.Repeat(" ", 257<<10)},
		{"missing required fields", `{"schema_version": 1, "name": "core"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Import([]byte(tc.content)); err == nil {
				t.Fatal("import succeeded, want error")
			}
		})
	}
}

func TestImportRedactedExportOnlyFailsForMissingFields(t *testing.T) {
	template := validTemplate()
	template.Default.Access = "sk-proj-xyz"
	raw, err := Export(template)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// The redacted document carries no secret, so a refusal must come from
	// validation of the blanked fields, not the secret screen.
	if _, err := Import(raw); err == nil || errors.Is(err, ErrImportSecret) {
		t.Fatalf("import err = %v, want a validation error about missing fields", err)
	}
	var redacted Template
	if err := json.Unmarshal(raw, &redacted); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	redacted.Default.Access = "api"
	refilled, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := Import(refilled)
	if err != nil {
		t.Fatalf("import refilled export: %v", err)
	}
	if got.Default.Access != "api" || !reflect.DeepEqual(got.Roles, template.Roles) {
		t.Fatalf("import = %+v", got)
	}
}

func TestRoleLimitsRoundTripThroughStoreAndExport(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	want := validTemplate()
	builder := want.Roles[string(team.RoleBuilder)]
	builder.MaxModelCalls, builder.MaxTokens = 4, 40_000
	want.Roles[string(team.RoleBuilder)] = builder
	want.Default.MaxTokens = 250_000
	if err := store.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Roles[string(team.RoleBuilder)] != builder {
		t.Fatalf("builder selection = %+v, want %+v", got.Roles[string(team.RoleBuilder)], builder)
	}
	if got.Default != want.Default {
		t.Fatalf("default = %+v, want %+v", got.Default, want.Default)
	}
	// Export/import is the owner-visible path; the limits must survive it.
	raw, err := Export(got)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	imported, err := Import(raw)
	if err != nil {
		t.Fatalf("import export: %v", err)
	}
	if imported.Roles[string(team.RoleBuilder)] != builder || imported.Default != want.Default {
		t.Fatalf("import = %+v", imported)
	}
}

func TestRoleLimitsOmittedWhenZero(t *testing.T) {
	raw, err := Export(validTemplate())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if strings.Contains(string(raw), "max_model_calls") || strings.Contains(string(raw), "max_tokens") {
		t.Fatalf("zero limits must be omitted from the export: %s", raw)
	}
}

func TestValidateRejectsOutOfRangeRoleLimits(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*RoleSelection)
		wantErr string
	}{
		{"negative model calls", func(s *RoleSelection) { s.MaxModelCalls = -1 }, "max_model_calls"},
		{"negative tokens", func(s *RoleSelection) { s.MaxTokens = -1 }, "max_tokens"},
		{"model calls above cap", func(s *RoleSelection) { s.MaxModelCalls = MaxRoleModelCalls + 1 }, "max_model_calls"},
		{"tokens above cap", func(s *RoleSelection) { s.MaxTokens = MaxRoleTokens + 1 }, "max_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			template := validTemplate()
			tc.mutate(&template.Default)
			if err := Validate(template); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("default validate err = %v, want %q", err, tc.wantErr)
			}
			template = validTemplate()
			role := template.Roles[string(team.RoleBuilder)]
			tc.mutate(&role)
			template.Roles[string(team.RoleBuilder)] = role
			if err := Validate(template); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("role validate err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAcceptsBoundaryRoleLimits(t *testing.T) {
	template := validTemplate()
	template.Default.MaxModelCalls, template.Default.MaxTokens = MaxRoleModelCalls, MaxRoleTokens
	if err := Validate(template); err != nil {
		t.Fatalf("validate boundary limits: %v", err)
	}
}

// A template file written before the limit keys existed must load with zero
// (unlimited) limits.
func TestLegacyFileWithoutLimitsLoadsUnlimited(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "team-template.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	legacy := `{"schema_version": 1, "name": "core", "default": {"company": "anthropic", "access": "api", "model": "claude-sonnet-4"}, "roles": {"builder": {"company": "anthropic", "access": "api", "model": "claude-opus-4"}}, "updated_at": "2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(store.Path(), []byte(legacy), 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if got.Default.MaxModelCalls != 0 || got.Default.MaxTokens != 0 || got.Roles["builder"].MaxModelCalls != 0 || got.Roles["builder"].MaxTokens != 0 {
		t.Fatalf("legacy file gained limits: %+v", got)
	}
}
