package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogEnforcesFileSizeAndEntryLimits(t *testing.T) {
	t.Run("oversized manifest", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "skill", "1.0.0")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "manifest.json"), []byte(strings.Repeat("x", maxManifestBytes+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("oversized manifest must fail closed")
		}
	})
	t.Run("oversized adapter", func(t *testing.T) {
		root := t.TempDir()
		writeAdapterFixture(t, root, "adapter", "---\nname: A\ndescription: D\n---\n"+strings.Repeat("x", maxSkillBytes), nil)
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("oversized SKILL.md must fail closed")
		}
	})
	t.Run("oversized references", func(t *testing.T) {
		root := t.TempDir()
		writeAdapterFixture(t, root, "adapter", "---\nname: A\ndescription: D\n---\nInstructions", map[string]string{
			"one.md": strings.Repeat("x", maxReferencesBytes),
			"two.md": "x",
		})
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("oversized references must fail closed")
		}
	})
	t.Run("entry limit", func(t *testing.T) {
		root := t.TempDir()
		for index := 0; index < maxCatalogEntries+1; index++ {
			id := "skill-" + leftPad(index, 3)
			writeAdapterFixture(t, root, id, "---\nname: A\ndescription: D\n---\nInstructions", nil)
		}
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("catalog entry limit must fail closed")
		}
	})
}

func TestCatalogRejectsSymlinkAndNonRegularFiles(t *testing.T) {
	tests := []struct {
		name  string
		link  func(root, outside string) string
		setup func(t *testing.T, root string)
	}{
		{
			name: "manifest symlink",
			setup: func(t *testing.T, root string) {
				writeNativeFixture(t, root, "skill", baseManifest(), map[string]string{"SKILL.md": "Instructions\n"})
			},
			link: func(root, outside string) string {
				return filepath.Join(root, "skill", "1.0.0", "manifest.json")
			},
		},
		{
			name: "native content symlink",
			setup: func(t *testing.T, root string) {
				writeNativeFixture(t, root, "skill", baseManifest(), map[string]string{"SKILL.md": "Instructions\n"})
			},
			link: func(root, outside string) string {
				return filepath.Join(root, "skill", "1.0.0", "SKILL.md")
			},
		},
		{
			name: "adapter references directory symlink",
			setup: func(t *testing.T, root string) {
				writeAdapterFixture(t, root, "adapter", "---\nname: A\ndescription: D\n---\nInstructions", nil)
			},
			link: func(root, outside string) string {
				return filepath.Join(root, "adapter", "references")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(t, root)
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := test.link(root, outside)
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}
			catalog, _ := NewCatalog(root)
			if _, err := catalog.List(); err == nil {
				t.Fatal("symlink must fail closed")
			}
		})
	}
	t.Run("non-regular SKILL.md", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "adapter", "SKILL.md")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("non-regular SKILL.md must fail closed")
		}
	})
}

func TestNativeManifestRejectsURLAndEncodedPaths(t *testing.T) {
	t.Run("url", func(t *testing.T) {
		root := t.TempDir()
		writeNativeFixture(t, root, "skill", baseManifest(), map[string]string{"SKILL.md": "Instructions\n"})
		rewriteManifestContent(t, root, []string{"SKILL.md", "https://example.test/instructions"})
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("URL content path must fail closed")
		}
	})
	t.Run("existing encoded path with valid digest", func(t *testing.T) {
		root := t.TempDir()
		const encoded = "references/%2e%2e%2foutside"
		files := map[string]string{"SKILL.md": "Instructions\n", encoded: "literal encoded file\n"}
		manifest := baseManifest()
		manifest.ContentFiles = []string{"SKILL.md", encoded}
		writeNativeFixture(t, root, "skill", manifest, files)
		if _, err := os.Stat(filepath.Join(root, "skill", "1.0.0", "references", "%2e%2e%2foutside")); err != nil {
			t.Fatalf("encoded fixture was not created: %v", err)
		}
		catalog, _ := NewCatalog(root)
		if _, err := catalog.List(); err == nil {
			t.Fatal("existing encoded path with a valid Digest must fail closed")
		}
	})
}

func TestCatalogRejectsExecutableContentWithoutExecutingIt(t *testing.T) {
	for _, kind := range []string{"native", "adapter"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(t.TempDir(), "executed")
			script := "#!/bin/sh\ntouch " + marker + "\n"
			if kind == "native" {
				manifest := baseManifest()
				manifest.ContentFiles = []string{"SKILL.md", "references/install.sh"}
				writeNativeFixture(t, root, "skill", manifest, map[string]string{
					"SKILL.md": "Instructions\n", "references/install.sh": script,
				})
				if err := os.Chmod(filepath.Join(root, "skill", "1.0.0", "references", "install.sh"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				writeAdapterFixture(t, root, "adapter", "---\nname: A\ndescription: D\n---\nInstructions", map[string]string{"install.sh": script})
				if err := os.Chmod(filepath.Join(root, "adapter", "references", "install.sh"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			catalog, _ := NewCatalog(root)
			if _, err := catalog.List(); err == nil {
				t.Fatal("executable Skill content must fail closed")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("executable Skill content created its marker")
			}
		})
	}
}

func TestCatalogLoadsNonExecutableReadOnlyReference(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "adapter", "---\nname: A\ndescription: D\n---\nInstructions", map[string]string{"guide.md": "Read only\n"})
	path := filepath.Join(root, "adapter", "references", "guide.md")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	catalog, _ := NewCatalog(root)
	items, err := catalog.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].References["references/guide.md"] != "Read only\n" {
		t.Fatalf("read-only reference was not loaded: %#v", items)
	}
}

func leftPad(value, width int) string {
	text := strings.Repeat("0", width) + strconvItoa(value)
	return text[len(text)-width:]
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
