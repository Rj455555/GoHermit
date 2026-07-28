package knowledge

import (
	"encoding/json"
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
	crlf := source
	crlf.ManualText = strings.ReplaceAll(source.ManualText, "\n", "\r\n")
	crlfSource, crlfIndex, err := catalog.Index(crlf)
	if err != nil {
		t.Fatal(err)
	}
	if crlfSource.Digest != firstSource.Digest || crlfIndex.Documents[0].Digest != first.Documents[0].Digest {
		t.Fatal("canonical line endings did not produce stable Digests")
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

func TestValidateSourceRejectsUnsafePersistedTextAndStaticPaths(t *testing.T) {
	valid := Source{
		SchemaVersion: SchemaVersion, ID: "source", EmployeeID: "employee-a",
		Kind: KindManualText, Title: "Title", ManualText: "bounded content",
		Digest: strings.Repeat("a", 64), Status: StatusReady,
	}
	textCases := map[string]func(*Source){
		"title NUL":          func(value *Source) { value.Title = "bad\x00title" },
		"title invalid UTF8": func(value *Source) { value.Title = string([]byte{0xff}) },
		"title secret":       func(value *Source) { value.Title = "authorization: bearer hidden-value" },
		"title private":      func(value *Source) { value.Title = "Hidden system prompt: internal" },
		"manual NUL":         func(value *Source) { value.ManualText = "bad\x00manual" },
		"manual invalid UTF8": func(value *Source) {
			value.ManualText = string([]byte{0xff})
		},
		"manual private": func(value *Source) { value.ManualText = "raw tool arguments: hidden" },
	}
	for name, mutate := range textCases {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if err := ValidateSource(value, true); err == nil {
				t.Fatal("unsafe persisted text was accepted")
			}
		})
	}

	local := valid
	local.Kind, local.ManualText, local.RelativePath = KindFile, "", "docs/guide.md"
	pathCases := map[string]string{
		"absolute":        "/tmp/guide.md",
		"URL":             "https://example.test/guide.md",
		"network host":    "//example.test/guide.md",
		"encoded":         "docs/%2e%2e%2fguide.md",
		"backslash":       `docs\guide.md`,
		"newline":         "docs/\nguide.md",
		"traversal":       "../guide.md",
		"empty component": "docs//guide.md",
		"dot component":   "docs/./guide.md",
		"forbidden":       ".git/guide.md",
		"not clean":       "docs/sub/../guide.md",
		"oversized":       strings.Repeat("a", MaxPathBytes+1),
	}
	for name, path := range pathCases {
		t.Run(name, func(t *testing.T) {
			value := local
			value.RelativePath = path
			if err := ValidateSource(value, true); err == nil {
				t.Fatalf("unsafe persisted path %q was accepted", path)
			}
		})
	}
}

func TestValidateIndexRejectsCanonicalIntegrityTampering(t *testing.T) {
	catalog, _ := NewCatalog("")
	source, index, err := catalog.Index(Source{
		ID: "source", EmployeeID: "employee-a", Kind: KindManualText, Title: "Title",
		ManualText: "# Heading\n\nDeterministic bounded content.",
	})
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*Index){
		"snippet": func(value *Index) {
			value.Documents[0].Citations[0].Snippet += " tampered"
		},
		"terms": func(value *Index) {
			value.Documents[0].Terms = append(value.Documents[0].Terms, "tampered")
		},
		"duplicate terms": func(value *Index) {
			value.Documents[0].Terms = append(value.Documents[0].Terms, value.Documents[0].Terms[len(value.Documents[0].Terms)-1])
		},
		"path": func(value *Index) {
			value.Documents[0].Path = "changed.md"
			value.Documents[0].Citations[0].Path = "changed.md"
		},
		"document traversal": func(value *Index) {
			value.Documents[0].Path = "../changed.md"
			value.Documents[0].Citations[0].Path = "../changed.md"
		},
		"document digest": func(value *Index) {
			value.Documents[0].Digest = strings.Repeat("b", 64)
			value.Documents[0].Citations[0].Digest = strings.Repeat("b", 64)
		},
		"citation metadata": func(value *Index) {
			value.Documents[0].Citations[0].EndLine++
		},
		"duplicate document": func(value *Index) {
			value.Documents = append(value.Documents, value.Documents[0])
		},
		"empty documents": func(value *Index) {
			value.Documents = nil
		},
		"oversized documents": func(value *Index) {
			document := value.Documents[0]
			value.Documents = make([]Document, MaxFilesPerSource+1)
			for i := range value.Documents {
				value.Documents[i] = document
				value.Documents[i].Path = fmt.Sprintf("manual-%03d", i)
				value.Documents[i].Citations[0].Path = value.Documents[i].Path
			}
		},
		"duplicate citation": func(value *Index) {
			value.Documents[0].Citations = append(value.Documents[0].Citations, value.Documents[0].Citations[0])
		},
		"citation order": func(value *Index) {
			first := value.Documents[0].Citations[0]
			second := first
			second.StartLine, second.EndLine = first.EndLine+1, first.EndLine+1
			second.ID = citationID(source.ID, first.Path, second.StartLine, second.EndLine, first.Digest)
			value.Documents[0].Citations = []Citation{second, first}
		},
		"empty citations": func(value *Index) {
			value.Documents[0].Citations = nil
		},
		"oversized citations": func(value *Index) {
			citation := value.Documents[0].Citations[0]
			value.Documents[0].Citations = make([]Citation, MaxCitations+1)
			for i := range value.Documents[0].Citations {
				value.Documents[0].Citations[i] = citation
				value.Documents[0].Citations[i].ID = fmt.Sprintf("cite-%024d", i)
				value.Documents[0].Citations[i].StartLine = i + 1
				value.Documents[0].Citations[i].EndLine = i + 1
			}
		},
		"document order": func(value *Index) {
			second := value.Documents[0]
			second.Path = "aaa"
			second.Citations[0].Path = second.Path
			value.Documents = append(value.Documents, second)
		},
		"term order": func(value *Index) {
			value.Documents[0].Terms = []string{"zeta", "alpha"}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneIndex(t, index)
			mutate(&value)
			if err := ValidateIndex(value, source); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("tampered Index error = %v", err)
			}
		})
	}
}

func TestValidateIndexRejectsUnsafeCitationTextWithRecomputedDigest(t *testing.T) {
	catalog, _ := NewCatalog("")
	source, index, err := catalog.Index(Source{
		ID: "source", EmployeeID: "employee-a", Kind: KindManualText, Title: "Title",
		ManualText: "# Heading\n\nDeterministic bounded content.",
	})
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*Citation){
		"heading NUL":          func(value *Citation) { value.Heading = "bad\x00heading" },
		"heading invalid UTF8": func(value *Citation) { value.Heading = string([]byte{0xff}) },
		"heading secret":       func(value *Citation) { value.Heading = "authorization: bearer hidden-value" },
		"heading private":      func(value *Citation) { value.Heading = "hidden system prompt: internal" },
		"snippet NUL":          func(value *Citation) { value.Snippet = "bad\x00snippet" },
		"snippet invalid UTF8": func(value *Citation) { value.Snippet = string([]byte{0xff}) },
		"snippet secret":       func(value *Citation) { value.Snippet = "api_key=abcdefghijklmnopqrstuvwxyz123456" },
		"snippet private":      func(value *Citation) { value.Snippet = "raw tool arguments: hidden" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			sourceCopy, indexCopy := source, cloneIndex(t, index)
			mutate(&indexCopy.Documents[0].Citations[0])
			recomputed := sourceIndexDigest(sourceCopy, indexCopy.Documents)
			sourceCopy.Digest, indexCopy.SourceDigest = recomputed, recomputed
			if err := ValidateIndex(indexCopy, sourceCopy); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("unsafe Citation text error = %v", err)
			}
		})
	}
}

func cloneIndex(t *testing.T, value Index) Index {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone Index
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
