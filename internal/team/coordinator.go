package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Assignment struct {
	MissionID   string        `json:"mission_id"`
	Goal        string        `json:"goal"`
	WorkItem    WorkItem      `json:"work_item"`
	Inputs      []Handoff     `json:"inputs,omitempty"`
	MaxTokens   int           `json:"max_tokens"`
	MaxDuration time.Duration `json:"max_duration"`
}

type Result struct {
	Handoff    Handoff `json:"handoff"`
	ModelCalls int     `json:"model_calls"`
	Tokens     int     `json:"tokens"`
}

type Worker interface {
	Execute(context.Context, Assignment) (Result, error)
}

type TeamEventType string

const (
	MissionStarted  TeamEventType = "mission_started"
	WorkItemStarted TeamEventType = "work_started"
	WorkItemDone    TeamEventType = "work_completed"
	WorkItemFailed  TeamEventType = "work_failed"
	MissionFinished TeamEventType = "mission_completed"
	MissionFailed   TeamEventType = "mission_failed"
)

type TeamEvent struct {
	Type       TeamEventType `json:"type"`
	MissionID  string        `json:"mission_id"`
	WorkItemID string        `json:"work_item_id,omitempty"`
	Role       Role          `json:"role,omitempty"`
	Message    string        `json:"message,omitempty"`
	Time       time.Time     `json:"time"`
}

type Coordinator struct {
	Worker            Worker
	MaxParallel       int
	MaxRepairAttempts int
	Sink              func(TeamEvent)
	Checkpoint        func(*Mission) error
}

func DefaultMission(id, runID, goal string, budget Budget) (*Mission, error) {
	mission, err := NewMission(id, runID, goal, budget)
	if err != nil {
		return nil, err
	}
	items := []WorkItem{
		{ID: "explore", Title: "Inspect project and constraints", Goal: "Inspect the workspace, relevant rules, architecture, and current state. Return evidence and a focused implementation brief.", Role: RoleExplorer},
		{ID: "build", Title: "Implement the requested outcome", Goal: "Implement the owner's goal using the explorer handoff. Run focused checks and report every modified file.", Role: RoleBuilder, DependsOn: []string{"explore"}, MutatesWorkspace: true},
		{ID: "review", Title: "Review the implementation", Goal: "Review the current diff and builder evidence for correctness, regressions, security, and missing requirements. Return prioritized issues.", Role: RoleReviewer, DependsOn: []string{"build"}},
		{ID: "repair", Title: "Address review findings", Goal: "Inspect the reviewer handoff, fix every actionable issue, and leave the workspace ready for independent verification. If no fix is needed, prove why.", Role: RoleBuilder, DependsOn: []string{"review"}, MutatesWorkspace: true},
		{ID: "verify", Title: "Independently verify the result", Goal: "Run the required deterministic checks after the final mutation. Do not modify implementation files. Return explicit pass/fail evidence.", Role: RoleVerifier, DependsOn: []string{"repair"}},
		{ID: "lead", Title: "Synthesize the owner handoff", Goal: "Using only the structured handoffs, confirm the requested outcome, important changes, verification, remaining risks, and concise next actions for the owner.", Role: RoleLead, DependsOn: []string{"verify"}},
	}
	for _, item := range items {
		if err = mission.AddWork(item); err != nil {
			return nil, err
		}
	}
	return mission, nil
}

// AdaptiveMission selects a bounded team topology from the task intent. Both
// topologies preserve independent verification and a lead-only final handoff.
func AdaptiveMission(id, runID, goal string, budget Budget) (*Mission, error) {
	mission, err := NewMission(id, runID, goal, budget)
	if err != nil {
		return nil, err
	}
	subject := compactMissionGoal(goal)
	var items []WorkItem
	if mutationRequested(goal) {
		items = []WorkItem{
			{ID: "explore", Title: "梳理「" + subject + "」的代码与约束", Goal: "Inspect the relevant architecture, rules, current state, and concrete implementation boundaries.", Role: RoleExplorer},
			{ID: "preflight", Title: "独立识别风险与验收条件", Goal: "Independently identify regressions, security risks, and deterministic acceptance checks before implementation.", Role: RoleReviewer},
			{ID: "build", Title: "实现「" + subject + "」", Goal: "Implement the owner's goal using both preflight handoffs. Run focused checks and report every modified file.", Role: RoleBuilder, DependsOn: []string{"explore", "preflight"}, MutatesWorkspace: true},
			{ID: "review", Title: "独立审查本次实现", Goal: "Review the current diff and evidence for correctness, regressions, security, and missing requirements.", Role: RoleReviewer, DependsOn: []string{"build"}},
			{ID: "repair", Title: "处理审查或验证发现", Goal: "Fix every actionable review or verification issue and leave the workspace ready for another independent verification.", Role: RoleBuilder, DependsOn: []string{"review"}, MutatesWorkspace: true},
			{ID: "verify", Title: "独立验证「" + subject + "」", Goal: "Run deterministic checks after the final mutation. Do not modify implementation files. Return explicit pass/fail evidence.", Role: RoleVerifier, DependsOn: []string{"repair"}},
			{ID: "lead", Title: "汇总证据并交付结果", Goal: "Using only structured handoffs, confirm the outcome, verification, remaining risks, and concise next actions.", Role: RoleLead, DependsOn: []string{"verify"}},
		}
	} else {
		items = []WorkItem{
			{ID: "explore", Title: "梳理「" + subject + "」的项目事实", Goal: "Inspect the workspace and return source-backed facts relevant to the owner's question.", Role: RoleExplorer},
			{ID: "review", Title: "独立检查假设与风险", Goal: "Independently inspect the same question, challenge assumptions, and identify missing evidence.", Role: RoleReviewer},
			{ID: "verify", Title: "交叉验证分析证据", Goal: "Cross-check both handoffs and return explicit pass/fail evidence for the key claims without modifying files.", Role: RoleVerifier, DependsOn: []string{"explore", "review"}},
			{ID: "lead", Title: "汇总结论并交付建议", Goal: "Synthesize a concise, source-backed answer and distinguish facts, inferences, and remaining uncertainty.", Role: RoleLead, DependsOn: []string{"verify"}},
		}
	}
	for _, item := range items {
		if err = mission.AddWork(item); err != nil {
			return nil, err
		}
	}
	return mission, nil
}

func mutationRequested(goal string) bool {
	lower := strings.ToLower(goal)
	markers := []string{"implement", "build", "create", "add ", "fix", "modify", "update", "refactor", "delete", "remove", "write", "develop", "deploy", "实现", "开发", "修复", "修改", "新增", "添加", "重构", "删除", "升级", "生成", "发布", "部署", "完成"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func compactMissionGoal(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	runes := []rune(goal)
	if len(runes) > 36 {
		return string(runes[:36]) + "…"
	}
	if goal == "" {
		return "当前任务"
	}
	return goal
}

func (c *Coordinator) Run(ctx context.Context, mission *Mission) error {
	if c.Worker == nil || mission == nil {
		return errors.New("coordinator requires a worker and mission")
	}
	parallel := c.MaxParallel
	if parallel <= 0 {
		parallel = 3
	}
	runCtx, cancel := context.WithTimeout(ctx, mission.Budget.Timeout)
	defer cancel()
	mission.Status = Running
	mission.UpdatedAt = time.Now().UTC()
	c.emit(TeamEvent{Type: MissionStarted, MissionID: mission.ID})
	if err := c.checkpoint(mission); err != nil {
		return err
	}
	for mission.Status == Running {
		if err := runCtx.Err(); err != nil {
			mission.Interrupt(err.Error())
			_ = c.checkpoint(mission)
			return err
		}
		ready := mission.Ready()
		if len(ready) == 0 {
			if mission.Status == Completed {
				break
			}
			mission.FailMission("mission has no runnable work items")
			c.emit(TeamEvent{Type: MissionFailed, MissionID: mission.ID, Message: mission.Error})
			_ = c.checkpoint(mission)
			return errors.New(mission.Error)
		}
		ready = selectBatch(mission, ready, parallel)
		if err := c.runBatch(runCtx, mission, ready); err != nil {
			return err
		}
	}
	if mission.Status != Completed {
		return fmt.Errorf("mission ended in status %s", mission.Status)
	}
	c.emit(TeamEvent{Type: MissionFinished, MissionID: mission.ID})
	return c.checkpoint(mission)
}

func selectBatch(mission *Mission, ready []string, limit int) []string {
	selected := make([]string, 0, min(limit, len(ready)))
	writerSelected := false
	for _, id := range ready {
		if len(selected) >= limit {
			break
		}
		item := mission.work(id)
		if item == nil {
			continue
		}
		if item.MutatesWorkspace {
			if writerSelected {
				continue
			}
			writerSelected = true
		}
		selected = append(selected, id)
	}
	return selected
}

type workResult struct {
	id     string
	role   Role
	result Result
	err    error
}

func (c *Coordinator) runBatch(ctx context.Context, mission *Mission, ready []string) error {
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()
	results := make(chan workResult, len(ready))
	var group sync.WaitGroup
	started := 0
	for _, id := range ready {
		item := mission.work(id)
		if item == nil {
			continue
		}
		if item.Role == RoleLead && !verificationPassed(mission) {
			message := "independent verification did not pass"
			mission.FailMission(message)
			c.emit(TeamEvent{Type: MissionFailed, MissionID: mission.ID, Message: message})
			_ = c.checkpoint(mission)
			return errors.New(message)
		}
		if mission.Usage.ModelCalls >= mission.Budget.MaxModelCalls || mission.Usage.Tokens >= mission.Budget.MaxTokens {
			message := "mission model budget exceeded"
			mission.FailMission(message)
			c.emit(TeamEvent{Type: MissionFailed, MissionID: mission.ID, WorkItemID: id, Role: item.Role, Message: message})
			_ = c.checkpoint(mission)
			return errors.New(message)
		}
		if item.ExecutionSessionID == "" {
			item.ExecutionSessionID = "worker-" + mission.ID + "-" + item.ID
		}
		if err := mission.Start(id); err != nil {
			return err
		}
		c.emit(TeamEvent{Type: WorkItemStarted, MissionID: mission.ID, WorkItemID: id, Role: item.Role, Message: item.Title})
		if err := c.checkpoint(mission); err != nil {
			return err
		}
		assignment := Assignment{MissionID: mission.ID, Goal: mission.Goal, WorkItem: *item, Inputs: dependencyHandoffs(mission, *item), MaxTokens: max(1024, (mission.Budget.MaxTokens-mission.Usage.Tokens)/max(1, len(mission.WorkItems))), MaxDuration: mission.Budget.Timeout}
		started++
		group.Add(1)
		go func(id string, role Role, assignment Assignment) {
			defer group.Done()
			result, err := c.Worker.Execute(batchCtx, assignment)
			results <- workResult{id: id, role: role, result: result, err: err}
		}(id, item.Role, assignment)
	}
	go func() {
		group.Wait()
		close(results)
	}()
	if started == 0 {
		return errors.New("mission batch did not start work")
	}
	var batchErr error
	for outcome := range results {
		mission.Usage.ModelCalls += max(0, outcome.result.ModelCalls)
		mission.Usage.Tokens += max(0, outcome.result.Tokens)
		if batchErr != nil {
			continue
		}
		if mission.Usage.ModelCalls > mission.Budget.MaxModelCalls || mission.Usage.Tokens > mission.Budget.MaxTokens {
			message := "mission model budget exceeded"
			mission.FailMission(message)
			c.emit(TeamEvent{Type: MissionFailed, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: message})
			_ = c.checkpoint(mission)
			batchErr = errors.New(message)
			cancelBatch()
			continue
		}
		if outcome.err != nil {
			_ = mission.Fail(outcome.id, outcome.err.Error())
			c.emit(TeamEvent{Type: WorkItemFailed, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: outcome.err.Error()})
			_ = c.checkpoint(mission)
			batchErr = outcome.err
			cancelBatch()
			continue
		}
		if err := mission.Complete(outcome.id, outcome.result.Handoff); err != nil {
			mission.FailMission(err.Error())
			batchErr = err
			cancelBatch()
			continue
		}
		if outcome.role == RoleVerifier && !handoffChecksPassed(outcome.result.Handoff) {
			attempts := c.MaxRepairAttempts
			if attempts <= 0 {
				attempts = 3
			}
			requeued, retryErr := mission.RequeueAfterVerification(outcome.id, attempts)
			if retryErr != nil {
				mission.FailMission(retryErr.Error())
				batchErr = retryErr
				cancelBatch()
				continue
			}
			if !requeued {
				message := "independent verification did not pass"
				mission.FailMission(message)
				c.emit(TeamEvent{Type: WorkItemFailed, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: message})
				_ = c.checkpoint(mission)
				batchErr = errors.New(message)
				cancelBatch()
				continue
			}
			outcome.result.Handoff.Summary += "；验证未通过，已进入有界修复循环"
		}
		c.emit(TeamEvent{Type: WorkItemDone, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: outcome.result.Handoff.Summary})
		if err := c.checkpoint(mission); err != nil {
			batchErr = err
			cancelBatch()
		}
	}
	if batchErr != nil {
		_ = c.checkpoint(mission)
	}
	return batchErr
}

func dependencyHandoffs(mission *Mission, item WorkItem) []Handoff {
	needed := make(map[string]bool, len(item.DependsOn))
	for _, id := range item.DependsOn {
		needed[id] = true
	}
	result := make([]Handoff, 0, len(needed))
	for _, handoff := range mission.Handoffs {
		if needed[handoff.WorkItemID] {
			result = append(result, handoff)
		}
	}
	return result
}

func verificationPassed(mission *Mission) bool {
	for i := len(mission.Handoffs) - 1; i >= 0; i-- {
		handoff := mission.Handoffs[i]
		if handoff.Role != RoleVerifier || len(handoff.Checks) == 0 {
			continue
		}
		for _, check := range handoff.Checks {
			if !check.Passed {
				return false
			}
		}
		return true
	}
	return false
}

func handoffChecksPassed(handoff Handoff) bool {
	if len(handoff.Checks) == 0 {
		return false
	}
	for _, check := range handoff.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (c *Coordinator) emit(event TeamEvent) {
	event.Time = time.Now().UTC()
	if c.Sink != nil {
		c.Sink(event)
	}
}

func (c *Coordinator) checkpoint(mission *Mission) error {
	if c.Checkpoint == nil {
		return nil
	}
	return c.Checkpoint(mission)
}
