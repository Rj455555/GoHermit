package employeememory

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestP023TokenizeMemoryPreservesHorizontalWhitespaceAdjacency(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "single space", value: "go proxy", want: []string{"go proxy"}},
		{name: "repeated spaces", value: "go  proxy", want: []string{"go proxy"}},
		{name: "tab", value: "go\tproxy", want: []string{"go proxy"}},
		{name: "case normalization", value: "GO PROXY", want: []string{"go proxy"}},
		{name: "trailing punctuation after phrase", value: "go proxy.", want: []string{"go proxy"}},
		{name: "duplicate phrase", value: "go proxy, go proxy", want: []string{"go proxy"}},
		{name: "comma boundary", value: "go, proxy"},
		{name: "period boundary", value: "go. proxy"},
		{name: "semicolon boundary", value: "go; proxy"},
		{name: "colon boundary", value: "go: proxy"},
		{name: "exclamation boundary", value: "go! proxy"},
		{name: "question boundary", value: "go? proxy"},
		{name: "newline boundary", value: "go\nproxy"},
		{name: "crlf boundary", value: "go\r\nproxy"},
		{name: "parenthesis boundary", value: "go (proxy)"},
		{name: "slash boundary", value: "go / proxy"},
		{name: "stopword boundary", value: "go the proxy"},
		{name: "second stopword boundary", value: "go and proxy"},
		{name: "third stopword boundary", value: "go to proxy"},
		{name: "CJK language boundary", value: "go 数据库 proxy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sortedPhraseKeys(tokenizeMemory(test.value).phrases)
			want := test.want
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("phrases(%q) = %#v, want %#v", test.value, got, want)
			}
		})
	}
}

func TestP023BigramUsesConservativeTokenNormalization(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "singular dependency", value: "dependency workflow", want: []string{"dependency workflow"}},
		{name: "explicit dependency plural", value: "dependencies workflow", want: []string{"dependency workflow"}},
		{name: "unmapped words are not stemmed", value: "analysis status", want: []string{"analysis status"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sortedPhraseKeys(tokenizeMemory(test.value).phrases)
			want := test.want
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("phrases(%q) = %#v, want %#v", test.value, got, want)
			}
		})
	}
}

func TestP023PhraseScoreRanksPhraseAlignedFactAheadOfTokenOnlyFact(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	tokenOnly := p023RecallFact(t, "fact-a-token", "preference", "proxy go", now)
	phraseAligned := p023RecallFact(t, "fact-z-phrase", "preference", "go proxy", now)

	got := RecallFacts("go proxy", "employee-a", []Fact{tokenOnly, phraseAligned}, nil)
	if len(got) != 2 {
		t.Fatalf("RecallFacts() = %#v, want two eligible facts", got)
	}
	if got[0].ID != phraseAligned.ID {
		t.Fatalf("phrase-aligned fact ranked first: got %#v, want %s first", got, phraseAligned.ID)
	}
}

func sortedPhraseKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func p023RecallFact(t *testing.T, id, category, value string, updatedAt time.Time) Fact {
	t.Helper()
	candidate, err := NewCandidate(Candidate{
		ID: id + "-candidate", EmployeeID: "employee-a", Category: category, Value: value,
		Provenance: []Provenance{{SourceType: "owner", SourceID: id, VerifiedAt: updatedAt}},
	}, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := Promote(candidate, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	fact.ID = id
	fact.UpdatedAt = updatedAt
	fact.Digest = FactDigest(fact)
	if err := ValidateFact(fact); err != nil {
		t.Fatal(err)
	}
	return fact
}
