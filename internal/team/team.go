// Package team defines the bounded, recoverable multi-agent orchestration model.
package team

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	DefaultTemplate = "personal-development-team"
	MaxWorkItems    = 24
	MaxHandoffs     = 48
	MaxTextBytes    = 32 << 10
)

type Role string

const (
	RoleLead     Role = "lead"
	RoleExplorer Role = "explorer"
	RoleBuilder  Role = "builder"
	RoleReviewer Role = "reviewer"
	RoleVerifier Role = "verifier"
	RoleOperator Role = "operator"
)

type Status string

const (
	Queued      Status = "queued"
	Running     Status = "running"
	Completed   Status = "completed"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

type WorkStatus string

const (
	WorkQueued      WorkStatus = "queued"
	WorkRunning     WorkStatus = "running"
	WorkCompleted   WorkStatus = "completed"
	WorkFailed      WorkStatus = "failed"
	WorkCancelled   WorkStatus = "cancelled"
	WorkInterrupted WorkStatus = "interrupted"
)

type Budget struct {
	MaxWorkItems  int           `json:"max_work_items"`
	MaxModelCalls int           `json:"max_model_calls"`
	MaxTokens     int           `json:"max_tokens"`
	Timeout       time.Duration `json:"timeout"`
}

type Usage struct {
	ModelCalls int `json:"model_calls"`
	Tokens     int `json:"tokens"`
}

type WorkItem struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	Goal               string     `json:"goal"`
	Role               Role       `json:"role"`
	Status             WorkStatus `json:"status"`
	DependsOn          []string   `json:"depends_on,omitempty"`
	MutatesWorkspace   bool       `json:"mutates_workspace,omitempty"`
	Attempt            int        `json:"attempt,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	HandoffID          string     `json:"handoff_id,omitempty"`
	ExecutionSessionID string     `json:"execution_session_id,omitempty"`
	Error              string     `json:"error,omitempty"`
}

type Check struct {
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary,omitempty"`
}

// Handoff is the only durable agent-to-agent communication format. It excludes
// private reasoning, full prompts, stream chunks, and raw unbounded tool output.
type Handoff struct {
	ID            string    `json:"id"`
	WorkItemID    string    `json:"work_item_id"`
	Role          Role      `json:"role"`
	Summary       string    `json:"summary"`
	Evidence      []string  `json:"evidence,omitempty"`
	ModifiedFiles []string  `json:"modified_files,omitempty"`
	Checks        []Check   `json:"checks,omitempty"`
	Issues        []string  `json:"issues,omitempty"`
	NextSteps     []string  `json:"next_steps,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Mission struct {
	ID        string     `json:"id"`
	RunID     string     `json:"run_id"`
	Goal      string     `json:"goal"`
	Template  string     `json:"template"`
	Status    Status     `json:"status"`
	Budget    Budget     `json:"budget"`
	Usage     Usage      `json:"usage"`
	WorkItems []WorkItem `json:"work_items"`
	Handoffs  []Handoff  `json:"handoffs"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Error     string     `json:"error,omitempty"`
}

func DefaultBudget() Budget {
	return Budget{MaxWorkItems: 12, MaxModelCalls: 30, MaxTokens: 250_000, Timeout: 45 * time.Minute}
}

func NewMission(id, runID, goal string, budget Budget) (*Mission, error) {
	id, runID, goal = strings.TrimSpace(id), strings.TrimSpace(runID), strings.TrimSpace(goal)
	if id == "" || runID == "" || goal == "" {
		return nil, errors.New("mission id, run id, and goal are required")
	}
	if len(goal) > MaxTextBytes {
		return nil, errors.New("mission goal is too large")
	}
	if budget.MaxWorkItems <= 0 || budget.MaxWorkItems > MaxWorkItems || budget.MaxModelCalls <= 0 || budget.MaxTokens <= 0 || budget.Timeout <= 0 {
		return nil, errors.New("invalid mission budget")
	}
	now := time.Now().UTC()
	return &Mission{ID: id, RunID: runID, Goal: goal, Template: DefaultTemplate, Status: Queued, Budget: budget, CreatedAt: now, UpdatedAt: now}, nil
}

// AddWork validates dependency order and keeps the task graph acyclic by only
// allowing dependencies on work already present in the mission.
func (m *Mission) AddWork(item WorkItem) error {
	if len(m.WorkItems) >= m.Budget.MaxWorkItems {
		return errors.New("mission work-item budget exceeded")
	}
	item.ID, item.Title, item.Goal = strings.TrimSpace(item.ID), strings.TrimSpace(item.Title), strings.TrimSpace(item.Goal)
	if item.ID == "" || item.Title == "" || item.Goal == "" || !validRole(item.Role) {
		return errors.New("work item requires id, title, goal, and a valid role")
	}
	if len(item.Goal) > MaxTextBytes || len(item.Title) > 200 {
		return errors.New("work item text is too large")
	}
	known := make(map[string]bool, len(m.WorkItems))
	for _, existing := range m.WorkItems {
		known[existing.ID] = true
		if existing.ID == item.ID {
			return fmt.Errorf("duplicate work item %q", item.ID)
		}
	}
	for _, dependency := range item.DependsOn {
		if !known[dependency] {
			return fmt.Errorf("unknown or forward dependency %q", dependency)
		}
	}
	if item.MutatesWorkspace && item.Role != RoleBuilder && item.Role != RoleOperator {
		return fmt.Errorf("role %s cannot own a mutating work item", item.Role)
	}
	now := time.Now().UTC()
	item.Status, item.UpdatedAt = WorkQueued, now
	m.WorkItems = append(m.WorkItems, item)
	m.UpdatedAt = now
	return nil
}

func (m *Mission) Ready() []string {
	completed := make(map[string]bool, len(m.WorkItems))
	for _, item := range m.WorkItems {
		completed[item.ID] = item.Status == WorkCompleted
	}
	ready := make([]string, 0)
	for _, item := range m.WorkItems {
		if item.Status != WorkQueued && item.Status != WorkInterrupted {
			continue
		}
		ok := true
		for _, dependency := range item.DependsOn {
			ok = ok && completed[dependency]
		}
		if ok {
			ready = append(ready, item.ID)
		}
	}
	sort.Strings(ready)
	return ready
}

func (m *Mission) Start(id string) error {
	item := m.work(id)
	if item == nil || (item.Status != WorkQueued && item.Status != WorkInterrupted) {
		return fmt.Errorf("work item %q cannot start", id)
	}
	ready := m.Ready()
	if !contains(ready, id) {
		return fmt.Errorf("work item %q has incomplete dependencies", id)
	}
	if item.MutatesWorkspace {
		for _, other := range m.WorkItems {
			if other.ID != id && other.MutatesWorkspace && other.Status == WorkRunning {
				return errors.New("another workspace writer is already active")
			}
		}
	}
	now := time.Now().UTC()
	item.Status, item.StartedAt, item.UpdatedAt = WorkRunning, &now, now
	item.Attempt++
	m.Status, m.UpdatedAt = Running, now
	return nil
}

func (m *Mission) Complete(id string, handoff Handoff) error {
	item := m.work(id)
	if item == nil || item.Status != WorkRunning {
		return fmt.Errorf("work item %q is not running", id)
	}
	if len(m.Handoffs) >= MaxHandoffs {
		return errors.New("mission handoff limit exceeded")
	}
	if err := validateHandoff(item, &handoff); err != nil {
		return err
	}
	now := time.Now().UTC()
	handoff.CreatedAt = now
	item.Status, item.HandoffID, item.CompletedAt, item.UpdatedAt = WorkCompleted, handoff.ID, &now, now
	m.Handoffs = append(m.Handoffs, handoff)
	m.UpdatedAt = now
	if allCompleted(m.WorkItems) {
		m.Status = Completed
	}
	return nil
}

// RequeueAfterVerification schedules the verifier and its mutating dependency
// for another bounded attempt while preserving prior handoffs as audit history.
func (m *Mission) RequeueAfterVerification(verifierID string, maxAttempts int) (bool, error) {
	verifier := m.work(verifierID)
	if verifier == nil || verifier.Role != RoleVerifier || verifier.Status != WorkCompleted {
		return false, fmt.Errorf("work item %q is not a completed verifier", verifierID)
	}
	if maxAttempts < 1 || verifier.Attempt >= maxAttempts {
		return false, nil
	}
	repairIDs := make([]string, 0, len(verifier.DependsOn))
	for _, dependency := range verifier.DependsOn {
		item := m.work(dependency)
		if item != nil && item.MutatesWorkspace && item.Status == WorkCompleted {
			repairIDs = append(repairIDs, item.ID)
		}
	}
	if len(repairIDs) == 0 {
		return false, nil
	}
	now := time.Now().UTC()
	reset := func(item *WorkItem, detail string) {
		item.Status, item.Error, item.HandoffID, item.UpdatedAt = WorkQueued, detail, "", now
		item.StartedAt, item.CompletedAt = nil, nil
	}
	for _, id := range repairIDs {
		reset(m.work(id), "requeued after failed verification")
	}
	reset(verifier, "requeued after failed verification")
	m.Status, m.Error, m.UpdatedAt = Running, "", now
	return true, nil
}

func (m *Mission) Fail(id, message string) error {
	item := m.work(id)
	if item == nil || item.Status != WorkRunning {
		return fmt.Errorf("work item %q is not running", id)
	}
	now := time.Now().UTC()
	item.Status, item.Error, item.CompletedAt, item.UpdatedAt = WorkFailed, clip(message), &now, now
	m.failRunning(item.ID, item.Error, now)
	return nil
}

// FailMission terminates a mission when the failure is not attributable to a
// single Worker, such as a budget, verification, or scheduling failure.
func (m *Mission) FailMission(message string) {
	now := time.Now().UTC()
	m.failRunning("", clip(message), now)
}

func (m *Mission) failRunning(exceptID, message string, now time.Time) {
	for i := range m.WorkItems {
		if m.WorkItems[i].ID != exceptID && m.WorkItems[i].Status == WorkRunning {
			m.WorkItems[i].Status = WorkCancelled
			m.WorkItems[i].UpdatedAt = now
			m.WorkItems[i].CompletedAt = &now
			m.WorkItems[i].Error = "mission stopped after a failure"
		}
	}
	m.Status, m.Error, m.UpdatedAt = Failed, message, now
}

func (m *Mission) Interrupt(message string) {
	now := time.Now().UTC()
	for i := range m.WorkItems {
		if m.WorkItems[i].Status == WorkRunning {
			m.WorkItems[i].Status = WorkInterrupted
			m.WorkItems[i].UpdatedAt = now
			m.WorkItems[i].Error = clip(message)
		}
	}
	m.Status, m.Error, m.UpdatedAt = Interrupted, clip(message), now
}

// Cancel makes the mission terminal. Unlike Interrupt, cancelled work is not
// eligible for resume and queued work must never be scheduled later.
func (m *Mission) Cancel(message string) {
	now := time.Now().UTC()
	for i := range m.WorkItems {
		if m.WorkItems[i].Status == WorkQueued || m.WorkItems[i].Status == WorkRunning || m.WorkItems[i].Status == WorkInterrupted {
			m.WorkItems[i].Status = WorkCancelled
			m.WorkItems[i].UpdatedAt = now
			m.WorkItems[i].CompletedAt = &now
			m.WorkItems[i].Error = clip(message)
		}
	}
	m.Status, m.Error, m.UpdatedAt = Cancelled, clip(message), now
}

func (m *Mission) work(id string) *WorkItem {
	for i := range m.WorkItems {
		if m.WorkItems[i].ID == id {
			return &m.WorkItems[i]
		}
	}
	return nil
}

func validateHandoff(item *WorkItem, handoff *Handoff) error {
	handoff.ID, handoff.WorkItemID, handoff.Summary = strings.TrimSpace(handoff.ID), strings.TrimSpace(handoff.WorkItemID), strings.TrimSpace(handoff.Summary)
	if handoff.ID == "" || handoff.WorkItemID != item.ID || handoff.Role != item.Role || handoff.Summary == "" {
		return errors.New("handoff identity, role, and summary must match the work item")
	}
	if len(handoff.Summary) > MaxTextBytes || len(handoff.Evidence) > 128 || len(handoff.ModifiedFiles) > 128 || len(handoff.Checks) > 64 || len(handoff.Issues) > 128 || len(handoff.NextSteps) > 128 {
		return errors.New("handoff exceeds bounded limits")
	}
	return nil
}

func validRole(role Role) bool {
	switch role {
	case RoleLead, RoleExplorer, RoleBuilder, RoleReviewer, RoleVerifier, RoleOperator:
		return true
	default:
		return false
	}
}

func allCompleted(items []WorkItem) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.Status != WorkCompleted {
			return false
		}
	}
	return true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func clip(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > MaxTextBytes {
		return value[:MaxTextBytes]
	}
	return value
}
