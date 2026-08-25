package employeememory

import (
	"sort"
	"strings"
	"unicode"
)

// RecallFacts returns the accepted Facts eligible for deterministic automatic
// recall. Invalid Facts, Facts owned by another Employee, and manually pinned
// Facts are excluded. The returned slice is ordered independently of the
// input/store order.
func RecallFacts(prompt, employeeID string, facts []Fact, excludedIDs []string) []Fact {
	excluded := make(map[string]struct{}, len(excludedIDs))
	for _, id := range excludedIDs {
		excluded[id] = struct{}{}
	}
	promptTokens := memoryTokens(prompt)
	items := make([]recalledFact, 0, len(facts))
	for _, fact := range facts {
		if fact.EmployeeID != employeeID {
			continue
		}
		if _, exists := excluded[fact.ID]; exists {
			continue
		}
		if err := ValidateFact(fact); err != nil {
			continue
		}
		intersection := tokenIntersection(promptTokens, memoryTokens(fact.Value))
		if intersection == 0 && !fact.OwnerEdited && fact.Category == "verified-run" {
			continue
		}
		items = append(items, recalledFact{fact: fact, intersection: intersection})
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].intersection != items[right].intersection {
			return items[left].intersection > items[right].intersection
		}
		if items[left].fact.OwnerEdited != items[right].fact.OwnerEdited {
			return items[left].fact.OwnerEdited
		}
		leftVerified := items[left].fact.Category == "verified-run"
		rightVerified := items[right].fact.Category == "verified-run"
		if leftVerified != rightVerified {
			return !leftVerified
		}
		if !items[left].fact.UpdatedAt.Equal(items[right].fact.UpdatedAt) {
			return items[left].fact.UpdatedAt.After(items[right].fact.UpdatedAt)
		}
		return items[left].fact.ID < items[right].fact.ID
	})
	result := make([]Fact, len(items))
	for index, item := range items {
		result[index] = item.fact
	}
	return result
}

type recalledFact struct {
	fact         Fact
	intersection int
}

func memoryTokens(value string) map[string]struct{} {
	runes := []rune(strings.ToLower(value))
	tokens := make(map[string]struct{})
	for index := 0; index < len(runes); {
		if isCJKRune(runes[index]) {
			end := index + 1
			for end < len(runes) && isCJKRune(runes[end]) {
				end++
			}
			for pair := index; pair+1 < end; pair++ {
				tokens[string(runes[pair:pair+2])] = struct{}{}
			}
			index = end
			continue
		}
		if isWordRune(runes[index]) {
			end := index + 1
			for end < len(runes) && isWordRune(runes[end]) {
				end++
			}
			tokens[string(runes[index:end])] = struct{}{}
			index = end
			continue
		}
		index++
	}
	return tokens
}

func isWordRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || unicode.IsMark(value)
}

func isCJKRune(value rune) bool {
	return unicode.Is(unicode.Han, value) || unicode.Is(unicode.Hiragana, value) ||
		unicode.Is(unicode.Katakana, value) || unicode.Is(unicode.Hangul, value)
}

func tokenIntersection(left, right map[string]struct{}) int {
	if len(left) > len(right) {
		left, right = right, left
	}
	count := 0
	for token := range left {
		if _, exists := right[token]; exists {
			count++
		}
	}
	return count
}
