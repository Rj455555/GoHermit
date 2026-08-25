package employeememory

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRecallFactsUsesUnicodeTokensAndStablePriority(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	makeFact := func(id, category, value string, updatedAt time.Time, ownerEdited bool) Fact {
		candidate, err := NewCandidate(Candidate{
			ID: id + "-candidate", EmployeeID: "employee-a", Category: category, Value: value,
			Provenance: []Provenance{{SourceType: "owner", SourceID: id, VerifiedAt: now}},
		}, now)
		if err != nil {
			t.Fatal(err)
		}
		fact, err := Promote(candidate, now)
		if err != nil {
			t.Fatal(err)
		}
		fact.ID = id
		fact.UpdatedAt = updatedAt
		fact.OwnerEdited = ownerEdited
		fact.Digest = FactDigest(fact)
		if err := ValidateFact(fact); err != nil {
			t.Fatal(err)
		}
		return fact
	}

	facts := []Fact{
		makeFact("fact-z", "verified-run", "Database migration checklist", now.Add(2*time.Hour), false),
		makeFact("fact-a", "preference", "Unrelated owner preference", now, false),
		makeFact("fact-b", "verified-run", "database migrations", now.Add(time.Hour), false),
		makeFact("fact-c", "verified-run", "Database migration owner note", now.Add(time.Hour), true),
		makeFact("fact-manual", "verified-run", "database migration", now.Add(3*time.Hour), false),
		makeFact("fact-foreign", "verified-run", "database migration", now.Add(4*time.Hour), false),
	}
	facts[len(facts)-1].EmployeeID = "employee-b"
	facts[len(facts)-1].Digest = FactDigest(facts[len(facts)-1])
	corrupt := makeFact("fact-corrupt", "verified-run", "database migration", now.Add(5*time.Hour), false)
	corrupt.Digest = strings.Repeat("0", 64)
	facts = append(facts, corrupt)

	got := RecallFacts("Please fix the DATABASE migration", "employee-a", facts, []string{"fact-manual"})
	ids := make([]string, 0, len(got))
	for _, fact := range got {
		ids = append(ids, fact.ID)
	}
	want := []string{"fact-c", "fact-z", "fact-b", "fact-a"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("RecallFacts() ids = %#v, want %#v", ids, want)
	}

	chinese := makeFact("fact-cn", "verified-run", "发布前检查数据库迁移", now.Add(6*time.Hour), false)
	got = RecallFacts("请检查数据库迁移", "employee-a", []Fact{chinese}, nil)
	if len(got) != 1 || got[0].ID != chinese.ID {
		t.Fatalf("Chinese recall = %#v", got)
	}
}

func TestRecallFactsAllowsOwnerEditedAndConfirmedNonRunFacts(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	makeFact := func(id, category string, ownerEdited bool) Fact {
		candidate, err := NewCandidate(Candidate{
			ID: id + "-candidate", EmployeeID: "employee-a", Category: category, Value: "unrelated value",
			Provenance: []Provenance{{SourceType: "owner", SourceID: id, VerifiedAt: now}},
		}, now)
		if err != nil {
			t.Fatal(err)
		}
		fact, err := Promote(candidate, now)
		if err != nil {
			t.Fatal(err)
		}
		fact.ID = id
		fact.OwnerEdited = ownerEdited
		fact.Digest = FactDigest(fact)
		return fact
	}
	got := RecallFacts("no matching words", "employee-a", []Fact{
		makeFact("fact-run", "verified-run", false),
		makeFact("fact-owner", "verified-run", true),
		makeFact("fact-confirmed", "preference", false),
	}, nil)
	ids := make([]string, 0, len(got))
	for _, fact := range got {
		ids = append(ids, fact.ID)
	}
	if want := []string{"fact-owner", "fact-confirmed"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("eligible facts = %#v, want %#v", ids, want)
	}
}

func TestRecallFactsIsIndependentOfStoreOrder(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	facts := []Fact{
		recallTestFact(t, "first", "preference", "database migration", now.Add(time.Hour)),
		recallTestFact(t, "second", "verified-run", "database migration checklist", now.Add(2*time.Hour)),
		recallTestFact(t, "third", "preference", "database migration owner note", now.Add(3*time.Hour)),
	}
	first := RecallFacts("database migration", "employee-a", facts, nil)
	second := RecallFacts("database migration", "employee-a", []Fact{facts[2], facts[0], facts[1]}, nil)
	firstIDs := make([]string, 0, len(first))
	secondIDs := make([]string, 0, len(second))
	for _, fact := range first {
		firstIDs = append(firstIDs, fact.ID)
	}
	for _, fact := range second {
		secondIDs = append(secondIDs, fact.ID)
	}
	if !reflect.DeepEqual(firstIDs, secondIDs) {
		t.Fatalf("store-order-dependent recall: first=%#v second=%#v", firstIDs, secondIDs)
	}
}

func recallTestFact(t *testing.T, id, category, value string, updatedAt time.Time) Fact {
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
	return fact
}
