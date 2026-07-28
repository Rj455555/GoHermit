// Package knowledge provides deterministic, local-only Knowledge indexing.
// It never scans implicit locations, performs network access, or executes files.
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	SchemaVersion      = 1
	MaxSources         = 128
	MaxSourceBytes     = 256 << 10
	MaxManualTextBytes = 64 << 10
	MaxAggregateBytes  = 8 << 20
	MaxIndexBytes      = 2 << 20
	MaxFilesPerSource  = 256
	MaxCitations       = 1024
	MaxCitationBytes   = 16 << 10
	MaxSearchResults   = 32
	MaxSearchBytes     = 128 << 10
	MaxPathBytes       = 4 << 10
	maxDirectoryDepth  = 8
)

var (
	ErrInvalid = errors.New("invalid Knowledge data")
	ErrCorrupt = errors.New("Knowledge data is corrupt")
	ErrMissing = errors.New("Knowledge source is missing")
)

type Kind string

const (
	KindManualText Kind = "manual_text"
	KindFile       Kind = "file"
	KindDirectory  Kind = "directory"
	KindProjectDoc Kind = "project_docs"
)

type Status string

const (
	StatusReady  Status = "ready"
	StatusStale  Status = "stale"
	StatusFailed Status = "failed"
)

type Source struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	EmployeeID    string `json:"employee_id"`
	Kind          Kind   `json:"kind"`
	Title         string `json:"title"`
	RelativePath  string `json:"relative_path,omitempty"`
	ManualText    string `json:"manual_text,omitempty"`
	Digest        string `json:"digest"`
	Status        Status `json:"status"`
	Error         string `json:"error,omitempty"`
}

type Citation struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	EmployeeID    string `json:"employee_id"`
	SourceID      string `json:"source_id"`
	Path          string `json:"path"`
	Heading       string `json:"heading,omitempty"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	Digest        string `json:"digest"`
	Snippet       string `json:"snippet"`
}

type Document struct {
	Path      string     `json:"path"`
	Digest    string     `json:"digest"`
	Terms     []string   `json:"terms"`
	Citations []Citation `json:"citations"`
}

type Index struct {
	SchemaVersion int        `json:"schema_version"`
	EmployeeID    string     `json:"employee_id"`
	SourceID      string     `json:"source_id"`
	SourceDigest  string     `json:"source_digest"`
	Documents     []Document `json:"documents"`
}

type Result struct {
	SourceID string   `json:"source_id"`
	Title    string   `json:"title"`
	Score    int      `json:"score"`
	Citation Citation `json:"citation"`
}

type Catalog struct{ root string }

func NewCatalog(root string) (*Catalog, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("GOHERMIT_KNOWLEDGE_ROOT"))
	}
	if root == "" {
		return &Catalog{}, nil
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve root: %v", ErrInvalid, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("%w: configured root: %v", ErrInvalid, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%w: configured root must be a non-symlink directory", ErrInvalid)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve root: %v", ErrInvalid, err)
	}
	return &Catalog{root: filepath.Clean(real)}, nil
}

func (c *Catalog) Index(source Source) (Source, Index, error) {
	source.SchemaVersion = SchemaVersion
	source.Status = StatusReady
	source.Error = ""
	if err := ValidateSource(source, false); err != nil {
		return Source{}, Index{}, err
	}
	var documents []Document
	var err error
	switch source.Kind {
	case KindManualText:
		documents, err = indexDocuments(source, []rawDocument{{path: "manual", content: source.ManualText}})
	case KindFile:
		documents, err = c.readAndIndex(source, false)
	case KindDirectory, KindProjectDoc:
		documents, err = c.readAndIndex(source, true)
	default:
		err = fmt.Errorf("%w: unsupported source kind", ErrInvalid)
	}
	if err != nil {
		return Source{}, Index{}, err
	}
	digestHash := sha256.New()
	for _, document := range documents {
		_, _ = digestHash.Write([]byte(document.Path))
		_, _ = digestHash.Write([]byte{0})
		_, _ = digestHash.Write([]byte(document.Digest))
	}
	source.Digest = hex.EncodeToString(digestHash.Sum(nil))
	index := Index{SchemaVersion: SchemaVersion, EmployeeID: source.EmployeeID, SourceID: source.ID, SourceDigest: source.Digest, Documents: documents}
	if err := ValidateIndex(index, source); err != nil {
		return Source{}, Index{}, err
	}
	raw, err := json.Marshal(index)
	if err != nil || len(raw) > MaxIndexBytes {
		return Source{}, Index{}, fmt.Errorf("%w: Knowledge index exceeds size limit", ErrInvalid)
	}
	return source, index, nil
}

func (c *Catalog) readAndIndex(source Source, directory bool) ([]Document, error) {
	if c.root == "" {
		return nil, fmt.Errorf("%w: local Knowledge root is not configured", ErrInvalid)
	}
	path, err := c.safePath(source.RelativePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMissing, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: source is a symlink", ErrInvalid)
	}
	if directory != info.IsDir() {
		return nil, fmt.Errorf("%w: source kind and path type differ", ErrInvalid)
	}
	var raw []rawDocument
	if !directory {
		item, err := c.readFile(path, source.RelativePath)
		if err != nil {
			return nil, err
		}
		raw = append(raw, item)
	} else {
		err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, relErr := filepath.Rel(c.root, current)
			if relErr != nil {
				return relErr
			}
			if relative == "." {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: Knowledge path contains a symlink", ErrInvalid)
			}
			if entry.IsDir() {
				depth := len(strings.Split(filepath.ToSlash(relative), "/"))
				if depth > maxDirectoryDepth {
					return fmt.Errorf("%w: Knowledge directory depth exceeds limit", ErrInvalid)
				}
				if forbiddenPathComponent(entry.Name()) {
					return fmt.Errorf("%w: forbidden Knowledge directory", ErrInvalid)
				}
				return nil
			}
			if len(raw) >= MaxFilesPerSource {
				return fmt.Errorf("%w: Knowledge file count exceeds limit", ErrInvalid)
			}
			item, readErr := c.readFile(current, filepath.ToSlash(relative))
			if readErr != nil {
				return readErr
			}
			raw = append(raw, item)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].path < raw[j].path })
	return indexDocuments(source, raw)
}

func (c *Catalog) safePath(relative string) (string, error) {
	if relative == "" || len(relative) > MaxPathBytes || strings.Contains(relative, "%") ||
		strings.ContainsAny(relative, "\\\r\n") || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: Knowledge path is invalid", ErrInvalid)
	}
	if parsed, err := url.Parse(relative); err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "", fmt.Errorf("%w: remote or ambiguous Knowledge path", ErrInvalid)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: Knowledge path escapes root", ErrInvalid)
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "" || part == "." || part == ".." || forbiddenPathComponent(part) {
			return "", fmt.Errorf("%w: forbidden Knowledge path", ErrInvalid)
		}
	}
	path := filepath.Join(c.root, clean)
	rel, err := filepath.Rel(c.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: Knowledge path escapes root", ErrInvalid)
	}
	current := c.root
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", fmt.Errorf("%w: %v", ErrMissing, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: Knowledge path contains a symlink", ErrInvalid)
		}
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolve Knowledge path: %v", ErrMissing, err)
	}
	realRelative, err := filepath.Rel(c.root, real)
	if err != nil || realRelative == ".." || strings.HasPrefix(realRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(realRelative) {
		return "", fmt.Errorf("%w: Knowledge real path escapes root", ErrInvalid)
	}
	return path, nil
}

func (c *Catalog) readFile(path, relative string) (rawDocument, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return rawDocument{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return rawDocument{}, fmt.Errorf("%w: Knowledge content must be a regular non-symlink file", ErrInvalid)
	}
	if info.Mode().Perm()&0o111 != 0 {
		return rawDocument{}, fmt.Errorf("%w: executable Knowledge content is forbidden", ErrInvalid)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".rst", ".adoc":
	default:
		return rawDocument{}, fmt.Errorf("%w: unsupported Knowledge content type", ErrInvalid)
	}
	if info.Size() > MaxSourceBytes {
		return rawDocument{}, fmt.Errorf("%w: Knowledge file exceeds size limit", ErrInvalid)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return rawDocument{}, err
	}
	if len(data) > MaxSourceBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return rawDocument{}, fmt.Errorf("%w: Knowledge file is oversized, binary, or invalid UTF-8", ErrInvalid)
	}
	if owner.LooksSecret(string(data)) {
		return rawDocument{}, fmt.Errorf("%w: Knowledge content looks secret", ErrInvalid)
	}
	if containsPrivateRuntimeContext(string(data)) {
		return rawDocument{}, fmt.Errorf("%w: private runtime Knowledge content is forbidden", ErrInvalid)
	}
	return rawDocument{path: filepath.ToSlash(relative), content: string(data)}, nil
}

type rawDocument struct{ path, content string }

func indexDocuments(source Source, raw []rawDocument) ([]Document, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: Knowledge source contains no documents", ErrInvalid)
	}
	total := 0
	citationCount := 0
	documents := make([]Document, 0, len(raw))
	for _, item := range raw {
		total += len(item.content)
		if total > MaxAggregateBytes {
			return nil, fmt.Errorf("%w: Knowledge source aggregate exceeds limit", ErrInvalid)
		}
		if owner.LooksSecret(item.content) {
			return nil, fmt.Errorf("%w: Knowledge content looks secret", ErrInvalid)
		}
		if containsPrivateRuntimeContext(item.content) {
			return nil, fmt.Errorf("%w: private runtime Knowledge content is forbidden", ErrInvalid)
		}
		digest := digestText(item.content)
		citations := makeCitations(source, item.path, item.content, digest)
		citationCount += len(citations)
		if citationCount > MaxCitations {
			return nil, fmt.Errorf("%w: Knowledge citations exceed limit", ErrInvalid)
		}
		documents = append(documents, Document{Path: item.path, Digest: digest, Terms: terms(item.content), Citations: citations})
	}
	return documents, nil
}

func makeCitations(source Source, path, content, digest string) []Citation {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var result []Citation
	for start := 0; start < len(lines); start += 24 {
		end := start + 24
		if end > len(lines) {
			end = len(lines)
		}
		snippet := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if snippet == "" {
			continue
		}
		if len(snippet) > MaxCitationBytes {
			snippet = snippet[:MaxCitationBytes]
			for len(snippet) > 0 && !utf8.ValidString(snippet) {
				snippet = snippet[:len(snippet)-1]
			}
		}
		heading := ""
		for _, line := range lines[start:end] {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				heading = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
				break
			}
		}
		identity := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", source.ID, path, start+1, end, digest)
		result = append(result, Citation{
			SchemaVersion: SchemaVersion, ID: "cite-" + digestText(identity)[:24],
			EmployeeID: source.EmployeeID, SourceID: source.ID, Path: path, Heading: heading,
			StartLine: start + 1, EndLine: end, Digest: digest, Snippet: snippet,
		})
	}
	return result
}

func Search(sources []Source, indexes []Index, query string, limit int) ([]Result, error) {
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > MaxSearchResults {
		return nil, fmt.Errorf("%w: search limit", ErrInvalid)
	}
	queryTerms := terms(query)
	if len(queryTerms) == 0 {
		return []Result{}, nil
	}
	titles := map[string]string{}
	for _, source := range sources {
		titles[source.ID] = source.Title
	}
	var results []Result
	for _, index := range indexes {
		for _, document := range index.Documents {
			for _, citation := range document.Citations {
				haystack := strings.ToLower(titles[index.SourceID] + " " + document.Path + " " + citation.Heading + " " + citation.Snippet)
				score := 0
				for _, term := range queryTerms {
					if strings.Contains(haystack, term) {
						score++
					}
				}
				if score > 0 {
					results = append(results, Result{SourceID: index.SourceID, Title: titles[index.SourceID], Score: score, Citation: citation})
				}
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].SourceID != results[j].SourceID {
			return results[i].SourceID < results[j].SourceID
		}
		return results[i].Citation.ID < results[j].Citation.ID
	})
	bytes := 0
	bounded := make([]Result, 0, min(limit, len(results)))
	for _, result := range results {
		if len(bounded) == limit || bytes+len(result.Citation.Snippet) > MaxSearchBytes {
			break
		}
		bytes += len(result.Citation.Snippet)
		bounded = append(bounded, result)
	}
	return bounded, nil
}

func ValidateSource(source Source, persisted bool) error {
	if source.SchemaVersion != SchemaVersion || !validID(source.ID) || !validID(source.EmployeeID) ||
		strings.TrimSpace(source.Title) == "" || len(source.Title) > 256 {
		return fmt.Errorf("%w: source identity", ErrInvalid)
	}
	if owner.LooksSecret(source.Title + "\n" + source.ManualText + "\n" + source.RelativePath) {
		return fmt.Errorf("%w: source contains secret-like data", ErrInvalid)
	}
	if containsPrivateRuntimeContext(source.ManualText) {
		return fmt.Errorf("%w: source contains private runtime data", ErrInvalid)
	}
	switch source.Kind {
	case KindManualText:
		if source.RelativePath != "" || source.ManualText == "" || len(source.ManualText) > MaxManualTextBytes || !utf8.ValidString(source.ManualText) {
			return fmt.Errorf("%w: manual source", ErrInvalid)
		}
	case KindFile, KindDirectory, KindProjectDoc:
		if source.ManualText != "" || source.RelativePath == "" {
			return fmt.Errorf("%w: local source", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: source kind", ErrInvalid)
	}
	if persisted {
		if !validDigest(source.Digest) || source.Status != StatusReady && source.Status != StatusStale && source.Status != StatusFailed {
			return fmt.Errorf("%w: source digest or status", ErrCorrupt)
		}
	}
	return nil
}

func ValidateIndex(index Index, source Source) error {
	if index.SchemaVersion != SchemaVersion || index.EmployeeID != source.EmployeeID || index.SourceID != source.ID ||
		index.SourceDigest != source.Digest || !validDigest(index.SourceDigest) {
		return fmt.Errorf("%w: index identity", ErrCorrupt)
	}
	for _, document := range index.Documents {
		if document.Path == "" || !validDigest(document.Digest) {
			return fmt.Errorf("%w: document identity", ErrCorrupt)
		}
		for _, citation := range document.Citations {
			if citation.SchemaVersion != SchemaVersion || !validID(citation.ID) ||
				citation.EmployeeID != source.EmployeeID || citation.SourceID != source.ID ||
				citation.Path != document.Path || citation.Digest != document.Digest ||
				citation.StartLine < 1 || citation.EndLine < citation.StartLine ||
				citation.Snippet == "" || len(citation.Snippet) > MaxCitationBytes {
				return fmt.Errorf("%w: citation identity", ErrCorrupt)
			}
		}
	}
	return nil
}

func validID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
			continue
		}
		return false
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func terms(value string) []string {
	seen := map[string]struct{}{}
	for _, field := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(field) < 2 || len(field) > 64 {
			continue
		}
		seen[field] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for field := range seen {
		result = append(result, field)
	}
	sort.Strings(result)
	return result
}

func forbiddenPathComponent(value string) bool {
	lower := strings.ToLower(value)
	if lower == ".git" || lower == ".gohermit" {
		return true
	}
	for _, marker := range []string{"credential", "secret", ".env", "id_rsa", "token"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func containsPrivateRuntimeContext(value string) bool {
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"private reasoning:", "chain of thought:", "raw tool arguments:", "raw_tool_arguments",
		"full system prompt:", "hidden system prompt:",
	} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return false
}
