package evaluation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_LoadDatasets(t *testing.T) {
	datasets, err := LoadDatasets()
	require.NoError(t, err)

	languages := map[string]struct{}{}
	for _, dataset := range datasets {
		languages[dataset.Language] = struct{}{}
	}
	for _, language := range []string{"fr", "en", "de", "es", "it", "nl", "pt", "pl"} {
		assert.Contains(t, languages, language)
	}
}

func Test_Dataset_Validate(t *testing.T) {
	validCategories := []string{"personal", "contact"}

	t.Run("valid", func(t *testing.T) {
		dataset := &Dataset{
			Language: "fr",
			Tables: []*Table{
				{Schema: "public", Table: "clients", Columns: []*Column{
					{Name: "prenom", ExpectedCategory: "personal", Stage1Expected: true},
					{Name: "id", ExpectedCategory: ""},
				}},
			},
		}
		assert.NoError(t, dataset.Validate(validCategories))
	})

	t.Run("unknown category", func(t *testing.T) {
		dataset := &Dataset{
			Language: "fr",
			Tables: []*Table{
				{Schema: "public", Table: "clients", Columns: []*Column{
					{Name: "prenom", ExpectedCategory: "nope"},
				}},
			},
		}
		assert.Error(t, dataset.Validate(validCategories))
	})

	t.Run("stage1 without category", func(t *testing.T) {
		dataset := &Dataset{
			Language: "fr",
			Tables: []*Table{
				{Schema: "public", Table: "clients", Columns: []*Column{
					{Name: "id", ExpectedCategory: "", Stage1Expected: true},
				}},
			},
		}
		assert.Error(t, dataset.Validate(validCategories))
	})
}

func Test_Evaluate(t *testing.T) {
	dataset := &Dataset{
		Language: "fr",
		Tables: []*Table{
			{Schema: "public", Table: "clients", Columns: []*Column{
				{Name: "prenom", ExpectedCategory: "personal"},  // TP
				{Name: "courriel", ExpectedCategory: "contact"}, // FN (missed)
				{Name: "adresse", ExpectedCategory: "location"}, // FP(contact) + FN(location)
				{Name: "id", ExpectedCategory: ""},              // FP(personal)
				{Name: "note", ExpectedCategory: ""},            // true negative, no counter
			}},
		},
	}
	predictions := map[string]string{
		ColumnKey("public", "clients", "prenom"):  "personal",
		ColumnKey("public", "clients", "adresse"): "contact",
		ColumnKey("public", "clients", "id"):      "personal",
	}

	report := Evaluate(dataset, predictions)

	assert.Equal(t, 1, report.ByCategory["personal"].TruePositives)
	assert.Equal(t, 1, report.ByCategory["personal"].FalsePositives)
	assert.Equal(t, 0, report.ByCategory["personal"].FalseNegatives)
	assert.Equal(t, 1, report.ByCategory["contact"].FalsePositives)
	assert.Equal(t, 1, report.ByCategory["contact"].FalseNegatives)
	assert.Equal(t, 1, report.ByCategory["location"].FalseNegatives)

	assert.Equal(t, 1, report.Overall.TruePositives)
	assert.Equal(t, 2, report.Overall.FalsePositives)
	assert.Equal(t, 2, report.Overall.FalseNegatives)

	assert.InDelta(t, 0.5, report.ByCategory["personal"].Precision(), 0.0001)
	assert.InDelta(t, 1.0, report.ByCategory["personal"].Recall(), 0.0001)
	assert.InDelta(t, 0.0, report.ByCategory["contact"].Recall(), 0.0001)
	assert.Len(t, report.Misclassifications, 3)
	assert.NotEmpty(t, report.String())
}
