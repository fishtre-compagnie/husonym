package model

import (
	"strconv"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
)

// categoryNone is the answer for a column that holds no personal data. It is a label of
// the exchange with the model only: it is never stored.
const categoryNone = "none"

// columnId names a column in a request. Ids keep the schema valid whatever the name of
// a column, and spare the model from writing the names back.
func columnId(index int) string {
	return "c" + strconv.Itoa(index+1)
}

// answerSchema is the JSON schema of the answer to a request about count columns: one
// member per column, each a category and a confidence. Every member is required and no
// other is allowed, which is what an endpoint needs to hold the answer to the schema.
func answerSchema(count int) map[string]any {
	labels := make([]string, 0, len(report.Categories)+1)
	for _, category := range report.Categories {
		labels = append(labels, string(category))
	}
	labels = append(labels, categoryNone)

	ids := make([]string, 0, count)
	properties := make(map[string]any, count)
	for i := range count {
		id := columnId(i)
		ids = append(ids, id)
		properties[id] = map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"category", "confidence"},
			"properties": map[string]any{
				"category":   map[string]any{"type": "string", "enum": labels},
				"confidence": map[string]any{"type": "number"},
			},
		}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             ids,
		"properties":           properties,
	}
}
