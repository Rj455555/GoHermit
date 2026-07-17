// Package taskplan defines the durable owner-facing execution checklist.
package taskplan

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion  = 1
	MaxSteps       = 16
	MaxTitleBytes  = 240
	MaxDetailBytes = 2 << 10
)

type Status string

const (
	Active    Status = "active"
	Completed Status = "completed"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type StepStatus string

const (
	Pending       StepStatus = "pending"
	InProgress    StepStatus = "in_progress"
	StepDone      StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepCancelled StepStatus = "cancelled"
)

type StepSpec struct {
	ID    string
	Title string
}

type Step struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Status      StepStatus `json:"status"`
	Detail      string     `json:"detail,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Plan struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Status        Status    `json:"status"`
	Revision      int       `json:"revision"`
	AllowParallel bool      `json:"allow_parallel,omitempty"`
	Steps         []Step    `json:"steps"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func NewParallel(id string, specs []StepSpec) (*Plan, error) {
	plan, err := New(id, specs)
	if err != nil {
		return nil, err
	}
	plan.AllowParallel = true
	return plan, nil
}

func New(id string, specs []StepSpec) (*Plan, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(specs) == 0 || len(specs) > MaxSteps {
		return nil, errors.New("plan requires an id and bounded steps")
	}
	now := time.Now().UTC()
	plan := &Plan{SchemaVersion: SchemaVersion, ID: id, Status: Active, Revision: 1, CreatedAt: now, UpdatedAt: now}
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		spec.ID, spec.Title = strings.TrimSpace(spec.ID), strings.TrimSpace(spec.Title)
		if spec.ID == "" || strings.ContainsAny(spec.ID, "/\\") || strings.Contains(spec.ID, "..") || spec.Title == "" || len(spec.Title) > MaxTitleBytes {
			return nil, errors.New("plan step requires a safe id and bounded title")
		}
		if seen[spec.ID] {
			return nil, fmt.Errorf("duplicate plan step %q", spec.ID)
		}
		seen[spec.ID] = true
		plan.Steps = append(plan.Steps, Step{ID: spec.ID, Title: spec.Title, Status: Pending, UpdatedAt: now})
	}
	return plan, nil
}

func DefaultSingle(runID string) (*Plan, error) {
	return New("plan-"+runID, []StepSpec{
		{ID: "analyze", Title: "理解任务并检查相关上下文"},
		{ID: "execute", Title: "执行任务并记录改动"},
		{ID: "verify", Title: "验证结果与完成条件"},
		{ID: "report", Title: "整理结果并交付"},
	})
}

func DefaultTeam(runID string) (*Plan, error) {
	return New("plan-"+runID, []StepSpec{
		{ID: "explore", Title: "分析目标、项目与约束"},
		{ID: "build", Title: "实现请求的结果"},
		{ID: "review", Title: "独立审查实现"},
		{ID: "repair", Title: "处理审查发现"},
		{ID: "verify", Title: "独立运行验证"},
		{ID: "lead", Title: "汇总验证结果并交付"},
	})
}

// ForGoal creates a bounded plan whose user-visible steps name the current
// task while retaining stable step IDs used by the execution controller.
func ForGoal(runID, goal, agent string) (*Plan, error) {
	subject := compactGoal(goal)
	if agent == "team" {
		return New("plan-"+runID, []StepSpec{
			{ID: "explore", Title: "分析「" + subject + "」的目标、项目与约束"},
			{ID: "build", Title: "实现「" + subject + "」"},
			{ID: "review", Title: "独立审查本次实现"},
			{ID: "repair", Title: "处理审查发现并收紧结果"},
			{ID: "verify", Title: "独立验证「" + subject + "」"},
			{ID: "lead", Title: "汇总证据并交付结果"},
		})
	}
	return New("plan-"+runID, []StepSpec{
		{ID: "analyze", Title: "检查与「" + subject + "」相关的上下文"},
		{ID: "execute", Title: "完成「" + subject + "」"},
		{ID: "verify", Title: "验证「" + subject + "」的完成条件"},
		{ID: "report", Title: "整理证据并交付结果"},
	})
}

func compactGoal(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	if goal == "" {
		return "当前任务"
	}
	runes := []rune(goal)
	if len(runes) > 48 {
		goal = string(runes[:48]) + "…"
	}
	return goal
}

func (p *Plan) Start(id, detail string) (bool, error) {
	step := p.step(id)
	if step == nil {
		return false, fmt.Errorf("unknown plan step %q", id)
	}
	if step.Status == InProgress || step.Status == StepDone {
		return false, nil
	}
	if p.Status != Active || step.Status != Pending {
		return false, fmt.Errorf("plan step %q cannot start from %s", id, step.Status)
	}
	if !p.AllowParallel {
		for _, candidate := range p.Steps {
			if candidate.ID != id && candidate.Status == InProgress {
				return false, fmt.Errorf("plan step %q is already in progress", candidate.ID)
			}
		}
	}
	now := time.Now().UTC()
	step.Status, step.Detail, step.StartedAt, step.UpdatedAt = InProgress, clip(detail), &now, now
	p.changed(now)
	return true, nil
}

func (p *Plan) Complete(id, detail string) (bool, error) {
	step := p.step(id)
	if step == nil {
		return false, fmt.Errorf("unknown plan step %q", id)
	}
	if step.Status == StepDone {
		return false, nil
	}
	if p.Status != Active || (step.Status != Pending && step.Status != InProgress) {
		return false, fmt.Errorf("plan step %q cannot complete from %s", id, step.Status)
	}
	if step.Status == Pending && !p.AllowParallel {
		for _, candidate := range p.Steps {
			if candidate.ID != id && candidate.Status == InProgress {
				return false, fmt.Errorf("plan step %q is already in progress", candidate.ID)
			}
		}
	}
	now := time.Now().UTC()
	if step.StartedAt == nil {
		step.StartedAt = &now
	}
	step.Status, step.Detail, step.CompletedAt, step.UpdatedAt = StepDone, clip(detail), &now, now
	p.changed(now)
	if p.allDone() {
		p.Status = Completed
	}
	return true, nil
}

func (p *Plan) Fail(id, detail string) (bool, error) {
	step := p.step(id)
	if step == nil {
		return false, fmt.Errorf("unknown plan step %q", id)
	}
	if step.Status == StepFailed {
		return false, nil
	}
	if step.Status == StepDone {
		return false, fmt.Errorf("completed plan step %q cannot fail", id)
	}
	if p.Status != Active || (step.Status != Pending && step.Status != InProgress) {
		return false, fmt.Errorf("plan step %q cannot fail from %s", id, step.Status)
	}
	if step.Status == Pending && !p.AllowParallel {
		for _, candidate := range p.Steps {
			if candidate.ID != id && candidate.Status == InProgress {
				return false, fmt.Errorf("plan step %q is already in progress", candidate.ID)
			}
		}
	}
	now := time.Now().UTC()
	if step.StartedAt == nil {
		step.StartedAt = &now
	}
	step.Status, step.Detail, step.CompletedAt, step.UpdatedAt = StepFailed, clip(detail), &now, now
	p.Status = Failed
	p.changed(now)
	return true, nil
}

func (p *Plan) Note(id, detail string) (bool, error) {
	step := p.step(id)
	if step == nil {
		return false, fmt.Errorf("unknown plan step %q", id)
	}
	detail = clip(detail)
	if detail == step.Detail {
		return false, nil
	}
	if step.Status != InProgress {
		return false, fmt.Errorf("plan step %q is not in progress", id)
	}
	now := time.Now().UTC()
	step.Detail, step.UpdatedAt = detail, now
	p.changed(now)
	return true, nil
}

func (p *Plan) Cancel(detail string) (bool, string) {
	if p == nil || p.Status != Active {
		return false, ""
	}
	now := time.Now().UTC()
	stepID := ""
	for i := range p.Steps {
		step := &p.Steps[i]
		if step.Status != Pending && step.Status != InProgress {
			continue
		}
		if stepID == "" {
			stepID = step.ID
		}
		if step.StartedAt == nil {
			step.StartedAt = &now
		}
		step.Status, step.Detail, step.CompletedAt, step.UpdatedAt = StepCancelled, clip(detail), &now, now
		if !p.AllowParallel {
			break
		}
	}
	p.Status = Cancelled
	p.changed(now)
	return true, stepID
}

// Reopen makes a bounded set of completed or failed steps runnable again. It
// is used by the controller for evidence-driven repair/verify retries.
func (p *Plan) Reopen(ids []string, detail string) (bool, error) {
	if p == nil || len(ids) == 0 {
		return false, nil
	}
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		if p.step(id) == nil {
			return false, fmt.Errorf("unknown plan step %q", id)
		}
		requested[id] = true
	}
	now := time.Now().UTC()
	changed := false
	for i := range p.Steps {
		step := &p.Steps[i]
		if !requested[step.ID] || step.Status == Pending {
			continue
		}
		if step.Status == StepCancelled {
			return false, fmt.Errorf("cancelled plan step %q cannot reopen", step.ID)
		}
		step.Status, step.Detail, step.StartedAt, step.CompletedAt, step.UpdatedAt = Pending, clip(detail), nil, nil, now
		changed = true
	}
	if !changed {
		return false, nil
	}
	for _, step := range p.Steps {
		if step.Status == StepFailed && !requested[step.ID] {
			return false, errors.New("plan has an unrelated failed step")
		}
	}
	p.Status = Active
	p.changed(now)
	return true, nil
}

func (p *Plan) Current() *Step {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].Status == InProgress {
			return &p.Steps[i]
		}
	}
	return nil
}

func (p *Plan) NextPending() *Step {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].Status == Pending {
			return &p.Steps[i]
		}
	}
	return nil
}

func (p *Plan) Progress() (done, total int) {
	if p == nil {
		return 0, 0
	}
	for _, step := range p.Steps {
		if step.Status == StepDone {
			done++
		}
	}
	return done, len(p.Steps)
}

func (p *Plan) step(id string) *Step {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].ID == id {
			return &p.Steps[i]
		}
	}
	return nil
}

func (p *Plan) changed(now time.Time) {
	p.SchemaVersion = SchemaVersion
	p.Revision++
	p.UpdatedAt = now
}

func (p *Plan) allDone() bool {
	for _, step := range p.Steps {
		if step.Status != StepDone {
			return false
		}
	}
	return len(p.Steps) > 0
}

func Validate(p *Plan) error {
	if p == nil {
		return nil
	}
	if p.SchemaVersion != SchemaVersion || p.Revision < 1 || strings.TrimSpace(p.ID) == "" || len(p.Steps) == 0 || len(p.Steps) > MaxSteps {
		return errors.New("invalid plan header")
	}
	switch p.Status {
	case Active, Completed, Failed, Cancelled:
	default:
		return errors.New("invalid plan status")
	}
	seen := make(map[string]bool, len(p.Steps))
	current, failed, cancelled, completed := 0, 0, 0, 0
	for _, step := range p.Steps {
		if strings.TrimSpace(step.ID) == "" || strings.ContainsAny(step.ID, "/\\") || strings.Contains(step.ID, "..") || strings.TrimSpace(step.Title) == "" || len(step.Title) > MaxTitleBytes || len(step.Detail) > MaxDetailBytes || seen[step.ID] {
			return errors.New("invalid plan step")
		}
		seen[step.ID] = true
		switch step.Status {
		case Pending:
		case InProgress:
			current++
		case StepDone:
			completed++
		case StepFailed:
			failed++
		case StepCancelled:
			cancelled++
		default:
			return errors.New("invalid plan step status")
		}
	}
	terminalWithCurrent := p.Status != Active && current > 0
	activeWithoutWork := p.Status == Active && completed == len(p.Steps)
	tooManyCurrent := current > 1 && !p.AllowParallel
	if tooManyCurrent || terminalWithCurrent || activeWithoutWork || (p.Status == Completed && completed != len(p.Steps)) || (p.Status == Failed && failed == 0) || (p.Status == Cancelled && cancelled == 0) || (p.Status == Active && (failed > 0 || cancelled > 0)) {
		return errors.New("inconsistent plan state")
	}
	return nil
}

func clip(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > MaxDetailBytes {
		return value[:MaxDetailBytes]
	}
	return value
}
