package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/internal/piitest"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
)

var customers = Table{Name: "customers"}

func twoColumns() []Column {
	return []Column{
		{Name: "email", DataType: "text", Nullable: true, Profile: &profile.Profile{
			Rows: 200, Nulls: 3, Distinct: 197, Kind: profile.KindText,
			Hits: []profile.Share{{Name: "email", Share: 0.99}},
		}},
		{Name: "created_at", DataType: "timestamp"},
	}
}

func allAnswered(category string, confidence float64) func(int, map[string]any) (int, string) {
	return func(_ int, request map[string]any) (int, string) {
		schema := request["response_format"].(map[string]any)["json_schema"].(map[string]any)["schema"].(map[string]any)
		byId := map[string]answer{}
		for _, id := range schema["required"].([]any) {
			byId[id.(string)] = answer{Category: category, Confidence: confidence}
		}
		return http.StatusOK, answers(byId)
	}
}

// What is sent: the model, a temperature of 0, a system and a user message, and the schema
// of the answer. Nothing else.
func Test_Classify_Request(t *testing.T) {
	e := newEndpoint(t, allAnswered("none", 1))
	c, _ := e.classifier(t, Config{Model: "local-model", MinConfidence: 0.5})

	_, err := c.Classify(context.Background(), customers, twoColumns())
	require.NoError(t, err)

	require.Equal(t, "POST /v1/chat/completions", e.Requests()[0].Path)
	request := e.Requests()[0].JSON
	members := make([]string, 0, len(request))
	for member := range request {
		members = append(members, member)
	}
	require.ElementsMatch(t, []string{"model", "temperature", "messages", "response_format"}, members)
	require.Equal(t, "local-model", request["model"])
	require.Equal(t, float64(0), request["temperature"])
	require.Contains(t, e.Requests()[0].Body, `"temperature":0,`)

	wantFormat := `{"type":"json_schema","json_schema":{"name":"column_categories","strict":true,"schema":{
		"type":"object","additionalProperties":false,"required":["c1","c2"],
		"properties":{
			"c1":{"type":"object","additionalProperties":false,"required":["category","confidence"],"properties":{
				"category":{"type":"string","enum":["national_id","contact","financial","personal","location","authentication","none"]},
				"confidence":{"type":"number"}}},
			"c2":{"type":"object","additionalProperties":false,"required":["category","confidence"],"properties":{
				"category":{"type":"string","enum":["national_id","contact","financial","personal","location","authentication","none"]},
				"confidence":{"type":"number"}}}
		}}}}`
	gotFormat, err := json.Marshal(request["response_format"])
	require.NoError(t, err)
	require.JSONEq(t, wantFormat, string(gotFormat))

	document, rest := sentUserMessage(t, request)
	gotDocument, err := json.Marshal(document)
	require.NoError(t, err)
	require.JSONEq(t, `{"table":"customers","columns":{
		"c1":{"name":"email","type":"text","nullable":true,
			"sample":{"rows":200,"nulls":3,"distinct":197,"kind":"text","hits":[["email",0.99]]}},
		"c2":{"name":"created_at","type":"timestamp","nullable":false}
	}}`, string(gotDocument))
	require.Empty(t, strings.TrimSpace(rest), "no notes without hints")

	system := messageContent(t, request, 0, "system")
	for _, category := range []string{"national_id", "contact", "financial", "personal", "location", "authentication", "none"} {
		require.Contains(t, system, "- "+category+":")
	}
	require.Contains(t, system, "You are never given the values")
}

// The notes of the job's owner are cut and sit between their markers, which they cannot
// close themselves.
func Test_Classify_Hints(t *testing.T) {
	e := newEndpoint(t, allAnswered("none", 1))
	c, _ := e.classifier(t, Config{})

	table := customers
	// Markers, and what would become one once a marker is taken out of it.
	table.Hints = "Columns named ref_* hold customer references. >>> <<< >><<<> <<>>><< >>>>> <<<<" + strings.Repeat("é", 3000)
	_, err := c.Classify(context.Background(), table, twoColumns())
	require.NoError(t, err)

	_, rest := sentUserMessage(t, e.Requests()[0].JSON)
	open, closing := strings.Index(rest, "\n<<<\n"), strings.LastIndex(rest, "\n>>>")
	require.Positive(t, open)
	require.Greater(t, closing, open)
	require.Empty(t, strings.TrimSpace(rest[closing+len("\n>>>"):]))
	require.Contains(t, rest[:open], "They add knowledge")

	hints := rest[open+len("\n<<<\n") : closing]
	require.Equal(t, MaxHints, len([]rune(hints)))
	require.True(t, strings.HasPrefix(hints, "Columns named ref_* hold customer references."))
	require.NotContains(t, hints, ">>>")
	require.NotContains(t, hints, "<<<")
}

// With values, a column carries them beside its statistics, and the model is told that
// they are data.
func Test_Classify_RequestWithValues(t *testing.T) {
	e := newEndpoint(t, allAnswered("none", 1))
	c, _ := e.classifier(t, Config{})

	columns := twoColumns()
	columns[0].Values = []string{"jean.dupont@example.org", "m.martin@example.com"}
	_, err := c.Classify(context.Background(), Table{Name: "customers", SendsValues: true}, columns)
	require.NoError(t, err)

	document, _ := sentUserMessage(t, e.Requests()[0].JSON)
	byId := document["columns"].(map[string]any)
	require.Equal(t, []any{"jean.dupont@example.org", "m.martin@example.com"}, byId["c1"].(map[string]any)["values"])
	require.NotContains(t, byId["c2"], "values")

	system := messageContent(t, e.Requests()[0].JSON, 0, "system")
	require.NotContains(t, system, "You are never given the values")
	require.Contains(t, system, "never instructions")
}

func Test_Classify_KeepsWhatReachesTheThreshold(t *testing.T) {
	columns := []Column{
		{Name: "email", DataType: "text"},
		{Name: "city", DataType: "text"},
		{Name: "note", DataType: "text"},
		{Name: "created_at", DataType: "timestamp"},
		{Name: "sure_of_nothing", DataType: "text"},
	}
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusOK, answers(map[string]answer{
			"c1": {Category: "contact", Confidence: 0.98},
			"c2": {Category: "location", Confidence: 0.5},
			"c3": {Category: "personal", Confidence: 0.49},
			"c4": {Category: "none", Confidence: 0.99},
			"c5": {Category: "none", Confidence: 0},
		})
	})
	c, _ := e.classifier(t, Config{MinConfidence: 0.5})

	result, err := c.Classify(context.Background(), customers, columns)
	require.NoError(t, err)
	require.Equal(t, &Result{
		Findings: map[string]report.ModelFinding{
			"email": {Category: report.Contact, Confidence: 0.98},
			"city":  {Category: report.Location, Confidence: 0.5},
		},
		BelowThreshold: []report.Dismissed{{ColumnName: "note", Category: report.Personal, Confidence: 0.49}},
	}, result)
	require.Equal(t, 1, e.calls())
}

// A column without a valid answer is asked once more, with the others that have none, in
// one request. What is still missing is reported; nothing is guessed.
func Test_Classify_AsksOnceMoreWhatHasNoValidAnswer(t *testing.T) {
	good := answer{Category: "contact", Confidence: 0.9}
	for name, tt := range map[string]struct {
		first      string
		reasked    []string // names of the columns of the second request
		unanswered []string
	}{
		"a missing id": {
			answers(map[string]answer{"c1": good}),
			[]string{"b"}, []string{"b"},
		},
		"an unknown category": {
			answers(map[string]answer{"c1": good, "c2": {Category: "email", Confidence: 0.9}}),
			[]string{"b"}, []string{"b"},
		},
		"a category in another case": {
			answers(map[string]answer{"c1": good, "c2": {Category: "Contact", Confidence: 0.9}}),
			[]string{"b"}, []string{"b"},
		},
		"a confidence under 0": {
			answers(map[string]answer{"c1": good, "c2": {Category: "contact", Confidence: -0.1}}),
			[]string{"b"}, []string{"b"},
		},
		"a confidence above 1": {
			answers(map[string]answer{"c1": good, "c2": {Category: "contact", Confidence: 95}}),
			[]string{"b"}, []string{"b"},
		},
		"a confidence that is a text": {
			answers(map[string]answer{"c1": good, "c2": {Category: "contact", Confidence: "0.9"}}),
			[]string{"b"}, []string{"b"},
		},
		"no confidence": {
			answers(map[string]answer{"c1": good, "c2": {Category: "contact"}}),
			[]string{"b"}, []string{"b"},
		},
		"an answer that is not an object": {
			piitest.Completion(`{"c1":{"category":"contact","confidence":0.9},"c2":"contact"}`),
			[]string{"b"}, []string{"b"},
		},
		"a confidence that is not a number of JSON": {
			piitest.Completion(`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"contact","confidence":NaN}}`),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"not JSON": {
			piitest.Completion("The first column holds email addresses."),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"JSON that is not an object": {
			piitest.Completion(`["contact","contact"]`),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"an answer cut before its end": {
			piitest.CompletionEnding(`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"cont`, "length"),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"a fenced block that is not JSON": {
			piitest.Completion("```json\nnot an answer\n```"),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"a refusal": {
			`{"id":"1","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"","refusal":"I cannot help with that."}}]}`,
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"no choice": {
			`{"id":"1","object":"chat.completion","choices":[]}`,
			[]string{"a", "b"}, []string{"a", "b"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(call int, _ map[string]any) (int, string) {
				if call == 1 {
					return http.StatusOK, tt.first
				}
				// The second answer is no better: what was missing stays missing.
				return http.StatusOK, piitest.Completion("still not an answer")
			})
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, 2, e.calls(), "one request, and one more for what is missing")
			require.Equal(t, tt.unanswered, result.Unanswered)

			reasked := askedNames(t, e.Requests()[1].JSON)
			names := make([]string, 0, len(reasked))
			for i := 1; i <= len(reasked); i++ {
				names = append(names, reasked[fmt.Sprintf("c%d", i)])
			}
			require.Equal(t, tt.reasked, names)
			if len(tt.unanswered) == 1 {
				require.Equal(t, map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}}, result.Findings)
			} else {
				require.Empty(t, result.Findings)
			}
		})
	}
}

// An answer is read for what it holds: an endpoint that does not hold its model to the
// schema may wrap the document in a fenced block, put its reasoning before it, add a
// word after it, or end for another reason than "stop". The document is still checked
// member by member.
func Test_Classify_ReadsAnAnswerThatIsWrapped(t *testing.T) {
	const document = `{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"none","confidence":0.8}}`
	for name, body := range map[string]string{
		"a fenced block":                  piitest.Completion("```json\n" + document + "\n```"),
		"a fenced block without language": piitest.Completion("```\n" + document + "\n```"),
		"a reasoning block before it":     piitest.Completion("<think>\nThe first column holds {emails}.\n</think>\n\n" + document),
		"a reasoning block and a fence":   piitest.Completion("<think>c1 is contact</think>\n```json\n" + document + "\n```\n"),
		"a reasoning block that holds a draft": piitest.Completion(
			`<think>First guess: {"c1":{"category":"none","confidence":0.1},"c2":{"category":"contact","confidence":1}}</think>` + document),
		"a sentence before it":           piitest.Completion("Here is the classification:\n" + document),
		"a sentence after it":            piitest.Completion(document + "\nLet me know if you need anything else."),
		"spaces around it":               piitest.Completion("\n  " + document + "  \n"),
		"a complete document, cut after": piitest.CompletionEnding(document, "length"),
		"another reason to end":          piitest.CompletionEnding(document, "eos"),
		"no reason to end":               piitest.CompletionEnding(document, ""),
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, body })
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, 1, e.calls(), "nothing is asked again")
			require.Equal(t, &Result{
				Findings: map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}},
			}, result)
		})
	}
}

// A draft the model wrote before its answer never stands for the answer. Inside a
// reasoning block it is not read, however the block is written; outside one, the last
// object is the answer, and a column on which a draft says otherwise is not answered.
func Test_Classify_ADraftIsNotTheAnswer(t *testing.T) {
	const (
		draft    = `{"c1":{"category":"none","confidence":0.9},"c2":{"category":"contact","confidence":1}}`
		document = `{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"none","confidence":0.8}}`
	)
	answered := &Result{Findings: map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}}}
	unanswered := &Result{Unanswered: []string{"a", "b"}}
	for name, tc := range map[string]struct {
		body  string
		calls int
		want  *Result
	}{
		"a reasoning block that was cut": {
			piitest.CompletionEnding("<think>Let me try: "+draft+" no, wait", "length"), 2, unanswered,
		},
		"text before the reasoning block": {
			piitest.Completion("Sure.\n<think>" + draft + "</think>\n" + document), 1, answered,
		},
		"a reasoning block in capitals": {
			piitest.Completion("<THINK>" + draft + "</THINK>" + document), 1, answered,
		},
		"a reasoning block with attributes": {
			piitest.Completion(`<think type="x">` + draft + `</think>` + document), 1, answered,
		},
		"nested reasoning blocks": {
			piitest.Completion("<think>first <think>" + draft + "</think> then " + draft + "</think>" + document), 1, answered,
		},
		"two reasoning blocks": {
			piitest.Completion("<think>" + draft + "</think> so <reasoning>" + draft + "</reasoning>" + document), 1, answered,
		},
		"a block closed without having been opened": {
			piitest.Completion(draft + "</think>" + document), 1, answered,
		},
		"a draft in plain prose": {
			piitest.Completion("A first guess would be " + draft + " but the answer is " + document), 2, unanswered,
		},
		"only a draft, before a reasoning block that was cut": {
			piitest.CompletionEnding("<think>"+draft+"</think><think>"+document, "length"), 2, unanswered,
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, tc.body })
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, tc.calls, e.calls())
			require.Equal(t, tc.want, result)
		})
	}
}

// A completion the endpoint cut holds an answer only when the answer is the last thing it
// wrote: an object followed by anything else, or by a reasoning block, may be a draft of
// the answer that was cut.
func Test_Classify_ACompletionThatWasCut(t *testing.T) {
	const (
		draft    = `{"c1":{"category":"none","confidence":0.9},"c2":{"category":"contact","confidence":1}}`
		document = `{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"none","confidence":0.8}}`
	)
	answered := &Result{Findings: map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}}}
	unanswered := &Result{Unanswered: []string{"a", "b"}}
	for name, tc := range map[string]struct {
		body  string
		calls int
		want  *Result
	}{
		"a draft in prose, then the answer cut": {
			piitest.CompletionEnding("A first guess: "+draft+" On reflection, the answer is "+`{"c1":{"categ`, "length"), 2, unanswered,
		},
		"a draft, then prose that was cut": {
			piitest.CompletionEnding(draft+"\nWait, c1 looks like", "length"), 2, unanswered,
		},
		"a draft before a reasoning block that was cut": {
			piitest.CompletionEnding(draft+"\n<think>c1 could also be", "length"), 2, unanswered,
		},
		"a draft before a reasoning block, then nothing": {
			piitest.CompletionEnding(draft+"\n<think>c1 is fine</think>", "length"), 2, unanswered,
		},
		"a draft inside [THINK], cut": {
			piitest.CompletionEnding("[THINK]"+draft+" hmm", "length"), 2, unanswered,
		},
		"a draft inside <thought>, cut": {
			piitest.CompletionEnding("<thought>"+draft+" hmm", "length"), 2, unanswered,
		},
		"a draft inside <seed:think>, cut": {
			piitest.CompletionEnding("<seed:think>"+draft+" hmm", "length"), 2, unanswered,
		},
		"a wrapper that was cut, whose draft is whole": {
			piitest.CompletionEnding(`{"draft":`+draft+`,"final":{"c1":{"categ`, "length"), 2, unanswered,
		},
		"a draft stopped by a content filter": {
			piitest.CompletionEnding(draft+" but", "content_filter"), 2, unanswered,
		},
		"the answer, then the end of its fence": {
			piitest.CompletionEnding("```json\n"+document+"\n```\n", "length"), 1, answered,
		},
		"the answer after a reasoning block": {
			piitest.CompletionEnding("<think>"+draft+"</think>\n"+document+"\n", "length"), 1, answered,
		},
		"a draft then the answer, both whole": {
			piitest.CompletionEnding(document+" Checked again: "+document, "length"), 1, answered,
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, tc.body })
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, tc.calls, e.calls())
			require.Equal(t, tc.want, result)
		})
	}
}

// The blocks a model reasons in, by the tags the models in use write them with.
func Test_Classify_ReasoningBlocks(t *testing.T) {
	const (
		draft    = `{"c1":{"category":"none","confidence":0.9},"c2":{"category":"contact","confidence":1}}`
		document = `{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"none","confidence":0.8}}`
	)
	answered := &Result{Findings: map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}}}
	for _, block := range [][2]string{
		{"<think>", "</think>"}, {"<thinking>", "</thinking>"}, {"<thought>", "</thought>"},
		{"<Thoughts>", "</Thoughts>"}, {"<reasoning>", "</reasoning>"}, {"<reflection>", "</reflection>"},
		{"<REFLECTION>", "</REFLECTION>"}, {"<scratchpad>", "</scratchpad>"}, {"<seed:think>", "</seed:think>"},
		{"[THINK]", "[/THINK]"}, {"[think]", "[/think]"}, {`<thought id="1">`, "</thought>"},
	} {
		t.Run(block[0], func(t *testing.T) {
			body := piitest.Completion(block[0] + draft + block[1] + document)
			e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, body })
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, 1, e.calls())
			require.Equal(t, answered, result)
		})
	}

	// A closing tag that nothing opened closes a block that started at the beginning: what
	// stood before it is not read, earlier blocks included.
	body := piitest.Completion(draft + "<think>x</think> so </think>" + document)
	e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, body })
	c, _ := e.classifier(t, Config{MinConfidence: 0.5})
	result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
	require.NoError(t, err)
	require.Equal(t, 1, e.calls())
	require.Equal(t, answered, result)
}

// An object is an answer where it stands alone: not inside another object, whole or not,
// and not when it names no column of the request.
func Test_Classify_ObjectsThatAreNoAnswer(t *testing.T) {
	const document = `{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"none","confidence":0.8}}`
	answered := &Result{Findings: map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}}}
	unanswered := &Result{Unanswered: []string{"a", "b"}}
	for name, tc := range map[string]struct {
		body  string
		calls int
		want  *Result
	}{
		"inside an object that is whole": {piitest.Completion(`{"answer":` + document + `}`), 2, unanswered},
		"inside an object that is not JSON": {
			piitest.Completion(`{answer: ` + document + `}`), 2, unanswered,
		},
		"inside an object that is not closed": {
			piitest.Completion(`{"draft":` + document + `, "final": `), 2, unanswered,
		},
		"after braces in a sentence": {piitest.Completion("The set {a, b} gives " + document), 1, answered},
		"before an object that names no column": {
			piitest.Completion(document + ` {"summary":{"category":"none","confidence":1}}`), 1, answered,
		},
		"a brace inside a text of a broken object": {
			piitest.Completion(`{"note": "a } here", oops} ` + document), 1, answered,
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, tc.body })
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, tc.calls, e.calls())
			require.Equal(t, tc.want, result)
		})
	}
}

// A key written twice in an object answers nothing for its column: which of the two the
// model meant is not guessed.
func Test_Classify_AKeyWrittenTwice(t *testing.T) {
	bodies := []string{
		piitest.Completion(`{"c1":{"category":"none","confidence":0.9},"c2":{"category":"location","confidence":0.8},"c1":{"category":"contact","confidence":0.9}}`),
		piitest.Completion(`{"c1":{"category":"none","category":"contact","confidence":0.9},"c2":{"category":"location","confidence":0.8}}`),
		piitest.Completion(`{"c1":{"category":"contact","confidence":0.1,"confidence":0.9},"c2":{"category":"location","confidence":0.8}}`),
		// In a draft too: the draft says something else than the answer.
		piitest.Completion(`{"c1":{"category":"contact","confidence":0.9},"c1":{"category":"contact","confidence":0.9}} then ` +
			`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"location","confidence":0.8}}`),
	}
	for _, body := range bodies {
		e := newEndpoint(t, func(call int, _ map[string]any) (int, string) {
			if call == 1 {
				return http.StatusOK, body
			}
			return http.StatusOK, answers(map[string]answer{"c1": {Category: "none", Confidence: 0.9}})
		})
		c, _ := e.classifier(t, Config{MinConfidence: 0.5})

		result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"c1": "a"}, askedNames(t, e.Requests()[1].JSON), "only the column written twice is asked again")
		require.Equal(t, &Result{
			Findings: map[string]report.ModelFinding{"b": {Category: report.Location, Confidence: 0.8}},
		}, result)
	}
}

// Two objects outside a reasoning block: the last one answers, except for the columns
// they do not agree on.
func Test_Classify_TwoObjectsThatPartlyAgree(t *testing.T) {
	body := piitest.Completion(
		`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"personal","confidence":0.9}}` +
			" or rather " +
			`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"none","confidence":0.9},"c3":{"category":"location","confidence":0.7}}`)
	e := newEndpoint(t, func(call int, _ map[string]any) (int, string) {
		if call == 1 {
			return http.StatusOK, body
		}
		return http.StatusOK, answers(map[string]answer{"c1": {Category: "none", Confidence: 0.9}})
	})
	c, _ := e.classifier(t, Config{MinConfidence: 0.5})

	result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"c1": "b"}, askedNames(t, e.Requests()[1].JSON), "only the disputed column is asked again")
	require.Equal(t, &Result{Findings: map[string]report.ModelFinding{
		"a": {Category: report.Contact, Confidence: 0.9},
		"c": {Category: report.Location, Confidence: 0.7},
	}}, result)
}

func Test_Classify_TheSecondAnswerCompletesTheFirst(t *testing.T) {
	e := newEndpoint(t, func(call int, _ map[string]any) (int, string) {
		if call == 1 {
			return http.StatusOK, answers(map[string]answer{
				"c1": {Category: "contact", Confidence: 0.9},
				"c3": {Category: "none", Confidence: 0.9},
			})
		}
		return http.StatusOK, answers(map[string]answer{"c1": {Category: "financial", Confidence: 0.8}})
	})
	c, _ := e.classifier(t, Config{MinConfidence: 0.5})

	result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"c1": "b"}, askedNames(t, e.Requests()[1].JSON))
	require.Equal(t, &Result{Findings: map[string]report.ModelFinding{
		"a": {Category: report.Contact, Confidence: 0.9},
		"b": {Category: report.Financial, Confidence: 0.8},
	}}, result)
}

// An id the batch does not hold names no column: it is counted and never stored.
func Test_Classify_IgnoresAnIdThatIsNotOfTheBatch(t *testing.T) {
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusOK, answers(map[string]answer{
			"c1":       {Category: "contact", Confidence: 0.9},
			"c7":       {Category: "contact", Confidence: 0.9},
			"password": {Category: "authentication", Confidence: 1},
		})
	})
	c, _ := e.classifier(t, Config{MinConfidence: 0.5})

	result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}})
	require.NoError(t, err)
	require.Equal(t, &Result{
		Findings: map[string]report.ModelFinding{"a": {Category: report.Contact, Confidence: 0.9}},
		Ignored:  2,
	}, result)
	require.Equal(t, 1, e.calls())
}

// When the second request cannot be made, the first answer is kept.
func Test_Classify_AFailedSecondRequestLeavesTheColumnsUnanswered(t *testing.T) {
	e := newEndpoint(t, func(call int, _ map[string]any) (int, string) {
		if call == 1 {
			return http.StatusOK, answers(map[string]answer{"c1": {Category: "contact", Confidence: 0.9}})
		}
		return http.StatusServiceUnavailable, errorBody("server_error", "", "overloaded")
	})
	c, _ := e.classifier(t, Config{MinConfidence: 0.5})

	result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, result.Unanswered)
	require.Len(t, result.Findings, 1)
}

// Which failures a new attempt may cure. The library does not retry by itself: each
// failure is one request.
func Test_Classify_Failures(t *testing.T) {
	for _, tt := range []struct {
		status    int
		reason    Reason
		permanent bool
	}{
		{http.StatusBadRequest, ReasonRejected, true},
		{http.StatusUnauthorized, ReasonRejected, true},
		{http.StatusForbidden, ReasonRejected, true},
		{http.StatusNotFound, ReasonRejected, true},
		{http.StatusUnprocessableEntity, ReasonRejected, true},
		{http.StatusRequestTimeout, ReasonUnavailable, false},
		{http.StatusConflict, ReasonUnavailable, false},
		{http.StatusTooManyRequests, ReasonUnavailable, false},
		{http.StatusInternalServerError, ReasonUnavailable, false},
		{http.StatusServiceUnavailable, ReasonUnavailable, false},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			// The status decides, whatever the body: the error object of the API, an
			// error that is a text, a page, nothing.
			for body, detail := range map[string]string{
				errorBody("some_type", "some_code", "the endpoint says why"): "some_type some_code",
				`{"error":"Unauthorized"}`:                                   "",
				`<html><body>proxy error</body></html>`:                      "",
				``:                                                           "",
			} {
				e := newEndpoint(t, func(int, map[string]any) (int, string) { return tt.status, body })
				c, _ := e.classifier(t, Config{})

				_, err := c.Classify(context.Background(), customers, twoColumns())
				var failure *Error
				require.ErrorAs(t, err, &failure, body)
				require.Equal(t, tt.reason, failure.Reason, body)
				require.Equal(t, tt.status, failure.Status, body)
				require.Equal(t, tt.permanent, failure.Permanent(), body)
				require.Equal(t, detail, failure.Detail, body)
				require.Contains(t, failure.Error(), fmt.Sprint(tt.status))
				require.Equal(t, 1, e.calls())
			}
		})
	}
}

// A request the endpoint refuses for its form says what the endpoint must accept.
func Test_Error_SaysWhatTheEndpointMustAccept(t *testing.T) {
	err := &Error{Reason: ReasonRejected, Status: http.StatusBadRequest, Detail: "invalid_request_error"}
	require.Contains(t, err.Error(), "JSON schema")
	require.Contains(t, err.Error(), "invalid_request_error")
	require.NotContains(t, (&Error{Reason: ReasonRejected, Status: http.StatusUnauthorized}).Error(), "JSON schema")
}

func Test_Classify_ARequestThatTimesOutIsATransportFailure(t *testing.T) {
	release := make(chan struct{})
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		<-release
		return http.StatusOK, piitest.Completion("{}")
	})
	defer close(release)
	c, _ := e.classifier(t, Config{})
	c.requestTimeout = 50 * time.Millisecond

	_, err := c.Classify(context.Background(), customers, twoColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, ReasonTransport, failure.Reason)
	require.Zero(t, failure.Status)
	require.False(t, failure.Permanent())
	require.Equal(t, 1, e.calls())
}

func Test_Classify_AClosedConnectionIsATransportFailure(t *testing.T) {
	e := newEndpoint(t, nil)
	c, _ := e.classifier(t, Config{})
	e.Close()

	_, err := c.Classify(context.Background(), customers, twoColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, ReasonTransport, failure.Reason)
	require.False(t, failure.Permanent())
}

// A cancellation is not a failure of the endpoint.
func Test_Classify_ACancelledContextIsReturnedAsItIs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		cancel()
		time.Sleep(20 * time.Millisecond)
		return http.StatusOK, piitest.Completion("{}")
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(ctx, customers, twoColumns())
	require.ErrorIs(t, err, context.Canceled)
	var failure *Error
	require.NotErrorAs(t, err, &failure)
}

// What the configuration says is all that is sent: the client library reads none of its
// own variables, so that a key of OpenAI in the environment never reaches another host.
func Test_Classify_SendsOnlyTheCredentialsOfItsConfiguration(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-the-environment")
	t.Setenv("OPENAI_ORG_ID", "org-from-the-environment")
	t.Setenv("OPENAI_PROJECT_ID", "proj-from-the-environment")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1/never-called")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "X-From-The-Environment: 1")
	e := newEndpoint(t, allAnswered("none", 1))

	withoutKey, _ := e.classifier(t, Config{})
	_, err := withoutKey.Classify(context.Background(), customers, twoColumns())
	require.NoError(t, err)
	require.Empty(t, e.Requests()[0].Header.Values("Authorization"), "a local server needs no key")
	require.Empty(t, e.Requests()[0].Header.Values("Openai-Organization"))
	require.Empty(t, e.Requests()[0].Header.Values("Openai-Project"))
	require.Empty(t, e.Requests()[0].Header.Values("X-From-The-Environment"))

	withKey, _ := e.classifier(t, Config{APIKey: "the-key", Organization: "org-1", Project: "proj-1"})
	_, err = withKey.Classify(context.Background(), customers, twoColumns())
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer the-key"}, e.Requests()[1].Header.Values("Authorization"))
	require.Equal(t, []string{"org-1"}, e.Requests()[1].Header.Values("Openai-Organization"))
	require.Equal(t, []string{"proj-1"}, e.Requests()[1].Header.Values("Openai-Project"))
}

// An answer is read up to a bound: an endpoint cannot make the worker hold an answer of
// any size.
func Test_Classify_AnAnswerLargerThanTheBoundIsAFailure(t *testing.T) {
	require.Equal(t, 1048576, maxAnswerBytes, "the bound is one mebibyte")

	huge := piitest.Completion(`{"c1":{"category":"none","confidence":1},"padding":"` + strings.Repeat("x", 2*maxAnswerBytes) + `"}`)
	e := newEndpoint(t, func(int, map[string]any) (int, string) { return http.StatusOK, huge })
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, twoColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, ReasonTransport, failure.Reason)
	require.Contains(t, failure.Detail, "larger than")

	// An answer under the bound is read.
	e = newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusOK, piitest.Completion(`{"c1":{"category":"none","confidence":1},"c2":{"category":"none","confidence":1},"padding":"` + strings.Repeat("x", maxAnswerBytes/2) + `"}`)
	})
	c, _ = e.classifier(t, Config{})
	_, err = c.Classify(context.Background(), customers, twoColumns())
	require.NoError(t, err)
}

func Test_NewClassifier_WithoutModel(t *testing.T) {
	c, err := NewClassifier(&Config{})
	require.NoError(t, err)
	require.Nil(t, c)
}

func Test_Classifier_Model(t *testing.T) {
	c, err := NewClassifier(&Config{Model: "local-model", BaseURL: "http://localhost:1/v1"})
	require.NoError(t, err)
	require.Equal(t, "local-model", c.Model())
}

func Test_Batches(t *testing.T) {
	columns := make([]Column, 60)
	for i := range columns {
		columns[i].Name = fmt.Sprintf("column_%d", i)
	}
	batches := Batches(columns)
	require.Len(t, batches, 3)
	require.Len(t, batches[0], 25)
	require.Len(t, batches[1], 25)
	require.Len(t, batches[2], 10)
	require.Equal(t, "column_25", batches[1][0].Name)

	// A column weighs more with its values: fewer of them in a request.
	columns[40].Values = []string{"a value"}
	batches = Batches(columns)
	require.Len(t, batches, 4)
	for _, batch := range batches {
		require.Len(t, batch, 15)
	}

	require.Empty(t, Batches(nil))
	require.Len(t, Batches(columns[:25]), 1)
	require.Len(t, Batches(columns[:1]), 1)
}
