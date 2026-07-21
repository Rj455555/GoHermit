// Package runcontrol owns run and plan state transitions independently from
// HTTP, CLI, or other presentation layers.
package runcontrol

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
)

type Transition struct {
	Changed bool
	StepID  string
	Detail  string
}

func ApplyTeamEvent(plan *taskplan.Plan, teamEvent team.TeamEvent, mission *team.Mission) (Transition, error) {
	if plan == nil || plan.Status != taskplan.Active {
		return Transition{}, nil
	}
	stepID, detail := teamEvent.WorkItemID, teamEvent.Message
	var changed bool
	var err error
	switch teamEvent.Type {
	case team.WorkItemStarted:
		changed, err = plan.Start(stepID, detail)
	case team.WorkItemDone:
		if teamEvent.Role == team.RoleVerifier && !verifierHandoffPassed(mission, stepID) {
			detail = "独立验证未通过"
			if retrySteps := queuedVerificationRetry(mission, stepID); len(retrySteps) > 0 {
				detail = "独立验证未通过，已重新进入修复与验证"
				changed, err = plan.Reopen(retrySteps, detail)
				stepID = retrySteps[0]
			} else {
				changed, err = plan.Fail(stepID, detail)
			}
		} else {
			changed, err = plan.Complete(stepID, detail)
		}
	case team.WorkItemFailed:
		changed, err = plan.Fail(stepID, detail)
	case team.SubstepsAccepted:
		// The coordinator message is a bounded JSON array of {id, title};
		// a malformed payload must never panic or change the plan.
		var specs []taskplan.StepSpec
		if decodeErr := json.Unmarshal([]byte(teamEvent.Message), &specs); decodeErr != nil {
			return Transition{}, nil
		}
		changed, err = plan.AddSteps(specs)
		if changed {
			stepID = specs[0].ID
			detail = fmt.Sprintf("Explorer 提议 %d 个任务子步骤", len(specs))
		}
	case team.SubstepsRejected:
		// Rejected proposals never change the plan; the reason still reaches
		// the owner as a session event via the web sink's default mapping.
		return Transition{}, nil
	case team.MissionFailed:
		if stepID == "" {
			if current := plan.Current(); current != nil {
				stepID = current.ID
			} else if next := plan.NextPending(); next != nil {
				stepID = next.ID
			}
		}
		if stepID != "" {
			changed, err = plan.Fail(stepID, detail)
		}
	}
	return Transition{Changed: changed, StepID: stepID, Detail: detail}, err
}

func queuedVerificationRetry(mission *team.Mission, verifierID string) []string {
	if mission == nil {
		return nil
	}
	var verifier *team.WorkItem
	for i := range mission.WorkItems {
		if mission.WorkItems[i].ID == verifierID {
			verifier = &mission.WorkItems[i]
			break
		}
	}
	if verifier == nil || verifier.Status != team.WorkQueued || verifier.Attempt == 0 {
		return nil
	}
	queued := make([]string, 0, len(verifier.DependsOn)+1)
	for _, dependency := range verifier.DependsOn {
		for i := range mission.WorkItems {
			item := mission.WorkItems[i]
			// A queued mutating dependency at this point was requeued by
			// RequeueAfterVerification. Attempt stays 0 for a repair that was
			// skipped after an advisory-only review and un-skipped by this
			// verification failure, so the attempt count must not gate it.
			if item.ID == dependency && item.MutatesWorkspace && item.Status == team.WorkQueued {
				queued = append(queued, item.ID)
			}
		}
	}
	if len(queued) == 0 {
		return nil
	}
	return append(queued, verifierID)
}

func Interrupt(plan *taskplan.Plan, detail string) (Transition, error) {
	if plan == nil || plan.Status != taskplan.Active {
		return Transition{}, nil
	}
	step := plan.Current()
	if step == nil {
		return Transition{}, errors.New("active plan has no current step to interrupt")
	}
	changed, err := plan.Note(step.ID, detail)
	return Transition{Changed: changed, StepID: step.ID, Detail: detail}, err
}

func Cancel(plan *taskplan.Plan, detail string) (Transition, error) {
	if plan == nil {
		return Transition{}, nil
	}
	changed, stepID := plan.Cancel(detail)
	return Transition{Changed: changed, StepID: stepID, Detail: detail}, nil
}

func verifierHandoffPassed(mission *team.Mission, workItemID string) bool {
	if mission == nil {
		return false
	}
	for i := len(mission.Handoffs) - 1; i >= 0; i-- {
		handoff := mission.Handoffs[i]
		if handoff.WorkItemID != workItemID || handoff.Role != team.RoleVerifier || len(handoff.Checks) == 0 {
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
