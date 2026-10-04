package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
)

var customers = Table{Schema: "public", Name: "customers"}

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

	require.Equal(t, []string{"POST /v1/chat/completions"}, e.paths)
	request := e.requests[0]
	members := make([]string, 0, len(request))
	for member := range request {
		members = append(members, member)
	}
	require.ElementsMatch(t, []string{"model", "temperature", "messages", "response_format"}, members)
	require.Equal(t, "local-model", request["model"])
	require.EqualValues(t, 0, request["temperature"])

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
	table.Hints = "Columns named ref_* hold customer references. >>> <<< " + strings.Repeat("é", 3000)
	_, err := c.Classify(context.Background(), table, twoColumns())
	require.NoError(t, err)

	_, rest := sentUserMessage(t, e.requests[0])
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
	_, err := c.Classify(context.Background(), customers, columns)
	require.NoError(t, err)

	document, _ := sentUserMessage(t, e.requests[0])
	byId := document["columns"].(map[string]any)
	require.Equal(t, []any{"jean.dupont@example.org", "m.martin@example.com"}, byId["c1"].(map[string]any)["values"])
	require.NotContains(t, byId["c2"], "values")

	system := messageContent(t, e.requests[0], 0, "system")
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
			completion(`{"c1":{"category":"contact","confidence":0.9},"c2":"contact"}`),
			[]string{"b"}, []string{"b"},
		},
		"a confidence that is not a number of JSON": {
			completion(`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"contact","confidence":NaN}}`),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"not JSON": {
			completion("The first column holds email addresses."),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"JSON that is not an object": {
			completion(`["contact","contact"]`),
			[]string{"a", "b"}, []string{"a", "b"},
		},
		"a truncated answer": {
			completionEnding(`{"c1":{"category":"contact","confidence":0.9},"c2":{"category":"contact","confidence":0.9}}`, "length"),
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
				return http.StatusOK, completion("still not an answer")
			})
			c, _ := e.classifier(t, Config{MinConfidence: 0.5})

			result, err := c.Classify(context.Background(), customers, []Column{{Name: "a"}, {Name: "b"}})
			require.NoError(t, err)
			require.Equal(t, 2, e.calls(), "one request, and one more for what is missing")
			require.Equal(t, tt.unanswered, result.Unanswered)

			reasked := askedNames(t, e.requests[1])
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
	require.Equal(t, map[string]string{"c1": "b"}, askedNames(t, e.requests[1]))
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
			e := newEndpoint(t, func(int, map[string]any) (int, string) {
				return tt.status, errorBody("some_type", "some_code", "the endpoint says why")
			})
			c, _ := e.classifier(t, Config{})

			_, err := c.Classify(context.Background(), customers, twoColumns())
			var failure *Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tt.reason, failure.Reason)
			require.Equal(t, tt.status, failure.Status)
			require.Equal(t, tt.permanent, failure.Permanent())
			require.Contains(t, failure.Detail, "the endpoint says why")
			require.Contains(t, failure.Error(), fmt.Sprint(tt.status))
			require.Equal(t, 1, e.calls())
		})
	}
}

// A request the endpoint refuses for its form says what the endpoint must accept.
func Test_Error_SaysWhatTheEndpointMustAccept(t *testing.T) {
	err := &Error{Reason: ReasonRejected, Status: http.StatusBadRequest, Detail: "unknown field response_format"}
	require.Contains(t, err.Error(), "JSON schema")
	require.Contains(t, err.Error(), "unknown field response_format")
	require.NotContains(t, (&Error{Reason: ReasonRejected, Status: http.StatusUnauthorized}).Error(), "JSON schema")
}

func Test_Classify_ARequestThatTimesOutIsATransportFailure(t *testing.T) {
	release := make(chan struct{})
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		<-release
		return http.StatusOK, completion("{}")
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
	e.server.Close()

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
		return http.StatusOK, completion("{}")
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(ctx, customers, twoColumns())
	require.ErrorIs(t, err, context.Canceled)
	var failure *Error
	require.NotErrorAs(t, err, &failure)
}

func Test_Classify_Authorization(t *testing.T) {
	// The client library reads these by itself when it finds them.
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL"} {
		if value, set := os.LookupEnv(name); set {
			require.NoError(t, os.Unsetenv(name))
			t.Cleanup(func() { _ = os.Setenv(name, value) })
		}
	}
	e := newEndpoint(t, allAnswered("none", 1))

	withoutKey, _ := e.classifier(t, Config{})
	_, err := withoutKey.Classify(context.Background(), customers, twoColumns())
	require.NoError(t, err)
	require.Empty(t, e.headers[0].Values("Authorization"), "a local server needs no key")

	withKey, _ := e.classifier(t, Config{APIKey: "the-key"})
	_, err = withKey.Classify(context.Background(), customers, twoColumns())
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer the-key"}, e.headers[1].Values("Authorization"))
}

func Test_NewClassifier_WithoutModel(t *testing.T) {
	c, err := NewClassifier(Config{})
	require.NoError(t, err)
	require.Nil(t, c)
}

func Test_Classifier_Model(t *testing.T) {
	c, err := NewClassifier(Config{Model: "local-model", BaseURL: "http://localhost:1/v1"})
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
