package model

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/stretchr/testify/require"
)

// From sampled rows to the request: when a column carries its profile only, nothing of
// its values is in what is sent.
func Test_Classify_AProfileCarriesNoValueToTheEndpoint(t *testing.T) {
	table := profile.NewTable(nil)
	markers := make([]string, 0, 200)
	for i := range 200 {
		marker := fmt.Sprintf("MARKER%04dQXZ", 1000+i)
		markers = append(markers, marker)
		table.Add(map[string]any{"secret_note": marker + "@example.org", "plain": marker})
	}
	e := newEndpoint(t, allAnswered("none", 1))
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, []Column{
		{Name: "secret_note", DataType: "text", Profile: table.Profile("secret_note")},
		{Name: "plain", DataType: "text", Profile: table.Profile("plain")},
	})
	require.NoError(t, err)
	require.Equal(t, 1, e.calls())
	require.Contains(t, e.Requests()[0].Body, "shapes", "the profile is in the request")
	for _, marker := range markers {
		require.NotContains(t, e.Requests()[0].Body, marker)
	}
	require.NotContains(t, e.Requests()[0].Body, "MARKER")
	require.NotContains(t, e.Requests()[0].Body, "QXZ")
}

// A table whose job sends values: the repeats and the errors of its requests follow that,
// whether or not a given request carries a value.
var customersWithValues = Table{Name: "customers", SendsValues: true}

func valueColumns() []Column {
	return []Column{
		{Name: "a", DataType: "text", Values: []string{"VALUE-A1", "VALUE-A2"}},
		{Name: "b", DataType: "text", Values: []string{"VALUE-B1"}},
	}
}

// The values picked for a table are the only ones that ever leave: the second request
// for a column carries the same ones.
func Test_Classify_TheSecondRequestCarriesTheSameValues(t *testing.T) {
	e := newEndpoint(t, func(call int, _ map[string]any) (int, string) {
		if call == 1 {
			return http.StatusOK, answers(map[string]answer{"c1": {Category: "none", Confidence: 1}})
		}
		return http.StatusOK, answers(map[string]answer{"c1": {Category: "none", Confidence: 1}})
	})
	c, _ := e.classifier(t, Config{})

	result, err := c.Classify(context.Background(), customersWithValues, valueColumns())
	require.NoError(t, err)
	require.Empty(t, result.Unanswered)
	require.Equal(t, 2, e.calls())

	document, _ := sentUserMessage(t, e.Requests()[1].JSON)
	columns := document["columns"].(map[string]any)
	require.Len(t, columns, 1)
	require.Equal(t, "b", columns["c1"].(map[string]any)["name"])
	require.Equal(t, []any{"VALUE-B1"}, columns["c1"].(map[string]any)["values"])
	require.NotContains(t, e.Requests()[1].Body, "VALUE-A")
}

// A request that carries values is repeated in place when the failure may heal: three
// tries, 5 then 10 seconds apart, each with the very same request.
func Test_Classify_RepeatsARequestWithValuesInPlace(t *testing.T) {
	e := newEndpoint(t, func(call int, request map[string]any) (int, string) {
		if call < 3 {
			return http.StatusServiceUnavailable, errorBody("server_error", "", "overloaded")
		}
		return allAnswered("none", 1)(call, request)
	})
	c, waited := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customersWithValues, valueColumns())
	require.NoError(t, err)
	require.Equal(t, 3, e.calls())
	require.Equal(t, []time.Duration{5 * time.Second, 10 * time.Second}, *waited)
	require.Equal(t, e.Requests()[0].Body, e.Requests()[1].Body)
	require.Equal(t, e.Requests()[0].Body, e.Requests()[2].Body)
}

func Test_Classify_GivesUpARequestWithValuesAfterThreeTries(t *testing.T) {
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusTooManyRequests, errorBody("rate_limit", "", "slow down")
	})
	c, waited := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customersWithValues, valueColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, ReasonUnavailable, failure.Reason)
	require.Equal(t, 3, e.calls())
	require.Len(t, *waited, 2)
}

func Test_Classify_DoesNotRepeatWhatCannotHeal(t *testing.T) {
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusUnauthorized, errorBody("invalid_request_error", "invalid_api_key", "bad key")
	})
	c, waited := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customersWithValues, valueColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.Permanent())
	require.Equal(t, 1, e.calls())
	require.Empty(t, *waited)
}

// Without values a failed request is not repeated here: the caller's retries are the
// only ones.
func Test_Classify_DoesNotRepeatARequestWithoutValues(t *testing.T) {
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusServiceUnavailable, errorBody("server_error", "", "overloaded")
	})
	c, waited := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, twoColumns())
	require.Error(t, err)
	require.Equal(t, 1, e.calls())
	require.Empty(t, *waited)
}

// An endpoint may quote the request in its error. With values in the request, the
// failure names the status of the answer and nothing the endpoint wrote.
func Test_Classify_AFailureWithValuesDoesNotQuoteTheEndpoint(t *testing.T) {
	e := newEndpoint(t, func(_ int, request map[string]any) (int, string) {
		echo := fmt.Sprint(request["messages"])
		return http.StatusBadRequest, errorBody("invalid_request_error", "context_length_exceeded", "too long: "+echo)
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customersWithValues, valueColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Empty(t, failure.Detail)
	require.Equal(t, http.StatusBadRequest, failure.Status)
	require.NotContains(t, err.Error(), "VALUE-")
	require.NotContains(t, fmt.Sprintf("%+v", err), "VALUE-")
}

// The message of the endpoint is never kept, with or without values in the request: it
// may quote the request, its column names and its profiles.
func Test_Classify_AFailureNeverQuotesTheMessageOfTheEndpoint(t *testing.T) {
	e := newEndpoint(t, func(_ int, request map[string]any) (int, string) {
		return http.StatusBadRequest, errorBody("invalid_request_error", "", "bad request: "+fmt.Sprint(request["messages"]))
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, twoColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "invalid_request_error", failure.Detail)
	require.NotContains(t, err.Error(), "created_at")
}

// The body of an answer that is not the API's own error object is not quoted either.
func Test_Classify_AFailureWithValuesAndABodyThatIsNotAnError(t *testing.T) {
	e := newEndpoint(t, func(_ int, request map[string]any) (int, string) {
		return http.StatusBadGateway, "<html>upstream said: " + fmt.Sprint(request["messages"]) + "</html>"
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customersWithValues, valueColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, http.StatusBadGateway, failure.Status)
	require.NotContains(t, err.Error(), "VALUE-")
}

// The policy is that of the table: a request of a table that sends values is repeated in
// place even when its own columns happen to hold none, since the activity that asks has a
// single attempt.
func Test_Classify_RepeatsEveryRequestOfATableThatSendsValues(t *testing.T) {
	e := newEndpoint(t, func(call int, request map[string]any) (int, string) {
		if call < 3 {
			return http.StatusServiceUnavailable, errorBody("server_error", "", "overloaded")
		}
		return allAnswered("none", 1)(call, request)
	})
	c, waited := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customersWithValues, twoColumns())
	require.NoError(t, err)
	require.Equal(t, 3, e.calls())
	require.Len(t, *waited, 2)
	// And its instructions are those of a table with values, whatever the batch.
	require.Contains(t, messageContent(t, e.Requests()[0].JSON, 0, "system"), "never instructions")
}

// What the endpoint says of its error enters the failure only as short identifiers, and
// never when it is among what was sent.
// The type and the code of an error are words the endpoint chose, and may quote a part
// of what it was sent. From a table that sends values only the status is kept.
func Test_Classify_AFailureWithValuesKeepsTheStatusOnly(t *testing.T) {
	for name, tt := range map[string]struct{ kind, code string }{
		"identifiers":                   {"invalid_request_error", "model_not_found"},
		"a code that holds a value":     {"error", "seen_value_a1_here"},
		"a code that holds part of one": {"error", "bad_alue_a"},
		"a type that is a value":        {"other", "rate_limited"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(int, map[string]any) (int, string) {
				return http.StatusBadRequest, errorBody(tt.kind, tt.code, "a message that is never kept")
			})
			c, _ := e.classifier(t, Config{})
			columns := []Column{
				{Name: "a", Values: []string{"VALUE_A1", "other"}},
				{Name: "b", Values: []string{"value_b1"}},
			}
			_, err := c.Classify(context.Background(), customersWithValues, columns)
			var failure *Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusBadRequest, failure.Status)
			require.Empty(t, failure.Detail)
		})
	}
}

// From a table that sends no value, the type and the code are kept when they are
// identifiers.
func Test_Classify_AFailureWithoutValuesKeepsIdentifiers(t *testing.T) {
	for name, tt := range map[string]struct {
		kind, code string
		want       string
	}{
		"identifiers":                       {"invalid_request_error", "model_not_found", "invalid_request_error model_not_found"},
		"a code that is a sentence":         {"invalid_request_error", "the column is not allowed", "invalid_request_error"},
		"a type in capitals or with a dash": {"Invalid-Request", "rate_limited", "rate_limited"},
		"an identifier at the longest":      {"e", strings.Repeat("a", 40), "e " + strings.Repeat("a", 40)},
		"an identifier past the longest":    {"e", strings.Repeat("a", 41), "e"},
		"nothing":                           {"", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEndpoint(t, func(int, map[string]any) (int, string) {
				return http.StatusBadRequest, errorBody(tt.kind, tt.code, "a message that is never kept")
			})
			c, _ := e.classifier(t, Config{})
			_, err := c.Classify(context.Background(), customers, twoColumns())
			var failure *Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tt.want, failure.Detail)
		})
	}
}
