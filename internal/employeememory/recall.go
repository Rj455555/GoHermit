package employeememory

import (
	"sort"
	"strings"
	"unicode"
)

// The set is fixed so eligibility is deterministic and auditable. Business
// words such as frontend, go, before, and without intentionally remain valid.
var englishStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
	"for": {}, "from": {}, "in": {}, "is": {}, "it": {}, "of": {}, "on": {}, "or": {},
	"that": {}, "the": {}, "this": {}, "to": {}, "with": {},
}

// Only explicitly audited plural forms are normalized; this is not stemming.
var englishSingulars = map[string]string{
	"dependencies": "dependency",
	"managers":     "manager",
	"migrations":   "migration",
	"packages":     "package",
	"repositories": "repository",
	"tests":        "test",
}

// RecallFacts returns accepted Facts eligible for deterministic automatic
// recall. Invalid Facts, Facts owned by another Employee, manually pinned
// Facts, and Facts without a qualifying relevance match are excluded.
func RecallFacts(prompt, employeeID string, facts []Fact, excludedIDs []string) []Fact {
	excluded := make(map[string]struct{}, len(excludedIDs))
	for _, id := range excludedIDs {
		excluded[id] = struct{}{}
	}
	promptTokens := tokenizeMemory(prompt)
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
		match := calculateRecallMatch(promptTokens, tokenizeMemory(fact.Value))
		if !match.eligible {
			continue
		}
		items = append(items, recalledFact{
			fact:               fact,
			tokenIntersection:  match.tokenIntersection,
			phraseIntersection: match.phraseIntersection,
			cjkIntersection:    match.cjkIntersection,
		})
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].tokenIntersection != items[right].tokenIntersection {
			return items[left].tokenIntersection > items[right].tokenIntersection
		}
		if items[left].phraseIntersection != items[right].phraseIntersection {
			return items[left].phraseIntersection > items[right].phraseIntersection
		}
		if items[left].cjkIntersection != items[right].cjkIntersection {
			return items[left].cjkIntersection > items[right].cjkIntersection
		}
		if items[left].fact.OwnerEdited != items[right].fact.OwnerEdited {
			return items[left].fact.OwnerEdited
		}
		if items[left].fact.Category != items[right].fact.Category {
			return items[left].fact.Category < items[right].fact.Category
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
	fact               Fact
	tokenIntersection  int
	phraseIntersection int
	cjkIntersection    int
}

type memoryTokenData struct {
	all     map[string]struct{}
	english map[string]struct{}
	cjk     map[string]struct{}
	phrases map[string]struct{}
}

type recallMatch struct {
	tokenIntersection  int
	phraseIntersection int
	cjkIntersection    int
	eligible           bool
}

func calculateRecallMatch(prompt, fact memoryTokenData) recallMatch {
	englishIntersection := tokenIntersection(prompt.english, fact.english)
	cjkIntersection := tokenIntersection(prompt.cjk, fact.cjk)
	phraseIntersection := tokenIntersection(prompt.phrases, fact.phrases)
	tokenIntersection := englishIntersection + cjkIntersection
	oneExactEnglishToken := len(prompt.english) == 1 && len(prompt.all) == 1 && englishIntersection == 1
	return recallMatch{
		tokenIntersection:  tokenIntersection,
		phraseIntersection: phraseIntersection,
		cjkIntersection:    cjkIntersection,
		eligible: phraseIntersection > 0 || englishIntersection >= 2 ||
			oneExactEnglishToken || cjkIntersection > 0,
	}
}

func tokenizeMemory(value string) memoryTokenData {
	data := memoryTokenData{
		all:     make(map[string]struct{}),
		english: make(map[string]struct{}),
		cjk:     make(map[string]struct{}),
		phrases: make(map[string]struct{}),
	}
	runes := []rune(strings.ToLower(value))
	previousEnglish := ""
	previousWasEnglish := false
	for index := 0; index < len(runes); {
		if isCJKRune(runes[index]) {
			end := index + 1
			for end < len(runes) && isCJKRune(runes[end]) {
				end++
			}
			for pair := index; pair+1 < end; pair++ {
				token := string(runes[pair : pair+2])
				data.all[token] = struct{}{}
				data.cjk[token] = struct{}{}
			}
			previousEnglish = ""
			previousWasEnglish = false
			index = end
			continue
		}
		if isWordRune(runes[index]) {
			end := index + 1
			for end < len(runes) && isWordRune(runes[end]) {
				end++
			}
			token := normalizeEnglishToken(string(runes[index:end]))
			if token == "" {
				previousEnglish = ""
				previousWasEnglish = false
				index = end
				continue
			}
			data.all[token] = struct{}{}
			data.english[token] = struct{}{}
			if previousWasEnglish {
				data.phrases[previousEnglish+" "+token] = struct{}{}
			}
			previousEnglish = token
			previousWasEnglish = true
			index = end
			continue
		}
		if !isHorizontalWhitespace(runes[index]) {
			previousEnglish = ""
			previousWasEnglish = false
		}
		index++
	}
	return data
}

func normalizeEnglishToken(token string) string {
	token = strings.ToLower(token)
	if _, stopword := englishStopwords[token]; stopword {
		return ""
	}
	if singular, ok := englishSingulars[token]; ok {
		return singular
	}
	return token
}

func memoryTokens(value string) map[string]struct{} {
	return tokenizeMemory(value).all
}

func isHorizontalWhitespace(value rune) bool {
	switch value {
	case ' ', '\t',
		'\u00a0', '\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005',
		'\u2006', '\u2007', '\u2008', '\u2009', '\u200a',
		'\u202f', '\u205f', '\u3000':
		return true
	default:
		return false
	}
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
