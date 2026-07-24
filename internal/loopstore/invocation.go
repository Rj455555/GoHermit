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

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/storage"
)

// invocationPath maps an invocation id to its file, rejecting anything that
// could escape the invocations directory.
func (s *Store) invocationPath(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsRune(id, '/') || strings.ContainsRune(id, '\\') {
		return "", fmt.Errorf("invalid invocation id %q", id)
	}
	return filepath.Join(s.dir, invocationsDir, id+".json"), nil
}

// SaveInvocation persists one invocation atomically. The invocation is
// validated before write; transitions are applied by the caller through the
// loop package, so the file is simply replaced on each save.
func (s *Store) SaveInvocation(inv loop.Invocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := loop.ValidateInvocation(inv); err != nil {
		return err
	}
	path, err := s.invocationPath(inv.ID)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return fmt.Errorf("encode loop invocation: %w", err)
	}
	if len(raw) > MaxStoreBytes {
		return errors.New("loop invocation exceeds size limit")
	}
	if err = storage.AtomicWrite(path, append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("save loop invocation: %w", err)
	}
	return nil
}

// loadInvocation reads one invocation file with the same strict decode
// discipline as the definitions document.
func (s *Store) loadInvocation(path string) (loop.Invocation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return loop.Invocation{}, fmt.Errorf("read loop invocation: %w", err)
	}
	if len(raw) > MaxStoreBytes {
		return loop.Invocation{}, errors.New("loop invocation exceeds size limit")
	}
	inv := loop.Invocation{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&inv); err != nil {
		return loop.Invocation{}, fmt.Errorf("decode loop invocation: %w", err)
	}
	if err = loop.ValidateInvocation(inv); err != nil {
		return loop.Invocation{}, err
	}
	return inv, nil
}

// GetInvocation returns the stored invocation for id.
func (s *Store) GetInvocation(id string) (loop.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.invocationPath(id)
	if err != nil {
		return loop.Invocation{}, err
	}
	inv, err := s.loadInvocation(path)
	if errors.Is(err, os.ErrNotExist) {
		return loop.Invocation{}, fmt.Errorf("loop invocation %q not found", id)
	}
	return inv, err
}

// ListInvocations returns the invocations for one loop — or every
// invocation when loopID is empty — sorted by created_at then id, bounded
// by MaxInvocations.
func (s *Store) ListInvocations(loopID string) ([]loop.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.dir, invocationsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read loop invocations: %w", err)
	}
	var invocations []loop.Invocation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if len(invocations) >= MaxInvocations {
			return nil, errors.New("loop invocation limit exceeded")
		}
		inv, err := s.loadInvocation(filepath.Join(s.dir, invocationsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if loopID == "" || inv.LoopID == loopID {
			invocations = append(invocations, inv)
		}
	}
	sort.Slice(invocations, func(i, j int) bool {
		if !invocations[i].CreatedAt.Equal(invocations[j].CreatedAt) {
			return invocations[i].CreatedAt.Before(invocations[j].CreatedAt)
		}
		return invocations[i].ID < invocations[j].ID
	})
	return invocations, nil
}

// ErrImportSecret marks an import rejected because a field matched the
// credential markers in owner.LooksSecret. It stays distinct from generic
// validation failures so a poisoned file is refused explicitly.
var ErrImportSecret = errors.New("loop definition import contains a credential or token marker")

// ExportDefinition returns the definition as indented JSON for download,
// with redaction applied to a copy: every string field is screened with
// owner.LooksSecret and any match is blanked to "". A clean definition
// exports byte-identical to a plain marshal; a tampered in-memory definition
// exports with zero secret content.
func (s *Store) ExportDefinition(id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.loadDefinitions()
	if err != nil {
		return nil, err
	}
	for _, d := range body.Definitions {
		if d.ID != id {
			continue
		}
		redacted := loop.RedactDefinition(d)
		raw, err := json.MarshalIndent(redacted, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode loop definition: %w", err)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("loop definition %q not found", id)
}

// ImportDefinition parses an exported definition file without saving it. The
// input is size-capped and strictly decoded like the store file, then every
// string field is screened with owner.LooksSecret BEFORE generic validation
// so a poisoned file is rejected with ErrImportSecret, never silently
// accepted.
func ImportDefinition(data []byte) (loop.Definition, error) {
	if len(data) > MaxStoreBytes {
		return loop.Definition{}, errors.New("loop definition exceeds size limit")
	}
	definition := loop.Definition{SchemaVersion: loop.SchemaVersion}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return loop.Definition{}, fmt.Errorf("decode loop definition: %w", err)
	}
	if definition.SchemaVersion != loop.SchemaVersion {
		return loop.Definition{}, fmt.Errorf("unsupported loop definition version %d", definition.SchemaVersion)
	}
	for _, field := range loop.SecretFields(definition) {
		if owner.LooksSecret(field.Value) {
			return loop.Definition{}, fmt.Errorf("%w: %s", ErrImportSecret, field.Label)
		}
	}
	if err := loop.ValidateDefinition(definition); err != nil {
		return loop.Definition{}, err
	}
	return definition, nil
}
