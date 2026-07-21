package evals

import "testing"

func TestPlanFidelityFixtures(t *testing.T) {
	fixture, err := LoadFixture[PlanFidelityFixture]("testdata/plan_fidelity.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range fixture.TransitionScripts {
		t.Run("transition/"+script.Name, func(t *testing.T) {
			GradeTransitionScript(t, script)
		})
	}
	for _, script := range fixture.TeamEventScripts {
		t.Run("team_event/"+script.Name, func(t *testing.T) {
			GradeTeamEventScript(t, script)
		})
	}
	for _, script := range fixture.SubstepProposalScripts {
		t.Run("substep_proposal/"+script.Name, func(t *testing.T) {
			GradeSubstepProposalScript(t, script)
		})
	}
}

func TestHandoffQualityFixtures(t *testing.T) {
	fixture, err := LoadFixture[HandoffQualityFixture]("testdata/handoff_quality.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			GradeHandoffScenario(t, scenario)
		})
	}
}
