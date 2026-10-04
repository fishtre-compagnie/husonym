package model

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"

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
// The object is read where it is: after a reasoning block, inside a fenced block, before
// a closing sentence. Why the model stopped is not asked: an object that is complete and
// valid is an answer, and one that was cut is not an object.
func readAnswer(completion *openai.ChatCompletion, count int) (answers map[string]columnAnswer, ignored int) {
	answers = map[string]columnAnswer{}
	if completion == nil || len(completion.Choices) == 0 {
		return answers, 0
	}
	choice := completion.Choices[0]
	if choice.Message.Refusal != "" {
		return answers, 0
	}
	members, ok := answerObject(choice.Message.Content)
	if !ok {
		return answers, 0
	}

	asked := make(map[string]bool, count)
	for i := range count {
		asked[columnId(i)] = true
	}
	for id, raw := range members {
		if !asked[id] {
			ignored++
			continue
		}
		if answer, ok := readColumnAnswer(raw); ok {
			answers[id] = answer
		}
	}
	return answers, ignored
}

// A block in which a model writes its reasoning before its answer.
var reasoningBlock = regexp.MustCompile(`(?s)^\s*<(think|thinking|reasoning)>.*?</(think|thinking|reasoning)>`)

// answerObject finds the JSON object of an answer: the first one that starts after the
// reasoning block, if any, and that decodes whole. What follows it is not read.
func answerObject(content string) (map[string]json.RawMessage, bool) {
	content = reasoningBlock.ReplaceAllString(content, "")
	for start := strings.IndexByte(content, '{'); start >= 0; {
		var members map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(content[start:])).Decode(&members); err == nil {
			return members, true
		}
		next := strings.IndexByte(content[start+1:], '{')
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return nil, false
}

func readColumnAnswer(raw json.RawMessage) (columnAnswer, bool) {
	var member struct {
		Category   *string  `json:"category"`
		Confidence *float64 `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &member); err != nil || member.Category == nil || member.Confidence == nil {
		return columnAnswer{}, false
	}
	confidence := *member.Confidence
	if math.IsNaN(confidence) || confidence < 0 || confidence > 1 {
		return columnAnswer{}, false
	}
	if *member.Category != categoryNone && !report.Category(*member.Category).Valid() {
		return columnAnswer{}, false
	}
	return columnAnswer{category: *member.Category, confidence: confidence}, true
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
