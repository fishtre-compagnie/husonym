package presidio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This measure sends the values of two data sets of business text to the analyzer, one value per
// request, and counts what it returns for PERSON, per span of text.
//
// The data sets (testdata/free-text-fr.json and free-text-en.json) hold 12 columns of 50 values
// each, all invented: 6 columns name persons in some of their values, 6 name none. For the
// columns that name persons, "persons" lists the positions of the values that do, and "names"
// gives, for each of them, the names exactly as written in the value, every occurrence, in
// order. A title ("Mme", "M.", "Madame", "Monsieur", "Dr", "Mr", "Mrs", "Ms") is not part of an
// annotated name: "Mme Lemoine" is annotated "Lemoine".
//
// Scoring:
//   - a PERSON span is correct when it overlaps an annotated name of its value, by at least one
//     character; a span in a value without annotation, or overlapping none, is wrong;
//   - an annotated name is found when at least one PERSON span overlaps it;
//   - precision is correct spans over PERSON spans, recall is found names over annotated names;
//   - a span is exact when it is the annotated name, give or take a leading title, and the same
//     goes for a name: the exact figures are logged and not asserted.
//
// Every value is cut to its first 200 characters and sent alone at the score threshold the
// product sends, 0.35.
//
// "Sensitive" below is any entity type the analyzer returns but DATE_TIME and URL, which the
// product does not map to a transformer. It approximates the product's own list of the types it
// keeps.
const (
	measureScoreThreshold = 0.35
	measureMaxCharacters  = 200
	// A column counts as tagged from this many values holding a finding.
	taggedColumnMinValues = 2
	noPersonColumn        = "none"
	testdataDir           = "testdata"
)

// notSensitive are the entity types that are not counted as sensitive.
var notSensitive = map[string]bool{"DATE_TIME": true, "URL": true}

var leadingTitle = regexp.MustCompile(`^(?:Mme|M\.|Madame|Monsieur|Dr|Mr|Mrs|Ms)\s+`)

type textDataset struct {
	Language string `json:"language"`
	Tables   []struct {
		Name    string       `json:"name"`
		Columns []textColumn `json:"columns"`
	} `json:"tables"`
}

type textColumn struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	// Persons are the positions of the values that name a person.
	Persons []int `json:"persons"`
	// Names maps each of those positions to the names written in the value.
	Names  map[int][]string `json:"names"`
	Values []string         `json:"values"`
}

func loadTextDataset(t *testing.T, language string) []textColumn {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(testdataDir, "free-text-"+language+".json"))
	require.NoError(t, err)
	var dataset textDataset
	require.NoError(t, json.Unmarshal(content, &dataset))
	require.Equal(t, language, dataset.Language)
	require.Len(t, dataset.Tables, 1)
	columns := dataset.Tables[0].Columns
	require.Len(t, columns, 12)
	for _, column := range columns {
		require.Len(t, column.Values, 50, column.Name)
		if column.Expected == noPersonColumn {
			require.Empty(t, column.Persons, column.Name)
			require.Empty(t, column.Names, column.Name)
			continue
		}
		require.GreaterOrEqual(t, len(column.Persons), taggedColumnMinValues, column.Name)
		require.Len(t, column.Names, len(column.Persons), column.Name)
		for _, position := range column.Persons {
			require.NotEmpty(t, column.Names[position], "%s, value %d", column.Name, position)
		}
	}
	return columns
}

// span is a stretch of a value, in characters.
type span struct {
	start, end int
	text       string
}

func (s span) overlaps(o span) bool { return s.start < o.end && o.start < s.end }

// locateNames returns where each name is written in text: the first occurrence of each, after
// the end of the previous one.
func locateNames(t *testing.T, text string, names []string) []span {
	t.Helper()
	runes := []rune(text)
	var located []span
	cursor := 0
	for _, name := range names {
		rest := string(runes[cursor:])
		index := strings.Index(rest, name)
		require.GreaterOrEqualf(t, index, 0, "name %q is not written in %q", name, text)
		start := cursor + utf8.RuneCountInString(rest[:index])
		end := start + utf8.RuneCountInString(name)
		located = append(located, span{start: start, end: end, text: name})
		cursor = end
	}
	return located
}

// isExact tells whether a PERSON span is the annotated name, give or take a leading title.
func isExact(found, name span) bool {
	return found.end == name.end && leadingTitle.ReplaceAllString(found.text, "") == name.text
}

// measure is what the analyzer returned for one language.
type measure struct {
	language string

	spans, correctSpans, exactSpans int
	names, foundNames, exactNames   int

	// Values holding at least one PERSON span, in the columns that name persons: all of them,
	// and those that do name a person.
	taggedInPersonColumns, taggedNamingAPerson int
	// Values holding at least one PERSON span in the columns that name none.
	taggedInNoPersonColumns int
	// Columns with at least two values holding a PERSON span.
	noPersonColumnsTagged, personColumnsTagged int
	// Columns that name none, with at least two values holding a sensitive entity, and the number
	// of values of those columns holding each type.
	noPersonColumnsSensitive int
	sensitiveTypes           map[string]int

	// A line per column with a wrong span or a missed name.
	columnLines []string
}

func (m measure) precision() float64 { return ratio(m.correctSpans, m.spans) }
func (m measure) recall() float64    { return ratio(m.foundNames, m.names) }

func ratio(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// firstCharacters returns the first n characters of s.
func firstCharacters(s string, n int) string {
	if runes := []rune(s); len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// measurePersons sends every value of the data set of dataLanguage to the analyzer in language.
func measurePersons(t *testing.T, baseURL, dataLanguage, language string) measure {
	t.Helper()
	result := measure{language: language, sensitiveTypes: map[string]int{}}
	for _, column := range loadTextDataset(t, dataLanguage) {
		hasPersons := column.Expected != noPersonColumn
		var wrong, missed []string
		taggedValues, sensitiveValues := 0, 0
		for position, value := range column.Values {
			text := firstCharacters(value, measureMaxCharacters)
			findings := analyzeAbove(t, baseURL, language, text, measureScoreThreshold)

			names := locateNames(t, text, column.Names[position])
			var persons []span
			for _, f := range ofType(findings, entityPerson) {
				persons = append(persons, span{start: f.Start, end: f.End, text: f.text(text)})
			}
			for _, person := range persons {
				result.spans++
				correct, exact := false, false
				for _, name := range names {
					correct = correct || person.overlaps(name)
					exact = exact || isExact(person, name)
				}
				if correct {
					result.correctSpans++
				} else {
					wrong = append(wrong, fmt.Sprintf("%d: %q", position, person.text))
				}
				if exact {
					result.exactSpans++
				}
			}
			for _, name := range names {
				result.names++
				found, exact := false, false
				for _, person := range persons {
					found = found || person.overlaps(name)
					exact = exact || isExact(person, name)
				}
				if found {
					result.foundNames++
				} else {
					missed = append(missed, fmt.Sprintf("%d: %q", position, name.text))
				}
				if exact {
					result.exactNames++
				}
			}

			if len(persons) > 0 {
				taggedValues++
				switch {
				case !hasPersons:
					result.taggedInNoPersonColumns++
				default:
					result.taggedInPersonColumns++
					if len(names) > 0 {
						result.taggedNamingAPerson++
					}
				}
			}
			if !hasPersons {
				types := map[string]bool{}
				for _, f := range findings {
					if !notSensitive[f.EntityType] {
						types[f.EntityType] = true
					}
				}
				if len(types) > 0 {
					sensitiveValues++
				}
				for entityType := range types {
					result.sensitiveTypes[entityType]++
				}
			}
		}
		if taggedValues >= taggedColumnMinValues {
			if hasPersons {
				result.personColumnsTagged++
			} else {
				result.noPersonColumnsTagged++
			}
		}
		if !hasPersons && sensitiveValues >= taggedColumnMinValues {
			result.noPersonColumnsSensitive++
		}
		if len(wrong) > 0 || len(missed) > 0 {
			result.columnLines = append(result.columnLines, fmt.Sprintf(
				"%s %s: wrong spans [%s], missed names [%s]",
				language, column.Name, strings.Join(wrong, "; "), strings.Join(missed, "; ")))
		}
	}
	return result
}

// log writes the table of a measure: a line for the language, then a line per column that has a
// wrong span or a missed name.
func (m measure) log(t *testing.T) {
	t.Helper()
	types := make([]string, 0, len(m.sensitiveTypes))
	for entityType, values := range m.sensitiveTypes {
		types = append(types, fmt.Sprintf("%s %d", entityType, values))
	}
	sort.Strings(types)
	t.Logf("%s: PERSON spans %d, correct %d, precision %.3f (exact %d, %.3f) | "+
		"names %d, found %d, recall %.3f (exact %d, %.3f) | "+
		"values tagged in the person columns %d (naming a person %d), in the other columns %d | "+
		"columns with %d values tagged or more: %d of the 6 that name none, %d of the 6 that name persons | "+
		"columns that name none with %d values holding a sensitive entity or more: %d (values per type: %s)",
		m.language, m.spans, m.correctSpans, m.precision(), m.exactSpans, ratio(m.exactSpans, m.spans),
		m.names, m.foundNames, m.recall(), m.exactNames, ratio(m.exactNames, m.names),
		m.taggedInPersonColumns, m.taggedNamingAPerson, m.taggedInNoPersonColumns,
		taggedColumnMinValues, m.noPersonColumnsTagged, m.personColumnsTagged,
		taggedColumnMinValues, m.noPersonColumnsSensitive, strings.Join(types, ", "))
	for _, line := range m.columnLines {
		t.Log(line)
	}
}

func Test_Measure_PersonsPerSpan_French(t *testing.T) {
	baseURL := startAnalyzer(t)

	result := measurePersons(t, baseURL, langFr, langFr)

	result.log(t)
	assert.GreaterOrEqual(t, result.precision(), 0.90, "precision per span")
	assert.GreaterOrEqual(t, result.recall(), 0.90, "recall per name")
	assert.LessOrEqual(t, result.noPersonColumnsTagged, 1,
		"columns that name none with %d values tagged or more", taggedColumnMinValues)
	assert.Equal(t, 6, result.personColumnsTagged,
		"columns that name persons with %d values tagged or more", taggedColumnMinValues)
}

// The English engine is the one of the base image: the counts below were recorded on this image,
// so that a change on the English side shows.
func Test_Measure_PersonsPerSpan_English(t *testing.T) {
	baseURL := startAnalyzer(t)

	result := measurePersons(t, baseURL, langEn, langEn)

	result.log(t)
	assert.Equal(t, englishCounts, countsOf(result))
}

// counts are the figures of a measure that are whole numbers.
type counts struct {
	Spans, CorrectSpans, Names, FoundNames            int
	TaggedInNoPersonColumns                           int
	NoPersonColumnsTagged, PersonColumnsTagged        int
	NoPersonColumnsSensitive                          int
	TaggedInPersonColumns, TaggedInPersonColumnsNamed int
}

func countsOf(m measure) counts {
	return counts{
		Spans: m.spans, CorrectSpans: m.correctSpans, Names: m.names, FoundNames: m.foundNames,
		TaggedInNoPersonColumns: m.taggedInNoPersonColumns,
		NoPersonColumnsTagged:   m.noPersonColumnsTagged, PersonColumnsTagged: m.personColumnsTagged,
		NoPersonColumnsSensitive: m.noPersonColumnsSensitive,
		TaggedInPersonColumns:    m.taggedInPersonColumns, TaggedInPersonColumnsNamed: m.taggedNamingAPerson,
	}
}

var englishCounts = counts{
	Spans: 48, CorrectSpans: 30, Names: 32, FoundNames: 30,
	TaggedInNoPersonColumns: 16,
	NoPersonColumnsTagged:   3, PersonColumnsTagged: 6,
	NoPersonColumnsSensitive: 4,
	TaggedInPersonColumns:    30, TaggedInPersonColumnsNamed: 29,
}
