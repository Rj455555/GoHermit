package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// substepMission returns a running mission whose explorer completed and whose
// verifier and lead are still queued.
func substepMission(t *testing.T) *Mission {
	t.Helper()
	m, err := NewMission("mission-substeps", "run-1", "inspect and advise", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []WorkItem{
		{ID: "explore", Title: "Explore", Goal: "inspect", Role: RoleExplorer},
		{ID: "verify", Title: "Verify", Goal: "verify", Role: RoleVerifier, DependsOn: []string{"explore"}},
		{ID: "lead", Title: "Lead", Goal: "synthesize", Role: RoleLead, DependsOn: []string{"verify"}},
	} {
		if err = m.AddWork(item); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.Start("explore"); err != nil {
		t.Fatal(err)
	}
	if err = m.Complete("explore", Handoff{ID: "handoff-explore", WorkItemID: "explore", Role: RoleExplorer, Summary: "inspected"}); err != nil {
		t.Fatal(err)
	}
	return m
}

func validSubstepSpecs() []SubstepSpec {
	return []SubstepSpec{
		{ID: "inspect_auth", Title: "梳理认证流程", Goal: "inspect the auth flow", Role: RoleExplorer, DependsOn: []string{"verify"}},
		{ID: "cross_check", Title: "交叉核对", Goal: "cross-check the evidence", Role: RoleVerifier, DependsOn: []string{"inspect_auth"}},
	}
}

func TestAddSubstepsAcceptsValidProposalAndRewiresQueuedLead(t *testing.T) {
	m := substepMission(t)
	if err := m.AddSubsteps(validSubstepSpecs()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"inspect_auth", "cross_check"} {
		item := m.work(id)
		if item == nil || item.MutatesWorkspace || item.Status != WorkQueued {
			t.Fatalf("substep %q is not a read-only queued work item: %+v", id, item)
		}
	}
	if got := strings.Join(m.work("inspect_auth").DependsOn, ","); got != "verify" {
		t.Fatalf("inspect_auth depends_on=%s", got)
	}
	if got := strings.Join(m.work("cross_check").DependsOn, ","); got != "inspect_auth" {
		t.Fatalf("cross_check depends_on=%s", got)
	}
	lead := m.work("lead")
	if got := strings.Join(lead.DependsOn, ","); got != "verify,inspect_auth,cross_check" {
		t.Fatalf("lead depends_on=%s", got)
	}
	if len(m.Handoffs) != 1 || m.Handoffs[0].ID != "handoff-explore" || m.work("explore").Status != WorkCompleted {
		t.Fatalf("prior history was not preserved: %+v", m)
	}
}

func TestAddSubstepsAcceptsOutOfOrderPeerDependencies(t *testing.T) {
	m := substepMission(t)
	specs := []SubstepSpec{
		{ID: "cross_check", Title: "交叉核对证据", Goal: "cross-check evidence", Role: RoleVerifier, DependsOn: []string{"inspect_auth"}},
		{ID: "inspect_auth", Title: "核查认证流程", Goal: "inspect auth flow", Role: RoleExplorer, DependsOn: []string{"verify"}},
	}
	if err := m.AddSubsteps(specs); err != nil {
		t.Fatalf("out-of-order peer dependencies must not partially apply: %v", err)
	}
	if got := strings.Join(m.work("cross_check").DependsOn, ","); got != "inspect_auth" {
		t.Fatalf("cross_check depends_on=%s", got)
	}
	if got := strings.Join(m.work("lead").DependsOn, ","); got != "verify,inspect_auth,cross_check" {
		t.Fatalf("lead depends_on=%s", got)
	}
}

func TestValidateSubstepProposalAcceptsRunningDependency(t *testing.T) {
	m := substepMission(t)
	if err := m.Start("verify"); err != nil {
		t.Fatal(err)
	}
	specs := []SubstepSpec{{ID: "probe_runtime", Title: "核查运行时状态", Goal: "probe runtime state", Role: RoleExplorer, DependsOn: []string{"verify"}}}
	if err := ValidateSubstepProposal(m, specs); err != nil {
		t.Fatalf("dependency on a running work item should be allowed: %v", err)
	}
}

func TestValidateSubstepProposalRejectsInvalidProposals(t *testing.T) {
	oversized := make([]SubstepSpec, 0, MaxProposedSubsteps+1)
	for i := 0; i <= MaxProposedSubsteps; i++ {
		oversized = append(oversized, SubstepSpec{ID: fmt.Sprintf("substep_%d", i), Title: "子步骤", Goal: "goal", Role: RoleExplorer})
	}
	manyDeps := make([]string, 0, MaxProposedSubsteps+1)
	for i := 0; i <= MaxProposedSubsteps; i++ {
		manyDeps = append(manyDeps, fmt.Sprintf("dep_%d", i))
	}
	valid := SubstepSpec{ID: "inspect_auth", Title: "梳理认证流程", Goal: "inspect the auth flow", Role: RoleExplorer}
	cases := []struct {
		name  string
		specs []SubstepSpec
		want  string
	}{
		{"empty proposal", nil, "must contain"},
		{"oversized proposal", oversized, "must contain"},
		{"unsafe id slash", []SubstepSpec{{ID: "a/b", Title: "t", Goal: "g", Role: RoleExplorer}}, "empty or unsafe"},
		{"unsafe id traversal", []SubstepSpec{{ID: "../x", Title: "t", Goal: "g", Role: RoleExplorer}}, "empty or unsafe"},
		{"duplicate within proposal", []SubstepSpec{valid, valid}, "duplicate substep id"},
		{"collision with completed id", []SubstepSpec{{ID: "explore", Title: "t", Goal: "g", Role: RoleExplorer}}, "already exists"},
		{"collision with queued id", []SubstepSpec{{ID: "verify", Title: "t", Goal: "g", Role: RoleExplorer}}, "already exists"},
		{"empty title", []SubstepSpec{{ID: "sub", Title: " ", Goal: "g", Role: RoleExplorer}}, "bounded title"},
		{"oversized title", []SubstepSpec{{ID: "sub", Title: strings.Repeat("t", 201), Goal: "g", Role: RoleExplorer}}, "bounded title"},
		{"empty goal", []SubstepSpec{{ID: "sub", Title: "t", Goal: " ", Role: RoleExplorer}}, "bounded goal"},
		{"too many dependencies", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleExplorer, DependsOn: manyDeps}}, "too many dependencies"},
		{"lead role rejected", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleLead}}, "not a read-only role"},
		{"builder role rejected", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleBuilder}}, "not a read-only role"},
		{"operator role rejected", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleOperator}}, "not a read-only role"},
		{"unknown dependency", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleExplorer, DependsOn: []string{"ghost"}}}, "unknown dependency"},
		{"completed dependency", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleExplorer, DependsOn: []string{"explore"}}}, "cannot depend on completed work item"},
		{"self cycle", []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleExplorer, DependsOn: []string{"sub"}}}, "dependency cycle"},
		{"peer cycle", []SubstepSpec{
			{ID: "sub_a", Title: "a", Goal: "g", Role: RoleExplorer, DependsOn: []string{"sub_b"}},
			{ID: "sub_b", Title: "b", Goal: "g", Role: RoleReviewer, DependsOn: []string{"sub_a"}},
		}, "dependency cycle"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			m := substepMission(t)
			err := ValidateSubstepProposal(m, testCase.specs)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("err=%v want substring %q", err, testCase.want)
			}
		})
	}
}

func TestValidateSubstepProposalRejectsCancelledDependency(t *testing.T) {
	m := substepMission(t)
	m.Cancel("owner stopped")
	specs := []SubstepSpec{{ID: "sub", Title: "t", Goal: "g", Role: RoleExplorer, DependsOn: []string{"verify"}}}
	err := ValidateSubstepProposal(m, specs)
	if err == nil || !strings.Contains(err.Error(), "cannot depend on cancelled work item") {
		t.Fatalf("err=%v", err)
	}
}

func TestAddSubstepsIsAtomicOnInvalidProposal(t *testing.T) {
	m := substepMission(t)
	before := len(m.WorkItems)
	specs := append(validSubstepSpecs(), SubstepSpec{ID: "bad", Title: "t", Goal: "g", Role: RoleBuilder})
	if err := m.AddSubsteps(specs); err == nil {
		t.Fatal("expected role rejection")
	}
	if len(m.WorkItems) != before || strings.Join(m.work("lead").DependsOn, ",") != "verify" {
		t.Fatalf("rejected proposal mutated the mission: %+v", m.WorkItems)
	}
}

func TestValidateHandoffRejectsOversizedSubsteps(t *testing.T) {
	m := substepMission(t)
	if err := m.Start("verify"); err != nil {
		t.Fatal(err)
	}
	handoff := Handoff{ID: "handoff-verify", WorkItemID: "verify", Role: RoleVerifier, Summary: "verified"}
	for i := 0; i <= MaxProposedSubsteps; i++ {
		handoff.Substeps = append(handoff.Substeps, SubstepSpec{ID: fmt.Sprintf("substep_%d", i), Title: "t", Goal: "g", Role: RoleExplorer})
	}
	if err := m.Complete("verify", handoff); err == nil || !strings.Contains(err.Error(), "bounded limits") {
		t.Fatalf("err=%v", err)
	}
	if m.work("verify").Status != WorkRunning || len(m.Handoffs) != 1 {
		t.Fatalf("rejected handoff polluted mission state: %+v", m)
	}
}

// substepWorker makes the explorer propose substeps, valid or colliding.
type substepWorker struct {
	colliding bool
}

func (w substepWorker) Execute(_ context.Context, assignment Assignment) (Result, error) {
	handoff := Handoff{ID: "handoff-" + assignment.WorkItem.ID, WorkItemID: assignment.WorkItem.ID, Role: assignment.WorkItem.Role, Summary: "done " + assignment.WorkItem.ID}
	if assignment.WorkItem.Role == RoleExplorer && assignment.WorkItem.ID == "explore" {
		if w.colliding {
			handoff.Substeps = []SubstepSpec{{ID: "explore", Title: "复用标识", Goal: "collide", Role: RoleExplorer}}
		} else {
			handoff.Substeps = []SubstepSpec{{ID: "inspect_auth", Title: "梳理认证流程", Goal: "inspect the auth flow", Role: RoleExplorer}}
		}
	}
	if assignment.WorkItem.Role == RoleVerifier {
		handoff.Checks = []Check{{Command: "go test ./...", Passed: true, Summary: "ok"}}
	}
	return Result{Handoff: handoff, ModelCalls: 1, Tokens: 100}, nil
}

func TestCoordinatorAcceptsExplorerSubstepsAndKeepsRunning(t *testing.T) {
	mission, err := DefaultMission("mission-substeps", "run-1", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	events := make([]TeamEvent, 0)
	coordinator := Coordinator{Worker: substepWorker{}, Sink: func(event TeamEvent) { events = append(events, event) }}
	if err = coordinator.Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	var accepted *TeamEvent
	for i := range events {
		if events[i].Type == SubstepsAccepted {
			accepted = &events[i]
		}
		if events[i].Type == SubstepsRejected {
			t.Fatalf("valid proposal was rejected: %+v", events[i])
		}
	}
	if accepted == nil || accepted.WorkItemID != "explore" || accepted.Role != RoleExplorer {
		t.Fatalf("missing substeps_accepted event: %+v", events)
	}
	var summaries []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err = json.Unmarshal([]byte(accepted.Message), &summaries); err != nil || len(summaries) != 1 || summaries[0].ID != "inspect_auth" {
		t.Fatalf("accepted message=%q err=%v", accepted.Message, err)
	}
	item := mission.work("inspect_auth")
	if item == nil || item.MutatesWorkspace || item.Status != WorkCompleted {
		t.Fatalf("accepted substep did not run as read-only work: %+v", item)
	}
	if lead := mission.work("lead"); !contains(lead.DependsOn, "inspect_auth") {
		t.Fatalf("lead was not rewired: %+v", lead.DependsOn)
	}
	if mission.Status != Completed {
		t.Fatalf("mission=%s", mission.Status)
	}
}

func TestCoordinatorRejectsInvalidSubstepsWithoutFailingMission(t *testing.T) {
	mission, err := DefaultMission("mission-reject", "run-1", "build it", DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	events := make([]TeamEvent, 0)
	coordinator := Coordinator{Worker: substepWorker{colliding: true}, Sink: func(event TeamEvent) { events = append(events, event) }}
	if err = coordinator.Run(context.Background(), mission); err != nil {
		t.Fatal(err)
	}
	rejected := false
	for _, event := range events {
		if event.Type == SubstepsAccepted {
			t.Fatalf("invalid proposal was accepted: %+v", event)
		}
		if event.Type == SubstepsRejected && event.WorkItemID == "explore" && strings.Contains(event.Message, "already exists") {
			rejected = true
		}
	}
	if !rejected {
		t.Fatalf("missing substeps_rejected event: %+v", events)
	}
	if len(mission.WorkItems) != 6 || mission.Status != Completed {
		t.Fatalf("rejected proposal changed the topology: %+v", mission.WorkItems)
	}
}
