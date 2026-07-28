package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeterministicIndexStableCitationAndContentChange(t *testing.T) {
	catalog, err := NewCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "handbook", EmployeeID: "employee-a", Kind: KindManualText, Title: "Handbook", ManualText: "# Rules\n\nPrefer deterministic systems."}
	firstSource, first, err := catalog.Index(source)
	if err != nil {
		t.Fatal(err)
	}
	secondSource, second, err := catalog.Index(source)
	if err != nil {
		t.Fatal(err)
	}
	if firstSource.Digest != secondSource.Digest || first.Documents[0].Citations[0].ID != second.Documents[0].Citations[0].ID {
		t.Fatal("identical content did not produce stable digest and Citation")
	}
	source.ManualText += "\nNew content."
	changedSource, changed, err := catalog.Index(source)
	if err != nil {
		t.Fatal(err)
	}
	if changedSource.Digest == firstSource.Digest || changed.Documents[0].Citations[0].ID == first.Documents[0].Citations[0].ID {
		t.Fatal("content change did not change digest and Citation")
	}
	results, err := Search([]Source{firstSource}, []Index{first}, "deterministic", 10)
	if err != nil || len(results) != 1 || results[0].Citation.ID != first.Documents[0].Citations[0].ID {
		t.Fatalf("search = %#v, %v", results, err)
	}
}

func TestLocalKnowledgeRejectsTraversalEncodedSymlinkAndExecutable(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "references", name), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("%2e%2e%2foutside", "literal encoded filename", 0o600)
	write("install.sh", "#!/bin/sh\necho marker > marker", 0o700)
	write("notes.md", "ordinary read-only reference", 0o400)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "references", "link.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	catalog, err := NewCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, relative := range map[string]string{
		"traversal":  "../outside",
		"encoded":    "references/%2e%2e%2foutside",
		"symlink":    "references/link.md",
		"executable": "references/install.sh",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := catalog.Index(Source{ID: name, EmployeeID: "employee-a", Kind: KindFile, Title: name, RelativePath: relative})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(root, "marker")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("executable content ran")
			}
		})
	}
	if _, _, err := catalog.Index(Source{ID: "notes", EmployeeID: "employee-a", Kind: KindFile, Title: "Notes", RelativePath: "references/notes.md"}); err != nil {
		t.Fatalf("ordinary reference rejected: %v", err)
	}
}

func TestKnowledgeCapacityAndCorruptIndexFailClosed(t *testing.T) {
	catalog, _ := NewCatalog("")
	oversized := strings.Repeat("x", MaxManualTextBytes+1)
	if _, _, err := catalog.Index(Source{ID: "large", EmployeeID: "employee-a", Kind: KindManualText, Title: "Large", ManualText: oversized}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized error = %v", err)
	}
	source, index, err := catalog.Index(Source{ID: "valid", EmployeeID: "employee-a", Kind: KindManualText, Title: "Valid", ManualText: "bounded"})
	if err != nil {
		t.Fatal(err)
	}
	index.SourceDigest = strings.Repeat("0", 64)
	if err := ValidateIndex(index, source); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt index error = %v", err)
	}
}

func TestKnowledgeDirectoryFileCountIsBounded(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "docs")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= MaxFilesPerSource; index++ {
		name := filepath.Join(directory, fmt.Sprintf("doc-%03d.md", index))
		// Include the index in content to avoid relying on duplicate bytes.
		if err := os.WriteFile(name, []byte(fmt.Sprintf("bounded document %d", index)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := NewCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = catalog.Index(Source{ID: "docs", EmployeeID: "employee-a", Kind: KindDirectory, Title: "Docs", RelativePath: "docs"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("file count error = %v", err)
	}
}

func TestKnowledgeDirectoryIndexAndSearchOrdering(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"a.md":  "# Alpha\nDeterministic local index.",
		"b.txt": "Deterministic citations.",
	} {
		if err := os.WriteFile(filepath.Join(root, "docs", name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := NewCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	source, index, err := catalog.Index(Source{ID: "docs", EmployeeID: "employee-a", Kind: KindDirectory, Title: "Docs", RelativePath: "docs"})
	if err != nil || len(index.Documents) != 2 || index.Documents[0].Path != "docs/a.md" {
		t.Fatalf("index = %#v, %v", index, err)
	}
	results, err := Search([]Source{source}, []Index{index}, "deterministic", 2)
	if err != nil || len(results) != 2 || results[0].Citation.ID > results[1].Citation.ID {
		t.Fatalf("results = %#v, %v", results, err)
	}
	if _, err := Search([]Source{source}, []Index{index}, "query", MaxSearchResults+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid limit = %v", err)
	}
}

func TestKnowledgeRejectsUnconfiguredLocalAndNoncanonicalPersistedSource(t *testing.T) {
	catalog, _ := NewCatalog("")
	if _, _, err := catalog.Index(Source{ID: "file", EmployeeID: "employee-a", Kind: KindFile, Title: "File", RelativePath: "doc.md"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unconfigured root error = %v", err)
	}
	source := Source{
		SchemaVersion: SchemaVersion, ID: "source", EmployeeID: "employee-a", Kind: KindManualText,
		Title: "Source", ManualText: "bounded", Digest: strings.Repeat("A", 64), Status: StatusReady,
	}
	if err := ValidateSource(source, true); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("noncanonical digest = %v", err)
	}
}

func TestKnowledgeRejectsSymlinkRootAndUnsupportedKind(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "knowledge-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := NewCatalog(link); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink root error = %v", err)
	}
	catalog, _ := NewCatalog("")
	if _, _, err := catalog.Index(Source{ID: "bad", EmployeeID: "employee-a", Kind: Kind("remote"), Title: "Bad"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported kind error = %v", err)
	}
}
