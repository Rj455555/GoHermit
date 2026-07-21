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
	DefaultTemplate     = "personal-development-team"
	MaxWorkItems        = 24
	MaxHandoffs         = 48
	MaxTextBytes        = 32 << 10
	MaxProposedSubsteps = 8
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
	// WorkSkipped marks a repair stage bypassed because the Reviewer found
	// nothing blocking; it satisfies dependents like WorkCompleted.
	WorkSkipped WorkStatus = "skipped"
)

// Severity gates repair scheduling: only blocking findings schedule repair.
type Severity string

const (
	SeverityBlocking Severity = "blocking"
	SeverityAdvisory Severity = "advisory"
)

// Finding is one bounded Reviewer verdict attached to a handoff.
type Finding struct {
	Severity Severity `json:"severity"`
	Summary  string   `json:"summary"`
}

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

// SubstepSpec is one bounded task-specific substep proposed by the Explorer.
// Accepted substeps become real read-only work items.
type SubstepSpec struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Goal      string   `json:"goal"`
	Role      Role     `json:"role"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// Handoff is the only durable agent-to-agent communication format. It excludes
// private reasoning, full prompts, stream chunks, and raw unbounded tool output.
type Handoff struct {
	ID            string        `json:"id"`
	WorkItemID    string        `json:"work_item_id"`
	Role          Role          `json:"role"`
	Summary       string        `json:"summary"`
	Evidence      []string      `json:"evidence,omitempty"`
	ModifiedFiles []string      `json:"modified_files,omitempty"`
	Checks        []Check       `json:"checks,omitempty"`
	Issues        []string      `json:"issues,omitempty"`
	NextSteps     []string      `json:"next_steps,omitempty"`
	Substeps      []SubstepSpec `json:"substeps,omitempty"`
	Findings      []Finding     `json:"findings,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
}

// HasBlockingFindings reports whether the handoff carries at least one
// finding that must be repaired before delivery.
func (h Handoff) HasBlockingFindings() bool {
	for _, finding := range h.Findings {
		if finding.Severity == SeverityBlocking {
			return true
		}
	}
	return false
}

type Mission struct {
	ID       string `json:"id"`
	RunID    string `json:"run_id"`
	Goal     string `json:"goal"`
	Template string `json:"template"`
	Status   Status `json:"status"`
	Budget   Budget `json:"budget"`
	Usage    Usage  `json:"usage"`
	// UsageByRole aggregates Usage per role across every outcome, including
	// failed and retried calls. Values are counts only, never prompt text.
	UsageByRole map[Role]Usage `json:"usage_by_role,omitempty"`
	WorkItems   []WorkItem     `json:"work_items"`
	Handoffs    []Handoff      `json:"handoffs"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Error       string         `json:"error,omitempty"`
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
	return &Mission{ID: id, RunID: runID, Goal: goal, Template: DefaultTemplate, Status: Queued, Budget: budget, UsageByRole: map[Role]Usage{}, CreatedAt: now, UpdatedAt: now}, nil
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

// ValidateSubstepProposal strictly checks an Explorer substep proposal against
// the live mission without changing it. Ids must be safe and unused — completed
// work-item ids are never reused — roles stay read-only, dependencies must
// reference runnable work or peer substeps and never completed work, and the
// combined graph must remain acyclic.
func ValidateSubstepProposal(m *Mission, specs []SubstepSpec) error {
	if m == nil {
		return errors.New("mission is required")
	}
	if len(specs) == 0 || len(specs) > MaxProposedSubsteps {
		return fmt.Errorf("substep proposal must contain 1 to %d entries", MaxProposedSubsteps)
	}
	if len(m.WorkItems)+len(specs) > m.Budget.MaxWorkItems {
		return errors.New("substep proposal exceeds the mission work-item budget")
	}
	existing := make(map[string]WorkStatus, len(m.WorkItems))
	for _, item := range m.WorkItems {
		existing[item.ID] = item.Status
	}
	proposalIDs := make(map[string]bool, len(specs))
	for _, spec := range specs {
		id, title, goal := strings.TrimSpace(spec.ID), strings.TrimSpace(spec.Title), strings.TrimSpace(spec.Goal)
		if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
			return fmt.Errorf("substep id %q is empty or unsafe", spec.ID)
		}
		if _, taken := existing[id]; taken {
			return fmt.Errorf("substep id %q already exists in the mission", id)
		}
		if proposalIDs[id] {
			return fmt.Errorf("duplicate substep id %q in proposal", id)
		}
		proposalIDs[id] = true
		if title == "" || len(title) > 200 {
			return fmt.Errorf("substep %q requires a bounded title", id)
		}
		if goal == "" || len(goal) > MaxTextBytes {
			return fmt.Errorf("substep %q requires a bounded goal", id)
		}
		switch spec.Role {
		case RoleExplorer, RoleReviewer, RoleVerifier:
		default:
			return fmt.Errorf("substep %q role %q is not a read-only role", id, spec.Role)
		}
		if len(spec.DependsOn) > MaxProposedSubsteps {
			return fmt.Errorf("substep %q has too many dependencies", id)
		}
	}
	for _, spec := range specs {
		id := strings.TrimSpace(spec.ID)
		for _, dependency := range spec.DependsOn {
			if proposalIDs[dependency] {
				continue
			}
			status, known := existing[dependency]
			if !known {
				return fmt.Errorf("substep %q has unknown dependency %q", id, dependency)
			}
			switch status {
			case WorkCompleted, WorkFailed, WorkCancelled:
				return fmt.Errorf("substep %q cannot depend on %s work item %q", id, status, dependency)
			}
		}
	}
	// Existing work items are acyclic by construction, so only dependencies
	// between peer substeps can close a cycle.
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(specs))
	deps := make(map[string][]string, len(specs))
	for _, spec := range specs {
		deps[strings.TrimSpace(spec.ID)] = spec.DependsOn
	}
	var visit func(id string) bool
	visit = func(id string) bool {
		state[id] = visiting
		for _, dependency := range deps[id] {
			if !proposalIDs[dependency] {
				continue
			}
			if state[dependency] == visiting || (state[dependency] == unvisited && visit(dependency)) {
				return true
			}
		}
		state[id] = visited
		return false
	}
	for _, spec := range specs {
		id := strings.TrimSpace(spec.ID)
		if state[id] == unvisited && visit(id) {
			return fmt.Errorf("substep proposal contains a dependency cycle involving %q", id)
		}
	}
	return nil
}

// AddSubsteps atomically accepts a validated Explorer proposal: every spec
// becomes a real read-only queued work item and every queued lead is rewired
// to depend on the new items so the final synthesis receives their handoffs.
// Specs are applied in dependency order so a proposal may list peers in any
// order; validation guarantees AddWork cannot fail afterwards. Prior handoffs
// and history are preserved.
func (m *Mission) AddSubsteps(specs []SubstepSpec) error {
	if err := ValidateSubstepProposal(m, specs); err != nil {
		return err
	}
	newIDs := make([]string, 0, len(specs))
	for _, spec := range orderSubsteps(specs) {
		item := WorkItem{ID: strings.TrimSpace(spec.ID), Title: spec.Title, Goal: spec.Goal, Role: spec.Role, DependsOn: spec.DependsOn}
		if err := m.AddWork(item); err != nil {
			return fmt.Errorf("add substep %q: %w", spec.ID, err)
		}
		newIDs = append(newIDs, item.ID)
	}
	now := time.Now().UTC()
	for i := range m.WorkItems {
		item := &m.WorkItems[i]
		if item.Role == RoleLead && item.Status == WorkQueued {
			item.DependsOn = append(item.DependsOn, newIDs...)
			item.UpdatedAt = now
		}
	}
	if m.Status == Completed {
		m.Status = Running
	}
	m.UpdatedAt = now
	return nil
}

// orderSubsteps returns the proposal in a stable dependency order so peer
// dependencies are always added before their dependents. The proposal is
// acyclic by validation, and edges to pre-existing work are already satisfied.
func orderSubsteps(specs []SubstepSpec) []SubstepSpec {
	ordered := make([]SubstepSpec, 0, len(specs))
	added := make(map[string]bool, len(specs))
	remaining := make([]SubstepSpec, len(specs))
	copy(remaining, specs)
	for len(remaining) > 0 {
		next := remaining[:0]
		progressed := false
		for _, spec := range remaining {
			ready := true
			for _, dependency := range spec.DependsOn {
				if added[dependency] {
					continue
				}
				for _, peer := range specs {
					if strings.TrimSpace(peer.ID) == dependency {
						ready = false
						break
					}
				}
				if !ready {
					break
				}
			}
			if ready {
				ordered = append(ordered, spec)
				added[strings.TrimSpace(spec.ID)] = true
				progressed = true
			} else {
				next = append(next, spec)
			}
		}
		if !progressed {
			// Unreachable after validation; keep the original order rather
			// than looping forever.
			return specs
		}
		remaining = next
	}
	return ordered
}

func (m *Mission) Ready() []string {
	completed := make(map[string]bool, len(m.WorkItems))
	for _, item := range m.WorkItems {
		completed[item.ID] = item.Status == WorkCompleted || item.Status == WorkSkipped
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
		if item != nil && item.MutatesWorkspace && (item.Status == WorkCompleted || item.Status == WorkSkipped) {
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

// SkipRepairsAfterReview bypasses the repair stage when the Reviewer found
// nothing blocking: every queued mutating WorkItem gated on the review becomes
// WorkSkipped (Attempt stays 0) so dependents may run without executing it.
// A later verification failure can still requeue a skipped repair.
//
// The guard below restricts skipping to post-implementation reviews: a review
// whose own dependencies include a mutating WorkItem. Without it a preflight
// review (which finds nothing blocking by construction) would skip the primary
// build stage that depends on it.
func (m *Mission) SkipRepairsAfterReview(reviewID string) []string {
	skipped := make([]string, 0)
	review := m.work(reviewID)
	if review == nil || review.Role != RoleReviewer {
		return skipped
	}
	postImplementation := false
	for _, dependency := range review.DependsOn {
		if item := m.work(dependency); item != nil && item.MutatesWorkspace {
			postImplementation = true
			break
		}
	}
	if !postImplementation {
		return skipped
	}
	now := time.Now().UTC()
	for i := range m.WorkItems {
		item := &m.WorkItems[i]
		if item.Status != WorkQueued || !item.MutatesWorkspace || !contains(item.DependsOn, reviewID) {
			continue
		}
		item.Status, item.UpdatedAt = WorkSkipped, now
		skipped = append(skipped, item.ID)
	}
	if len(skipped) > 0 {
		m.UpdatedAt = now
	}
	return skipped
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
	if len(handoff.Summary) > MaxTextBytes || len(handoff.Evidence) > 128 || len(handoff.ModifiedFiles) > 128 || len(handoff.Checks) > 64 || len(handoff.Issues) > 128 || len(handoff.NextSteps) > 128 || len(handoff.Substeps) > MaxProposedSubsteps || len(handoff.Findings) > 128 {
		return errors.New("handoff exceeds bounded limits")
	}
	// Findings fail closed: an unknown severity or empty summary rejects the
	// whole handoff rather than silently downgrading the repair gate.
	for _, finding := range handoff.Findings {
		if finding.Severity != SeverityBlocking && finding.Severity != SeverityAdvisory || strings.TrimSpace(finding.Summary) == "" || len(finding.Summary) > MaxTextBytes {
			return errors.New("handoff findings require a blocking or advisory severity and a bounded summary")
		}
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
		if item.Status != WorkCompleted && item.Status != WorkSkipped {
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
