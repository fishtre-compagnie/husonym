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
// The object is read where it is: inside a fenced block, between sentences. What the
// model wrote in a reasoning block is not read. Outside one, the last object that names
// a column of the request is the answer; a column for which an object before it says
// something else is not answered, by either. Why the model stopped is not asked: an
// object that is complete and valid is an answer, and one that was cut is not an object.
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
	objects := answerObjects(withoutReasoning(choice.Message.Content), asked)
	if len(objects) == 0 {
		return answers, 0
	}

	last := objects[len(objects)-1]
	for id, raw := range last {
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

// disputed tells whether one of the earlier objects holds, for a column, anything else
// than the answer.
func disputed(earlier []map[string]json.RawMessage, id string, answer columnAnswer) bool {
	for _, object := range earlier {
		raw, held := object[id]
		if !held {
			continue
		}
		if other, ok := readColumnAnswer(raw); !ok || other != answer {
			return true
		}
	}
	return false
}

// The tags of a block in which a model writes its reasoning, opening or closing, in any
// case, with or without attributes.
var reasoningTag = regexp.MustCompile(`(?i)<(/?)(?:think|thinking|reasoning)(?:\s[^>]*)?>`)

// withoutReasoning returns the content outside the reasoning blocks. Blocks may follow
// each other or hold one another. A block that is not closed runs to the end; a block
// that is closed without having been opened started at the beginning.
func withoutReasoning(content string) string {
	var kept strings.Builder
	depth, from := 0, 0
	for _, tag := range reasoningTag.FindAllStringSubmatchIndex(content, -1) {
		start, end, closing := tag[0], tag[1], tag[3] > tag[2]
		switch {
		case !closing:
			if depth == 0 {
				kept.WriteString(content[from:start])
			}
			depth++
		case depth > 0:
			depth--
		default:
			kept.Reset()
		}
		from = end
	}
	if depth == 0 {
		kept.WriteString(content[from:])
	}
	return kept.String()
}

// answerObjects finds, in their order, the JSON objects that decode whole and name at
// least one column of the request. An object inside another is part of it.
func answerObjects(content string, asked map[string]bool) []map[string]json.RawMessage {
	var objects []map[string]json.RawMessage
	for start := strings.IndexByte(content, '{'); start >= 0; {
		var members map[string]json.RawMessage
		decoder := json.NewDecoder(strings.NewReader(content[start:]))
		step := 1
		if err := decoder.Decode(&members); err == nil {
			step = int(decoder.InputOffset())
			if namesAColumn(members, asked) {
				objects = append(objects, members)
			}
		}
		next := strings.IndexByte(content[start+step:], '{')
		if next < 0 {
			break
		}
		start += step + next
	}
	return objects
}

func namesAColumn(members map[string]json.RawMessage, asked map[string]bool) bool {
	for id := range members {
		if asked[id] {
			return true
		}
	}
	return false
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
