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
	for name, path := range map[string]string{
		"url":     "https://example.test/instructions",
		"encoded": "references/%2e%2e%2foutside",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeNativeFixture(t, root, "skill", baseManifest(), map[string]string{"SKILL.md": "Instructions\n"})
			rewriteManifestContent(t, root, []string{"SKILL.md", path})
			catalog, _ := NewCatalog(root)
			if _, err := catalog.List(); err == nil {
				t.Fatal("unsafe content path must fail closed")
			}
		})
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
