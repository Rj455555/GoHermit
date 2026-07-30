// Package loopstore persists owner-scoped loop definitions and invocations
// outside repositories. Like the Owner Profile and Team Template stores, the
// resolved directory must never live inside a workspace or repository: an
// Agent running in a workspace has no write access to it, so path resolution
// never consults the working directory.
package loopstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/storage"
)

const (
	// MaxStoreBytes caps every persisted file, mirroring teamtemplate.
	MaxStoreBytes = 256 << 10
	// MaxDefinitions bounds the definitions document.
	MaxDefinitions = 64
	// MaxInvocations bounds the number of invocation files listed per query.
	MaxInvocations = 256
)

const (
	definitionsFile = "definitions.json"
	invocationsDir  = "invocations"
	contractsDir    = "contracts"
	statesDir       = "states"
)

// ErrDefinitionNotFound lets application services distinguish a missing
// definition from a corrupt or unavailable owner store without parsing an
// error string.
var ErrDefinitionNotFound = errors.New("loop definition not found")

// definitionsFileBody is the on-disk shape of definitions.json.
type definitionsFileBody struct {
	SchemaVersion int               `json:"schema_version"`
	Definitions   []loop.Definition `json:"definitions"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore resolves the loop store directory: the explicit path, then
// GOHERMIT_LOOP_STORE, then the user config dir.
func NewStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("GOHERMIT_LOOP_STORE"))
	}
	if path == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve loop store directory: %w", err)
		}
		path = filepath.Join(root, "gohermit", "loops")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve loop store path: %w", err)
	}
	return &Store{dir: abs}, nil
}

// Dir returns the resolved store directory.
func (s *Store) Dir() string {
	return s.dir
}

// SaveDefinition inserts or updates a definition. The store owns the
// revision counter: an insert starts at revision 1, an update bumps the
// stored revision by one. UpdatedAt is stamped on every write; CreatedAt is
// set on insert and preserved on update. The definition is validated before
// anything is written.
func (s *Store) SaveDefinition(d loop.Definition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateLoopPathID(d.ID); err != nil {
		return err
	}
	body, err := s.loadDefinitions()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	d.SchemaVersion = loop.SchemaVersion
	d.UpdatedAt = now
	for i := range body.Definitions {
		if body.Definitions[i].ID == d.ID {
			d.Revision = body.Definitions[i].Revision + 1
			d.CreatedAt = body.Definitions[i].CreatedAt
			if err = loop.ValidateDefinition(d); err != nil {
				return err
			}
			body.Definitions[i] = d
			if err = s.saveDefinitions(body); err != nil {
				return err
			}
			return s.saveContract(d)
		}
	}
	if len(body.Definitions) >= MaxDefinitions {
		return errors.New("loop definition limit exceeded")
	}
	d.Revision = 1
	d.CreatedAt = now
	if err = loop.ValidateDefinition(d); err != nil {
		return err
	}
	body.Definitions = append(body.Definitions, d)
	if err = s.saveDefinitions(body); err != nil {
		return err
	}
	return s.saveContract(d)
}

// GetDefinition returns the stored definition for id.
func (s *Store) GetDefinition(id string) (loop.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.loadDefinitions()
	if err != nil {
		return loop.Definition{}, err
	}
	for _, d := range body.Definitions {
		if d.ID == id {
			return d, nil
		}
	}
	return loop.Definition{}, fmt.Errorf("%w: %q", ErrDefinitionNotFound, id)
}

// ListDefinitions returns every stored definition, sorted by id.
func (s *Store) ListDefinitions() ([]loop.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.loadDefinitions()
	if err != nil {
		return nil, err
	}
	definitions := append([]loop.Definition(nil), body.Definitions...)
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions, nil
}

// DeleteDefinition removes the definition for id.
func (s *Store) DeleteDefinition(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.loadDefinitions()
	if err != nil {
		return err
	}
	for i := range body.Definitions {
		if body.Definitions[i].ID != id {
			continue
		}
		body.Definitions = append(body.Definitions[:i], body.Definitions[i+1:]...)
		if err = s.saveDefinitions(body); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(s.dir, contractsDir, id, "LOOP.md"))
		_ = os.Remove(filepath.Join(s.dir, statesDir, id+".json"))
		return nil
	}
	return fmt.Errorf("%w: %q", ErrDefinitionNotFound, id)
}

func (s *Store) saveContract(definition loop.Definition) error {
	if err := validateLoopPathID(definition.ID); err != nil {
		return err
	}
	path := filepath.Join(s.dir, contractsDir, definition.ID, "LOOP.md")
	if definition.EmployeeID == "" {
		_ = os.Remove(path)
		return nil
	}
	markdown, err := loop.RenderContractMarkdown(definition)
	if err != nil {
		return err
	}
	if len(markdown) > MaxStoreBytes {
		return errors.New("loop contract exceeds size limit")
	}
	if err = storage.AtomicWrite(path, []byte(markdown), 0600); err != nil {
		return fmt.Errorf("save loop contract: %w", err)
	}
	return nil
}

func validateLoopPathID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > loop.MaxIDBytes || filepath.IsAbs(id) ||
		filepath.Clean(id) != id || filepath.Base(id) != id || id == "." || id == ".." ||
		strings.Contains(id, "%") || strings.ContainsAny(id, `/\`) {
		return errors.New("loop id is invalid")
	}
	return nil
}

// loadDefinitions reads definitions.json. A missing file yields an empty
// document; a corrupt or unsupported file is an error and is never silently
// wiped. Unknown fields fail closed so a newer format is never truncated.
func (s *Store) loadDefinitions() (definitionsFileBody, error) {
	body := definitionsFileBody{SchemaVersion: loop.SchemaVersion}
	raw, err := os.ReadFile(filepath.Join(s.dir, definitionsFile))
	if errors.Is(err, os.ErrNotExist) {
		return body, nil
	}
	if err != nil {
		return definitionsFileBody{}, fmt.Errorf("read loop definitions: %w", err)
	}
	if len(raw) > MaxStoreBytes {
		return definitionsFileBody{}, errors.New("loop definitions exceed size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&body); err != nil {
		return definitionsFileBody{}, fmt.Errorf("decode loop definitions: %w", err)
	}
	if body.SchemaVersion != loop.SchemaVersion {
		return definitionsFileBody{}, fmt.Errorf("unsupported loop definitions version %d", body.SchemaVersion)
	}
	if len(body.Definitions) > MaxDefinitions {
		return definitionsFileBody{}, errors.New("loop definitions exceed count limit")
	}
	for _, d := range body.Definitions {
		if err = loop.ValidateDefinition(d); err != nil {
			return definitionsFileBody{}, fmt.Errorf("loop definition %q: %w", d.ID, err)
		}
	}
	return body, nil
}

func (s *Store) saveDefinitions(body definitionsFileBody) error {
	body.SchemaVersion = loop.SchemaVersion
	raw, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return fmt.Errorf("encode loop definitions: %w", err)
	}
	if len(raw) > MaxStoreBytes {
		return errors.New("loop definitions exceed size limit")
	}
	if err = storage.AtomicWrite(filepath.Join(s.dir, definitionsFile), append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("save loop definitions: %w", err)
	}
	return nil
}
