package controlplane

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeememory"
)

func TestCreateEmployeeTaskP021LiveCases(t *testing.T) {
	fixture := newMemoryPolicyTaskFixture(t)
	facts := make(map[string]employeememory.Fact)
	add := func(key, category, value string) {
		facts[key] = addAcceptedMemoryFact(t, fixture, key, category, value, time.Date(2026, 8, 26, 0, len(facts), 0, 0, time.UTC))
	}
	add("P", "preference", "GoHermit JavaScript dependencies use pnpm, not npm.")
	add("D", "decision", "Before release, run Go tests, race, frontend tests, and E2E.")
	add("G", "known_issue", "The official Go Proxy may time out; use only a temporary build overlay to switch the proxy and do not modify the repository.")
	add("H", "verified-run", "The most recent service health check returned 200.")
	add("I", "verified-run", "The quarterly invoice export is complete.")
	add("O", "preference", "Report risks before modifying code.")
	add("M", "verified-run", "\u53d1\u5e03\u524d\u5fc5\u987b\u68c0\u67e5\u6570\u636e\u5e93\u8fc1\u79fb\u72b6\u6001\u3002")
	add("U", "decision", "Do not deploy without Owner confirmation.")
	updateEmployeeMemoryPolicy(t, fixture.employees, fixture.employee, employee.MemoryPolicy{
		CandidateGeneration: true, Promotion: employee.MemoryPromotionOwnerConfirmation, AutomaticRecall: true,
		MaxContextFacts: 12, MaxContextBytes: 16 << 10,
	})

	tests := []struct {
		name   string
		prompt string
		manual string
		want   []string
	}{
		{name: "A", prompt: "Review the frontend dependency workflow and tell me which package manager to use.", want: []string{"P"}},
		{name: "B", prompt: "\u8bf7\u51c6\u5907\u53d1\u5e03\u68c0\u67e5\uff0c\u7279\u522b\u786e\u8ba4\u6570\u636e\u5e93\u8fc1\u79fb\u72b6\u6001\u3002", want: []string{"M"}},
		{name: "C", prompt: "The official Go proxy is timing out. Prepare a safe build plan without changing the repository.", want: []string{"G"}},
		{name: "D", prompt: "Summarize the top-level README structure.", want: []string{}},
		{name: "E", prompt: "Before release, list the Go race frontend and E2E tests that must pass.", manual: "U", want: []string{"U", "D"}},
		{name: "F", prompt: "Review the frontend dependency workflow and tell me which package manager to use.", want: []string{"P"}},
	}
	var first []employee.TaskMemoryFactSnapshot
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := fixture.input()
			input.Prompt = test.prompt
			if test.manual != "" {
				input.MemoryFactIDs = []string{facts[test.manual].ID}
			}
			created, err := fixture.service.CreateEmployeeTask(context.Background(), fixture.employee, input)
			if err != nil {
				t.Fatal(err)
			}
			got := memoryFactIDsFromSnapshots(created.MemoryFacts)
			want := make([]string, len(test.want))
			for index, key := range test.want {
				want[index] = facts[key].ID
			}
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("selected FactIDs = %#v, want %#v", got, want)
			}
			if test.name == "A" {
				first = append([]employee.TaskMemoryFactSnapshot(nil), created.MemoryFacts...)
			}
			if test.name == "F" && !reflect.DeepEqual(created.MemoryFacts, first) {
				t.Fatalf("repeat A snapshots differ: first=%#v repeat=%#v", first, created.MemoryFacts)
			}
		})
	}
}
