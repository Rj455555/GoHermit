package controlplane

import (
	"context"
	"errors"
	"strings"

	"github.com/Rj455555/GoHermit/internal/employee"
)

func (s *Service) ValidateChannelEmployee(employeeID string) error {
	record, err := s.GetEmployee(context.Background(), employeeID)
	if err != nil {
		return err
	}
	if record.Employee.State != employee.StateActive {
		return classified(KindConflict, errors.New("only an active Employee can receive Weixin messages"))
	}
	return nil
}

// CreateQueuedTask is the channel boundary. It snapshots the currently bound
// Employee and returns while the Task is still queued; it never prepares,
// starts, or otherwise touches the Session/Run execution kernel.
func (s *Service) CreateQueuedTask(ctx context.Context, employeeID, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", classified(KindInvalid, errors.New("channel message is empty"))
	}
	record, err := s.GetEmployee(ctx, employeeID)
	if err != nil {
		return "", err
	}
	readiness, err := s.DryRunEmployee(ctx, employeeID)
	if err != nil {
		return "", err
	}
	if !readiness.Ready {
		return "", classified(KindConflict, errors.New("Employee is not ready for channel task creation"))
	}
	if len(record.ProjectBindings) == 0 {
		return "", classified(KindConflict, errors.New("Employee has no ProjectBinding"))
	}
	binding := record.ProjectBindings[0]
	policy := employee.TaskPolicy{
		AllowedCapabilities: append([]string(nil), record.Employee.PermissionPolicy.AllowedCapabilities...),
		NetworkAllowed:      record.Employee.PermissionPolicy.NetworkAllowed,
		Budget:              record.Employee.BudgetPolicy,
	}
	view, err := s.CreateEmployeeTask(ctx, employeeID, EmployeeTaskCreateInput{
		Prompt:           prompt,
		Skills:           []EmployeeTaskSkillSelection{},
		Knowledge:        []EmployeeTaskKnowledgeSelection{},
		MemoryFactIDs:    []string{},
		ProjectBindingID: binding.ID,
		Policy:           policy,
	})
	if err != nil {
		return "", err
	}
	return view.ID, nil
}
