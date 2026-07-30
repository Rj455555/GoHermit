package loop

import (
	"strings"
	"testing"
	"time"
)

func employeeLoopDefinition() Definition {
	value := validDefinition()
	value.Name = "私人知识管家"
	value.EmployeeID = "employee-knowledge"
	value.Contract = Contract{
		Goal:             "Archive new owner knowledge into a durable, searchable collection.",
		Boundaries:       []string{"Never invent facts.", "Never persist credentials."},
		SOP:              []string{"Inspect new material.", "Classify and deduplicate it.", "Write a cited report."},
		DefinitionOfDone: []string{"Every accepted item has provenance.", "A bounded run report exists."},
		StopConditions:   []string{"Stop and ask the owner when classification is ambiguous."},
	}
	value.Schedule = Schedule{
		Kind:      ScheduleDaily,
		LocalTime: "02:00",
		Timezone:  "Asia/Shanghai",
	}
	return value
}

func TestEmployeeLoopContractIsRequiredAndBounded(t *testing.T) {
	valid := employeeLoopDefinition()
	if err := ValidateDefinition(valid); err != nil {
		t.Fatalf("ValidateDefinition(valid employee loop) = %v", err)
	}

	tests := map[string]func(*Definition){
		"missing goal":       func(value *Definition) { value.Contract.Goal = "" },
		"missing sop":        func(value *Definition) { value.Contract.SOP = nil },
		"missing boundaries": func(value *Definition) { value.Contract.Boundaries = nil },
		"secret in contract": func(value *Definition) { value.Contract.Goal = "api_key=deadbeef00000000000000000000" },
		"bad schedule time":  func(value *Definition) { value.Schedule.LocalTime = "25:90" },
		"bad timezone":       func(value *Definition) { value.Schedule.Timezone = "Mars/Olympus" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := employeeLoopDefinition()
			mutate(&value)
			if err := ValidateDefinition(value); err == nil {
				t.Fatalf("ValidateDefinition(%s) succeeded, want error", name)
			}
		})
	}
}

func TestLegacyLoopMayRemainManualWithoutEmployeeContract(t *testing.T) {
	value := validDefinition()
	if value.EmployeeID != "" || !value.Contract.Empty() || value.Schedule.Kind != "" {
		t.Fatal("legacy fixture unexpectedly carries employee loop fields")
	}
	if err := ValidateDefinition(value); err != nil {
		t.Fatalf("legacy loop compatibility = %v", err)
	}
}

func TestRenderContractMarkdownSeparatesContractFromRuntimeState(t *testing.T) {
	value := employeeLoopDefinition()
	markdown, err := RenderContractMarkdown(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{
		"# 私人知识管家",
		"## Goal",
		"## Boundaries",
		"## SOP",
		"## Definition of Done",
		"## Stop Conditions",
		"employee-knowledge",
		"02:00",
	} {
		if !strings.Contains(markdown, section) {
			t.Fatalf("contract markdown misses %q:\n%s", section, markdown)
		}
	}
	for _, forbidden := range []string{"## State", "## Logs", "last_invocation_id"} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("contract markdown contains mutable runtime field %q", forbidden)
		}
	}
}

func TestNextScheduledTimeUsesDeclaredTimezone(t *testing.T) {
	schedule := Schedule{Kind: ScheduleDaily, LocalTime: "02:00", Timezone: "Asia/Shanghai"}
	after := time.Date(2026, 7, 31, 17, 59, 0, 0, time.UTC) // 01:59 in Shanghai.
	next, err := NextScheduledTime(schedule, after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
	again, err := NextScheduledTime(schedule, next)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Equal(want.Add(24 * time.Hour)) {
		t.Fatalf("next after fire = %s", again)
	}
}

func TestRuntimeStateProjectsInvocationWithoutBecomingExecutionTruth(t *testing.T) {
	now := time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	state := NewRuntimeState(employeeLoopDefinition(), now)
	invocation, err := NewInvocation(employeeLoopDefinition(), TriggerSchedule, "archive new knowledge", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.Dispatch(); err != nil {
		t.Fatal(err)
	}
	if err := invocation.Attach("session-a", "run-a", now); err != nil {
		t.Fatal(err)
	}
	if err := invocation.Complete(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated, err := ProjectRuntimeState(state, invocation, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastInvocationID != invocation.ID || updated.LastStatus != Completed ||
		updated.ConsecutiveFailures != 0 || updated.TotalRuns != 1 || updated.SuccessfulRuns != 1 {
		t.Fatalf("runtime projection = %+v", updated)
	}
}
