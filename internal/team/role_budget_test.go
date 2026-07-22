package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRoleBudgetExceededUnit(t *testing.T) {
	cases := []struct {
		name   string
		limits map[Role]Usage
		used   Usage
		want   string
	}{
		{"nil limits never exceed", nil, Usage{ModelCalls: 99, Tokens: 999}, ""},
		{"missing role entry never exceeds", map[Role]Usage{RoleBuilder: {ModelCalls: 1}}, Usage{ModelCalls: 99}, ""},
		{"zero limit entry means no limit", map[Role]Usage{RoleExplorer: {}}, Usage{ModelCalls: 99}, ""},
		{"under limit", map[Role]Usage{RoleExplorer: {ModelCalls: 3, Tokens: 300}}, Usage{ModelCalls: 2, Tokens: 200}, ""},
		{"meeting call limit exceeds", map[Role]Usage{RoleExplorer: {ModelCalls: 2}}, Usage{ModelCalls: 2}, "role explorer model-call budget exceeded (2/2 calls)"},
		{"meeting token limit exceeds", map[Role]Usage{RoleExplorer: {Tokens: 200}}, Usage{ModelCalls: 1, Tokens: 200}, "role explorer token budget exceeded (200/200 tokens)"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mission, err := NewMission("mission-role-budget", "run", "goal", DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			mission.Budget.RoleLimits = testCase.limits
			mission.UsageByRole[RoleExplorer] = testCase.used
			if got := mission.RoleBudgetExceeded(RoleExplorer); got != testCase.want {
				t.Fatalf("RoleBudgetExceeded=%q want %q", got, testCase.want)
			}
		})
	}
}

// TestCoordinatorRoleBudgetExceededTerminatesMission drives the post-outcome
// per-role gate: one outcome pushes the role past its call ceiling.
func TestCoordinatorRoleBudgetExceededTerminatesMission(t *testing.T) {
	budget := DefaultBudget()
	budget.RoleLimits = map[Role]Usage{RoleExplorer: {ModelCalls: 1}}
	mission, err := NewMission("mission-role-cap", "run", "goal", budget)
	if err != nil {
		t.Fatal(err)
	}
	if err = mission.AddWork(WorkItem{ID: "explore", Title: "Explore", Goal: "inspect", Role: RoleExplorer}); err != nil {
		t.Fatal(err)
	}
	events := make([]TeamEvent, 0)
	coordinator := Coordinator{Worker: overBudgetWorker{}, Sink: func(event TeamEvent) { events = append(events, event) }}
	err = coordinator.Run(context.Background(), mission)
	if err == nil || mission.Status != Failed {
		t.Fatalf("status=%s err=%v", mission.Status, err)
	}
	if !strings.Contains(mission.Error, "explorer") || !strings.Contains(mission.Error, "model-call budget exceeded") {
		t.Fatalf("error must name the role and the hit limit: %q", mission.Error)
	}
	if len(events) == 0 || events[len(events)-1].Type != MissionFailed {
		t.Fatalf("events=%+v", events)
	}
}

// TestCoordinatorRoleBudgetBlocksNextStart drives the pre-start per-role gate:
// a mission restored with usage already at the role ceiling must not start
// another item of that role.
func TestCoordinatorRoleBudgetBlocksNextStart(t *testing.T) {
	budget := DefaultBudget()
	budget.RoleLimits = map[Role]Usage{RoleExplorer: {ModelCalls: 1}}
	mission, err := NewMission("mission-role-cap-start", "run", "goal", budget)
	if err != nil {
		t.Fatal(err)
	}
	if err = mission.AddWork(WorkItem{ID: "explore", Title: "Explore", Goal: "inspect", Role: RoleExplorer}); err != nil {
		t.Fatal(err)
	}
	mission.Usage = Usage{ModelCalls: 1, Tokens: 100}
	mission.UsageByRole[RoleExplorer] = Usage{ModelCalls: 1, Tokens: 100}
	worker := &fakeWorker{}
	err = (&Coordinator{Worker: worker}).Run(context.Background(), mission)
	if err == nil || mission.Status != Failed {
		t.Fatalf("status=%s err=%v", mission.Status, err)
	}
	if len(worker.calls) != 0 {
		t.Fatalf("no work may start past the role ceiling: calls=%v", worker.calls)
	}
	if !strings.Contains(mission.Error, "explorer") || !strings.Contains(mission.Error, "model-call budget exceeded") {
		t.Fatalf("error must name the role and the hit limit: %q", mission.Error)
	}
	if item := mission.work("explore"); item.Status != WorkQueued || item.Attempt != 0 {
		t.Fatalf("blocked item must stay queued: %+v", item)
	}
}

// TestCoordinatorRoleUnderLimitRunsUnaffected: a role below its ceiling and a
// role without any ceiling both run to completion.
func TestCoordinatorRoleUnderLimitRunsUnaffected(t *testing.T) {
	budget := DefaultBudget()
	budget.RoleLimits = map[Role]Usage{RoleBuilder: {ModelCalls: 3, Tokens: 300}}
	mission, err := DefaultMission("mission-role-ok", "run", "build it", budget)
	if err != nil {
		t.Fatal(err)
	}
	if err = (&Coordinator{Worker: &fakeWorker{}}).Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	if mission.Status != Completed {
		t.Fatalf("status=%s error=%q", mission.Status, mission.Error)
	}
	// Builder = build(1) + repair(1), below its ceiling of 3 calls/300 tokens.
	if got := mission.UsageByRole[RoleBuilder]; got != (Usage{ModelCalls: 2, Tokens: 200}) {
		t.Fatalf("builder usage=%+v", got)
	}
}

// TestCoordinatorNilRoleLimitsKeepsLegacyBehavior: without RoleLimits the
// per-role layer enforces nothing (backward compatibility).
func TestCoordinatorNilRoleLimitsKeepsLegacyBehavior(t *testing.T) {
	if DefaultBudget().RoleLimits != nil {
		t.Fatalf("DefaultBudget must not set RoleLimits: %+v", DefaultBudget())
	}
	mission, err := DefaultMission("mission-no-role-cap", "run", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if err = (&Coordinator{Worker: &fakeWorker{}}).Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	if mission.Status != Completed {
		t.Fatalf("status=%s error=%q", mission.Status, mission.Error)
	}
}

func TestMissionJSONRoundTripPreservesRoleLimits(t *testing.T) {
	budget := DefaultBudget()
	budget.RoleLimits = map[Role]Usage{RoleBuilder: {ModelCalls: 4, Tokens: 40_000}, RoleVerifier: {ModelCalls: 2}}
	mission, err := NewMission("mission-persist", "run", "goal", budget)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(mission)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Mission
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Budget.RoleLimits) != 2 || decoded.Budget.RoleLimits[RoleBuilder] != (Usage{ModelCalls: 4, Tokens: 40_000}) || decoded.Budget.RoleLimits[RoleVerifier] != (Usage{ModelCalls: 2}) {
		t.Fatalf("role_limits did not round-trip: %+v", decoded.Budget.RoleLimits)
	}

	// A mission persisted before RoleLimits existed loads with no enforcement.
	legacy := `{"id":"m","run_id":"r","goal":"g","template":"t","status":"queued",` +
		`"budget":{"max_work_items":12,"max_model_calls":30,"max_tokens":250000,"timeout":2700000000000},` +
		`"usage":{"model_calls":0,"tokens":0},"work_items":[],"handoffs":[],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	var old Mission
	if err = json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatal(err)
	}
	if old.Budget.RoleLimits != nil {
		t.Fatalf("legacy mission gained role limits: %+v", old.Budget.RoleLimits)
	}
	old.UsageByRole = map[Role]Usage{RoleBuilder: {ModelCalls: 999, Tokens: 999}}
	if reason := old.RoleBudgetExceeded(RoleBuilder); reason != "" {
		t.Fatalf("legacy mission must not enforce role budgets: %q", reason)
	}
}

// TestRetryKeepsIdentityAndRoleUsage: after a failed verification requeues the
// repair and the verifier, both retry under their own WorkItem identity — same
// ID, same role, own Attempt counter — and retry usage accrues only to their
// own roles in UsageByRole.
func TestRetryKeepsIdentityAndRoleUsage(t *testing.T) {
	mission, err := DefaultMission("mission-retry-owner", "run", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	worker := &retryVerifierWorker{}
	if err = (&Coordinator{Worker: worker, MaxRepairAttempts: 3}).Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	if mission.Status != Completed || worker.verifyCalls != 2 || worker.repairCalls != 2 {
		t.Fatalf("mission=%s verify=%d repair=%d", mission.Status, worker.verifyCalls, worker.repairCalls)
	}
	repair := mission.work("repair")
	if repair == nil || repair.ID != "repair" || repair.Role != RoleBuilder || repair.Attempt != 2 || !repair.MutatesWorkspace {
		t.Fatalf("repair did not retry under its own identity: %+v", repair)
	}
	verifier := mission.work("verify")
	if verifier == nil || verifier.ID != "verify" || verifier.Role != RoleVerifier || verifier.Attempt != 2 {
		t.Fatalf("verifier did not retry under its own identity: %+v", verifier)
	}
	// Both attempts produced handoffs under the same WorkItem IDs.
	var repairHandoffs, verifyHandoffs int
	for _, handoff := range mission.Handoffs {
		switch handoff.WorkItemID {
		case "repair":
			repairHandoffs++
			if handoff.Role != RoleBuilder {
				t.Fatalf("repair handoff role=%s", handoff.Role)
			}
		case "verify":
			verifyHandoffs++
			if handoff.Role != RoleVerifier {
				t.Fatalf("verify handoff role=%s", handoff.Role)
			}
		}
	}
	if repairHandoffs != 2 || verifyHandoffs != 2 {
		t.Fatalf("repair handoffs=%d verify handoffs=%d", repairHandoffs, verifyHandoffs)
	}
	// Retry usage accrues only to the owning role: builder = build(1) +
	// repair(2), verifier = verify(2), nobody else gained their usage.
	if got := mission.UsageByRole[RoleBuilder]; got != (Usage{ModelCalls: 3, Tokens: 300}) {
		t.Fatalf("builder usage=%+v", got)
	}
	if got := mission.UsageByRole[RoleVerifier]; got != (Usage{ModelCalls: 2, Tokens: 200}) {
		t.Fatalf("verifier usage=%+v", got)
	}
	if got := mission.UsageByRole[RoleExplorer]; got != (Usage{ModelCalls: 1, Tokens: 100}) {
		t.Fatalf("explorer usage=%+v", got)
	}
}
