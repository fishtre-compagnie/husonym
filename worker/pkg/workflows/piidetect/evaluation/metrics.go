package evaluation

import "math"

// None is the expected and the predicted label of a column that holds no personal data.
const None = "none"

// Outcome is what a detection said of one column of the data set, beside what the data
// set expects.
type Outcome struct {
	Language string `json:"language"`
	Table    string `json:"table"`
	Column   string `json:"column"`
	// Expected is the category the data set gives the column, or None.
	Expected string `json:"expected"`
	// Predicted is the category the detection reports for the column, or None.
	Predicted string `json:"predicted"`

	// What the model answered for the column, before any threshold: its category, or
	// None, and its confidence. Answered is false when it gave no valid answer.
	Category   string  `json:"category,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Answered   bool    `json:"answered"`
}

// Counts are the four outcomes of a yes-or-no detection.
type Counts struct {
	TruePositives  int `json:"true_positives"`
	FalsePositives int `json:"false_positives"`
	FalseNegatives int `json:"false_negatives"`
	TrueNegatives  int `json:"true_negatives"`
}

// Precision is the share of what was reported that was to be found: 1 when nothing was
// reported.
func (c Counts) Precision() float64 {
	if c.TruePositives+c.FalsePositives == 0 {
		return 1
	}
	return float64(c.TruePositives) / float64(c.TruePositives+c.FalsePositives)
}

// Recall is the share of what was to be found that was reported: 1 when there was
// nothing to find.
func (c Counts) Recall() float64 {
	if c.TruePositives+c.FalseNegatives == 0 {
		return 1
	}
	return float64(c.TruePositives) / float64(c.TruePositives+c.FalseNegatives)
}

// F2 weighs recall four times as much as precision: personal data that is missed costs
// more than a false alarm.
func (c Counts) F2() float64 {
	precision, recall := c.Precision(), c.Recall()
	if precision+recall == 0 {
		return 0
	}
	return 5 * precision * recall / (4*precision + recall)
}

func (c *Counts) add(expected, predicted bool) {
	switch {
	case expected && predicted:
		c.TruePositives++
	case !expected && predicted:
		c.FalsePositives++
	case expected && !predicted:
		c.FalseNegatives++
	default:
		c.TrueNegatives++
	}
}

// Detection counts whether personal data was found where there is some, whatever the
// category it was found under.
func Detection(outcomes []Outcome) Counts {
	var counts Counts
	for _, o := range outcomes {
		counts.add(o.Expected != None, o.Predicted != None)
	}
	return counts
}

// Categorized counts whether personal data was found under the category it is expected
// under. A finding under another category is not a hit: it is a false alarm, since what
// it says is wrong, and a miss, since what is there was not said.
func Categorized(outcomes []Outcome) Counts {
	var counts Counts
	for _, o := range outcomes {
		switch {
		case o.Expected == o.Predicted && o.Expected != None:
			counts.TruePositives++
		case o.Expected == o.Predicted:
			counts.TrueNegatives++
		default:
			if o.Predicted != None {
				counts.FalsePositives++
			}
			if o.Expected != None {
				counts.FalseNegatives++
			}
		}
	}
	return counts
}

// ByCategory counts, for each category that is expected or predicted somewhere, the
// columns it was expected for and predicted for.
func ByCategory(outcomes []Outcome) map[string]Counts {
	categories := map[string]bool{}
	for _, o := range outcomes {
		categories[o.Expected], categories[o.Predicted] = true, true
	}
	delete(categories, None)
	byCategory := make(map[string]Counts, len(categories))
	for category := range categories {
		var counts Counts
		for _, o := range outcomes {
			counts.add(o.Expected == category, o.Predicted == category)
		}
		byCategory[category] = counts
	}
	return byCategory
}

// Confusion counts the columns by what was expected, then by what was predicted.
func Confusion(outcomes []Outcome) map[string]map[string]int {
	confusion := map[string]map[string]int{}
	for _, o := range outcomes {
		if confusion[o.Expected] == nil {
			confusion[o.Expected] = map[string]int{}
		}
		confusion[o.Expected][o.Predicted]++
	}
	return confusion
}

// Unanswered is the share of the columns the model gave no valid answer for.
func Unanswered(outcomes []Outcome) float64 {
	if len(outcomes) == 0 {
		return 0
	}
	missing := 0
	for _, o := range outcomes {
		if !o.Answered {
			missing++
		}
	}
	return float64(missing) / float64(len(outcomes))
}

// Bin is the answers of the model whose confidence is from From up to To, and how many
// of them name the expected category.
type Bin struct {
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Count   int     `json:"count"`
	Correct int     `json:"correct"`
}

// Reliability sorts the answers of the model that name a category in ten bins of
// confidence: a confidence that means something has more correct answers in its higher
// bins.
func Reliability(outcomes []Outcome) []Bin {
	const bins = 10
	reliability := make([]Bin, bins)
	for i := range reliability {
		reliability[i].From = float64(i) / bins
		reliability[i].To = float64(i+1) / bins
	}
	for _, o := range outcomes {
		if !o.Answered || o.Category == None || o.Category == "" {
			continue
		}
		bin := min(int(math.Floor(o.Confidence*bins+1e-9)), bins-1)
		reliability[bin].Count++
		if o.Category == o.Expected {
			reliability[bin].Correct++
		}
	}
	return reliability
}

// Point is the detection of the model when its answers count from a threshold up.
type Point struct {
	Threshold float64 `json:"threshold"`
	Counts    Counts  `json:"counts"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
}

// Thresholds counts the detection of the model for each threshold from 0 to 1, by steps
// of 0.05: it is what the default threshold is chosen from.
func Thresholds(outcomes []Outcome) []Point {
	const steps = 20
	points := make([]Point, 0, steps+1)
	for step := range steps + 1 {
		threshold := float64(step) / steps
		var counts Counts
		for _, o := range outcomes {
			found := o.Answered && o.Category != None && o.Category != "" && o.Confidence >= threshold-1e-9
			counts.add(o.Expected != None, found)
		}
		points = append(points, Point{
			Threshold: threshold, Counts: counts, Precision: counts.Precision(), Recall: counts.Recall(),
		})
	}
	return points
}
