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
	require.Contains(t, e.bodies[0], "shapes", "the profile is in the request")
	for _, marker := range markers {
		require.NotContains(t, e.bodies[0], marker)
	}
	require.NotContains(t, e.bodies[0], "MARKER")
	require.NotContains(t, e.bodies[0], "QXZ")
}

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

	result, err := c.Classify(context.Background(), customers, valueColumns())
	require.NoError(t, err)
	require.Empty(t, result.Unanswered)
	require.Equal(t, 2, e.calls())

	document, _ := sentUserMessage(t, e.requests[1])
	columns := document["columns"].(map[string]any)
	require.Len(t, columns, 1)
	require.Equal(t, "b", columns["c1"].(map[string]any)["name"])
	require.Equal(t, []any{"VALUE-B1"}, columns["c1"].(map[string]any)["values"])
	require.NotContains(t, e.bodies[1], "VALUE-A")
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

	_, err := c.Classify(context.Background(), customers, valueColumns())
	require.NoError(t, err)
	require.Equal(t, 3, e.calls())
	require.Equal(t, []time.Duration{5 * time.Second, 10 * time.Second}, *waited)
	require.Equal(t, e.bodies[0], e.bodies[1])
	require.Equal(t, e.bodies[0], e.bodies[2])
}

func Test_Classify_GivesUpARequestWithValuesAfterThreeTries(t *testing.T) {
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusTooManyRequests, errorBody("rate_limit", "", "slow down")
	})
	c, waited := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, valueColumns())
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

	_, err := c.Classify(context.Background(), customers, valueColumns())
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

// An endpoint may quote the request in its error message. With values in the request,
// the failure names the status, the type and the code of the error, never its message.
func Test_Classify_AFailureWithValuesDoesNotQuoteTheEndpoint(t *testing.T) {
	e := newEndpoint(t, func(_ int, request map[string]any) (int, string) {
		echo := fmt.Sprint(request["messages"])
		return http.StatusBadRequest, errorBody("invalid_request_error", "context_length_exceeded", "too long: "+echo)
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, valueColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "invalid_request_error context_length_exceeded", failure.Detail)
	require.NotContains(t, err.Error(), "VALUE-")
	require.NotContains(t, fmt.Sprintf("%+v", err), "VALUE-")
}

// Without values the message of the endpoint is kept, cut to 300 characters.
func Test_Classify_AFailureWithoutValuesQuotesTheEndpointInShort(t *testing.T) {
	e := newEndpoint(t, func(int, map[string]any) (int, string) {
		return http.StatusBadRequest, errorBody("invalid_request_error", "", strings.Repeat("é", 1000))
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, twoColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, strings.Repeat("é", 300), failure.Detail)
}

// The body of an answer that is not the API's own error object is not quoted either.
func Test_Classify_AFailureWithValuesAndABodyThatIsNotAnError(t *testing.T) {
	e := newEndpoint(t, func(_ int, request map[string]any) (int, string) {
		return http.StatusBadGateway, "<html>upstream said: " + fmt.Sprint(request["messages"]) + "</html>"
	})
	c, _ := e.classifier(t, Config{})

	_, err := c.Classify(context.Background(), customers, valueColumns())
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, http.StatusBadGateway, failure.Status)
	require.NotContains(t, err.Error(), "VALUE-")
}
