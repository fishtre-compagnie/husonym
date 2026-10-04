package model

import (
	"encoding/json"
	"math"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/openai/openai-go/v3"
)

// columnAnswer is the valid answer of the model for one column.
type columnAnswer struct {
	category   string // one of the six categories, or categoryNone
	confidence float64
}

// readAnswer holds the answer of the model to its form, whatever the endpoint claims to
// enforce: an endpoint may ignore the schema it was given. It returns the valid answers
// by column id, and the number of members that name no column of the request.
//
// Only the form is checked. An answer that is refused, or that holds no JSON object,
// answers for no column; a member without an allowed category and a confidence from 0
// to 1 answers for none either. Nothing is repaired: a confidence of 95 is not read as
// 0.95, an unknown label is not taken for the nearest one.
//
// The object is read where it is: inside a fenced block, between sentences. What the
// model wrote in a reasoning block is not read, nor an object inside another. Outside
// those, the last object that names a column of the request is the answer; a column for
// which an object before it says something else is not answered, by either, and neither
// is a column whose key the object holds twice.
//
// A completion that did not stop by itself — its finish reason is given and is not
// "stop" — may have been cut after a draft. Its last object is the answer only when
// nothing follows it but spaces or the end of a code fence; otherwise it answers for no
// column.
func readAnswer(completion *openai.ChatCompletion, count int) (answers map[string]columnAnswer, ignored int) {
	answers = map[string]columnAnswer{}
	if completion == nil || len(completion.Choices) == 0 {
		return answers, 0
	}
	choice := completion.Choices[0]
	if choice.Message.Refusal != "" {
		return answers, 0
	}
	asked := make(map[string]bool, count)
	for i := range count {
		asked[columnId(i)] = true
	}
	pieces, reachesEnd := outsideReasoning(choice.Message.Content)
	objects := answerObjects(pieces, reachesEnd, asked)
	if len(objects) == 0 {
		return answers, 0
	}

	last := objects[len(objects)-1]
	if stopped := choice.FinishReason == "" || choice.FinishReason == finishStop; !stopped && !last.closes {
		return answers, 0
	}
	for id, raw := range last.members {
		if !asked[id] {
			ignored++
			continue
		}
		answer, ok := readColumnAnswer(raw)
		if !ok || disputed(objects[:len(objects)-1], id, answer) {
			continue
		}
		answers[id] = answer
	}
	return answers, ignored
}

// The finish reason of a completion the model ended by itself.
const finishStop = "stop"

// disputed tells whether one of the earlier objects holds, for a column, anything else
// than the answer.
func disputed(earlier []answerObject, id string, answer columnAnswer) bool {
	for _, object := range earlier {
		raw, held := object.members[id]
		if !held {
			continue
		}
		if other, ok := readColumnAnswer(raw); !ok || other != answer {
			return true
		}
	}
	return false
}

// readColumnAnswer reads the answer for one column: an object with an allowed category
// and a confidence from 0 to 1, each written once.
func readColumnAnswer(raw json.RawMessage) (columnAnswer, bool) {
	members, _, ok := decodeObject(string(raw))
	if !ok {
		return columnAnswer{}, false
	}
	var category *string
	var confidence *float64
	if json.Unmarshal(members["category"], &category) != nil || category == nil {
		return columnAnswer{}, false
	}
	if json.Unmarshal(members["confidence"], &confidence) != nil || confidence == nil {
		return columnAnswer{}, false
	}
	if math.IsNaN(*confidence) || *confidence < 0 || *confidence > 1 {
		return columnAnswer{}, false
	}
	if *category != categoryNone && !report.Category(*category).Valid() {
		return columnAnswer{}, false
	}
	return columnAnswer{category: *category, confidence: *confidence}, true
}

// Result is what the model found in the columns of one request.
type Result struct {
	// Findings are the columns the model says hold personal data, with a confidence at
	// or above the threshold, by column name.
	Findings map[string]report.ModelFinding
	// BelowThreshold are the columns it says hold personal data with less confidence.
	BelowThreshold []report.Dismissed
	// Unanswered are the columns it gave no valid answer for, asked twice.
	Unanswered []string
	// Ignored counts the members of its answers that named no column of the request.
	Ignored int
}

// keep files a valid answer: a column that holds no personal data produces nothing, the
// others are findings from the threshold up.
func (r *Result) keep(column string, answer columnAnswer, minConfidence float64) {
	if answer.category == categoryNone {
		return
	}
	category, confidence := report.Category(answer.category), float32(answer.confidence)
	if answer.confidence < minConfidence {
		r.BelowThreshold = append(r.BelowThreshold, report.Dismissed{
			ColumnName: column, Category: category, Confidence: confidence,
		})
		return
	}
	if r.Findings == nil {
		r.Findings = map[string]report.ModelFinding{}
	}
	r.Findings[column] = report.ModelFinding{Category: category, Confidence: confidence}
}
