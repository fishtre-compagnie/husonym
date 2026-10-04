package piidetect

import "slices"

// matcher is a rule ready to be compared with the words of a name.
type matcher struct {
	rule     *rule
	keywords []phrase
	excluded map[string]bool
	own      map[string]bool
}

var (
	vocabulary = newVocabulary()
	matchers   = newMatchers()
)

// newVocabulary gathers every word the rules and the qualifiers hold.
func newVocabulary() *lexicon {
	l := &lexicon{words: map[string]bool{}, anchors: map[string]bool{}}
	for i := range rules {
		ru := &rules[i]
		l.add(ru.keywords...)
		l.addAnchors(ru.keywords...)
		l.add(ru.excludeTokens...)
		l.add(ru.ownTokens...)
		for _, g := range ru.guarded {
			// A word that only counts as the whole name is not part of a glued one.
			if !g.alone && !g.apart {
				l.add(g.word)
			}
			l.add(g.among...)
			l.add(g.unless...)
		}
	}
	l.add(gluedWords...)
	for _, set := range []map[string]bool{
		qualifierNouns, qualifierAdjectives, qualifierFlags, qualifierEvents, referenceSuffixes,
	} {
		for word := range set {
			l.words[word] = true
		}
	}
	return l
}

func newMatchers() []matcher {
	out := make([]matcher, len(rules))
	for i := range rules {
		ru := &rules[i]
		m := matcher{rule: ru, excluded: wordSet(ru.excludeTokens...), own: wordSet(ru.ownTokens...)}
		for _, keyword := range ru.keywords {
			m.keywords = append(m.keywords, parsePhrase(keyword))
		}
		out[i] = m
	}
	return out
}

// matches tells whether the rule finds its datum among the words of a name.
func (m *matcher) matches(words []string) bool {
	for _, word := range words {
		if m.excluded[word] {
			return false
		}
	}
	for _, keyword := range m.keywords {
		if keyword.in(words) {
			return true
		}
	}
	for i := range m.rule.guarded {
		if m.rule.guarded[i].in(words) {
			return true
		}
	}
	return false
}

func (g *guarded) in(words []string) bool {
	if !slices.Contains(words, g.word) {
		return false
	}
	if len(words) == 1 {
		return !g.beside
	}
	if g.alone {
		return false
	}
	if holdsOneOf(words, g.unless) {
		return false
	}
	return len(g.among) == 0 || holdsOneOf(words, g.among)
}

// holdsOneOf tells whether one of among is a word of the name.
func holdsOneOf(words, among []string) bool {
	for _, word := range among {
		if slices.Contains(words, word) {
			return true
		}
	}
	return false
}

// Classify says what a column holds, from its name and its SQL type. ok is false when no
// rule finds anything.
//
// Every rule that matches the name is asked, in order. A rule sets the name aside when
// the name is a reference to another row (user_id, id_usuario) or a qualifier of the
// datum (email_format) and the rule does not own the word that says so; the next rule
// that matches may own it: address_zip_code is not an address, and is a postal code.
func Classify(columnName, dataType string) (Classification, bool) {
	words := wordsOf(columnName)
	if len(words) == 0 {
		return Classification{}, false
	}
	kind := kindOf(dataType)
	for i := range matchers {
		m := &matchers[i]
		if !m.rule.holds(kind) || !m.matches(words) {
			continue
		}
		if refers(words, m.own) || qualifies(words, m.own) {
			continue
		}
		return Classification{
			Category:  m.rule.category,
			Sensitive: m.rule.sensitive,
			Suggested: suggestionFor(dataType, m.rule.suggested, m.rule.suggestIfInteger, m.rule.suggestIfTemporal),
		}, true
	}
	return Classification{}, false
}

// NameCategories are the categories the name rules answer, each once, in the order of
// the rules.
func NameCategories() []string {
	var categories []string
	for i := range rules {
		if !slices.Contains(categories, rules[i].category) {
			categories = append(categories, rules[i].category)
		}
	}
	return categories
}
