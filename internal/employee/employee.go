// Package employee defines the pure Electronic Employee domain. It owns no
// persistence, runtime, Session, Run, HTTP, or presentation behavior.
package employee

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	SchemaVersion         = 1
	MaxEmployeeBytes      = 256 << 10
	MaxTextBytes          = 8 << 10
	MaxIDBytes            = 128
	MaxAvatarBytes        = 64
	MaxCollectionItems    = 64
	MaxSkillBindings      = 64
	MaxProjectBindings    = 64
	MaxSkillConfigBytes   = 32 << 10
	MaxWorkspacePathBytes = 4 << 10
)

var (
	ErrInvalidTransition = errors.New("invalid employee state transition")
	ErrArchived          = errors.New("employee is archived")
	ErrImmutableField    = errors.New("employee immutable field changed")
)

type State string

const (
	StateActive   State = "active"
	StateDisabled State = "disabled"
	StateArchived State = "archived"
)

type AvatarKind string

const (
	AvatarInitials AvatarKind = "initials"
	AvatarEmoji    AvatarKind = "emoji"
)

type Avatar struct {
	Kind  AvatarKind `json:"kind"`
	Value string     `json:"value"`
}

// ModelSelection contains names only. Credentials stay in the existing
// server-side credential store.
type ModelSelection struct {
	Company string `json:"company"`
	Access  string `json:"access"`
	Model   string `json:"model"`
}

// SkillBinding is a pinned declarative reference. Skill discovery, manifest
// validation, and capability intersection belong to Phase 3.
type SkillBinding struct {
	SkillID       string          `json:"skill_id"`
	Version       string          `json:"version"`
	Digest        string          `json:"digest"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	Enabled       bool            `json:"enabled"`
}

// Employee is an owner-scoped identity and policy, not a Role, Session,
// model, Skill, Project, process, or execution state machine.
type Employee struct {
	ID                 string            `json:"id"`
	SchemaVersion      int               `json:"schema_version"`
	Revision           int               `json:"revision"`
	State              State             `json:"state"`
	Name               string            `json:"name"`
	Avatar             Avatar            `json:"avatar"`
	JobTitle           string            `json:"job_title"`
	Charter            string            `json:"charter"`
	Responsibilities   []string          `json:"responsibilities,omitempty"`
	BehaviorBoundaries []string          `json:"behavior_boundaries,omitempty"`
	DefaultSelection   ModelSelection    `json:"default_selection"`
	AgentProfile       string            `json:"agent_profile"`
	SkillBindings      []SkillBinding    `json:"skill_bindings,omitempty"`
	ProjectBindingIDs  []string          `json:"project_binding_ids,omitempty"`
	PermissionPolicy   PermissionPolicy  `json:"permission_policy"`
	BudgetPolicy       BudgetPolicy      `json:"budget_policy"`
	ConcurrencyPolicy  ConcurrencyPolicy `json:"concurrency_policy"`
	MemoryPolicy       MemoryPolicy      `json:"memory_policy"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	DisabledAt         *time.Time        `json:"disabled_at,omitempty"`
	ArchivedAt         *time.Time        `json:"archived_at,omitempty"`
}

// ProjectBinding grants one Employee a narrowing policy for one canonical
// workspace. Phase 6 additionally requires equality with Service.Workspace.
type ProjectBinding struct {
	ID                      string        `json:"id"`
	EmployeeID              string        `json:"employee_id"`
	Label                   string        `json:"label"`
	WorkspaceRealPath       string        `json:"workspace_real_path"`
	WorkspaceFingerprint    string        `json:"workspace_fingerprint"`
	ReadAllowed             bool          `json:"read_allowed"`
	MutationAllowed         bool          `json:"mutation_allowed"`
	AllowedToolCapabilities []string      `json:"allowed_tool_capabilities,omitempty"`
	NetworkAllowed          bool          `json:"network_allowed"`
	BudgetOverride          *BudgetPolicy `json:"budget_override,omitempty"`
	CreatedAt               time.Time     `json:"created_at"`
	UpdatedAt               time.Time     `json:"updated_at"`
}

// Create normalizes a new Employee and starts its immutable revision history
// at revision one.
func Create(draft Employee, now time.Time) (Employee, error) {
	if now.IsZero() {
		return Employee{}, errors.New("employee creation time is required")
	}
	employee := normalizeEmployee(draft)
	employee.SchemaVersion = SchemaVersion
	employee.Revision = 1
	employee.State = StateActive
	employee.CreatedAt = now.UTC()
	employee.UpdatedAt = now.UTC()
	employee.DisabledAt = nil
	employee.ArchivedAt = nil
	if err := Validate(employee); err != nil {
		return Employee{}, err
	}
	return employee, nil
}

// Revise applies editable fields while preserving identity and lifecycle
// metadata. Lifecycle state changes require Disable, Enable, or Archive.
func Revise(current, proposed Employee, now time.Time) (Employee, error) {
	if err := Validate(current); err != nil {
		return Employee{}, fmt.Errorf("current employee: %w", err)
	}
	if current.State == StateArchived {
		return Employee{}, ErrArchived
	}
	if proposed.ID != current.ID ||
		proposed.SchemaVersion != current.SchemaVersion ||
		proposed.Revision != current.Revision ||
		proposed.State != current.State ||
		!proposed.CreatedAt.Equal(current.CreatedAt) ||
		!equalTimePointer(proposed.DisabledAt, current.DisabledAt) ||
		!equalTimePointer(proposed.ArchivedAt, current.ArchivedAt) {
		return Employee{}, ErrImmutableField
	}
	if err := validateTransitionTime(current, now); err != nil {
		return Employee{}, err
	}
	revised := normalizeEmployee(proposed)
	revised.SchemaVersion = current.SchemaVersion
	revised.Revision = current.Revision + 1
	revised.State = current.State
	revised.CreatedAt = current.CreatedAt
	revised.UpdatedAt = now.UTC()
	revised.DisabledAt = cloneTimePointer(current.DisabledAt)
	revised.ArchivedAt = cloneTimePointer(current.ArchivedAt)
	if err := Validate(revised); err != nil {
		return Employee{}, err
	}
	return revised, nil
}

func Disable(current Employee, now time.Time) (Employee, error) {
	if err := Validate(current); err != nil {
		return Employee{}, fmt.Errorf("current employee: %w", err)
	}
	if current.State == StateArchived {
		return Employee{}, ErrArchived
	}
	if current.State != StateActive {
		return Employee{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.State, StateDisabled)
	}
	next, err := transition(current, StateDisabled, now)
	if err != nil {
		return Employee{}, err
	}
	next.DisabledAt = timePointer(now.UTC())
	return validatedTransition(next)
}

func Enable(current Employee, now time.Time) (Employee, error) {
	if err := Validate(current); err != nil {
		return Employee{}, fmt.Errorf("current employee: %w", err)
	}
	if current.State == StateArchived {
		return Employee{}, ErrArchived
	}
	if current.State != StateDisabled {
		return Employee{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.State, StateActive)
	}
	next, err := transition(current, StateActive, now)
	if err != nil {
		return Employee{}, err
	}
	next.DisabledAt = nil
	return validatedTransition(next)
}

func Archive(current Employee, now time.Time) (Employee, error) {
	if err := Validate(current); err != nil {
		return Employee{}, fmt.Errorf("current employee: %w", err)
	}
	if current.State == StateArchived {
		return Employee{}, ErrArchived
	}
	if current.State != StateActive && current.State != StateDisabled {
		return Employee{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.State, StateArchived)
	}
	next, err := transition(current, StateArchived, now)
	if err != nil {
		return Employee{}, err
	}
	next.ArchivedAt = timePointer(now.UTC())
	return validatedTransition(next)
}

func transition(current Employee, state State, now time.Time) (Employee, error) {
	if err := validateTransitionTime(current, now); err != nil {
		return Employee{}, err
	}
	next := cloneEmployee(current)
	next.State = state
	next.Revision++
	next.UpdatedAt = now.UTC()
	return next, nil
}

func validatedTransition(next Employee) (Employee, error) {
	if err := Validate(next); err != nil {
		return Employee{}, err
	}
	return next, nil
}

func validateTransitionTime(current Employee, now time.Time) error {
	if now.IsZero() {
		return errors.New("employee transition time is required")
	}
	if now.Before(current.UpdatedAt) {
		return errors.New("employee transition time precedes updated_at")
	}
	return nil
}

// Validate enforces the complete Employee v1 domain contract.
func Validate(employee Employee) error {
	if employee.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported employee schema version %d", employee.SchemaVersion)
	}
	if employee.Revision < 1 {
		return errors.New("employee revision must be positive")
	}
	if err := validateIdentifier("employee id", employee.ID); err != nil {
		return err
	}
	required := []struct {
		label string
		value string
	}{
		{"name", employee.Name},
		{"job_title", employee.JobTitle},
		{"charter", employee.Charter},
		{"selection company", employee.DefaultSelection.Company},
		{"selection access", employee.DefaultSelection.Access},
		{"selection model", employee.DefaultSelection.Model},
		{"agent_profile", employee.AgentProfile},
	}
	for _, field := range required {
		if err := validateRequiredText(field.label, field.value); err != nil {
			return err
		}
	}
	if err := validateAvatar(employee.Name, employee.Avatar); err != nil {
		return err
	}
	if err := validateTexts("responsibilities", employee.Responsibilities); err != nil {
		return err
	}
	if err := validateTexts("behavior_boundaries", employee.BehaviorBoundaries); err != nil {
		return err
	}
	if err := validateSkillBindings(employee.SkillBindings); err != nil {
		return err
	}
	if err := validateIdentifierList("project_binding_ids", employee.ProjectBindingIDs, MaxProjectBindings); err != nil {
		return err
	}
	if err := validatePermissionPolicy(employee.PermissionPolicy); err != nil {
		return err
	}
	if err := validateBudgetPolicy(employee.BudgetPolicy); err != nil {
		return fmt.Errorf("employee budget policy: %w", err)
	}
	if err := validateConcurrencyPolicy(employee.ConcurrencyPolicy); err != nil {
		return err
	}
	if err := validateMemoryPolicy(employee.MemoryPolicy); err != nil {
		return err
	}
	if employee.CreatedAt.IsZero() || employee.UpdatedAt.IsZero() {
		return errors.New("employee created_at and updated_at are required")
	}
	if employee.UpdatedAt.Before(employee.CreatedAt) {
		return errors.New("employee updated_at precedes created_at")
	}
	if !lifecycleTimeValid(employee.DisabledAt, employee.CreatedAt, employee.UpdatedAt) ||
		!lifecycleTimeValid(employee.ArchivedAt, employee.CreatedAt, employee.UpdatedAt) {
		return errors.New("employee lifecycle timestamp is outside its revision lifetime")
	}
	switch employee.State {
	case StateActive:
		if employee.DisabledAt != nil || employee.ArchivedAt != nil {
			return errors.New("active employee cannot have disabled_at or archived_at")
		}
	case StateDisabled:
		if employee.DisabledAt == nil || employee.ArchivedAt != nil {
			return errors.New("disabled employee requires disabled_at and no archived_at")
		}
	case StateArchived:
		if employee.ArchivedAt == nil {
			return errors.New("archived employee requires archived_at")
		}
	default:
		return fmt.Errorf("unsupported employee state %q", employee.State)
	}
	raw, err := json.MarshalIndent(employee, "", "  ")
	if err != nil {
		return fmt.Errorf("encode employee for validation: %w", err)
	}
	if len(raw) > MaxEmployeeBytes {
		return errors.New("employee exceeds size limit")
	}
	return nil
}

// CreateProjectBinding normalizes a canonical workspace grant. Resolving
// symlinks and matching Service.Workspace remain readiness responsibilities.
func CreateProjectBinding(draft ProjectBinding, now time.Time) (ProjectBinding, error) {
	if now.IsZero() {
		return ProjectBinding{}, errors.New("project binding creation time is required")
	}
	binding := normalizeProjectBinding(draft)
	binding.WorkspaceFingerprint = workspaceFingerprint(binding.WorkspaceRealPath)
	binding.CreatedAt = now.UTC()
	binding.UpdatedAt = now.UTC()
	if err := ValidateProjectBinding(binding); err != nil {
		return ProjectBinding{}, err
	}
	return binding, nil
}

func ValidateProjectBinding(binding ProjectBinding) error {
	if err := validateIdentifier("project binding id", binding.ID); err != nil {
		return err
	}
	if err := validateIdentifier("project binding employee_id", binding.EmployeeID); err != nil {
		return err
	}
	if err := validateRequiredText("project binding label", binding.Label); err != nil {
		return err
	}
	path := binding.WorkspaceRealPath
	if path == "" || len(path) > MaxWorkspacePathBytes || !filepath.IsAbs(path) {
		return errors.New("project binding workspace_real_path must be a bounded absolute path")
	}
	if filepath.Clean(path) != path {
		return errors.New("project binding workspace_real_path must be canonical and clean")
	}
	if binding.WorkspaceFingerprint != workspaceFingerprint(path) {
		return errors.New("project binding workspace fingerprint does not match canonical path")
	}
	if err := validateProjectPolicy(ProjectPolicy{
		ReadAllowed:             binding.ReadAllowed,
		MutationAllowed:         binding.MutationAllowed,
		AllowedToolCapabilities: binding.AllowedToolCapabilities,
		NetworkAllowed:          binding.NetworkAllowed,
		BudgetOverride:          binding.BudgetOverride,
	}); err != nil {
		return err
	}
	if binding.CreatedAt.IsZero() || binding.UpdatedAt.IsZero() || binding.UpdatedAt.Before(binding.CreatedAt) {
		return errors.New("project binding timestamps are invalid")
	}
	return nil
}

// MatchesCanonicalWorkspace performs the final exact-identity comparison after
// the caller has resolved Service.Workspace to its canonical real path.
func (binding ProjectBinding) MatchesCanonicalWorkspace(workspace string) bool {
	return workspace != "" &&
		filepath.IsAbs(workspace) &&
		filepath.Clean(workspace) == workspace &&
		binding.WorkspaceRealPath == workspace &&
		binding.WorkspaceFingerprint == workspaceFingerprint(workspace)
}

func normalizeEmployee(employee Employee) Employee {
	employee = cloneEmployee(employee)
	employee.ID = clean(employee.ID)
	employee.Name = clean(employee.Name)
	employee.JobTitle = clean(employee.JobTitle)
	employee.Charter = clean(employee.Charter)
	employee.AgentProfile = clean(employee.AgentProfile)
	employee.DefaultSelection.Company = clean(employee.DefaultSelection.Company)
	employee.DefaultSelection.Access = clean(employee.DefaultSelection.Access)
	employee.DefaultSelection.Model = clean(employee.DefaultSelection.Model)
	employee.Avatar.Kind = AvatarKind(clean(string(employee.Avatar.Kind)))
	employee.Avatar.Value = clean(employee.Avatar.Value)
	if employee.Avatar.Kind == AvatarInitials {
		employee.Avatar.Value = initials(employee.Name)
	}
	employee.Responsibilities = normalizeTextList(employee.Responsibilities)
	employee.BehaviorBoundaries = normalizeTextList(employee.BehaviorBoundaries)
	for i := range employee.SkillBindings {
		binding := &employee.SkillBindings[i]
		binding.SkillID = clean(binding.SkillID)
		binding.Version = clean(binding.Version)
		binding.Digest = strings.ToLower(clean(binding.Digest))
	}
	sort.Slice(employee.SkillBindings, func(i, j int) bool {
		if employee.SkillBindings[i].SkillID == employee.SkillBindings[j].SkillID {
			return employee.SkillBindings[i].Version < employee.SkillBindings[j].Version
		}
		return employee.SkillBindings[i].SkillID < employee.SkillBindings[j].SkillID
	})
	employee.ProjectBindingIDs = normalizeIdentifierList(employee.ProjectBindingIDs)
	employee.PermissionPolicy = normalizePermissionPolicy(employee.PermissionPolicy)
	return employee
}

func normalizeProjectBinding(binding ProjectBinding) ProjectBinding {
	binding.ID = clean(binding.ID)
	binding.EmployeeID = clean(binding.EmployeeID)
	binding.Label = clean(binding.Label)
	binding.WorkspaceRealPath = clean(binding.WorkspaceRealPath)
	policy := normalizeProjectPolicy(ProjectPolicy{
		ReadAllowed:             binding.ReadAllowed,
		MutationAllowed:         binding.MutationAllowed,
		AllowedToolCapabilities: binding.AllowedToolCapabilities,
		NetworkAllowed:          binding.NetworkAllowed,
		BudgetOverride:          binding.BudgetOverride,
	})
	binding.AllowedToolCapabilities = policy.AllowedToolCapabilities
	binding.BudgetOverride = policy.BudgetOverride
	return binding
}

func cloneEmployee(employee Employee) Employee {
	employee.Responsibilities = cloneStrings(employee.Responsibilities)
	employee.BehaviorBoundaries = cloneStrings(employee.BehaviorBoundaries)
	employee.ProjectBindingIDs = cloneStrings(employee.ProjectBindingIDs)
	employee.PermissionPolicy.AllowedCapabilities = cloneStrings(employee.PermissionPolicy.AllowedCapabilities)
	if employee.SkillBindings != nil {
		bindings := make([]SkillBinding, len(employee.SkillBindings))
		for i, binding := range employee.SkillBindings {
			bindings[i] = binding
			bindings[i].Configuration = append(json.RawMessage(nil), binding.Configuration...)
		}
		employee.SkillBindings = bindings
	}
	employee.DisabledAt = cloneTimePointer(employee.DisabledAt)
	employee.ArchivedAt = cloneTimePointer(employee.ArchivedAt)
	return employee
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func validateAvatar(name string, avatar Avatar) error {
	switch avatar.Kind {
	case AvatarInitials:
		if avatar.Value != initials(name) {
			return errors.New("initials avatar must equal generated employee initials")
		}
	case AvatarEmoji:
		if !isEmojiAvatar(avatar.Value) {
			return errors.New("emoji avatar must contain one bounded emoji and no path or URL")
		}
	default:
		return fmt.Errorf("unsupported avatar kind %q", avatar.Kind)
	}
	return nil
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	first, _ := utf8.DecodeRuneInString(parts[0])
	out := string(unicode.ToUpper(first))
	if len(parts) > 1 {
		last, _ := utf8.DecodeRuneInString(parts[len(parts)-1])
		out += string(unicode.ToUpper(last))
	}
	return out
}

func isEmojiAvatar(value string) bool {
	if value == "" || len(value) > MaxAvatarBytes || strings.ContainsAny(value, `/\:`) || strings.Contains(strings.ToLower(value), "http") {
		return false
	}
	hasSymbol := false
	runes := 0
	for _, r := range value {
		runes++
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
		if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) {
			hasSymbol = true
		}
	}
	return hasSymbol && runes <= 8
}

func validateSkillBindings(bindings []SkillBinding) error {
	if len(bindings) > MaxSkillBindings {
		return fmt.Errorf("employee exceeds %d skill bindings", MaxSkillBindings)
	}
	seen := make(map[string]struct{}, len(bindings))
	totalConfigBytes := 0
	for _, binding := range bindings {
		if err := validateIdentifier("skill id", binding.SkillID); err != nil {
			return err
		}
		if _, exists := seen[binding.SkillID]; exists {
			return fmt.Errorf("duplicate skill binding %q", binding.SkillID)
		}
		seen[binding.SkillID] = struct{}{}
		if err := validateRequiredText("skill version", binding.Version); err != nil {
			return err
		}
		if !validSHA256(binding.Digest) {
			return fmt.Errorf("skill %q digest must be a SHA-256 hex value", binding.SkillID)
		}
		if len(binding.Configuration) > 0 {
			totalConfigBytes += len(binding.Configuration)
			if !json.Valid(binding.Configuration) {
				return fmt.Errorf("skill %q configuration must be valid JSON", binding.SkillID)
			}
			if looksSecret(string(binding.Configuration)) {
				return fmt.Errorf("skill %q configuration must not contain credentials or tokens", binding.SkillID)
			}
		}
	}
	if totalConfigBytes > MaxSkillConfigBytes {
		return errors.New("skill binding configuration exceeds size limit")
	}
	return nil
}

func validateTexts(label string, values []string) error {
	if len(values) > MaxCollectionItems {
		return fmt.Errorf("%s exceeds %d items", label, MaxCollectionItems)
	}
	for _, value := range values {
		if err := validateRequiredText(label, value); err != nil {
			return err
		}
	}
	return nil
}

func validateRequiredText(label, value string) error {
	if clean(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(value) > MaxTextBytes {
		return fmt.Errorf("%s exceeds size limit", label)
	}
	if looksSecret(value) {
		return fmt.Errorf("%s must not contain credentials, tokens, or private keys", label)
	}
	return nil
}

func validateIdentifier(label, value string) error {
	if value == "" || len(value) > MaxIDBytes {
		return fmt.Errorf("%s must be non-empty and at most %d bytes", label, MaxIDBytes)
	}
	if looksSecret(value) {
		return fmt.Errorf("%s must not contain credentials or tokens", label)
	}
	for _, r := range value {
		if isASCIIAlphaNumeric(r) || strings.ContainsRune("_-.", r) {
			continue
		}
		return fmt.Errorf("%s contains an unsupported character", label)
	}
	return nil
}

func validateIdentifierList(label string, values []string, maximum int) error {
	if len(values) > maximum {
		return fmt.Errorf("%s exceeds %d items", label, maximum)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateIdentifier(label, value); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func normalizeTextList(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = clean(value)
	}
	return out
}

func normalizeIdentifierList(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = clean(value)
	}
	sort.Strings(out)
	return out
}

func looksSecret(value string) bool {
	lower := strings.ToLower(value)
	return owner.LooksSecret(value) ||
		strings.Contains(lower, "private_key=") ||
		(strings.Contains(lower, "-----begin ") && strings.Contains(lower, " private key-----"))
}

func clean(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
}

func isASCIIAlphaNumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func workspaceFingerprint(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePointer(*value)
}

func equalTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func lifecycleTimeValid(value *time.Time, createdAt, updatedAt time.Time) bool {
	return value == nil || !value.Before(createdAt) && !value.After(updatedAt)
}
