package employee

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	TaskSchemaVersion  = 1
	MaxTaskPromptBytes = 16 << 10
	MaxTaskFileBytes   = 512 << 10
	MaxTaskKnowledge   = 128
	MaxTaskCitations   = 1024
	MaxTaskMemoryFacts = 512
)

var ErrInvalidTaskTransition = errors.New("invalid Employee Task transition")

type TaskState string

const (
	TaskQueued    TaskState = "queued"
	TaskCancelled TaskState = "cancelled"
)

// TaskPolicy is an immutable per-Task narrowing ceiling. It never grants a
// capability or budget beyond the Employee or selected ProjectBinding.
type TaskPolicy struct {
	AllowedCapabilities []string     `json:"allowed_capabilities"`
	NetworkAllowed      bool         `json:"network_allowed"`
	Budget              BudgetPolicy `json:"budget"`
}

// TaskCitationReference pins bounded Citation identity only, never a Knowledge
// corpus or unbounded excerpt.
type TaskCitationReference struct {
	CitationID string `json:"citation_id"`
	Path       string `json:"path"`
	Digest     string `json:"digest"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

type TaskKnowledgeSnapshot struct {
	SourceID     string                  `json:"source_id"`
	SourceDigest string                  `json:"source_digest"`
	Citations    []TaskCitationReference `json:"citations"`
}

type TaskMemoryFactSnapshot struct {
	FactID string `json:"fact_id"`
	Digest string `json:"digest"`
}

// EmployeeTask is a durable pre-dispatch owner request. Phase 5 permits only
// queued and cancelled. Session/Run execution truth is deliberately absent.
type EmployeeTask struct {
	SchemaVersion    int                      `json:"schema_version"`
	ID               string                   `json:"id"`
	EmployeeID       string                   `json:"employee_id"`
	EmployeeRevision int                      `json:"employee_revision"`
	Prompt           string                   `json:"prompt"`
	State            TaskState                `json:"state"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	CancelledAt      *time.Time               `json:"cancelled_at,omitempty"`
	EmployeeSnapshot RevisionSnapshot         `json:"employee_snapshot"`
	Skills           []SkillBinding           `json:"skills"`
	Knowledge        []TaskKnowledgeSnapshot  `json:"knowledge"`
	MemoryFacts      []TaskMemoryFactSnapshot `json:"memory_facts"`
	ProjectBinding   ProjectBinding           `json:"project_binding"`
	Policy           TaskPolicy               `json:"policy"`
	SnapshotDigest   string                   `json:"snapshot_digest"`
	SessionID        string                   `json:"session_id"`
	RunID            string                   `json:"run_id"`
}

func NewTask(draft EmployeeTask, now time.Time) (EmployeeTask, error) {
	if now.IsZero() {
		return EmployeeTask{}, errors.New("Employee Task creation time is required")
	}
	if draft.SchemaVersion != 0 && draft.SchemaVersion != TaskSchemaVersion {
		return EmployeeTask{}, errors.New("unsupported Employee Task request schema")
	}
	if draft.State != "" && draft.State != TaskQueued {
		return EmployeeTask{}, fmt.Errorf("%w: only queued creation is supported", ErrInvalidTaskTransition)
	}
	if !draft.CreatedAt.IsZero() || !draft.UpdatedAt.IsZero() || draft.CancelledAt != nil ||
		draft.SnapshotDigest != "" || draft.SessionID != "" || draft.RunID != "" {
		return EmployeeTask{}, errors.New("Employee Task lifecycle, digest, Session ID, and Run ID are store-assigned")
	}
	task := draft.Clone()
	task.SchemaVersion = TaskSchemaVersion
	task.State = TaskQueued
	task.CreatedAt = now.UTC()
	task.UpdatedAt = now.UTC()
	normalizeTaskSnapshot(&task)
	digest, err := taskSnapshotDigest(task)
	if err != nil {
		return EmployeeTask{}, err
	}
	task.SnapshotDigest = digest
	if err := ValidateTask(task); err != nil {
		return EmployeeTask{}, err
	}
	return task, nil
}

func CancelTask(current EmployeeTask, now time.Time) (EmployeeTask, error) {
	if err := ValidateTask(current); err != nil {
		return EmployeeTask{}, err
	}
	if current.State == TaskCancelled {
		return current.Clone(), nil
	}
	if current.State != TaskQueued {
		return EmployeeTask{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTaskTransition, current.State, TaskCancelled)
	}
	if now.IsZero() || now.Before(current.UpdatedAt) {
		return EmployeeTask{}, fmt.Errorf("%w: cancellation time precedes Task state", ErrInvalidTaskTransition)
	}
	next := current.Clone()
	next.State = TaskCancelled
	next.UpdatedAt = now.UTC()
	next.CancelledAt = timePointer(now.UTC())
	if err := ValidateTask(next); err != nil {
		return EmployeeTask{}, err
	}
	return next, nil
}

func ValidateTask(task EmployeeTask) error {
	if task.SchemaVersion != TaskSchemaVersion {
		return fmt.Errorf("unsupported Employee Task schema version %d", task.SchemaVersion)
	}
	if err := validateIdentifier("Task id", task.ID); err != nil {
		return err
	}
	if err := validateIdentifier("Task Employee id", task.EmployeeID); err != nil {
		return err
	}
	if task.EmployeeRevision < 1 {
		return errors.New("Task Employee revision must be positive")
	}
	if err := validateTaskText(task.Prompt); err != nil {
		return err
	}
	if task.SessionID != "" || task.RunID != "" {
		return errors.New("Phase 5 Employee Task must not bind a Session or Run")
	}
	if err := ValidateRevisionSnapshot(task.EmployeeSnapshot); err != nil {
		return fmt.Errorf("Task Employee snapshot: %w", err)
	}
	if task.EmployeeID != task.EmployeeSnapshot.EmployeeID ||
		task.EmployeeRevision != task.EmployeeSnapshot.Revision {
		return errors.New("Task identity does not match Employee revision snapshot")
	}
	if err := validateTaskSkills(task.Skills, task.EmployeeSnapshot.Employee.SkillBindings); err != nil {
		return err
	}
	if err := validateTaskKnowledge(task.Knowledge); err != nil {
		return err
	}
	if err := validateTaskMemory(task.MemoryFacts); err != nil {
		return err
	}
	if err := validateTaskProject(task.ProjectBinding, task.EmployeeSnapshot); err != nil {
		return err
	}
	if err := validateTaskPolicy(task.Policy, task.EmployeeSnapshot.Employee, task.ProjectBinding); err != nil {
		return err
	}
	if task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() || task.UpdatedAt.Before(task.CreatedAt) {
		return errors.New("Employee Task timestamps are invalid")
	}
	switch task.State {
	case TaskQueued:
		if task.CancelledAt != nil || !task.UpdatedAt.Equal(task.CreatedAt) {
			return errors.New("queued Employee Task has invalid lifecycle timestamps")
		}
	case TaskCancelled:
		if task.CancelledAt == nil || !task.CancelledAt.Equal(task.UpdatedAt) {
			return errors.New("cancelled Employee Task requires matching cancelled_at")
		}
	default:
		return fmt.Errorf("unsupported Employee Task state %q", task.State)
	}
	if !canonicalSHA256(task.SnapshotDigest) {
		return errors.New("Employee Task Snapshot Digest must be canonical lowercase SHA-256")
	}
	expected, err := taskSnapshotDigest(task)
	if err != nil {
		return err
	}
	if expected != task.SnapshotDigest {
		return errors.New("Employee Task Snapshot Digest mismatch")
	}
	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Employee Task for validation: %w", err)
	}
	if len(raw) > MaxTaskFileBytes {
		return errors.New("Employee Task exceeds 512 KiB file limit")
	}
	return nil
}

func (task EmployeeTask) VerifySnapshotDigest() bool {
	return ValidateTask(task) == nil
}

func (task EmployeeTask) Clone() EmployeeTask {
	task.EmployeeSnapshot = task.EmployeeSnapshot.Clone()
	task.Skills = cloneSkillBindings(task.Skills)
	if task.Knowledge != nil {
		items := make([]TaskKnowledgeSnapshot, len(task.Knowledge))
		for index, item := range task.Knowledge {
			items[index] = item
			if item.Citations != nil {
				items[index].Citations = append([]TaskCitationReference{}, item.Citations...)
			}
		}
		task.Knowledge = items
	}
	if task.MemoryFacts != nil {
		task.MemoryFacts = append([]TaskMemoryFactSnapshot{}, task.MemoryFacts...)
	}
	task.ProjectBinding = cloneProjectBindings([]ProjectBinding{task.ProjectBinding})[0]
	task.Policy.AllowedCapabilities = cloneStrings(task.Policy.AllowedCapabilities)
	task.CancelledAt = cloneTimePointer(task.CancelledAt)
	return task
}

func normalizeTaskSnapshot(task *EmployeeTask) {
	task.Skills = cloneSkillBindings(task.Skills)
	sort.Slice(task.Skills, func(i, j int) bool {
		if task.Skills[i].SkillID == task.Skills[j].SkillID {
			return task.Skills[i].Version < task.Skills[j].Version
		}
		return task.Skills[i].SkillID < task.Skills[j].SkillID
	})
	for index := range task.Knowledge {
		sort.Slice(task.Knowledge[index].Citations, func(i, j int) bool {
			return task.Knowledge[index].Citations[i].CitationID < task.Knowledge[index].Citations[j].CitationID
		})
	}
	sort.Slice(task.Knowledge, func(i, j int) bool {
		return task.Knowledge[i].SourceID < task.Knowledge[j].SourceID
	})
	sort.Slice(task.MemoryFacts, func(i, j int) bool {
		return task.MemoryFacts[i].FactID < task.MemoryFacts[j].FactID
	})
	task.Policy.AllowedCapabilities = normalizeCapabilities(task.Policy.AllowedCapabilities)
	if task.Skills == nil {
		task.Skills = []SkillBinding{}
	}
	if task.Knowledge == nil {
		task.Knowledge = []TaskKnowledgeSnapshot{}
	}
	if task.MemoryFacts == nil {
		task.MemoryFacts = []TaskMemoryFactSnapshot{}
	}
}

func validateTaskText(value string) error {
	lower := strings.ToLower(value)
	if strings.TrimSpace(value) == "" || len(value) > MaxTaskPromptBytes ||
		!utf8.ValidString(value) || strings.ContainsRune(value, '\x00') ||
		strings.ContainsRune(value, unicode.ReplacementChar) {
		return errors.New("Task prompt must be non-empty, valid UTF-8, and at most 16 KiB")
	}
	if looksSecret(value) {
		return errors.New("Task prompt must not contain credentials, tokens, or private keys")
	}
	for _, forbidden := range []string{
		"private reasoning:", "chain of thought:", "raw tool arguments:",
		"raw_tool_arguments", "raw tool output:", "hidden system prompt:",
		"full system prompt:",
	} {
		if strings.Contains(lower, forbidden) {
			return errors.New("Task prompt contains private runtime data")
		}
	}
	return nil
}

func validateTaskSkills(selected, available []SkillBinding) error {
	if len(selected) > MaxSkillBindings {
		return errors.New("Task Skill snapshot exceeds item limit")
	}
	availableByKey := make(map[string]SkillBinding, len(available))
	for _, item := range available {
		availableByKey[item.SkillID+"\x00"+item.Version] = item
	}
	last := ""
	for _, item := range selected {
		key := item.SkillID + "\x00" + item.Version
		if key <= last {
			return errors.New("Task Skills must be unique and strictly sorted")
		}
		last = key
		expected, exists := availableByKey[key]
		if !exists || expected.Digest != item.Digest || expected.Enabled != item.Enabled ||
			!equalTaskConfiguration(expected.Configuration, item.Configuration) {
			return fmt.Errorf("Task Skill %s@%s does not match Employee revision", item.SkillID, item.Version)
		}
	}
	return nil
}

func validateTaskKnowledge(items []TaskKnowledgeSnapshot) error {
	if len(items) > MaxTaskKnowledge {
		return errors.New("Task Knowledge snapshot exceeds source limit")
	}
	lastSource := ""
	totalCitations := 0
	for _, item := range items {
		if err := validateIdentifier("Task Knowledge source id", item.SourceID); err != nil {
			return err
		}
		if item.SourceID <= lastSource || !canonicalSHA256(item.SourceDigest) {
			return errors.New("Task Knowledge sources must be canonical, unique, and strictly sorted")
		}
		lastSource = item.SourceID
		if len(item.Citations) == 0 {
			return errors.New("Task Knowledge source requires at least one Citation reference")
		}
		lastCitation := ""
		for _, citation := range item.Citations {
			totalCitations++
			if totalCitations > MaxTaskCitations {
				return errors.New("Task Knowledge snapshot exceeds Citation limit")
			}
			if err := validateIdentifier("Task Citation id", citation.CitationID); err != nil {
				return err
			}
			if citation.CitationID <= lastCitation || !validTaskReferencePath(citation.Path) ||
				!canonicalSHA256(citation.Digest) || citation.StartLine < 1 ||
				citation.EndLine < citation.StartLine {
				return errors.New("Task Citation reference is invalid or not strictly sorted")
			}
			lastCitation = citation.CitationID
		}
	}
	return nil
}

func validateTaskMemory(items []TaskMemoryFactSnapshot) error {
	if len(items) > MaxTaskMemoryFacts {
		return errors.New("Task Memory snapshot exceeds fact limit")
	}
	last := ""
	for _, item := range items {
		if err := validateIdentifier("Task Memory Fact id", item.FactID); err != nil {
			return err
		}
		if item.FactID <= last || !canonicalSHA256(item.Digest) {
			return errors.New("Task Memory Facts must be canonical, unique, and strictly sorted")
		}
		last = item.FactID
	}
	return nil
}

func validateTaskProject(project ProjectBinding, snapshot RevisionSnapshot) error {
	if err := ValidateProjectBinding(project); err != nil {
		return fmt.Errorf("Task ProjectBinding: %w", err)
	}
	if project.EmployeeID != snapshot.EmployeeID {
		return errors.New("Task ProjectBinding belongs to another Employee")
	}
	for _, item := range snapshot.ProjectBindings {
		if item.ID == project.ID {
			if !reflect.DeepEqual(item, project) {
				return errors.New("Task ProjectBinding does not match Employee revision")
			}
			return nil
		}
	}
	return errors.New("Task ProjectBinding is absent from Employee revision")
}

func validateTaskPolicy(policy TaskPolicy, employeeValue Employee, project ProjectBinding) error {
	if err := validateBudgetPolicy(policy.Budget); err != nil {
		return fmt.Errorf("Task budget policy: %w", err)
	}
	if err := validateUniqueCapabilities(policy.AllowedCapabilities); err != nil {
		return fmt.Errorf("Task permission policy: %w", err)
	}
	if !sort.StringsAreSorted(policy.AllowedCapabilities) {
		return errors.New("Task capabilities must be canonical and sorted")
	}
	if !capabilitySubset(policy.AllowedCapabilities, employeeValue.PermissionPolicy.AllowedCapabilities) ||
		!capabilitySubset(policy.AllowedCapabilities, project.AllowedToolCapabilities) {
		return errors.New("Task capabilities exceed Employee or Project policy")
	}
	if policy.NetworkAllowed && (!employeeValue.PermissionPolicy.NetworkAllowed || !project.NetworkAllowed) {
		return errors.New("Task network policy exceeds Employee or Project policy")
	}
	ceiling := employeeValue.BudgetPolicy
	if project.BudgetOverride != nil {
		ceiling = minimumBudget(ceiling, *project.BudgetOverride)
	}
	if policy.Budget.MaxModelCalls > ceiling.MaxModelCalls ||
		policy.Budget.MaxTokens > ceiling.MaxTokens ||
		policy.Budget.TimeoutSeconds > ceiling.TimeoutSeconds {
		return errors.New("Task budget exceeds Employee or Project policy")
	}
	return nil
}

func taskSnapshotDigest(task EmployeeTask) (string, error) {
	snapshot := task.EmployeeSnapshot.Clone()
	snapshot.Employee.SkillBindings = canonicalTaskSkillConfigurations(snapshot.Employee.SkillBindings)
	body := struct {
		SchemaVersion    int                      `json:"schema_version"`
		ID               string                   `json:"id"`
		EmployeeID       string                   `json:"employee_id"`
		EmployeeRevision int                      `json:"employee_revision"`
		Prompt           string                   `json:"prompt"`
		CreatedAt        time.Time                `json:"created_at"`
		EmployeeSnapshot RevisionSnapshot         `json:"employee_snapshot"`
		Skills           []SkillBinding           `json:"skills"`
		Knowledge        []TaskKnowledgeSnapshot  `json:"knowledge"`
		MemoryFacts      []TaskMemoryFactSnapshot `json:"memory_facts"`
		ProjectBinding   ProjectBinding           `json:"project_binding"`
		Policy           TaskPolicy               `json:"policy"`
	}{
		SchemaVersion: task.SchemaVersion, ID: task.ID,
		EmployeeID: task.EmployeeID, EmployeeRevision: task.EmployeeRevision,
		Prompt: task.Prompt, CreatedAt: task.CreatedAt,
		EmployeeSnapshot: snapshot, Skills: canonicalTaskSkillConfigurations(task.Skills),
		Knowledge: task.Knowledge, MemoryFacts: task.MemoryFacts,
		ProjectBinding: task.ProjectBinding, Policy: task.Policy,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode Employee Task Snapshot Digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalTaskSkillConfigurations(items []SkillBinding) []SkillBinding {
	items = cloneSkillBindings(items)
	for index := range items {
		if len(items[index].Configuration) == 0 {
			continue
		}
		var buffer bytes.Buffer
		if err := json.Compact(&buffer, items[index].Configuration); err == nil {
			items[index].Configuration = append(json.RawMessage(nil), buffer.Bytes()...)
		}
	}
	return items
}

func equalTaskConfiguration(left, right json.RawMessage) bool {
	leftItems := canonicalTaskSkillConfigurations([]SkillBinding{{Configuration: left}})
	rightItems := canonicalTaskSkillConfigurations([]SkillBinding{{Configuration: right}})
	return bytes.Equal(leftItems[0].Configuration, rightItems[0].Configuration)
}

func cloneSkillBindings(items []SkillBinding) []SkillBinding {
	if items == nil {
		return nil
	}
	out := make([]SkillBinding, len(items))
	for index, item := range items {
		out[index] = item
		out[index].Configuration = append(json.RawMessage(nil), item.Configuration...)
	}
	return out
}

func validTaskReferencePath(value string) bool {
	if value == "" || len(value) > MaxWorkspacePathBytes || strings.ContainsAny(value, "%\\\r\n") ||
		path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." ||
			component == ".git" || component == ".gohermit" {
			return false
		}
	}
	return true
}

func canonicalSHA256(value string) bool {
	return value == strings.ToLower(value) && validSHA256(value)
}

func capabilitySubset(values, ceiling []string) bool {
	allowed := make(map[string]struct{}, len(ceiling))
	for _, value := range ceiling {
		allowed[value] = struct{}{}
	}
	for _, value := range values {
		if _, exists := allowed[value]; !exists {
			return false
		}
	}
	return true
}

func minimumBudget(left, right BudgetPolicy) BudgetPolicy {
	return BudgetPolicy{
		MaxModelCalls:  min(left.MaxModelCalls, right.MaxModelCalls),
		MaxTokens:      min(left.MaxTokens, right.MaxTokens),
		TimeoutSeconds: min(left.TimeoutSeconds, right.TimeoutSeconds),
	}
}
