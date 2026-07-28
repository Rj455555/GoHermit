package employeememory

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCandidateRequiresProvenanceAndOwnerPromotionPreservesIt(t *testing.T) {
	now := time.Now().UTC()
	candidate, err := NewCandidate(Candidate{
		ID: "candidate-a", EmployeeID: "employee-a", Category: "preference", Value: "Use deterministic ordering.",
		Provenance: []Provenance{{SourceType: "owner", SourceID: "owner-note", VerifiedAt: now}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := Promote(candidate, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if fact.CandidateID != candidate.ID || fact.Provenance[0] != candidate.Provenance[0] || fact.OwnerEdited {
		t.Fatalf("promotion lost provenance: %#v", fact)
	}
	edited, err := Edit(fact, "Use stable deterministic ordering.", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !edited.OwnerEdited || edited.Digest == fact.Digest || edited.Provenance[0] != fact.Provenance[0] {
		t.Fatalf("edit semantics invalid: %#v", edited)
	}
}

func TestMemoryRejectsSensitiveOversizedAndTamperedValues(t *testing.T) {
	now := time.Now().UTC()
	base := Candidate{
		ID: "candidate-a", EmployeeID: "employee-a", Category: "fact",
		Provenance: []Provenance{{SourceType: "owner", SourceID: "note", VerifiedAt: now}},
	}
	for name, value := range map[string]string{
		"secret":      "api_key=abcdefghijklmnopqrstuvwxyz123456",
		"reasoning":   "private reasoning: hidden deliberation",
		"oversized":   strings.Repeat("x", MaxValueBytes+1),
		"raw_tools":   "raw tool arguments: sensitive input",
		"full_prompt": "full system prompt: hidden input",
		"replacement": "invalid \uFFFD value",
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			input.Value = value
			if _, err := NewCandidate(input, now); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	valid, err := NewCandidate(func() Candidate { value := base; value.Value = "bounded"; return value }(), now)
	if err != nil {
		t.Fatal(err)
	}
	valid.Value = "tampered"
	if err := ValidateCandidate(valid); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestMemoryRejectsIncompleteRunProvenanceAndFactTampering(t *testing.T) {
	now := time.Now().UTC()
	_, err := NewCandidate(Candidate{
		ID: "candidate-run", EmployeeID: "employee-a", Category: "fact", Value: "verified",
		Provenance: []Provenance{{
			SourceType: "run", SourceID: "run-source", SourceTaskID: "task-a",
			VerifiedAt: now,
		}},
	}, now)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("incomplete provenance = %v", err)
	}
	candidate, err := NewCandidate(Candidate{
		ID: "candidate-a", EmployeeID: "employee-a", Category: "fact", Value: "verified",
		Provenance: []Provenance{{SourceType: "owner", SourceID: "note", VerifiedAt: now}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := Promote(candidate, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	fact.Category = "changed"
	if err := ValidateFact(fact); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tampered fact = %v", err)
	}
	facts := []Fact{{ID: "z"}, {ID: "a"}}
	SortFacts(facts)
	if facts[0].ID != "a" {
		t.Fatal("facts were not sorted deterministically")
	}
	candidates := []Candidate{{ID: "z"}, {ID: "a"}}
	SortCandidates(candidates)
	if candidates[0].ID != "a" {
		t.Fatal("Candidates were not sorted deterministically")
	}
}

func TestProvenanceDigestUsesCompleteCanonicalTuple(t *testing.T) {
	now := time.Now().UTC()
	first := Provenance{
		SourceType: "run", SourceID: "verification", SourceTaskID: "task-a",
		SourceSessionID: "session-a", SourceRunID: "run-a", VerifiedAt: now,
	}
	second := Provenance{
		SourceType: "run", SourceID: "verification", SourceTaskID: "task-a",
		SourceSessionID: "session-a", SourceRunID: "run-b", VerifiedAt: now.Add(time.Second),
	}
	base := Candidate{
		SchemaVersion: SchemaVersion, ID: "candidate-a", EmployeeID: "employee-a",
		Category: "fact", Value: "Verified fact.", CreatedAt: now,
	}
	forward, reverse := base, base
	forward.Provenance = []Provenance{first, second}
	reverse.Provenance = []Provenance{second, first}
	if CandidateDigest(forward) != CandidateDigest(reverse) {
		t.Fatal("Candidate Digest depends on Provenance input order")
	}
	forward.Digest, reverse.Digest = CandidateDigest(forward), CandidateDigest(reverse)
	if err := ValidateCandidate(forward); err != nil {
		t.Fatal(err)
	}
	factForward, err := Promote(forward, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	factReverse := factForward
	factReverse.Provenance = []Provenance{second, first}
	factReverse.Digest = FactDigest(factReverse)
	if factForward.Digest != factReverse.Digest {
		t.Fatal("Fact Digest depends on Provenance input order")
	}
	duplicate := base
	duplicate.Provenance = []Provenance{first, first}
	duplicate.Digest = CandidateDigest(duplicate)
	if err := ValidateCandidate(duplicate); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate Provenance error = %v", err)
	}
}
