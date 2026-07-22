package teamtemplate

import (
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
