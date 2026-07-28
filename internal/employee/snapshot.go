package employee

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SnapshotSchemaVersion = 1
	MaxSnapshotBytes      = 256 << 10
	// MaxCompactSnapshotBytes is an independent Session/hidden-Worker recovery
	// limit. A compact snapshot must never become a repeated full revision.
	MaxCompactSnapshotBytes      = 64 << 10
	CompactSnapshotSchemaVersion = 1
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

// CompactSnapshot is the immutable, model-context-ready subset a schema-v6
// Session needs to recover an EmployeeTask. It deliberately excludes the full
// Employee revision, lifecycle state, credentials, and execution bindings.
type CompactSnapshot struct {
	SchemaVersion      int                `json:"schema_version"`
	EmployeeID         string             `json:"employee_id"`
	EmployeeRevision   int                `json:"employee_revision"`
	TaskID             string             `json:"task_id"`
	TaskSnapshotDigest string             `json:"task_snapshot_digest"`
	Identity           CompactIdentity    `json:"identity"`
	EffectivePolicy    EffectivePolicy    `json:"effective_policy"`
	Budget             BudgetPolicy       `json:"budget"`
	Project            CompactProject     `json:"project"`
	Skills             []CompactSkill     `json:"skills"`
	Knowledge          []CompactKnowledge `json:"knowledge"`
	Memory             []CompactMemory    `json:"memory"`
	Digest             string             `json:"digest"`
}

type CompactIdentity struct {
	Name               string   `json:"name"`
	JobTitle           string   `json:"job_title"`
	Charter            string   `json:"charter"`
	Responsibilities   []string `json:"responsibilities"`
	BehaviorBoundaries []string `json:"behavior_boundaries"`
}

type CompactProject struct {
	BindingID            string `json:"binding_id"`
	WorkspaceFingerprint string `json:"workspace_fingerprint"`
	ReadAllowed          bool   `json:"read_allowed"`
	MutationAllowed      bool   `json:"mutation_allowed"`
	NetworkAllowed       bool   `json:"network_allowed"`
	WorkspaceSummary     string `json:"workspace_summary"`
}

type CompactSkillReference struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type CompactSkill struct {
	SkillID      string                  `json:"skill_id"`
	Version      string                  `json:"version"`
	Digest       string                  `json:"digest"`
	Instructions string                  `json:"instructions"`
	References   []CompactSkillReference `json:"references"`
}

type CompactKnowledge struct {
	SourceID     string `json:"source_id"`
	SourceDigest string `json:"source_digest"`
	CitationID   string `json:"citation_id"`
	Digest       string `json:"digest"`
	Title        string `json:"title"`
	Path         string `json:"path"`
	StartLine    int    `json:"start_line"`
	EndLine      int    `json:"end_line"`
	Snippet      string `json:"snippet"`
}

type CompactMemory struct {
	FactID     string `json:"fact_id"`
	Digest     string `json:"digest"`
	Category   string `json:"category"`
	Value      string `json:"value"`
	Provenance string `json:"provenance"`
}

// SealCompactSnapshot canonicalizes and hashes a newly built compact
// snapshot. Callers cannot use it to make invalid or oversized content valid.
func SealCompactSnapshot(snapshot *CompactSnapshot) error {
	if snapshot == nil {
		return errors.New("compact snapshot is required")
	}
	normalizeCompactSnapshot(snapshot)
	snapshot.Digest = ""
	digest, err := compactSnapshotDigest(*snapshot)
	if err != nil {
		return err
	}
	snapshot.Digest = digest
	return ValidateCompactSnapshot(*snapshot)
}

func (snapshot CompactSnapshot) Clone() CompactSnapshot {
	snapshot.Identity.Responsibilities = cloneStrings(snapshot.Identity.Responsibilities)
	snapshot.Identity.BehaviorBoundaries = cloneStrings(snapshot.Identity.BehaviorBoundaries)
	snapshot.EffectivePolicy.AllowedCapabilities = cloneStrings(snapshot.EffectivePolicy.AllowedCapabilities)
	if snapshot.Skills != nil {
		skills := make([]CompactSkill, len(snapshot.Skills))
		copy(skills, snapshot.Skills)
		for index := range skills {
			if skills[index].References != nil {
				skills[index].References = append([]CompactSkillReference{}, skills[index].References...)
			}
		}
		snapshot.Skills = skills
	}
	if snapshot.Knowledge != nil {
		snapshot.Knowledge = append([]CompactKnowledge{}, snapshot.Knowledge...)
	}
	if snapshot.Memory != nil {
		snapshot.Memory = append([]CompactMemory{}, snapshot.Memory...)
	}
	return snapshot
}

// ValidateCompactSnapshot verifies identity, canonical ordering, content
// bounds, the independent 64 KiB ceiling, and the deterministic digest.
func ValidateCompactSnapshot(snapshot CompactSnapshot) error {
	if snapshot.SchemaVersion != CompactSnapshotSchemaVersion {
		return fmt.Errorf("unsupported compact snapshot schema version %d", snapshot.SchemaVersion)
	}
	if err := validateIdentifier("compact snapshot Employee id", snapshot.EmployeeID); err != nil {
		return err
	}
	if err := validateIdentifier("compact snapshot Task id", snapshot.TaskID); err != nil {
		return err
	}
	if snapshot.EmployeeRevision < 1 || !canonicalDigest(snapshot.TaskSnapshotDigest) {
		return errors.New("compact snapshot Task identity is invalid")
	}
	for name, value := range map[string]string{
		"name": snapshot.Identity.Name, "job title": snapshot.Identity.JobTitle,
		"charter": snapshot.Identity.Charter, "workspace summary": snapshot.Project.WorkspaceSummary,
	} {
		if err := validateCompactText(name, value, 16<<10, name == "workspace summary"); err != nil {
			return err
		}
	}
	for _, value := range append(append([]string{}, snapshot.Identity.Responsibilities...), snapshot.Identity.BehaviorBoundaries...) {
		if err := validateCompactText("identity item", value, 4<<10, false); err != nil {
			return err
		}
	}
	if err := ValidateRequestedCapabilities(snapshot.EffectivePolicy.AllowedCapabilities); err != nil {
		return fmt.Errorf("compact effective policy: %w", err)
	}
	if !sort.StringsAreSorted(snapshot.EffectivePolicy.AllowedCapabilities) {
		return errors.New("compact effective capabilities are not canonical")
	}
	if err := validateBudgetPolicy(snapshot.Budget); err != nil {
		return fmt.Errorf("compact budget: %w", err)
	}
	if err := validateIdentifier("compact ProjectBinding id", snapshot.Project.BindingID); err != nil ||
		!canonicalDigest(snapshot.Project.WorkspaceFingerprint) {
		return errors.New("compact project identity is invalid")
	}
	if err := validateCompactCollections(snapshot); err != nil {
		return err
	}
	if !canonicalDigest(snapshot.Digest) {
		return errors.New("compact snapshot digest is invalid")
	}
	expected, err := compactSnapshotDigest(snapshot)
	if err != nil {
		return err
	}
	if snapshot.Digest != expected {
		return errors.New("compact snapshot digest mismatch")
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > MaxCompactSnapshotBytes {
		return errors.New("compact snapshot exceeds 64 KiB")
	}
	return nil
}

func validateCompactCollections(snapshot CompactSnapshot) error {
	if snapshot.Identity.Responsibilities == nil || snapshot.Identity.BehaviorBoundaries == nil ||
		snapshot.Skills == nil || snapshot.Knowledge == nil || snapshot.Memory == nil {
		return errors.New("compact snapshot collections must be canonical arrays")
	}
	lastSkill := ""
	for _, item := range snapshot.Skills {
		key := item.SkillID + "\x00" + item.Version
		if err := validateIdentifier("compact Skill id", item.SkillID); err != nil {
			return err
		}
		if err := validateIdentifier("compact Skill version", item.Version); err != nil {
			return err
		}
		if key <= lastSkill || !canonicalDigest(item.Digest) {
			return errors.New("compact Skills are invalid or not strictly sorted")
		}
		lastSkill = key
		if err := validateCompactText("compact Skill instructions", item.Instructions, 64<<10, false); err != nil {
			return err
		}
		if item.References == nil {
			return errors.New("compact Skill references must be a canonical array")
		}
		lastReference := ""
		for _, reference := range item.References {
			if reference.Path <= lastReference || !strings.HasPrefix(reference.Path, "references/") ||
				!validTaskReferencePath(reference.Path) {
				return errors.New("compact Skill references are invalid or not strictly sorted")
			}
			lastReference = reference.Path
			if err := validateCompactText("compact Skill reference", reference.Content, 64<<10, false); err != nil {
				return err
			}
		}
	}
	lastKnowledge := ""
	for _, item := range snapshot.Knowledge {
		key := item.SourceID + "\x00" + item.CitationID
		if err := validateIdentifier("compact Knowledge source id", item.SourceID); err != nil {
			return err
		}
		if err := validateIdentifier("compact Citation id", item.CitationID); err != nil {
			return err
		}
		if key <= lastKnowledge || !canonicalDigest(item.SourceDigest) || !canonicalDigest(item.Digest) ||
			!validTaskReferencePath(item.Path) || item.StartLine < 1 || item.EndLine < item.StartLine {
			return errors.New("compact Knowledge is invalid or not strictly sorted")
		}
		lastKnowledge = key
		if err := validateCompactText("compact Knowledge title", item.Title, 16<<10, false); err != nil {
			return err
		}
		if err := validateCompactText("compact Knowledge snippet", item.Snippet, 16<<10, false); err != nil {
			return err
		}
	}
	lastMemory := ""
	for _, item := range snapshot.Memory {
		if err := validateIdentifier("compact Memory Fact id", item.FactID); err != nil {
			return err
		}
		if err := validateIdentifier("compact Memory category", item.Category); err != nil {
			return err
		}
		if item.FactID <= lastMemory || !canonicalDigest(item.Digest) {
			return errors.New("compact Memory Facts are invalid or not strictly sorted")
		}
		lastMemory = item.FactID
		for name, value := range map[string]string{
			"compact Memory category": item.Category, "compact Memory value": item.Value,
			"compact Memory provenance": item.Provenance,
		} {
			if err := validateCompactText(name, value, 8<<10, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeCompactSnapshot(snapshot *CompactSnapshot) {
	snapshot.EffectivePolicy.AllowedCapabilities = normalizeCapabilities(snapshot.EffectivePolicy.AllowedCapabilities)
	sort.Slice(snapshot.Skills, func(i, j int) bool {
		if snapshot.Skills[i].SkillID == snapshot.Skills[j].SkillID {
			return snapshot.Skills[i].Version < snapshot.Skills[j].Version
		}
		return snapshot.Skills[i].SkillID < snapshot.Skills[j].SkillID
	})
	for index := range snapshot.Skills {
		sort.Slice(snapshot.Skills[index].References, func(i, j int) bool {
			return snapshot.Skills[index].References[i].Path < snapshot.Skills[index].References[j].Path
		})
		if snapshot.Skills[index].References == nil {
			snapshot.Skills[index].References = []CompactSkillReference{}
		}
	}
	sort.Slice(snapshot.Knowledge, func(i, j int) bool {
		if snapshot.Knowledge[i].SourceID == snapshot.Knowledge[j].SourceID {
			return snapshot.Knowledge[i].CitationID < snapshot.Knowledge[j].CitationID
		}
		return snapshot.Knowledge[i].SourceID < snapshot.Knowledge[j].SourceID
	})
	sort.Slice(snapshot.Memory, func(i, j int) bool { return snapshot.Memory[i].FactID < snapshot.Memory[j].FactID })
	if snapshot.Identity.Responsibilities == nil {
		snapshot.Identity.Responsibilities = []string{}
	}
	if snapshot.Identity.BehaviorBoundaries == nil {
		snapshot.Identity.BehaviorBoundaries = []string{}
	}
	if snapshot.Skills == nil {
		snapshot.Skills = []CompactSkill{}
	}
	if snapshot.Knowledge == nil {
		snapshot.Knowledge = []CompactKnowledge{}
	}
	if snapshot.Memory == nil {
		snapshot.Memory = []CompactMemory{}
	}
}

func compactSnapshotDigest(snapshot CompactSnapshot) (string, error) {
	snapshot.Digest = ""
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode compact snapshot digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validateCompactText(name, value string, maximum int, allowEmpty bool) error {
	if (!allowEmpty && strings.TrimSpace(value) == "") || len(value) > maximum ||
		!utf8.ValidString(value) || strings.ContainsRune(value, '\x00') ||
		strings.ContainsRune(value, unicode.ReplacementChar) || looksSecret(value) {
		return fmt.Errorf("%s is invalid or exceeds its limit", name)
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"private reasoning:", "chain of thought:", "raw tool arguments:",
		"raw tool output:", "hidden system prompt:", "full system prompt:",
	} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("%s contains private runtime data", name)
		}
	}
	return nil
}

func canonicalDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
