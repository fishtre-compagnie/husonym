package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func outcome(expected, predicted string) Outcome {
	return Outcome{Expected: expected, Predicted: predicted, Answered: true}
}

func Test_Counts(t *testing.T) {
	counts := Counts{TruePositives: 6, FalsePositives: 2, FalseNegatives: 4, TrueNegatives: 8}
	require.InDelta(t, 0.75, counts.Precision(), 1e-9)
	require.InDelta(t, 0.6, counts.Recall(), 1e-9)
	// F2 weighs recall four times as much as precision: 5PR / (4P + R).
	require.InDelta(t, 5*0.75*0.6/(4*0.75+0.6), counts.F2(), 1e-9)

	// Nothing predicted is no false alarm; nothing to find is nothing missed.
	require.InDelta(t, 1, Counts{}.Precision(), 1e-9)
	require.InDelta(t, 1, Counts{}.Recall(), 1e-9)
	require.InDelta(t, 1, Counts{}.F2(), 1e-9)
	require.Zero(t, Counts{FalseNegatives: 3}.Recall())
	require.Zero(t, Counts{FalseNegatives: 3}.F2())
}

// Whether a column holds personal data, whatever its category.
func Test_Detection(t *testing.T) {
	counts := Detection([]Outcome{
		outcome("contact", "contact"),
		outcome("contact", "personal"), // found, under another category
		outcome("personal", "none"),
		outcome("none", "location"),
		outcome("none", "none"),
		outcome("none", "none"),
	})
	require.Equal(t, Counts{TruePositives: 2, FalsePositives: 1, FalseNegatives: 1, TrueNegatives: 2}, counts)
}

// Whether a column was found under the category it is expected under: a finding in
// another category is not a hit. It is a false alarm for the category it names and a miss
// for the one that was expected.
func Test_Categorized(t *testing.T) {
	counts := Categorized([]Outcome{
		outcome("contact", "contact"),
		outcome("contact", "personal"), // found, under another category
		outcome("personal", "none"),
		outcome("none", "location"),
		outcome("none", "none"),
	})
	require.Equal(t, Counts{TruePositives: 1, FalsePositives: 2, FalseNegatives: 2, TrueNegatives: 1}, counts)
	require.InDelta(t, 1.0/3, counts.Precision(), 1e-9)
	require.InDelta(t, 1.0/3, counts.Recall(), 1e-9)
}

func Test_ByCategory_And_Confusion(t *testing.T) {
	outcomes := []Outcome{
		outcome("contact", "contact"),
		outcome("contact", "personal"),
		outcome("personal", "none"),
		outcome("none", "location"),
		outcome("none", "none"),
	}
	byCategory := ByCategory(outcomes)
	require.Equal(t, Counts{TruePositives: 1, FalseNegatives: 1, TrueNegatives: 3}, byCategory["contact"])
	require.Equal(t, Counts{FalsePositives: 1, FalseNegatives: 1, TrueNegatives: 3}, byCategory["personal"])
	require.Equal(t, Counts{FalsePositives: 1, TrueNegatives: 4}, byCategory["location"])
	require.NotContains(t, byCategory, "none")

	confusion := Confusion(outcomes)
	require.Equal(t, map[string]int{"contact": 1, "personal": 1}, confusion["contact"])
	require.Equal(t, map[string]int{"none": 1}, confusion["personal"])
	require.Equal(t, map[string]int{"location": 1, "none": 1}, confusion["none"])
}

func Test_Unanswered(t *testing.T) {
	require.Zero(t, Unanswered(nil))
	require.InDelta(t, 0.25, Unanswered([]Outcome{
		{Answered: true}, {Answered: true}, {Answered: true}, {Answered: false},
	}), 1e-9)
}

// How often the model is right, by how sure it says it is.
func Test_Reliability(t *testing.T) {
	bins := Reliability([]Outcome{
		{Expected: "contact", Category: "contact", Confidence: 0.95, Answered: true},
		{Expected: "contact", Category: "personal", Confidence: 0.91, Answered: true},
		{Expected: "contact", Category: "contact", Confidence: 1, Answered: true},
		{Expected: "none", Category: "location", Confidence: 0.12, Answered: true},
		{Expected: "none", Category: "none", Answered: true}, // not a category: in no bin
		{Expected: "none", Answered: false},                  // no answer: in no bin
	})
	require.Len(t, bins, 10)
	require.Equal(t, Bin{From: 0.9, To: 1, Count: 3, Correct: 2}, bins[9])
	require.Equal(t, Bin{From: 0.1, To: 0.2, Count: 1, Correct: 0}, bins[1])
	require.Zero(t, bins[5].Count)
}

// Precision and recall of the model for each threshold its confidence could be held to.
func Test_Thresholds(t *testing.T) {
	sweep := Thresholds([]Outcome{
		{Expected: "contact", Category: "contact", Confidence: 0.9, Answered: true},
		{Expected: "personal", Category: "personal", Confidence: 0.4, Answered: true},
		{Expected: "none", Category: "location", Confidence: 0.6, Answered: true},
		{Expected: "none", Category: "none", Confidence: 0.9, Answered: true},
	})
	require.Len(t, sweep, 21)
	require.InDelta(t, 0, sweep[0].Threshold, 1e-9)
	require.InDelta(t, 1, sweep[20].Threshold, 1e-9)

	at := func(threshold float64) Counts {
		for _, point := range sweep {
			if point.Threshold > threshold-1e-9 && point.Threshold < threshold+1e-9 {
				return point.Counts
			}
		}
		t.Fatalf("no point at %v", threshold)
		return Counts{}
	}
	require.Equal(t, Counts{TruePositives: 2, FalsePositives: 1, TrueNegatives: 1}, at(0))
	require.Equal(t, Counts{TruePositives: 2, FalsePositives: 1, TrueNegatives: 1}, at(0.4))
	require.Equal(t, Counts{TruePositives: 1, FalsePositives: 1, FalseNegatives: 1, TrueNegatives: 1}, at(0.5))
	require.Equal(t, Counts{TruePositives: 1, FalseNegatives: 1, TrueNegatives: 2}, at(0.65))
	require.Equal(t, Counts{FalseNegatives: 2, TrueNegatives: 2}, at(0.95))
}
