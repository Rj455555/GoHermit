package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/skill"
)

func TestEmployeeSkillBindingsPinCatalogAndRecordBoundedActivity(t *testing.T) {
	root := t.TempDir()
	writeControlPlaneAdapter(t, root, "review", "Review", "Review carefully.")
	catalog, err := skill.NewCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	service := &Service{Workspace: t.TempDir(), employees: store, skills: catalog}
	created, err := store.Create(controlPlaneDraft("employee-a"), nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListSkills(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("catalog = %#v, %v", items, err)
	}
	binding := employee.SkillBinding{
		SkillID: items[0].SkillID, Version: items[0].Version, Digest: items[0].Digest,
		Configuration: json.RawMessage(`{}`), Enabled: true,
	}
	updated, err := service.UpdateEmployeeSkills(context.Background(), created.Employee.ID, EmployeeSkillsUpdateInput{
		ExpectedRevision: created.Employee.Revision, Bindings: []employee.SkillBinding{binding},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Employee.Revision != 2 || len(updated.Employee.SkillBindings) != 1 {
		t.Fatalf("updated = %#v", updated)
	}
	before, err := store.LoadRevision(created.Employee.ID, created.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.LoadRevision(created.Employee.ID, updated.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Employee.SkillBindings) != 0 || len(after.Employee.SkillBindings) != 1 ||
		after.Employee.SkillBindings[0].Digest != binding.Digest {
		t.Fatalf("immutable revision pinning failed: before=%#v after=%#v", before, after)
	}
	status, err := service.EmployeeSkills(context.Background(), created.Employee.ID)
	if err != nil || len(status.Bindings) != 1 || status.Bindings[0].Status != "current" {
		t.Fatalf("status = %#v, %v", status, err)
	}
	activity, err := store.Activity(created.Employee.ID, employeestore.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range activity.Events {
		if event.Type == employeestore.ActivitySkillBinding {
			found = true
			if event.SubjectID != "skill-bindings-r2" || event.TaskID != "" || event.SessionID != "" || event.RunID != "" {
				t.Fatalf("unbounded or execution activity = %#v", event)
			}
		}
	}
	if !found {
		t.Fatal("skill_binding_changed activity missing")
	}
}

func TestUppercaseManifestDigestBindingRemainsCurrentAfterStoreReopen(t *testing.T) {
	catalogRoot := t.TempDir()
	canonicalDigest := writeControlPlaneNativeUpperDigest(t, catalogRoot, "native", "1.0.0")
	catalog, err := skill.NewCatalog(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	items, err := catalog.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Manifest.Digest != canonicalDigest ||
		items[0].Manifest.Digest != strings.ToLower(items[0].Manifest.Digest) {
		t.Fatalf("Catalog Digest is not canonical: %#v", items)
	}

	storeRoot := filepath.Join(t.TempDir(), "employees")
	store, _ := employeestore.NewStore(storeRoot)
	created, err := store.Create(controlPlaneDraft("employee-uppercase"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Workspace: t.TempDir(), employees: store, skills: catalog}
	updated, err := service.UpdateEmployeeSkills(context.Background(), created.Employee.ID, EmployeeSkillsUpdateInput{
		ExpectedRevision: created.Employee.Revision,
		Bindings: []employee.SkillBinding{{
			SkillID: items[0].Manifest.SkillID, Version: items[0].Manifest.Version,
			Digest: items[0].Manifest.Digest, Configuration: json.RawMessage(`{}`), Enabled: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Employee.SkillBindings[0].Digest != canonicalDigest {
		t.Fatalf("persisted Digest = %q, want %q", updated.Employee.SkillBindings[0].Digest, canonicalDigest)
	}

	reopenedStore, err := employeestore.NewStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopenedStore.LoadRevision(created.Employee.ID, updated.Employee.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Employee.SkillBindings) != 1 ||
		snapshot.Employee.SkillBindings[0].Digest != canonicalDigest {
		t.Fatalf("reopened Snapshot Digest = %#v", snapshot.Employee.SkillBindings)
	}
	reopenedCatalog, err := skill.NewCatalog(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	reopenedService := &Service{Workspace: service.Workspace, employees: reopenedStore, skills: reopenedCatalog}
	status, err := reopenedService.EmployeeSkills(context.Background(), created.Employee.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Bindings) != 1 || status.Bindings[0].Status != "current" ||
		status.Bindings[0].Binding.Digest != canonicalDigest {
		t.Fatalf("reopened status = %#v", status)
	}
}

func TestEmployeeSkillBindingFailuresAreClassified(t *testing.T) {
	root := t.TempDir()
	writeControlPlaneAdapter(t, root, "review", "Review", "Review carefully.")
	catalog, _ := skill.NewCatalog(root)
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	service := &Service{Workspace: t.TempDir(), employees: store, skills: catalog}
	created, _ := store.Create(controlPlaneDraft("employee-a"), nil)
	items, _ := service.ListSkills(context.Background())
	valid := employee.SkillBinding{SkillID: items[0].SkillID, Version: items[0].Version, Digest: items[0].Digest, Configuration: json.RawMessage(`{}`), Enabled: true}
	tests := []struct {
		name     string
		revision int
		binding  employee.SkillBinding
		kind     Kind
	}{
		{"stale revision", created.Employee.Revision + 1, valid, KindConflict},
		{"missing skill", created.Employee.Revision, employee.SkillBinding{SkillID: "missing", Version: "1", Digest: valid.Digest, Configuration: json.RawMessage(`{}`)}, KindInvalid},
		{"digest drift", created.Employee.Revision, employee.SkillBinding{SkillID: valid.SkillID, Version: valid.Version, Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Configuration: json.RawMessage(`{}`)}, KindInvalid},
		{"secret config", created.Employee.Revision, employee.SkillBinding{SkillID: valid.SkillID, Version: valid.Version, Digest: valid.Digest, Configuration: json.RawMessage(`{"password":"value"}`)}, KindInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.UpdateEmployeeSkills(context.Background(), created.Employee.ID, EmployeeSkillsUpdateInput{
				ExpectedRevision: test.revision, Bindings: []employee.SkillBinding{test.binding},
			})
			if serviceErrorKind(err) != test.kind {
				t.Fatalf("error = %v, kind = %v", err, serviceErrorKind(err))
			}
		})
	}
	if _, err := service.UpdateEmployeeSkills(context.Background(), "../outside", EmployeeSkillsUpdateInput{ExpectedRevision: 1}); serviceErrorKind(err) != KindInvalid {
		t.Fatalf("invalid employee id = %v", err)
	}

	disabled, _ := store.Disable(created.Employee.ID, created.Employee.Revision)
	_, err := service.UpdateEmployeeSkills(context.Background(), created.Employee.ID, EmployeeSkillsUpdateInput{ExpectedRevision: disabled.Employee.Revision, Bindings: []employee.SkillBinding{valid}})
	if serviceErrorKind(err) != KindConflict {
		t.Fatalf("disabled Employee error = %v", err)
	}
}

func TestEmployeeSkillsReportsDigestDriftAndCatalogCorruption(t *testing.T) {
	root := t.TempDir()
	writeControlPlaneAdapter(t, root, "review", "Review", "Review carefully.")
	catalog, _ := skill.NewCatalog(root)
	store, _ := employeestore.NewStore(filepath.Join(t.TempDir(), "employees"))
	service := &Service{Workspace: t.TempDir(), employees: store, skills: catalog}
	draft := controlPlaneDraft("employee-a")
	items, _ := service.ListSkills(context.Background())
	draft.SkillBindings = []employee.SkillBinding{{
		SkillID: items[0].SkillID, Version: items[0].Version,
		Digest:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Configuration: json.RawMessage(`{}`), Enabled: true,
	}}
	if _, err := store.Create(draft, nil); err != nil {
		t.Fatal(err)
	}
	status, err := service.EmployeeSkills(context.Background(), draft.ID)
	if err != nil || status.Bindings[0].Status != "digest_drift" {
		t.Fatalf("drift = %#v, %v", status, err)
	}
	if err := os.WriteFile(filepath.Join(root, "review", "SKILL.md"), []byte("---\nname: Broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListSkills(context.Background()); serviceErrorKind(err) != KindInternal {
		t.Fatalf("corrupt catalog = %v", err)
	}
}

func writeControlPlaneAdapter(t *testing.T, root, id, name, description string) {
	t.Helper()
	directory := filepath.Join(root, id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# Instructions\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeControlPlaneNativeUpperDigest(t *testing.T, root, id, version string) string {
	t.Helper()
	base := filepath.Join(root, id, version)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"SKILL.md": "# Native\n"}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(base, filepath.FromSlash(path)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := skill.Manifest{
		SchemaVersion: 1, SkillID: id, Version: version, Title: "Native",
		Description: "Native Skill", RequestedCapabilities: []string{"read"},
		ConfigurationSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		ContentFiles:        []string{"SKILL.md"}, DigestAlgorithm: "sha256",
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, _ = hash.Write(raw)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(files[path]))
	}
	canonical := hex.EncodeToString(hash.Sum(nil))
	manifest.Digest = strings.ToUpper(canonical)
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "manifest.json"), manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return canonical
}
