package employee

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	SnapshotSchemaVersion = 1
	MaxSnapshotBytes      = 256 << 10
)

// RevisionSnapshot is the complete immutable Employee historical truth owned
// by EmployeeTask/Employee Store. Session and Team checkpoints must use their
// separate compact snapshot contract and never embed this document.
type RevisionSnapshot struct {
	SchemaVersion   int              `json:"schema_version"`
	EmployeeID      string           `json:"employee_id"`
	Revision        int              `json:"revision"`
	CapturedAt      time.Time        `json:"captured_at"`
	Employee        Employee         `json:"employee"`
	ProjectBindings []ProjectBinding `json:"project_bindings,omitempty"`
	Digest          string           `json:"digest"`
}

// NewRevisionSnapshot deep-copies the complete Employee revision and the exact
// ProjectBindings it references, then seals the immutable value with SHA-256.
func NewRevisionSnapshot(employee Employee, bindings []ProjectBinding) (RevisionSnapshot, error) {
	if err := Validate(employee); err != nil {
		return RevisionSnapshot{}, fmt.Errorf("snapshot employee: %w", err)
	}
	snapshot := RevisionSnapshot{
		SchemaVersion:   SnapshotSchemaVersion,
		EmployeeID:      employee.ID,
		Revision:        employee.Revision,
		CapturedAt:      employee.UpdatedAt,
		Employee:        cloneEmployee(employee),
		ProjectBindings: cloneProjectBindings(bindings),
	}
	sort.Slice(snapshot.ProjectBindings, func(i, j int) bool {
		return snapshot.ProjectBindings[i].ID < snapshot.ProjectBindings[j].ID
	})
	if err := validateSnapshotBindings(snapshot.Employee, snapshot.ProjectBindings); err != nil {
		return RevisionSnapshot{}, err
	}
	digest, err := snapshotDigest(snapshot)
	if err != nil {
		return RevisionSnapshot{}, err
	}
	snapshot.Digest = digest
	if err = ValidateRevisionSnapshot(snapshot); err != nil {
		return RevisionSnapshot{}, err
	}
	return snapshot, nil
}

// ValidateRevisionSnapshot verifies identity, completeness, size, and digest.
func ValidateRevisionSnapshot(snapshot RevisionSnapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return fmt.Errorf("unsupported employee snapshot schema version %d", snapshot.SchemaVersion)
	}
	if err := Validate(snapshot.Employee); err != nil {
		return fmt.Errorf("snapshot employee: %w", err)
	}
	if snapshot.EmployeeID != snapshot.Employee.ID ||
		snapshot.Revision != snapshot.Employee.Revision ||
		!snapshot.CapturedAt.Equal(snapshot.Employee.UpdatedAt) {
		return errors.New("employee snapshot identity does not match embedded revision")
	}
	if err := validateSnapshotBindings(snapshot.Employee, snapshot.ProjectBindings); err != nil {
		return err
	}
	if !validSHA256(snapshot.Digest) {
		return errors.New("employee snapshot digest must be a SHA-256 hex value")
	}
	expected, err := snapshotDigest(snapshot)
	if err != nil {
		return err
	}
	if snapshot.Digest != expected {
		return errors.New("employee snapshot digest mismatch")
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode employee snapshot for validation: %w", err)
	}
	if len(raw) > MaxSnapshotBytes {
		return errors.New("employee snapshot exceeds size limit")
	}
	return nil
}

// VerifyDigest reports whether the snapshot remains a valid sealed value.
func (snapshot RevisionSnapshot) VerifyDigest() bool {
	return ValidateRevisionSnapshot(snapshot) == nil
}

// Clone returns a fully independent copy. Its digest remains valid until a
// caller changes the clone.
func (snapshot RevisionSnapshot) Clone() RevisionSnapshot {
	snapshot.Employee = cloneEmployee(snapshot.Employee)
	snapshot.ProjectBindings = cloneProjectBindings(snapshot.ProjectBindings)
	return snapshot
}

func validateSnapshotBindings(employee Employee, bindings []ProjectBinding) error {
	if len(bindings) != len(employee.ProjectBindingIDs) {
		return errors.New("employee snapshot must contain exactly its referenced project bindings")
	}
	expected := make(map[string]struct{}, len(employee.ProjectBindingIDs))
	for _, id := range employee.ProjectBindingIDs {
		expected[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if err := ValidateProjectBinding(binding); err != nil {
			return fmt.Errorf("snapshot project binding %q: %w", binding.ID, err)
		}
		if binding.EmployeeID != employee.ID {
			return fmt.Errorf("snapshot project binding %q belongs to another employee", binding.ID)
		}
		if _, exists := expected[binding.ID]; !exists {
			return fmt.Errorf("snapshot contains unreferenced project binding %q", binding.ID)
		}
		if _, exists := seen[binding.ID]; exists {
			return fmt.Errorf("snapshot contains duplicate project binding %q", binding.ID)
		}
		seen[binding.ID] = struct{}{}
	}
	return nil
}

func snapshotDigest(snapshot RevisionSnapshot) (string, error) {
	body := struct {
		SchemaVersion   int              `json:"schema_version"`
		EmployeeID      string           `json:"employee_id"`
		Revision        int              `json:"revision"`
		CapturedAt      time.Time        `json:"captured_at"`
		Employee        Employee         `json:"employee"`
		ProjectBindings []ProjectBinding `json:"project_bindings,omitempty"`
	}{
		SchemaVersion:   snapshot.SchemaVersion,
		EmployeeID:      snapshot.EmployeeID,
		Revision:        snapshot.Revision,
		CapturedAt:      snapshot.CapturedAt,
		Employee:        snapshot.Employee,
		ProjectBindings: snapshot.ProjectBindings,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode employee snapshot digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cloneProjectBindings(bindings []ProjectBinding) []ProjectBinding {
	if bindings == nil {
		return nil
	}
	out := make([]ProjectBinding, len(bindings))
	for i, binding := range bindings {
		out[i] = binding
		out[i].AllowedToolCapabilities = cloneStrings(binding.AllowedToolCapabilities)
		if binding.BudgetOverride != nil {
			override := *binding.BudgetOverride
			out[i].BudgetOverride = &override
		}
	}
	return out
}
