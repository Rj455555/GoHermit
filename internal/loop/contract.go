package loop

import (
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Keep IANA schedule validation working in minimal containers.
)

const (
	ScheduleManual = "manual"
	ScheduleDaily  = "daily"

	MaxContractItems   = 32
	MaxContractBytes   = 6 << 10
	MaxTaskPromptBytes = 16 << 10
)

// Contract is the durable work agreement an Employee re-reads for every
// invocation. Mutable state and run logs deliberately live outside it.
type Contract struct {
	Goal             string   `json:"goal,omitempty"`
	Boundaries       []string `json:"boundaries,omitempty"`
	SOP              []string `json:"sop,omitempty"`
	DefinitionOfDone []string `json:"definition_of_done,omitempty"`
	StopConditions   []string `json:"stop_conditions,omitempty"`
}

func (contract Contract) Empty() bool {
	return clean(contract.Goal) == "" &&
		len(contract.Boundaries) == 0 &&
		len(contract.SOP) == 0 &&
		len(contract.DefinitionOfDone) == 0 &&
		len(contract.StopConditions) == 0
}

// Schedule declares the cheapest supported trigger. An empty kind is the
// backwards-compatible legacy/manual mode.
type Schedule struct {
	Kind      string `json:"kind,omitempty"`
	LocalTime string `json:"local_time,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
}

// RuntimeState is a bounded projection over Invocation/Session/Run truth. It
// never authorizes work and is safe to rebuild from the invocation journal.
type RuntimeState struct {
	SchemaVersion       int        `json:"schema_version"`
	LoopID              string     `json:"loop_id"`
	DefinitionRevision  int        `json:"definition_revision"`
	LastInvocationID    string     `json:"last_invocation_id,omitempty"`
	LastStatus          Status     `json:"last_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	TotalRuns           int        `json:"total_runs"`
	SuccessfulRuns      int        `json:"successful_runs"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func validateContract(contract Contract) error {
	if clean(contract.Goal) == "" {
		return errors.New("employee loop contract goal is required")
	}
	if len(contract.Boundaries) == 0 {
		return errors.New("employee loop contract requires at least one boundary")
	}
	if len(contract.SOP) == 0 {
		return errors.New("employee loop contract requires at least one SOP step")
	}
	collections := []struct {
		name   string
		values []string
	}{
		{"boundaries", contract.Boundaries},
		{"SOP", contract.SOP},
		{"definition_of_done", contract.DefinitionOfDone},
		{"stop_conditions", contract.StopConditions},
	}
	for _, collection := range collections {
		if len(collection.values) > MaxContractItems {
			return fmt.Errorf("employee loop contract %s exceeds item limit %d", collection.name, MaxContractItems)
		}
		for _, value := range collection.values {
			if clean(value) == "" {
				return fmt.Errorf("employee loop contract %s contains an empty item", collection.name)
			}
		}
	}
	aggregate := len(contract.Goal)
	for _, collection := range collections {
		for _, value := range collection.values {
			aggregate += len(value)
		}
	}
	if aggregate > MaxContractBytes {
		return fmt.Errorf("employee loop contract exceeds %d bytes", MaxContractBytes)
	}
	return nil
}

func validateSchedule(schedule Schedule) error {
	switch clean(schedule.Kind) {
	case "", ScheduleManual:
		if clean(schedule.LocalTime) != "" || clean(schedule.Timezone) != "" {
			return errors.New("manual loop schedule must not declare local_time or timezone")
		}
		return nil
	case ScheduleDaily:
		if _, err := time.Parse("15:04", clean(schedule.LocalTime)); err != nil {
			return errors.New("daily loop schedule local_time must be HH:MM")
		}
		if _, err := time.LoadLocation(clean(schedule.Timezone)); err != nil {
			return errors.New("daily loop schedule timezone is invalid")
		}
		return nil
	default:
		return fmt.Errorf("unsupported loop schedule kind %q", schedule.Kind)
	}
}

func NextScheduledTime(schedule Schedule, after time.Time) (time.Time, error) {
	if err := validateSchedule(schedule); err != nil {
		return time.Time{}, err
	}
	if clean(schedule.Kind) != ScheduleDaily {
		return time.Time{}, errors.New("manual loop schedule has no next run")
	}
	location, _ := time.LoadLocation(clean(schedule.Timezone))
	clock, _ := time.Parse("15:04", clean(schedule.LocalTime))
	local := after.In(location)
	next := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC(), nil
}

func NewRuntimeState(definition Definition, now time.Time) RuntimeState {
	state := RuntimeState{
		SchemaVersion:      SchemaVersion,
		LoopID:             definition.ID,
		DefinitionRevision: definition.Revision,
		UpdatedAt:          now.UTC(),
	}
	if next, err := NextScheduledTime(definition.Schedule, now); err == nil {
		state.NextRunAt = timePointer(next)
	}
	return state
}

func ProjectRuntimeState(state RuntimeState, invocation Invocation, now time.Time) (RuntimeState, error) {
	if invocation.LoopID != state.LoopID {
		return RuntimeState{}, errors.New("runtime state and invocation loop identities differ")
	}
	if !invocation.Status.Terminal() {
		return RuntimeState{}, errors.New("runtime state only projects terminal invocations")
	}
	next := state
	next.SchemaVersion = SchemaVersion
	next.DefinitionRevision = invocation.DefinitionRevision
	next.LastInvocationID = invocation.ID
	next.LastStatus = invocation.Status
	next.LastRunAt = timePointer(invocation.CreatedAt.UTC())
	next.TotalRuns++
	if invocation.Status == Completed {
		next.SuccessfulRuns++
		next.ConsecutiveFailures = 0
	} else {
		next.ConsecutiveFailures++
	}
	if scheduled, err := NextScheduledTime(invocation.DefinitionSnapshot.Schedule, now); err == nil {
		next.NextRunAt = timePointer(scheduled)
	} else {
		next.NextRunAt = nil
	}
	next.UpdatedAt = now.UTC()
	return next, ValidateRuntimeState(next)
}

func ValidateRuntimeState(state RuntimeState) error {
	if state.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported loop runtime state version %d", state.SchemaVersion)
	}
	if err := validateID("runtime state loop id", state.LoopID); err != nil {
		return err
	}
	if state.DefinitionRevision < 1 {
		return errors.New("loop runtime state definition revision must be positive")
	}
	if state.ConsecutiveFailures < 0 || state.TotalRuns < 0 || state.SuccessfulRuns < 0 ||
		state.SuccessfulRuns > state.TotalRuns {
		return errors.New("loop runtime state counters are invalid")
	}
	if state.UpdatedAt.IsZero() {
		return errors.New("loop runtime state updated_at is required")
	}
	return nil
}

func RenderContractMarkdown(definition Definition) (string, error) {
	if clean(definition.EmployeeID) == "" {
		return "", errors.New("LOOP.md is only available for Employee-owned loops")
	}
	if err := ValidateDefinition(definition); err != nil {
		return "", err
	}
	var output strings.Builder
	fmt.Fprintf(&output, "# %s\n\n", clean(definition.Name))
	fmt.Fprintf(&output, "- Loop: `%s`\n- Employee: `%s`\n- Revision: `%d`\n", definition.ID, definition.EmployeeID, definition.Revision)
	if definition.Schedule.Kind == ScheduleDaily {
		fmt.Fprintf(&output, "- Trigger: daily at `%s` (`%s`)\n", definition.Schedule.LocalTime, definition.Schedule.Timezone)
	} else {
		output.WriteString("- Trigger: manual\n")
	}
	writeMarkdownSection(&output, "Goal", []string{definition.Contract.Goal}, false)
	writeMarkdownSection(&output, "Boundaries", definition.Contract.Boundaries, true)
	writeMarkdownSection(&output, "SOP", definition.Contract.SOP, true)
	writeMarkdownSection(&output, "Definition of Done", definition.Contract.DefinitionOfDone, true)
	writeMarkdownSection(&output, "Stop Conditions", definition.Contract.StopConditions, true)
	return output.String(), nil
}

// EmployeeTaskPrompt is the bounded immutable brief copied into the Employee
// Task. The Employee context pipeline adds skills, knowledge and memory.
func EmployeeTaskPrompt(definition Definition) (string, error) {
	contract, err := RenderContractMarkdown(definition)
	if err != nil {
		return "", err
	}
	prompt := contract + "\n## Current Run Objective\n\n" + clean(definition.TaskSource.Prompt) +
		"\n\nFollow the contract for this run. Verify the result and produce a bounded report with provenance.\n"
	if len(prompt) > MaxTaskPromptBytes {
		return "", errors.New("employee loop task prompt exceeds 16384 bytes")
	}
	return prompt, nil
}

func writeMarkdownSection(output *strings.Builder, title string, values []string, numbered bool) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(output, "\n## %s\n\n", title)
	for index, value := range values {
		if numbered {
			fmt.Fprintf(output, "%d. %s\n", index+1, clean(value))
		} else {
			fmt.Fprintf(output, "%s\n", clean(value))
		}
	}
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}
