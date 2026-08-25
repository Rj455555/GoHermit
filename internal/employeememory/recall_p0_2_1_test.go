package employeememory

import (
	"reflect"
	"testing"
	"time"
)

func TestRecallFactsP021LiveCases(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	facts := []Fact{
		p021RecallFact(t, "fact-pnpm", "preference", "GoHermit JavaScript dependencies use pnpm, not npm.", now),
		p021RecallFact(t, "fact-release-tests", "decision", "Before release, run Go tests, race, frontend tests, and E2E.", now.Add(time.Minute)),
		p021RecallFact(t, "fact-proxy", "known_issue", "The official Go Proxy may time out; use only a temporary build overlay to switch the proxy and do not modify the repository.", now.Add(2*time.Minute)),
		p021RecallFact(t, "fact-health", "verified-run", "The most recent service health check returned 200.", now.Add(3*time.Minute)),
		p021RecallFact(t, "fact-invoice", "verified-run", "The quarterly invoice export is complete.", now.Add(4*time.Minute)),
		p021RecallFact(t, "fact-risk", "preference", "Report risks before modifying code.", now.Add(5*time.Minute)),
		p021RecallFact(t, "fact-migration", "verified-run", "\u53d1\u5e03\u524d\u5fc5\u987b\u68c0\u67e5\u6570\u636e\u5e93\u8fc1\u79fb\u72b6\u6001\u3002", now.Add(6*time.Minute)),
		p021RecallFact(t, "fact-owner-deploy", "decision", "Do not deploy without Owner confirmation.", now.Add(7*time.Minute)),
		p021RecallFact(t, "fact-foreign", "verified-run", "FOREIGN EMPLOYEE MEMORY MUST NEVER APPEAR", now.Add(8*time.Minute)),
	}
	facts[len(facts)-1].EmployeeID = "employee-b"
	facts[len(facts)-1].Digest = FactDigest(facts[len(facts)-1])

	tests := []struct {
		name     string
		prompt   string
		excluded []string
		want     []string
	}{
		{name: "A", prompt: "Review the frontend dependency workflow and tell me which package manager to use.", want: []string{"fact-pnpm"}},
		{name: "B", prompt: "\u8bf7\u51c6\u5907\u53d1\u5e03\u68c0\u67e5\uff0c\u7279\u522b\u786e\u8ba4\u6570\u636e\u5e93\u8fc1\u79fb\u72b6\u6001\u3002", want: []string{"fact-migration"}},
		{name: "C", prompt: "The official Go proxy is timing out. Prepare a safe build plan without changing the repository.", want: []string{"fact-proxy"}},
		{name: "D", prompt: "Summarize the top-level README structure.", want: []string{}},
		{name: "E", prompt: "Before release, list the Go race frontend and E2E tests that must pass.", excluded: []string{"fact-owner-deploy"}, want: []string{"fact-release-tests"}},
		{name: "F", prompt: "Review the frontend dependency workflow and tell me which package manager to use.", want: []string{"fact-pnpm"}},
	}
	var first []Fact
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RecallFacts(test.prompt, "employee-a", facts, test.excluded)
			if gotIDs := p021FactIDs(got); !reflect.DeepEqual(gotIDs, test.want) {
				t.Fatalf("RecallFacts() ids = %#v, want %#v", gotIDs, test.want)
			}
			if test.name == "A" {
				first = append([]Fact(nil), got...)
			}
			if test.name == "F" {
				if !reflect.DeepEqual(p021FactIDs(got), p021FactIDs(first)) {
					t.Fatalf("repeat A FactID set differs: first=%#v repeat=%#v", p021FactIDs(first), p021FactIDs(got))
				}
				if len(first) != len(got) || (len(first) == 1 && first[0].Digest != got[0].Digest) {
					t.Fatalf("repeat A Digest set differs")
				}
			}
		})
	}
}

func TestRecallFactsP021EligibilityRules(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		prompt string
		value  string
		want   bool
	}{
		{name: "stopword only", prompt: "the", value: "the", want: false},
		{name: "case and punctuation", prompt: "PNPM!", value: "pnpm", want: true},
		{name: "conservative plural", prompt: "JavaScript dependency", value: "JavaScript dependencies", want: true},
		{name: "two tokens", prompt: "frontend dependency workflow", value: "frontend dependency", want: true},
		{name: "one token in long prompt", prompt: "review the frontend dependency workflow", value: "frontend", want: false},
		{name: "single token exact", prompt: "pnpm", value: "pnpm", want: true},
		{name: "CJK bigram", prompt: "\u8bf7\u68c0\u67e5\u6570\u636e\u5e93", value: "\u53d1\u5e03\u524d\u5fc5\u987b\u68c0\u67e5\u6570\u636e\u5e93\u8fc1\u79fb\u72b6\u6001", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fact := p021RecallFact(t, "fact", "preference", test.value, now)
			got := RecallFacts(test.prompt, "employee-a", []Fact{fact}, nil)
			if (len(got) == 1) != test.want {
				t.Fatalf("eligible=%v, want %v; facts=%#v", len(got) == 1, test.want, got)
			}
		})
	}
}

func TestRecallFactsP021OwnerEditedAndCategoriesCannotBypassEligibility(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	facts := []Fact{
		p021RecallFact(t, "owner", "preference", "Report risks before modifying code.", now),
		p021RecallFact(t, "decision", "decision", "Run the release checklist.", now),
		p021RecallFact(t, "issue", "known_issue", "The proxy may time out.", now),
		p021RecallFact(t, "verified", "verified-run", "The service health check returned 200.", now),
	}
	facts[0].OwnerEdited = true
	facts[0].Digest = FactDigest(facts[0])
	if got := RecallFacts("Summarize the README structure", "employee-a", facts, nil); len(got) != 0 {
		t.Fatalf("zero-relevance facts bypassed eligibility: %#v", p021FactIDs(got))
	}
}

func p021RecallFact(t *testing.T, id, category, value string, updatedAt time.Time) Fact {
	t.Helper()
	fact := recallTestFact(t, id, category, value, updatedAt)
	fact.ID = id
	fact.Digest = FactDigest(fact)
	if err := ValidateFact(fact); err != nil {
		t.Fatal(err)
	}
	return fact
}

func p021FactIDs(facts []Fact) []string {
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.ID)
	}
	return ids
}
