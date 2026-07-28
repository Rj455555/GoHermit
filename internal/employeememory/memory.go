// Package employeememory defines bounded, provenance-bearing Employee Memory.
// Persistence and owner confirmation are coordinated by employeestore.
package employeememory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	SchemaVersion     = 1
	MaxFacts          = 512
	MaxCandidates     = 128
	MaxFactFileBytes  = 512 << 10
	MaxCandidateBytes = 256 << 10
	MaxValueBytes     = 8 << 10
	MaxSources        = 16
)

var (
	ErrInvalid = errors.New("invalid Employee Memory")
	ErrCorrupt = errors.New("Employee Memory is corrupt")
	ErrMissing = errors.New("Employee Memory item not found")
)

type Provenance struct {
	SourceType      string    `json:"source_type"`
	SourceID        string    `json:"source_id"`
	SourceTaskID    string    `json:"source_task_id,omitempty"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	SourceRunID     string    `json:"source_run_id,omitempty"`
	VerifiedAt      time.Time `json:"verified_at"`
}

type Candidate struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	EmployeeID    string       `json:"employee_id"`
	Category      string       `json:"category"`
	Value         string       `json:"value"`
	Provenance    []Provenance `json:"provenance"`
	CreatedAt     time.Time    `json:"created_at"`
	Digest        string       `json:"digest"`
}

type Fact struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	CandidateID   string       `json:"candidate_id"`
	EmployeeID    string       `json:"employee_id"`
	Category      string       `json:"category"`
	Value         string       `json:"value"`
	Provenance    []Provenance `json:"provenance"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Digest        string       `json:"digest"`
	OwnerEdited   bool         `json:"owner_edited"`
}

func NewCandidate(value Candidate, now time.Time) (Candidate, error) {
	value.SchemaVersion = SchemaVersion
	value.CreatedAt = now.UTC()
	value.Category = strings.TrimSpace(value.Category)
	value.Value = strings.TrimSpace(value.Value)
	value.Provenance = canonicalProvenance(value.Provenance)
	value.Digest = CandidateDigest(value)
	if err := ValidateCandidate(value); err != nil {
		return Candidate{}, err
	}
	return value, nil
}

func Promote(candidate Candidate, now time.Time) (Fact, error) {
	if err := ValidateCandidate(candidate); err != nil {
		return Fact{}, err
	}
	fact := Fact{
		SchemaVersion: SchemaVersion, ID: "mem-" + candidate.Digest[:24], CandidateID: candidate.ID,
		EmployeeID: candidate.EmployeeID, Category: candidate.Category, Value: candidate.Value,
		Provenance: canonicalProvenance(candidate.Provenance), CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	fact.Digest = FactDigest(fact)
	return fact, ValidateFact(fact)
}

func Edit(fact Fact, value string, now time.Time) (Fact, error) {
	if err := ValidateFact(fact); err != nil {
		return Fact{}, err
	}
	fact.Value = strings.TrimSpace(value)
	fact.UpdatedAt = now.UTC()
	fact.OwnerEdited = true
	fact.Digest = FactDigest(fact)
	return fact, ValidateFact(fact)
}

func ValidateCandidate(value Candidate) error {
	if value.SchemaVersion != SchemaVersion || !validID(value.ID) || !validID(value.EmployeeID) ||
		!validCategory(value.Category) || value.CreatedAt.IsZero() || len(value.Provenance) == 0 ||
		len(value.Provenance) > MaxSources || !validValue(value.Value) {
		return fmt.Errorf("%w: candidate identity, value, or provenance", ErrInvalid)
	}
	if err := validateProvenanceSet(value.Provenance); err != nil {
		return err
	}
	if value.Digest != CandidateDigest(value) {
		return fmt.Errorf("%w: candidate digest mismatch", ErrCorrupt)
	}
	return nil
}

func ValidateFact(value Fact) error {
	if value.SchemaVersion != SchemaVersion || !validID(value.ID) || !validID(value.CandidateID) ||
		!validID(value.EmployeeID) || !validCategory(value.Category) || !validValue(value.Value) ||
		value.CreatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) ||
		len(value.Provenance) == 0 || len(value.Provenance) > MaxSources {
		return fmt.Errorf("%w: fact identity, value, or provenance", ErrInvalid)
	}
	if err := validateProvenanceSet(value.Provenance); err != nil {
		return err
	}
	if value.Digest != FactDigest(value) {
		return fmt.Errorf("%w: fact digest mismatch", ErrCorrupt)
	}
	return nil
}

func CandidateDigest(value Candidate) string {
	parts := []string{value.ID, value.EmployeeID, value.Category, value.Value, value.CreatedAt.UTC().Format(time.RFC3339Nano)}
	parts = append(parts, provenanceParts(value.Provenance)...)
	return digest(strings.Join(parts, "\x00"))
}

func FactDigest(value Fact) string {
	parts := []string{
		value.ID, value.CandidateID, value.EmployeeID, value.Category, value.Value,
		value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano),
		fmt.Sprintf("%t", value.OwnerEdited),
	}
	parts = append(parts, provenanceParts(value.Provenance)...)
	return digest(strings.Join(parts, "\x00"))
}

func SortFacts(values []Fact) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}

func SortCandidates(values []Candidate) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}

func validateProvenance(value Provenance) error {
	if !validID(value.SourceType) || !validID(value.SourceID) || value.VerifiedAt.IsZero() {
		return fmt.Errorf("%w: provenance identity", ErrInvalid)
	}
	for _, id := range []string{value.SourceTaskID, value.SourceSessionID, value.SourceRunID} {
		if id != "" && !validID(id) {
			return fmt.Errorf("%w: provenance reference", ErrInvalid)
		}
	}
	if value.SourceType == "run" && (value.SourceTaskID == "" || value.SourceSessionID == "" || value.SourceRunID == "") {
		return fmt.Errorf("%w: run provenance references", ErrInvalid)
	}
	return nil
}

func validValue(value string) bool {
	if value == "" || len(value) > MaxValueBytes || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') ||
		strings.ContainsRune(value, unicode.ReplacementChar) ||
		owner.LooksSecret(value) {
		return false
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"private reasoning:", "chain of thought:", "raw tool arguments:", "raw_tool_arguments",
		"hidden system prompt:", "full system prompt:",
	} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func validCategory(value string) bool {
	return value != "" && len(value) <= 64 && validID(value)
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

func provenanceParts(values []Provenance) []string {
	copyValues := canonicalProvenance(values)
	result := make([]string, 0, len(copyValues)*6)
	for _, value := range copyValues {
		result = append(result, value.SourceType, value.SourceID, value.SourceTaskID, value.SourceSessionID, value.SourceRunID, value.VerifiedAt.UTC().Format(time.RFC3339Nano))
	}
	return result
}

func validateProvenanceSet(values []Provenance) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateProvenance(value); err != nil {
			return err
		}
		key := provenanceTuple(value)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("%w: duplicate provenance", ErrInvalid)
		}
		seen[key] = struct{}{}
	}
	// The same SourceType/SourceID may identify different verified Runs. The
	// complete tuple, including Task/Session/Run and VerifiedAt, is the identity.
	return nil
}

func canonicalProvenance(values []Provenance) []Provenance {
	copyValues := cloneProvenance(values)
	sort.Slice(copyValues, func(i, j int) bool {
		return provenanceTuple(copyValues[i]) < provenanceTuple(copyValues[j])
	})
	return copyValues
}

func provenanceTuple(value Provenance) string {
	return strings.Join([]string{
		value.SourceType, value.SourceID, value.SourceTaskID, value.SourceSessionID,
		value.SourceRunID, value.VerifiedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
}

func cloneProvenance(values []Provenance) []Provenance {
	return append([]Provenance(nil), values...)
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
