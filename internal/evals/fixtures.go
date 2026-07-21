// Package evals grades deterministic eval fixtures for plan fidelity and
// handoff quality. It also exports the fixture types and loader shared by the
// recovery, verification, and owner-summary graders, which live as test files
// inside the packages whose unexported hooks they exercise.
package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Rj455555/GoHermit/internal/team"
)

// LoadFixture decodes a checked-in JSON fixture, rejecting unknown fields so
// stale or mistyped scenarios fail loudly instead of being silently skipped.
func LoadFixture[T any](path string) (T, error) {
	var fixture T
	data, err := os.ReadFile(path)
	if err != nil {
		return fixture, fmt.Errorf("read fixture %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&fixture); err != nil {
		return fixture, fmt.Errorf("decode fixture %s: %w", path, err)
	}
	return fixture, nil
}

type StepSpecFixture struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
}

type CheckFixture struct {
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary,omitempty"`
}

// HandoffFixture describes a candidate handoff. The count fields generate
// filler entries so boundary scenarios stay human-auditable in JSON.
type HandoffFixture struct {
	ID                 string         `json:"id"`
	WorkItemID         string         `json:"work_item_id"`
	Role               string         `json:"role"`
	Summary            string         `json:"summary,omitempty"`
	SummarySize        int            `json:"summary_size,omitempty"`
	Evidence           []string       `json:"evidence,omitempty"`
	EvidenceCount      int            `json:"evidence_count,omitempty"`
	ModifiedFiles      []string       `json:"modified_files,omitempty"`
	ModifiedFilesCount int            `json:"modified_files_count,omitempty"`
	Checks             []CheckFixture `json:"checks,omitempty"`
	ChecksCount        int            `json:"checks_count,omitempty"`
	Issues             []string       `json:"issues,omitempty"`
	IssuesCount        int            `json:"issues_count,omitempty"`
	NextSteps          []string       `json:"next_steps,omitempty"`
	NextStepsCount     int            `json:"next_steps_count,omitempty"`
}

func (h HandoffFixture) Build() team.Handoff {
	handoff := team.Handoff{ID: h.ID, WorkItemID: h.WorkItemID, Role: team.Role(h.Role), Summary: h.Summary, Evidence: h.Evidence, ModifiedFiles: h.ModifiedFiles, Issues: h.Issues, NextSteps: h.NextSteps}
	if h.SummarySize > 0 {
		handoff.Summary = strings.Repeat("s", h.SummarySize)
	}
	for _, check := range h.Checks {
		handoff.Checks = append(handoff.Checks, team.Check{Command: check.Command, Passed: check.Passed, Summary: check.Summary})
	}
	if h.EvidenceCount > 0 {
		handoff.Evidence = filler(h.EvidenceCount, "evidence")
	}
	if h.ModifiedFilesCount > 0 {
		handoff.ModifiedFiles = filler(h.ModifiedFilesCount, "file")
	}
	if h.IssuesCount > 0 {
		handoff.Issues = filler(h.IssuesCount, "issue")
	}
	if h.NextStepsCount > 0 {
		handoff.NextSteps = filler(h.NextStepsCount, "next")
	}
	if h.ChecksCount > 0 {
		handoff.Checks = make([]team.Check, 0, h.ChecksCount)
		for i := 0; i < h.ChecksCount; i++ {
			handoff.Checks = append(handoff.Checks, team.Check{Command: fmt.Sprintf("check-%d", i), Passed: true})
		}
	}
	return handoff
}

func filler(count int, prefix string) []string {
	values := make([]string, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, fmt.Sprintf("%s-%d", prefix, i))
	}
	return values
}

type WorkItemFixture struct {
	ID               string   `json:"id"`
	Title            string   `json:"title,omitempty"`
	Goal             string   `json:"goal,omitempty"`
	Role             string   `json:"role"`
	Status           string   `json:"status,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty"`
	MutatesWorkspace bool     `json:"mutates_workspace,omitempty"`
	Attempt          int      `json:"attempt,omitempty"`
}

func (w WorkItemFixture) Build() team.WorkItem {
	title, goal := w.Title, w.Goal
	if title == "" {
		title = w.ID
	}
	if goal == "" {
		goal = w.ID
	}
	status := team.WorkStatus(w.Status)
	if status == "" {
		status = team.WorkQueued
	}
	return team.WorkItem{ID: w.ID, Title: title, Goal: goal, Role: team.Role(w.Role), Status: status, DependsOn: w.DependsOn, MutatesWorkspace: w.MutatesWorkspace, Attempt: w.Attempt}
}

type MissionFixture struct {
	WorkItems []WorkItemFixture `json:"work_items"`
	Handoffs  []HandoffFixture  `json:"handoffs,omitempty"`
}

func (m MissionFixture) Build() *team.Mission {
	mission := &team.Mission{ID: "mission-eval", RunID: "run-eval", Goal: "eval", Budget: team.DefaultBudget()}
	for _, item := range m.WorkItems {
		mission.WorkItems = append(mission.WorkItems, item.Build())
	}
	for _, handoff := range m.Handoffs {
		mission.Handoffs = append(mission.Handoffs, handoff.Build())
	}
	return mission
}

// Plan fidelity fixtures.

type PlanFidelityFixture struct {
	TransitionScripts      []TransitionScriptFixture      `json:"transition_scripts"`
	TeamEventScripts       []TeamEventScriptFixture       `json:"team_event_scripts"`
	SubstepProposalScripts []SubstepProposalScriptFixture `json:"substep_proposal_scripts,omitempty"`
}

type TransitionScriptFixture struct {
	Name          string            `json:"name"`
	AllowParallel bool              `json:"allow_parallel"`
	Steps         []StepSpecFixture `json:"steps"`
	Ops           []PlanOpFixture   `json:"ops"`
	Expected      PlanStateFixture  `json:"expected"`
}

type PlanOpFixture struct {
	Op            string   `json:"op"`
	ID            string   `json:"id,omitempty"`
	IDs           []string `json:"ids,omitempty"`
	Detail        string   `json:"detail,omitempty"`
	ExpectOK      bool     `json:"expect_ok"`
	ExpectChanged bool     `json:"expect_changed"`
}

type PlanStateFixture struct {
	Status   string            `json:"status"`
	Revision int               `json:"revision"`
	Steps    map[string]string `json:"steps"`
}

type TeamEventScriptFixture struct {
	Name          string                 `json:"name"`
	AllowParallel bool                   `json:"allow_parallel"`
	Steps         []StepSpecFixture      `json:"steps"`
	Mission       MissionFixture         `json:"mission"`
	Events        []TeamEventFixture     `json:"events"`
	Expected      TeamEventExpectFixture `json:"expected"`
}

type TeamEventFixture struct {
	Type       string `json:"type"`
	WorkItemID string `json:"work_item_id,omitempty"`
	Role       string `json:"role,omitempty"`
	Message    string `json:"message,omitempty"`
}

type TeamEventExpectFixture struct {
	Transitions []TransitionFixture `json:"transitions"`
	Final       PlanStateFixture    `json:"final"`
}

type TransitionFixture struct {
	Changed bool   `json:"changed"`
	StepID  string `json:"step_id"`
}

// Substep proposal fixtures.

type SubstepSpecFixture struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Goal      string   `json:"goal,omitempty"`
	Role      string   `json:"role"`
	DependsOn []string `json:"depends_on,omitempty"`
}

func (s SubstepSpecFixture) Build() team.SubstepSpec {
	goal := s.Goal
	if goal == "" {
		goal = s.ID
	}
	return team.SubstepSpec{ID: s.ID, Title: s.Title, Goal: goal, Role: team.Role(s.Role), DependsOn: s.DependsOn}
}

// SubstepProposalScriptFixture proposes Explorer substeps against a mission
// snapshot; accepted proposals are then mapped onto the Live Plan 1:1.
type SubstepProposalScriptFixture struct {
	Name      string               `json:"name"`
	Mission   MissionFixture       `json:"mission"`
	PlanSteps []StepSpecFixture    `json:"plan_steps"`
	Proposal  []SubstepSpecFixture `json:"proposal"`
	Expected  SubstepExpectFixture `json:"expected"`
}

type SubstepExpectFixture struct {
	Accept        bool     `json:"accept"`
	ErrorContains string   `json:"error_contains,omitempty"`
	LeadDependsOn []string `json:"lead_depends_on,omitempty"`
}

// Handoff quality fixtures.

type HandoffQualityFixture struct {
	Scenarios []HandoffScenarioFixture `json:"scenarios"`
}

type HandoffScenarioFixture struct {
	Name          string          `json:"name"`
	WorkItem      WorkItemFixture `json:"work_item"`
	Handoff       HandoffFixture  `json:"handoff"`
	ExpectAccept  bool            `json:"expect_accept"`
	ErrorContains string          `json:"error_contains,omitempty"`
}

// Team verification fixtures.

type TeamVerificationFixture struct {
	Scenarios []VerificationScenarioFixture `json:"scenarios"`
}

type VerificationScenarioFixture struct {
	Name              string                    `json:"name"`
	MaxRepairAttempts int                       `json:"max_repair_attempts"`
	WorkItems         []WorkItemFixture         `json:"work_items"`
	Script            []WorkerScriptFixture     `json:"script"`
	Expected          VerificationExpectFixture `json:"expected"`
}

// WorkerScriptFixture scripts one worker execution; result is one of ok,
// checks_pass, checks_fail, no_checks, or error. Unlisted (id, attempt)
// pairs default to ok.
type WorkerScriptFixture struct {
	ID      string `json:"id"`
	Attempt int    `json:"attempt"`
	Result  string `json:"result"`
}

type VerificationExpectFixture struct {
	MissionStatus         string         `json:"mission_status"`
	Attempts              map[string]int `json:"attempts"`
	Handoffs              int            `json:"handoffs"`
	PreservedFailedChecks bool           `json:"preserved_failed_checks,omitempty"`
}

// Recovery fixtures.

type RecoveryFixture struct {
	Scenarios []RecoveryScenarioFixture `json:"scenarios"`
}

type RecoveryScenarioFixture struct {
	Name       string `json:"name"`
	Events     int    `json:"events"`
	CrashStage string `json:"crash_stage"`
	Recoveries int    `json:"recoveries"`
	ActiveRun  bool   `json:"active_run"`
}

// Owner summary fixtures.

type OwnerSummaryFixture struct {
	Scenarios []OwnerSummaryScenarioFixture `json:"scenarios"`
}

type OwnerSummaryScenarioFixture struct {
	Name     string             `json:"name"`
	Run      bool               `json:"run,omitempty"`
	Handoffs []HandoffFixture   `json:"handoffs"`
	Expected OwnerSummaryExpect `json:"expected"`
}

type OwnerSummaryExpect struct {
	FinalSummary  string         `json:"final_summary"`
	ModifiedFiles []string       `json:"modified_files"`
	Checks        []CheckFixture `json:"checks,omitempty"`
}
