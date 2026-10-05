package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/rules"
	"github.com/stretchr/testify/require"
)

// businessWord is a column name of testdata/business-words.json: a name that holds a
// word the rules know, in a column that holds no personal data, or the same word in a
// column that does. The set is apart from the tables of the data set: it has no values,
// and measures the name rules alone.
type businessWord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Expected string `json:"expected"`
}

// The name rules read ordinary words of each language as what they are: a count, a
// limit, a piece of ground, a key of a product. Every name of the set is answered as it
// is labeled.
func Test_Rules_OnBusinessWords(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "business-words.json"))
	require.NoError(t, err)
	var words map[string][]businessWord
	require.NoError(t, json.Unmarshal(content, &words))

	languages := make([]string, 0, len(words))
	for language := range words {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	require.Equal(t, []string{"de", "en", "es", "fr", "it", "nl", "pl", "pt"}, languages)

	for _, language := range languages {
		var counts Counts
		for _, word := range words[language] {
			predicted := None
			if finding, ok := rules.Find(word.Name, word.Type, nil); ok {
				predicted = string(finding.Category)
			}
			counts.add(word.Expected != None, predicted != None)
			if predicted != word.Expected {
				t.Errorf("%s: %s %s: expected %s, the rules say %s", language, word.Name, word.Type, word.Expected, predicted)
			}
		}
		t.Logf("business words, %s: %d names, %d false alarms, %d misses",
			language, len(words[language]), counts.FalsePositives, counts.FalseNegatives)
	}
}
