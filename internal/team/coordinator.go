package team

import (
	"context"
	"encoding/json"
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
	MissionStarted   TeamEventType = "mission_started"
	WorkItemStarted  TeamEventType = "work_started"
	WorkItemDone     TeamEventType = "work_completed"
	WorkItemFailed   TeamEventType = "work_failed"
	SubstepsAccepted TeamEventType = "substeps_accepted"
	SubstepsRejected TeamEventType = "substeps_rejected"
	MissionFinished  TeamEventType = "mission_completed"
	MissionFailed    TeamEventType = "mission_failed"
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
		if reason := mission.RoleBudgetExceeded(item.Role); reason != "" {
			mission.FailMission(reason)
			c.emit(TeamEvent{Type: MissionFailed, MissionID: mission.ID, WorkItemID: id, Role: item.Role, Message: reason})
			_ = c.checkpoint(mission)
			return errors.New(reason)
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
		calls, tokens := max(0, outcome.result.ModelCalls), max(0, outcome.result.Tokens)
		mission.Usage.ModelCalls += calls
		mission.Usage.Tokens += tokens
		if mission.UsageByRole == nil {
			mission.UsageByRole = make(map[Role]Usage)
		}
		roleUsage := mission.UsageByRole[outcome.role]
		roleUsage.ModelCalls += calls
		roleUsage.Tokens += tokens
		mission.UsageByRole[outcome.role] = roleUsage
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
		if reason := mission.RoleBudgetExceeded(outcome.role); reason != "" {
			mission.FailMission(reason)
			c.emit(TeamEvent{Type: MissionFailed, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: reason})
			_ = c.checkpoint(mission)
			batchErr = errors.New(reason)
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
		if outcome.role == RoleExplorer && len(outcome.result.Handoff.Substeps) > 0 {
			if substepErr := mission.AddSubsteps(outcome.result.Handoff.Substeps); substepErr != nil {
				c.emit(TeamEvent{Type: SubstepsRejected, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: clip(substepErr.Error())})
			} else {
				c.emit(TeamEvent{Type: SubstepsAccepted, MissionID: mission.ID, WorkItemID: outcome.id, Role: outcome.role, Message: acceptedSubstepsMessage(outcome.result.Handoff.Substeps)})
			}
			if err := c.checkpoint(mission); err != nil {
				batchErr = err
				cancelBatch()
				continue
			}
		}
		if outcome.role == RoleVerifier && !HandoffChecksPassed(mission, outcome.result.Handoff) {
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
			continue
		}
		if outcome.role == RoleReviewer && !outcome.result.Handoff.HasBlockingFindings() {
			// No blocking finding: bypass the repair stage. The skip is emitted
			// after the reviewer's own completion so runcontrol completes the
			// repair plan step from a clean state; a later verification failure
			// can still requeue the skipped repair.
			skipped := mission.SkipRepairsAfterReview(outcome.id)
			for _, skippedID := range skipped {
				c.emit(TeamEvent{Type: WorkItemDone, MissionID: mission.ID, WorkItemID: skippedID, Role: RoleBuilder, Message: "审查无 blocking 发现，跳过修复"})
			}
			if len(skipped) > 0 {
				if err := c.checkpoint(mission); err != nil {
					batchErr = err
					cancelBatch()
				}
			}
		}
	}
	if batchErr != nil {
		_ = c.checkpoint(mission)
	}
	return batchErr
}

// acceptedSubstep is the bounded owner-facing summary of one accepted substep.
type acceptedSubstep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// acceptedSubstepsMessage encodes the accepted proposal as a bounded JSON
// array of {id, title} pairs consumed by runcontrol for Live Plan steps.
func acceptedSubstepsMessage(specs []SubstepSpec) string {
	accepted := make([]acceptedSubstep, 0, min(len(specs), MaxProposedSubsteps))
	for _, spec := range specs {
		accepted = append(accepted, acceptedSubstep{ID: strings.TrimSpace(spec.ID), Title: strings.TrimSpace(spec.Title)})
	}
	data, err := json.Marshal(accepted)
	if err != nil {
		return ""
	}
	return string(data)
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

// MissionHasMutation reports whether any WorkItem in the mission mutates the
// workspace. A mission with none is purely read-only/informational: there is
// no code change a Verifier could run a deterministic Check against, so
// "verification" for that mission means something different (see
// HandoffChecksPassed) than for a mission where a Builder actually ran.
// Exported so every package that decides whether a Verifier handoff counts
// as passed (internal/runcontrol's Live Plan transition included) shares this
// one definition instead of maintaining a second copy that can drift out of
// sync — that drift is exactly how this rule went half-fixed once already.
func MissionHasMutation(mission *Mission) bool {
	for _, item := range mission.WorkItems {
		if item.MutatesWorkspace {
			return true
		}
	}
	return false
}

func verificationPassed(mission *Mission) bool {
	mutating := MissionHasMutation(mission)
	for i := len(mission.Handoffs) - 1; i >= 0; i-- {
		handoff := mission.Handoffs[i]
		if handoff.Role != RoleVerifier {
			continue
		}
		if len(handoff.Checks) == 0 {
			if mutating {
				continue
			}
			// Read-only mission, nothing to run a Check against: the
			// Verifier's own cross-check reporting no issues is the pass
			// signal instead (HandoffChecksPassed applies the same rule).
			return len(handoff.Issues) == 0
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

// HandoffChecksPassed reports whether one Verifier handoff counts as passed.
// A mutation mission (a Builder actually ran) always requires at least one
// real, explicitly passing Check — never treat "nothing was run" as
// verified, that would let an unverified mutation through. A purely
// read-only/informational mission has nothing a deterministic Check could
// run against; there, an empty Issues list on the Verifier's own independent
// cross-check is the pass signal instead. A Verifier that actually found a
// problem must report it via Issues, which still fails the mission — this
// only changes the "genuinely nothing to check" case from an unconditional
// failure to a real signal. Exported: this is the single definition of
// "passed" for a Verifier handoff — internal/runcontrol calls this directly
// for the Live Plan transition rather than keeping its own copy.
func HandoffChecksPassed(mission *Mission, handoff Handoff) bool {
	if len(handoff.Checks) == 0 {
		if MissionHasMutation(mission) {
			return false
		}
		return len(handoff.Issues) == 0
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
