package evaluation

import (
	"fmt"
	"sort"
	"strings"
)

// CategoryMetrics accumulates detection counts for one PII category.
type CategoryMetrics struct {
	TruePositives  int
	FalsePositives int
	FalseNegatives int
}

// Precision returns TP/(TP+FP), or 1 when nothing was predicted for the category.
func (m *CategoryMetrics) Precision() float64 {
	if m.TruePositives+m.FalsePositives == 0 {
		return 1
	}
	return float64(m.TruePositives) / float64(m.TruePositives+m.FalsePositives)
}

// Recall returns TP/(TP+FN), or 1 when the category has no ground-truth columns.
func (m *CategoryMetrics) Recall() float64 {
	if m.TruePositives+m.FalseNegatives == 0 {
		return 1
	}
	return float64(m.TruePositives) / float64(m.TruePositives+m.FalseNegatives)
}

// Misclassification records one column where the prediction differs from the ground truth.
type Misclassification struct {
	Column    string // schema.table.column
	Expected  string // empty when the column is not PII
	Predicted string // empty when the column was not flagged
}

// Report holds the evaluation result of one dataset.
type Report struct {
	Language           string
	ByCategory         map[string]*CategoryMetrics
	Overall            CategoryMetrics
	Misclassifications []Misclassification
}

// Evaluate compares predictions (ColumnKey -> predicted category, absent or
// empty meaning "not PII") against the dataset ground truth.
//
// Counting rules per column, with truth T and prediction P:
//   - T != "" and P == T: true positive for T
//   - T != "" and P == "": false negative for T
//   - T != "" and P != T (both set): false positive for P and false negative for T
//   - T == "" and P != "": false positive for P
func Evaluate(dataset *Dataset, predictions map[string]string) *Report {
	report := &Report{
		Language:   dataset.Language,
		ByCategory: map[string]*CategoryMetrics{},
	}
	metricsFor := func(category string) *CategoryMetrics {
		if _, ok := report.ByCategory[category]; !ok {
			report.ByCategory[category] = &CategoryMetrics{}
		}
		return report.ByCategory[category]
	}

	for _, table := range dataset.Tables {
		for _, column := range table.Columns {
			key := ColumnKey(table.Schema, table.Table, column.Name)
			truth := column.ExpectedCategory
			predicted := predictions[key]

			switch {
			case truth != "" && predicted == truth:
				metricsFor(truth).TruePositives++
				report.Overall.TruePositives++
			case truth != "" && predicted == "":
				metricsFor(truth).FalseNegatives++
				report.Overall.FalseNegatives++
				report.Misclassifications = append(report.Misclassifications, Misclassification{key, truth, predicted})
			case truth != "" && predicted != truth:
				metricsFor(predicted).FalsePositives++
				metricsFor(truth).FalseNegatives++
				report.Overall.FalsePositives++
				report.Overall.FalseNegatives++
				report.Misclassifications = append(report.Misclassifications, Misclassification{key, truth, predicted})
			case truth == "" && predicted != "":
				metricsFor(predicted).FalsePositives++
				report.Overall.FalsePositives++
				report.Misclassifications = append(report.Misclassifications, Misclassification{key, truth, predicted})
			}
		}
	}
	return report
}

// String renders the report as a fixed-width table for test logs.
func (r *Report) String() string {
	categories := make([]string, 0, len(r.ByCategory))
	for category := range r.ByCategory {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%-16s %4s %4s %4s %10s %8s\n", "category", "tp", "fp", "fn", "precision", "recall")
	for _, category := range categories {
		m := r.ByCategory[category]
		fmt.Fprintf(&sb, "%-16s %4d %4d %4d %10.2f %8.2f\n",
			category, m.TruePositives, m.FalsePositives, m.FalseNegatives, m.Precision(), m.Recall())
	}
	fmt.Fprintf(&sb, "%-16s %4d %4d %4d %10.2f %8.2f\n",
		"overall", r.Overall.TruePositives, r.Overall.FalsePositives, r.Overall.FalseNegatives,
		r.Overall.Precision(), r.Overall.Recall())
	for _, m := range r.Misclassifications {
		fmt.Fprintf(&sb, "  miss: %s expected=%q predicted=%q\n", m.Column, m.Expected, m.Predicted)
	}
	return sb.String()
}
