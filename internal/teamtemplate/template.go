// Package teamtemplate stores the owner's team template — the default and
// per-role provider/model selections — outside repositories.
package teamtemplate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/storage"
	"github.com/Rj455555/GoHermit/internal/team"
)

const (
	SchemaVersion      = 2
	LegacySchemaV1     = 1
	MaxRoleEntries     = 5
	MaxTextBytes       = 8 << 10
	MaxEmployeeIDBytes = 128
	// MaxRoleModelCalls and MaxRoleTokens bound the optional per-role cost
	// ceilings so a tampered template cannot carry absurd limits.
	MaxRoleModelCalls = 1_000
	MaxRoleTokens     = 10_000_000
)

// RoleSelection pins one role to a provider/model pair. It mirrors
// session.Selection without the agent field and holds names, never keys.
type RoleSelection struct {
	Company    string `json:"company"`
	Access     string `json:"access"`
	Model      string `json:"model"`
	EmployeeID string `json:"employee_id,omitempty"`
	// MaxModelCalls and MaxTokens optionally cap the role's usage on top of
	// the mission budget; zero means unlimited. Both are additive so files
	// written before the keys existed load unchanged.
	MaxModelCalls int `json:"max_model_calls,omitempty"`
	MaxTokens     int `json:"max_tokens,omitempty"`
}

type Template struct {
	SchemaVersion int                      `json:"schema_version"`
	Name          string                   `json:"name"`
	Default       RoleSelection            `json:"default"`
	Roles         map[string]RoleSelection `json:"roles,omitempty"` // per-role overrides
	UpdatedAt     time.Time                `json:"updated_at"`
}

// allowedOverrides lists the roles a template may override. RoleOperator
// stays reserved and unavailable, so it is deliberately absent.
var allowedOverrides = map[string]bool{
	string(team.RoleLead):     true,
	string(team.RoleExplorer): true,
	string(team.RoleBuilder):  true,
	string(team.RoleReviewer): true,
	string(team.RoleVerifier): true,
}

// Empty reports whether the template holds no usable default selection —
// the case for a store whose file was never written.
func (t Template) Empty() bool {
	if clean(t.Default.EmployeeID) != "" {
		return false
	}
	return clean(t.Default.Company) == "" || clean(t.Default.Access) == "" || clean(t.Default.Model) == ""
}

// SelectionForRole returns the role's override when present, else the
// template default.
func (t Template) SelectionForRole(role string) RoleSelection {
	if selection, ok := t.Roles[role]; ok {
		return selection
	}
	return t.Default
}

// EffectiveSelections resolves the selection every validatable team role
// ends up with: the per-role override when set, the default otherwise.
func EffectiveSelections(t Template) map[string]RoleSelection {
	selections := make(map[string]RoleSelection, len(allowedOverrides))
	for role := range allowedOverrides {
		selections[role] = t.SelectionForRole(role)
	}
	return selections
}

type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore resolves the template path: the explicit path, then
// GOHERMIT_TEAM_TEMPLATE_STORE, then the user config dir. The file must
// never live inside a workspace or repository, so resolution never consults
// the working directory.
func NewStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("GOHERMIT_TEAM_TEMPLATE_STORE"))
	}
	if path == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve team template directory: %w", err)
		}
		path = filepath.Join(root, "gohermit", "team-template.json")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve team template path: %w", err)
	}
	return &Store{path: abs}, nil
}

// Path returns the resolved store path.
func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load() (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) Save(t Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(t)
}

func (s *Store) load() (Template, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Template{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return Template{}, fmt.Errorf("read team template: %w", err)
	}
	if len(raw) > 256<<10 {
		return Template{}, errors.New("team template exceeds size limit")
	}
	// Unknown fields fail closed so a newer file format is never silently
	// truncated on load.
	template, err := decodeTemplate(raw)
	if err != nil {
		return Template{}, err
	}
	if err = Validate(template); err != nil {
		return Template{}, err
	}
	return template, nil
}

func (s *Store) save(t Template) error {
	t.SchemaVersion = SchemaVersion
	t.UpdatedAt = time.Now().UTC()
	if err := Validate(t); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encode team template: %w", err)
	}
	if len(raw) > 256<<10 {
		return errors.New("team template exceeds size limit")
	}
	if err = storage.AtomicWrite(s.path, append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("save team template: %w", err)
	}
	return nil
}

// Validate enforces the template contract: a bounded name, a fully populated
// default selection, overrides only for non-reserved roles, and no field
// that looks like a credential — selections hold names, never keys.
func Validate(t Template) error {
	if clean(t.Name) == "" {
		return errors.New("team template name is required")
	}
	if err := validateText(t.Name); err != nil {
		return err
	}
	if err := validateSelection("default", t.Default); err != nil {
		return err
	}
	if len(t.Roles) > MaxRoleEntries {
		return errors.New("team template exceeds role override limit")
	}
	for role, selection := range t.Roles {
		if !allowedOverrides[role] {
			return fmt.Errorf("role %q is not an allowed override", role)
		}
		if err := validateSelection(fmt.Sprintf("role %q", role), selection); err != nil {
			return err
		}
	}
	return nil
}

func validateSelection(label string, selection RoleSelection) error {
	employeeID := clean(selection.EmployeeID)
	company, access, model := clean(selection.Company), clean(selection.Access), clean(selection.Model)
	populated := 0
	for _, value := range []string{company, access, model} {
		if value != "" {
			populated++
		}
	}
	if employeeID == "" && populated != 3 {
		return fmt.Errorf("%s selection requires company, access, and model", label)
	}
	if employeeID != "" && populated != 0 && populated != 3 {
		return fmt.Errorf("%s selection override must provide company, access, and model together", label)
	}
	if employeeID != "" {
		if err := validateEmployeeID(employeeID); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	for _, value := range []string{selection.Company, selection.Access, selection.Model, selection.EmployeeID} {
		if err := validateText(value); err != nil {
			return fmt.Errorf("%s selection: %w", label, err)
		}
	}
	if selection.MaxModelCalls < 0 || selection.MaxModelCalls > MaxRoleModelCalls {
		return fmt.Errorf("%s max_model_calls must be between 0 and %d", label, MaxRoleModelCalls)
	}
	if selection.MaxTokens < 0 || selection.MaxTokens > MaxRoleTokens {
		return fmt.Errorf("%s max_tokens must be between 0 and %d", label, MaxRoleTokens)
	}
	return nil
}

// ErrImportSecret marks an import rejected because a field matched the
// credential markers in owner.LooksSecret. It stays distinct from generic
// validation failures so a poisoned file is refused explicitly.
var ErrImportSecret = errors.New("team template import contains a credential or token marker")

// Export returns the template as indented JSON for download, with redaction
// applied to a copy: every string field is screened with owner.LooksSecret
// and any match is blanked to "". Blanking (rather than dropping the role
// entry) keeps the document structure so the owner sees which fields to
// refill. A clean template exports byte-identical to a plain marshal; a
// tampered in-memory template exports with zero secret content.
func Export(t Template) ([]byte, error) {
	t.SchemaVersion = SchemaVersion
	t.Name = redact(t.Name)
	t.Default = redactSelection(t.Default)
	if len(t.Roles) > 0 {
		roles := make(map[string]RoleSelection, len(t.Roles))
		for role, selection := range t.Roles {
			roles[role] = redactSelection(selection)
		}
		t.Roles = roles
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode team template: %w", err)
	}
	return raw, nil
}

// Import parses an exported template file without saving it. The input is
// size-capped and strictly decoded like the store file, then every string
// field is screened with owner.LooksSecret BEFORE generic validation so a
// poisoned file is rejected with ErrImportSecret, never silently accepted.
func Import(data []byte) (Template, error) {
	if len(data) > 256<<10 {
		return Template{}, errors.New("team template exceeds size limit")
	}
	template, err := decodeTemplate(data)
	if err != nil {
		return Template{}, err
	}
	for _, field := range secretFields(template) {
		if owner.LooksSecret(field.value) {
			return Template{}, fmt.Errorf("%w: %s", ErrImportSecret, field.label)
		}
	}
	if err := Validate(template); err != nil {
		return Template{}, err
	}
	return template, nil
}

// secretField pairs a bounded location label with a value to screen; labels
// name the field, never the value, so errors carry no secret content.
type secretField struct{ label, value string }

func secretFields(t Template) []secretField {
	fields := []secretField{
		{"name", t.Name},
		{"default selection company", t.Default.Company},
		{"default selection access", t.Default.Access},
		{"default selection model", t.Default.Model},
		{"default selection employee id", t.Default.EmployeeID},
	}
	for _, selection := range t.Roles {
		fields = append(fields,
			secretField{"role override company", selection.Company},
			secretField{"role override access", selection.Access},
			secretField{"role override model", selection.Model},
			secretField{"role override employee id", selection.EmployeeID},
		)
	}
	return fields
}

func redact(value string) string {
	if owner.LooksSecret(value) {
		return ""
	}
	return value
}

func redactSelection(selection RoleSelection) RoleSelection {
	selection.Company = redact(selection.Company)
	selection.Access = redact(selection.Access)
	selection.Model = redact(selection.Model)
	selection.EmployeeID = redact(selection.EmployeeID)
	return selection
}

func decodeTemplate(raw []byte) (Template, error) {
	var version struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &version); err != nil {
		return Template{}, fmt.Errorf("decode team template: %w", err)
	}
	if version.SchemaVersion != LegacySchemaV1 && version.SchemaVersion != SchemaVersion {
		return Template{}, fmt.Errorf("unsupported team template version %d", version.SchemaVersion)
	}
	template := Template{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&template); err != nil {
		return Template{}, fmt.Errorf("decode team template: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Template{}, errors.New("decode team template: trailing JSON value")
	}
	// v1 and v2 have identical legacy selection semantics; v2 only adds the
	// optional employee_id. Migration is therefore deterministic and cannot
	// invent an Employee assignment.
	if template.SchemaVersion == LegacySchemaV1 {
		template.SchemaVersion = SchemaVersion
	}
	return template, nil
}

func validateEmployeeID(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > MaxEmployeeIDBytes ||
		filepath.IsAbs(value) || strings.ContainsAny(value, `/\%`) ||
		value == "." || value == ".." || strings.Contains(value, "..") {
		return errors.New("employee_id is invalid or path-unsafe")
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9') {
			return errors.New("employee_id contains an unsupported character")
		}
	}
	return nil
}

func validateText(value string) error {
	if len(value) > MaxTextBytes {
		return errors.New("team template text exceeds size limit")
	}
	if owner.LooksSecret(value) {
		return errors.New("team template must not contain credentials or tokens")
	}
	return nil
}

func clean(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
}
