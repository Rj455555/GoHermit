package skill

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	manifestSchemaVersion = 1
	maxCatalogEntries     = 512
	maxManifestBytes      = 64 << 10
	maxSkillBytes         = 256 << 10
	maxReferencesBytes    = 2 << 20
	maxIdentifierBytes    = 128
	maxCapabilities       = 64
	maxFrontmatterBytes   = 8 << 10
)

var ErrCorrupt = errors.New("skill catalog is corrupt")

type Kind string

const (
	KindNative  Kind = "native"
	KindAdapter Kind = "skill_md_adapter"
)

type Manifest struct {
	SchemaVersion         int             `json:"schema_version"`
	SkillID               string          `json:"skill_id"`
	Version               string          `json:"version"`
	Title                 string          `json:"title"`
	Description           string          `json:"description"`
	RequestedCapabilities []string        `json:"requested_capabilities"`
	ConfigurationSchema   json.RawMessage `json:"configuration_schema"`
	ContentFiles          []string        `json:"content_files"`
	DigestAlgorithm       string          `json:"digest_algorithm"`
	Digest                string          `json:"digest"`
}

type Skill struct {
	Kind         Kind              `json:"kind"`
	Manifest     Manifest          `json:"manifest"`
	Instructions string            `json:"instructions"`
	References   map[string]string `json:"references,omitempty"`
}

type Catalog struct {
	root string
}

// NewCatalog creates a read-only catalog. An empty root uses
// GOHERMIT_SKILL_CATALOG; when both are empty the catalog is disabled.
func NewCatalog(root string) (*Catalog, error) {
	if root == "" {
		root = os.Getenv("GOHERMIT_SKILL_CATALOG")
	}
	if root == "" {
		return &Catalog{}, nil
	}
	if !filepath.IsAbs(root) {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, corrupt("resolve catalog root", err)
		}
		root = absolute
	}
	clean := filepath.Clean(root)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return nil, corrupt("resolve catalog root", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, corrupt("stat catalog root", err)
	}
	if !info.IsDir() {
		return nil, corrupt("catalog root is not a directory", nil)
	}
	return &Catalog{root: filepath.Clean(real)}, nil
}

func (c *Catalog) List() ([]Skill, error) {
	if c == nil || c.root == "" {
		return []Skill{}, nil
	}
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return nil, corrupt("read catalog root", err)
	}
	result := make([]Skill, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if err := validateID(entry.Name()); err != nil {
			return nil, corrupt("invalid catalog entry", err)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, corrupt("catalog entry is a symlink", nil)
		}
		if !entry.IsDir() {
			continue
		}
		skillDir, err := c.safeDirectory(entry.Name())
		if err != nil {
			return nil, err
		}
		adapterPath := filepath.Join(skillDir, "SKILL.md")
		if info, statErr := os.Lstat(adapterPath); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, corrupt("adapter SKILL.md is not a regular file", nil)
			}
			item, loadErr := c.loadAdapter(entry.Name())
			if loadErr != nil {
				return nil, loadErr
			}
			if err := appendUnique(&result, seen, item); err != nil {
				return nil, err
			}
			continue
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, corrupt("stat adapter SKILL.md", statErr)
		}
		versions, readErr := os.ReadDir(skillDir)
		if readErr != nil {
			return nil, corrupt("read native skill directory", readErr)
		}
		for _, versionEntry := range versions {
			if strings.HasPrefix(versionEntry.Name(), ".") {
				continue
			}
			if err := validateID(versionEntry.Name()); err != nil {
				return nil, corrupt("invalid native version directory", err)
			}
			if versionEntry.Type()&os.ModeSymlink != 0 || !versionEntry.IsDir() {
				return nil, corrupt("native version is not a regular directory", nil)
			}
			item, loadErr := c.loadNative(entry.Name(), versionEntry.Name())
			if loadErr != nil {
				return nil, loadErr
			}
			if err := appendUnique(&result, seen, item); err != nil {
				return nil, err
			}
		}
	}
	if len(result) > maxCatalogEntries {
		return nil, corrupt("catalog entry limit exceeded", nil)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Manifest.SkillID == result[j].Manifest.SkillID {
			return result[i].Manifest.Version < result[j].Manifest.Version
		}
		return result[i].Manifest.SkillID < result[j].Manifest.SkillID
	})
	return result, nil
}

func (c *Catalog) Resolve(skillID, version string) (Skill, error) {
	if err := validateID(skillID); err != nil {
		return Skill{}, err
	}
	if err := validateID(version); err != nil {
		return Skill{}, err
	}
	items, err := c.List()
	if err != nil {
		return Skill{}, err
	}
	for _, item := range items {
		if item.Manifest.SkillID == skillID && item.Manifest.Version == version {
			return item, nil
		}
	}
	return Skill{}, fs.ErrNotExist
}

func (c *Catalog) loadNative(skillDirectory, versionDirectory string) (Skill, error) {
	base, err := c.safeDirectory(skillDirectory, versionDirectory)
	if err != nil {
		return Skill{}, err
	}
	raw, err := c.readRegular(maxManifestBytes, skillDirectory, versionDirectory, "manifest.json")
	if err != nil {
		return Skill{}, err
	}
	var manifest Manifest
	if err := decodeStrict(raw, &manifest); err != nil {
		return Skill{}, corrupt("decode manifest", err)
	}
	if err := validateManifest(manifest, skillDirectory, versionDirectory); err != nil {
		return Skill{}, corrupt("validate manifest", err)
	}
	manifest.Digest = strings.ToLower(manifest.Digest)

	files := make(map[string][]byte, len(manifest.ContentFiles))
	seenPaths := make(map[string]struct{}, len(manifest.ContentFiles))
	var referenceBytes int
	for _, path := range manifest.ContentFiles {
		normalized, err := validateContentPath(path)
		if err != nil {
			return Skill{}, corrupt("validate content path", err)
		}
		if _, duplicate := seenPaths[normalized]; duplicate {
			return Skill{}, corrupt("duplicate content path", nil)
		}
		seenPaths[normalized] = struct{}{}
		parts := append([]string{skillDirectory, versionDirectory}, strings.Split(normalized, "/")...)
		limit := maxReferencesBytes
		if normalized == "SKILL.md" {
			limit = maxSkillBytes
		}
		content, readErr := c.readRegular(limit, parts...)
		if readErr != nil {
			return Skill{}, readErr
		}
		if normalized == "SKILL.md" && len(content) > maxSkillBytes {
			return Skill{}, corrupt("SKILL.md size limit exceeded", nil)
		}
		if strings.HasPrefix(normalized, "references/") {
			referenceBytes += len(content)
			if referenceBytes > maxReferencesBytes {
				return Skill{}, corrupt("references size limit exceeded", nil)
			}
		}
		files[normalized] = content
	}
	instructions, exists := files["SKILL.md"]
	if !exists {
		return Skill{}, corrupt("manifest does not include SKILL.md", nil)
	}
	if !utf8.Valid(instructions) {
		return Skill{}, corrupt("SKILL.md is not UTF-8", nil)
	}
	expected, err := digestManifest(manifest, files)
	if err != nil {
		return Skill{}, corrupt("calculate manifest digest", err)
	}
	if expected != manifest.Digest {
		return Skill{}, corrupt("manifest digest mismatch", nil)
	}
	references := make(map[string]string)
	for path, content := range files {
		if strings.HasPrefix(path, "references/") {
			if !utf8.Valid(content) {
				return Skill{}, corrupt("reference is not UTF-8", nil)
			}
			references[path] = string(content)
		}
	}
	_ = base
	return Skill{
		Kind: KindNative, Manifest: manifest, Instructions: string(instructions), References: references,
	}, nil
}

func (c *Catalog) loadAdapter(directory string) (Skill, error) {
	raw, err := c.readRegular(maxSkillBytes, directory, "SKILL.md")
	if err != nil {
		return Skill{}, err
	}
	if !utf8.Valid(raw) {
		return Skill{}, corrupt("adapter SKILL.md is not UTF-8", nil)
	}
	title, description, err := parseFrontmatter(raw)
	if err != nil {
		return Skill{}, corrupt("parse adapter frontmatter", err)
	}
	references, referenceBytes, err := c.readAdapterReferences(directory)
	if err != nil {
		return Skill{}, err
	}
	if referenceBytes > maxReferencesBytes {
		return Skill{}, corrupt("adapter references size limit exceeded", nil)
	}
	files := map[string][]byte{"SKILL.md": raw}
	for path, value := range references {
		files[path] = []byte(value)
	}
	digest := digestFiles(files)
	version := "synthetic-" + digest[:16]
	return Skill{
		Kind: KindAdapter,
		Manifest: Manifest{
			SchemaVersion: manifestSchemaVersion, SkillID: directory, Version: version,
			Title: title, Description: description, RequestedCapabilities: []string{},
			ConfigurationSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ContentFiles:        sortedFileNames(files), DigestAlgorithm: "sha256", Digest: digest,
		},
		Instructions: string(raw),
		References:   references,
	}, nil
}

func (c *Catalog) readAdapterReferences(directory string) (map[string]string, int, error) {
	result := make(map[string]string)
	referencesDir := filepath.Join(c.root, directory, "references")
	info, err := os.Lstat(referencesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return result, 0, nil
	}
	if err != nil {
		return nil, 0, corrupt("stat adapter references", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, 0, corrupt("adapter references is not a regular directory", nil)
	}
	total := 0
	err = filepath.WalkDir(referencesDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return corrupt("walk adapter references", walkErr)
		}
		if path == referencesDir {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return corrupt("adapter reference is a symlink", nil)
		}
		relative, err := filepath.Rel(filepath.Join(c.root, directory), path)
		if err != nil {
			return corrupt("resolve adapter reference", err)
		}
		normalized := filepath.ToSlash(relative)
		if _, err := validateContentPath(normalized); err != nil {
			return corrupt("validate adapter reference path", err)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return corrupt("adapter reference is not a regular file", nil)
		}
		parts := append([]string{directory}, strings.Split(normalized, "/")...)
		raw, err := c.readRegular(maxReferencesBytes, parts...)
		if err != nil {
			return err
		}
		if !utf8.Valid(raw) {
			return corrupt("adapter reference is not UTF-8", nil)
		}
		total += len(raw)
		if total > maxReferencesBytes {
			return corrupt("adapter references size limit exceeded", nil)
		}
		result[normalized] = string(raw)
		return nil
	})
	return result, total, err
}

func (c *Catalog) safeDirectory(parts ...string) (string, error) {
	path, err := c.lexicalPath(parts...)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", corrupt("stat catalog directory", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", corrupt("catalog path is not a regular directory", nil)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", corrupt("resolve catalog directory", err)
	}
	if !within(c.root, real) {
		return "", corrupt("catalog directory escapes root", nil)
	}
	return path, nil
}

func (c *Catalog) readRegular(limit int, parts ...string) ([]byte, error) {
	if len(parts) == 0 {
		return nil, corrupt("empty catalog path", nil)
	}
	for index := 1; index < len(parts); index++ {
		if _, err := c.safeDirectory(parts[:index]...); err != nil {
			return nil, err
		}
	}
	path, err := c.lexicalPath(parts...)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, corrupt("stat catalog file", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, corrupt("catalog file is not a regular file", nil)
	}
	if info.Mode().Perm()&0o111 != 0 {
		return nil, corrupt("catalog content file is executable", nil)
	}
	if info.Size() > int64(limit) {
		return nil, corrupt("catalog file size limit exceeded", nil)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, corrupt("open catalog file", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, corrupt("read catalog file", err)
	}
	if len(raw) > limit {
		return nil, corrupt("catalog file size limit exceeded", nil)
	}
	return raw, nil
}

func (c *Catalog) lexicalPath(parts ...string) (string, error) {
	for _, part := range parts {
		if part == "" || filepath.IsAbs(part) || filepath.Clean(part) != part ||
			part == "." || part == ".." || strings.ContainsAny(part, `/\`) {
			return "", corrupt("invalid catalog path component", nil)
		}
	}
	path := filepath.Join(append([]string{c.root}, parts...)...)
	if !within(c.root, path) {
		return "", corrupt("catalog path escapes root", nil)
	}
	return path, nil
}

func validateManifest(manifest Manifest, skillDirectory, versionDirectory string) error {
	if manifest.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", manifest.SchemaVersion)
	}
	if err := validateID(manifest.SkillID); err != nil {
		return err
	}
	if err := validateID(manifest.Version); err != nil {
		return err
	}
	if manifest.SkillID != skillDirectory || manifest.Version != versionDirectory {
		return errors.New("manifest identity does not match catalog path")
	}
	if err := validateText("title", manifest.Title, maxIdentifierBytes); err != nil {
		return err
	}
	if err := validateText("description", manifest.Description, 2048); err != nil {
		return err
	}
	if len(manifest.RequestedCapabilities) > maxCapabilities {
		return errors.New("capability limit exceeded")
	}
	if err := employee.ValidateRequestedCapabilities(manifest.RequestedCapabilities); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(manifest.RequestedCapabilities))
	for _, capability := range manifest.RequestedCapabilities {
		if err := validateID(capability); err != nil {
			return fmt.Errorf("invalid capability: %w", err)
		}
		if _, exists := seen[capability]; exists {
			return errors.New("duplicate capability")
		}
		seen[capability] = struct{}{}
	}
	if err := ValidateConfigurationSchema(manifest.ConfigurationSchema); err != nil {
		return err
	}
	if len(manifest.ContentFiles) == 0 {
		return errors.New("content_files is required")
	}
	if manifest.DigestAlgorithm != "sha256" {
		return errors.New("unsupported digest algorithm")
	}
	if len(manifest.Digest) != sha256.Size*2 {
		return errors.New("invalid digest")
	}
	if _, err := hex.DecodeString(manifest.Digest); err != nil {
		return errors.New("invalid digest")
	}
	return nil
}

func validateContentPath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, `\%`) {
		return "", errors.New("content path must be relative and unambiguous")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("content path is not canonical")
	}
	if clean != "SKILL.md" && !strings.HasPrefix(clean, "references/") {
		return "", errors.New("content path is outside the instruction allowlist")
	}
	lower := strings.ToLower(clean)
	for _, marker := range []string{"credential", "password", "passwd", "secret", "api_key", "apikey", "token", ".env", "id_rsa"} {
		if strings.Contains(lower, marker) {
			return "", errors.New("credential-like content path")
		}
	}
	if owner.LooksSecret(clean) {
		return "", errors.New("secret-like content path")
	}
	return clean, nil
}

func validateID(value string) error {
	if value == "" || len(value) > maxIdentifierBytes || !utf8.ValidString(value) {
		return errors.New("identifier is empty, oversized, or invalid UTF-8")
	}
	if filepath.IsAbs(value) || filepath.Clean(value) != value || value == "." || value == ".." ||
		strings.ContainsAny(value, `/\`) || strings.Contains(value, "%") {
		return errors.New("identifier is not path safe")
	}
	for _, character := range value {
		if !(character == '-' || character == '_' || character == '.' ||
			character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9') {
			return errors.New("identifier contains unsupported characters")
		}
	}
	return nil
}

func validateText(name, value string, limit int) error {
	if strings.TrimSpace(value) == "" || len(value) > limit || !utf8.ValidString(value) {
		return fmt.Errorf("%s is empty, oversized, or invalid UTF-8", name)
	}
	if owner.LooksSecret(value) {
		return fmt.Errorf("%s contains secret-like content", name)
	}
	return nil
}

func parseFrontmatter(raw []byte) (string, string, error) {
	if len(raw) > maxSkillBytes || !bytes.HasPrefix(raw, []byte("---\n")) {
		return "", "", errors.New("bounded YAML frontmatter is required")
	}
	end := bytes.Index(raw[4:], []byte("\n---\n"))
	if end < 0 || end+4 > maxFrontmatterBytes {
		return "", "", errors.New("frontmatter terminator missing or oversized")
	}
	block := string(raw[4 : 4+end])
	values := make(map[string]string)
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return "", "", errors.New("frontmatter must contain scalar key/value pairs")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "name" && key != "description" {
			return "", "", fmt.Errorf("unknown frontmatter field %q", key)
		}
		if _, duplicate := values[key]; duplicate {
			return "", "", fmt.Errorf("duplicate frontmatter field %q", key)
		}
		if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "|") ||
			strings.HasPrefix(value, ">") || strings.HasPrefix(value, "&") || strings.HasPrefix(value, "*") ||
			strings.Contains(value, "${") {
			return "", "", errors.New("frontmatter value must be a plain scalar")
		}
		values[key] = strings.Trim(value, `"'`)
	}
	if err := validateText("name", values["name"], maxIdentifierBytes); err != nil {
		return "", "", err
	}
	if err := validateText("description", values["description"], 2048); err != nil {
		return "", "", err
	}
	return values["name"], values["description"], nil
}

func decodeStrict(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func digestManifest(manifest Manifest, files map[string][]byte) (string, error) {
	manifest.Digest = ""
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write(raw)
	for _, path := range sortedByteFileNames(files) {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(files[path])
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestFiles(files map[string][]byte) string {
	hash := sha256.New()
	for _, path := range sortedByteFileNames(files) {
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(files[path])
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sortedByteFileNames(files map[string][]byte) []string {
	result := make([]string, 0, len(files))
	for path := range files {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func sortedFileNames(files map[string][]byte) []string {
	return sortedByteFileNames(files)
}

func appendUnique(result *[]Skill, seen map[string]struct{}, item Skill) error {
	key := item.Manifest.SkillID + "\x00" + item.Manifest.Version
	if _, exists := seen[key]; exists {
		return corrupt("duplicate skill identity", nil)
	}
	seen[key] = struct{}{}
	*result = append(*result, item)
	return nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

func corrupt(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrCorrupt, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrCorrupt, message, cause)
}
