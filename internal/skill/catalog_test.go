package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestCatalogDiscoversNativeAndAdapterDeterministically(t *testing.T) {
	root := t.TempDir()
	writeNativeFixture(t, root, "native", Manifest{
		SchemaVersion: 1, SkillID: "native", Version: "1.0.0", Title: "Native",
		Description: "Native skill", RequestedCapabilities: []string{"read"},
		ConfigurationSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		ContentFiles:        []string{"SKILL.md", "references/guide.md"}, DigestAlgorithm: "sha256",
	}, map[string]string{"SKILL.md": "# Native\n", "references/guide.md": "Guide\n"})
	writeAdapterFixture(t, root, "adapter", "---\nname: Adapter\ndescription: Adapter skill\n---\n# Instructions\n", map[string]string{"guide.md": "Reference\n"})

	catalog, err := NewCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := catalog.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Manifest.SkillID != "adapter" || items[1].Manifest.SkillID != "native" {
		t.Fatalf("catalog = %#v", items)
	}
	adapter := items[0]
	if adapter.Kind != KindAdapter || len(adapter.Manifest.RequestedCapabilities) != 0 ||
		adapter.Manifest.Version == "" || adapter.Manifest.Digest == "" {
		t.Fatalf("adapter projection = %#v", adapter)
	}
	reopened, _ := NewCatalog(root)
	again, err := reopened.List()
	if err != nil || again[0].Manifest.Version != adapter.Manifest.Version || again[0].Manifest.Digest != adapter.Manifest.Digest {
		t.Fatalf("adapter digest is not deterministic: %#v, %v", again, err)
	}
	if err := os.WriteFile(filepath.Join(root, "adapter", "SKILL.md"), []byte("---\nname: Adapter\ndescription: Adapter skill\n---\nChanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := reopened.List()
	if err != nil || changed[0].Manifest.Digest == adapter.Manifest.Digest {
		t.Fatalf("content change did not change digest: %#v, %v", changed, err)
	}
}

func TestCatalogDoesNotScanUnconfiguredLocationsOrExecuteFiles(t *testing.T) {
	t.Setenv("GOHERMIT_SKILL_CATALOG", "")
	catalog, err := NewCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	items, err := catalog.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("disabled catalog = %#v, %v", items, err)
	}
	root := t.TempDir()
	writeAdapterFixture(t, root, "adapter", "---\nname: Adapter\ndescription: Safe\n---\nInstructions\n", nil)
	if err := os.MkdirAll(filepath.Join(root, "adapter", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "executed")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(root, "adapter", "scripts", "install.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "adapter", "package.json"), []byte(`{"scripts":{"install":"touch `+marker+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, _ = NewCatalog(root)
	if _, err := catalog.List(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("adapter script or dependency hook executed")
	}
}

func TestNativeManifestFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"unknown field", func(t *testing.T, root string) {
			path := filepath.Join(root, "skill", "1.0.0", "manifest.json")
			var value map[string]any
			_ = json.Unmarshal(mustReadSkill(t, path), &value)
			value["unknown"] = true
			writeJSONSkill(t, path, value)
		}},
		{"unknown schema", func(t *testing.T, root string) {
			path := filepath.Join(root, "skill", "1.0.0", "manifest.json")
			var value map[string]any
			_ = json.Unmarshal(mustReadSkill(t, path), &value)
			value["schema_version"] = 99
			writeJSONSkill(t, path, value)
		}},
		{"digest drift", func(t *testing.T, root string) {
			_ = os.WriteFile(filepath.Join(root, "skill", "1.0.0", "SKILL.md"), []byte("changed"), 0o600)
		}},
		{"missing content", func(t *testing.T, root string) {
			_ = os.Remove(filepath.Join(root, "skill", "1.0.0", "SKILL.md"))
		}},
		{"absolute content", func(t *testing.T, root string) {
			rewriteManifestContent(t, root, []string{filepath.Join(t.TempDir(), "outside")})
		}},
		{"traversal content", func(t *testing.T, root string) {
			rewriteManifestContent(t, root, []string{"../outside"})
		}},
		{"credential path", func(t *testing.T, root string) {
			rewriteManifestContent(t, root, []string{"references/api_" + "key.txt"})
		}},
		{"install hook path", func(t *testing.T, root string) {
			rewriteManifestContent(t, root, []string{"scripts/install.sh"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeNativeFixture(t, root, "skill", baseManifest(), map[string]string{"SKILL.md": "Instructions\n"})
			test.mutate(t, root)
			catalog, _ := NewCatalog(root)
			if _, err := catalog.List(); err == nil {
				t.Fatal("invalid native manifest must fail closed")
			}
		})
	}
}

func TestCatalogRejectsDuplicateAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	manifest := baseManifest()
	manifest.RequestedCapabilities = []string{"read", "read"}
	writeNativeFixture(t, root, "skill", manifest, map[string]string{"SKILL.md": "Instructions\n"})
	catalog, _ := NewCatalog(root)
	if _, err := catalog.List(); err == nil {
		t.Fatal("duplicate capabilities must fail closed")
	}

	root = t.TempDir()
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "adapter")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "SKILL.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	catalog, _ = NewCatalog(root)
	if _, err := catalog.List(); err == nil {
		t.Fatal("adapter symlink must fail closed")
	}
}

func TestAdapterFrontmatterFailsClosed(t *testing.T) {
	for name, content := range map[string]string{
		"missing":      "# no frontmatter\n",
		"broken":       "---\nname: Adapter\n",
		"duplicate":    "---\nname: A\nname: B\ndescription: D\n---\n",
		"unknown":      "---\nname: A\ndescription: D\nscript: install.sh\n---\n",
		"missing desc": "---\nname: A\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeAdapterFixture(t, root, "adapter", content, nil)
			catalog, _ := NewCatalog(root)
			if _, err := catalog.List(); err == nil {
				t.Fatal("invalid frontmatter must fail closed")
			}
		})
	}
}

func baseManifest() Manifest {
	return Manifest{
		SchemaVersion: 1, SkillID: "skill", Version: "1.0.0", Title: "Skill",
		Description: "Description", RequestedCapabilities: []string{"read"},
		ConfigurationSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		ContentFiles:        []string{"SKILL.md"}, DigestAlgorithm: "sha256",
	}
}

func writeNativeFixture(t *testing.T, root, directory string, manifest Manifest, files map[string]string) {
	t.Helper()
	base := filepath.Join(root, directory, manifest.Version)
	for path, content := range files {
		full := filepath.Join(base, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest.Digest = fixtureDigest(manifest, files)
	writeJSONSkill(t, filepath.Join(base, "manifest.json"), manifest)
}

func writeAdapterFixture(t *testing.T, root, directory, content string, references map[string]string) {
	t.Helper()
	base := filepath.Join(root, directory)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, value := range references {
		full := filepath.Join(base, "references", filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func fixtureDigest(manifest Manifest, files map[string]string) string {
	manifest.Digest = ""
	raw, _ := json.Marshal(manifest)
	hash := sha256.New()
	hash.Write(raw)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		hash.Write([]byte{0})
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write([]byte(files[path]))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func rewriteManifestContent(t *testing.T, root string, content []string) {
	t.Helper()
	path := filepath.Join(root, "skill", "1.0.0", "manifest.json")
	var manifest Manifest
	if err := json.Unmarshal(mustReadSkill(t, path), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ContentFiles = content
	manifest.Digest = fixtureDigest(manifest, map[string]string{})
	writeJSONSkill(t, path, manifest)
}

func mustReadSkill(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeJSONSkill(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
