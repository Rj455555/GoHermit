// Package teamtemplate stores the owner's team template — the default and
// per-role provider/model selections — outside repositories.
package teamtemplate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	SchemaVersion  = 1
	MaxRoleEntries = 5
	MaxTextBytes   = 8 << 10
)

// RoleSelection pins one role to a provider/model pair. It mirrors
// session.Selection without the agent field and holds names, never keys.
type RoleSelection struct {
	Company string `json:"company"`
	Access  string `json:"access"`
	Model   string `json:"model"`
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
	template := Template{SchemaVersion: SchemaVersion}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return template, nil
	}
	if err != nil {
		return Template{}, fmt.Errorf("read team template: %w", err)
	}
	if len(raw) > 256<<10 {
		return Template{}, errors.New("team template exceeds size limit")
	}
	// Unknown fields fail closed so a newer file format is never silently
	// truncated on load.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&template); err != nil {
		return Template{}, fmt.Errorf("decode team template: %w", err)
	}
	if template.SchemaVersion != SchemaVersion {
		return Template{}, fmt.Errorf("unsupported team template version %d", template.SchemaVersion)
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
	if clean(selection.Company) == "" || clean(selection.Access) == "" || clean(selection.Model) == "" {
		return fmt.Errorf("%s selection requires company, access, and model", label)
	}
	for _, value := range []string{selection.Company, selection.Access, selection.Model} {
		if err := validateText(value); err != nil {
			return fmt.Errorf("%s selection: %w", label, err)
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
