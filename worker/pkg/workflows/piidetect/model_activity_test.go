package piidetect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

// modelEndpoint is a server that speaks the chat completion API. It keeps the bodies it
// was sent, and answers each request with what answer returns for it.
type modelEndpoint struct {
	server *httptest.Server
	mu     sync.Mutex
	bodies []string
}

// asked is what a request asks about: the columns of its document, by id.
type asked struct {
	call    int
	columns map[string]map[string]any
	body    string
}

func (a asked) ids() []string {
	ids := make([]string, 0, len(a.columns))
	for i := 1; i <= len(a.columns); i++ {
		ids = append(ids, fmt.Sprintf("c%d", i))
	}
	return ids
}

func newModelEndpoint(t *testing.T, answer func(request asked) (status int, body string)) (*modelEndpoint, *model.Classifier) {
	t.Helper()
	e := &modelEndpoint{}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &request)
		var document struct {
			Columns map[string]map[string]any `json:"columns"`
		}
		if len(request.Messages) == 2 {
			_ = json.NewDecoder(strings.NewReader(request.Messages[1].Content)).Decode(&document)
		}

		e.mu.Lock()
		e.bodies = append(e.bodies, string(body))
		call := len(e.bodies)
		e.mu.Unlock()

		status, answer := answer(asked{call: call, columns: document.Columns, body: string(body)})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(e.server.Close)

	classifier, err := model.NewClassifier(model.Config{BaseURL: e.server.URL + "/v1", Model: "local-model", MinConfidence: 0.5})
	require.NoError(t, err)
	return e, classifier
}

func (e *modelEndpoint) requests() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.bodies...)
}

// completionOf is the body of an answer that gives each id a category and a confidence.
func completionOf(byId map[string][2]any) string {
	content := map[string]any{}
	for id, answer := range byId {
		content[id] = map[string]any{"category": answer[0], "confidence": answer[1]}
	}
	encoded, _ := json.Marshal(content)
	body, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion", "model": "local-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": string(encoded)},
		}},
	})
	return string(body)
}

// everyColumn answers the same for every column of a request.
func everyColumn(category string, confidence float64) func(asked) (int, string) {
	return func(request asked) (int, string) {
		byId := map[string][2]any{}
		for _, id := range request.ids() {
			byId[id] = [2]any{category, confidence}
		}
		return http.StatusOK, completionOf(byId)
	}
}

func manyColumns(count int) []*ColumnData {
	columns := make([]*ColumnData, 0, count)
	for i := range count {
		columns = append(columns, &ColumnData{Column: fmt.Sprintf("column_%02d", i), DataType: "text"})
	}
	return columns
}

// Without a configured model nothing is asked, and the answer says so.
func Test_DetectPiiLLM_WithoutAModel(t *testing.T) {
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, nil, Config{}))

	_, payload, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users", ColumnData: manyColumns(3),
		ShouldSample: true, ConnectionId: "connection-1", Input: "values",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"PiiColumns":{},"Status":"none"}`, payload)
}

func Test_DetectPiiLLM_ATableWithoutColumnAsksNothing(t *testing.T) {
	endpoint, classifier := newModelEndpoint(t, everyColumn("contact", 0.9))
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))

	_, payload, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{TableSchema: "public", TableName: "empty"})
	require.NoError(t, err)
	require.JSONEq(t, `{"PiiColumns":{},"Status":"answered","Model":"local-model"}`, payload)
	require.Empty(t, endpoint.requests())
}

// The columns are asked a batch per request, in their order; a heartbeat follows each
// answer, with what was learned so far.
func Test_DetectPiiLLM_AsksInBatches(t *testing.T) {
	endpoint, classifier := newModelEndpoint(t, func(request asked) (int, string) {
		byId := map[string][2]any{}
		for _, id := range request.ids() {
			byId[id] = [2]any{"none", 0.9}
		}
		switch request.call {
		case 1:
			byId["c1"] = [2]any{"contact", 0.9}
		case 2:
			byId["c2"] = [2]any{"personal", 0.3}
		case 3:
			byId["c10"] = [2]any{"location", 0.7}
		}
		return http.StatusOK, completionOf(byId)
	})
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))

	response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "wide", ColumnData: manyColumns(60),
		// ShouldSample is not read: no row is read for it.
		ShouldSample: true, ConnectionId: "connection-1",
	})
	require.NoError(t, err)
	require.Len(t, endpoint.requests(), 3)
	require.Equal(t, &DetectPiiLLMResponse{
		PiiColumns: map[string]report.ModelFinding{
			"column_00": {Category: report.Contact, Confidence: 0.9},
			"column_59": {Category: report.Location, Confidence: 0.7},
		},
		Input: "names", Status: "answered", Model: "local-model",
		BelowThreshold: []report.Dismissed{{ColumnName: "column_26", Category: report.Personal, Confidence: 0.3}},
	}, response)

	run.mu.Lock()
	defer run.mu.Unlock()
	// The environment reports the first heartbeat at once and holds the next ones back,
	// as a worker does between two reports to its server.
	require.NotEmpty(t, run.heartbeats)
	require.Contains(t, run.heartbeats[0], "Batches:1")
	require.Contains(t, run.heartbeats[0], "column_00:map[category:contact confidence:0.9]")
}

// What the model was given: names when no row was sampled, profiles when rows were.
func Test_DetectPiiLLM_Input(t *testing.T) {
	endpoint, classifier := newModelEndpoint(t, everyColumn("none", 1))
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))

	response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users",
		ColumnData: []*ColumnData{
			{Column: "id", DataType: "uuid"},
			{Column: "email", DataType: "text", IsNullable: true, Profile: &profile.Profile{Rows: 200, Kind: profile.KindText}},
		},
		UserPrompt: "Columns named ref_* hold customer references.",
	})
	require.NoError(t, err)
	require.Equal(t, "profiles", response.Input)
	require.Len(t, endpoint.requests(), 1)
	body := endpoint.requests()[0]
	require.Contains(t, body, `\"sample\":{\"rows\":200,\"kind\":\"text\"}`)
	require.Contains(t, body, "Columns named ref_* hold customer references.")
	require.Contains(t, body, `\"table\":\"users\"`)
}

// A column without a valid answer is asked once more. An answer for an id the request
// does not hold is ignored, and the worker says so.
func Test_DetectPiiLLM_AColumnAnsweredAtTheSecondRequest(t *testing.T) {
	_, classifier := newModelEndpoint(t, func(request asked) (int, string) {
		return http.StatusOK, completionOf(map[string][2]any{"c1": {"contact", 0.9}, "c2": {"something else", 0.9}})
	})
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))

	response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users", ColumnData: manyColumns(2),
	})
	require.NoError(t, err)
	// The second request asks about the one column that had no answer, under the id c1:
	// the endpoint answers it there, and for a c2 that the request does not hold.
	require.Equal(t, "answered", response.Status)
	require.Empty(t, response.Unanswered)
	require.Len(t, response.PiiColumns, 2)
	require.Contains(t, run.logs.all(), "WARN the model answered for columns it was not asked about: these answers are ignored")
}

func Test_DetectPiiLLM_AColumnThatStaysWithoutAnswer(t *testing.T) {
	_, classifier := newModelEndpoint(t, func(request asked) (int, string) {
		if request.call == 1 {
			return http.StatusOK, completionOf(map[string][2]any{"c1": {"contact", 0.9}})
		}
		return http.StatusOK, completionOf(map[string][2]any{"c1": {"contact", 12}})
	})
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))

	response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users", ColumnData: manyColumns(2),
	})
	require.NoError(t, err)
	require.Equal(t, "partial", response.Status)
	require.Equal(t, []string{"column_01"}, response.Unanswered)
	require.Len(t, response.PiiColumns, 1)
	require.Contains(t, run.logs.all(), "WARN the model gave no valid answer for some columns")
}

// An attempt that follows another starts from what the heartbeats of the other recorded:
// it asks only the batches that are missing.
func Test_DetectPiiLLM_ASecondAttemptAsksOnlyTheMissingBatches(t *testing.T) {
	endpoint, classifier := newModelEndpoint(t, func(request asked) (int, string) {
		if request.call == 2 {
			return http.StatusServiceUnavailable, `{"error":{"type":"server_error","message":"overloaded"}}`
		}
		return everyColumn("contact", 0.9)(request)
	})
	request := &DetectPiiLLMRequest{TableSchema: "public", TableName: "wide", ColumnData: manyColumns(60)}

	first := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))
	_, _, err := execute[DetectPiiLLMResponse](t, first, "DetectPiiLLM", request)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.False(t, appErr.NonRetryable(), "an endpoint that cannot answer now may answer at the next attempt")
	require.Len(t, endpoint.requests(), 2)
	require.Len(t, first.heartbeats, 1)

	second := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))
	recorded := modelProgress{Batches: 1, Findings: map[string]report.ModelFinding{}}
	for i := range 25 {
		recorded.Findings[fmt.Sprintf("column_%02d", i)] = report.ModelFinding{Category: report.Contact, Confidence: 0.9}
	}
	second.env.SetHeartbeatDetails(recorded)
	response, _, err := execute[DetectPiiLLMResponse](t, second, "DetectPiiLLM", request)
	require.NoError(t, err)
	require.Len(t, endpoint.requests(), 4, "two more requests: the second and the third batch")
	require.Len(t, response.PiiColumns, 60)
	require.Contains(t, endpoint.requests()[2], "column_25")
	require.NotContains(t, endpoint.requests()[2], "column_24")
}

// A request the endpoint refuses is not attempted again.
func Test_DetectPiiLLM_ARefusedRequestIsNotRetried(t *testing.T) {
	endpoint, classifier := newModelEndpoint(t, func(asked) (int, string) {
		return http.StatusUnauthorized, `{"error":{"type":"invalid_request_error","code":"invalid_api_key","message":"bad key"}}`
	})
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))

	_, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users", ColumnData: manyColumns(2),
	})
	appErr := requireNotRetried(t, err, "ModelRejected")
	require.Equal(t, "the model could not be asked", appErr.Message())
	require.ErrorContains(t, err, "HTTP 401")
	require.Len(t, endpoint.requests(), 1)
	require.Contains(t, run.logs.all(), "WARN a request to the model failed")
	require.Contains(t, run.logs.all(), "status401")
}

// A user prompt longer than what the model is given is cut, and the worker says so
// without quoting it.
func Test_DetectPiiLLM_ALongUserPromptIsCut(t *testing.T) {
	endpoint, classifier := newModelEndpoint(t, everyColumn("none", 1))
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, classifier, Config{}))
	prompt := strings.Repeat("PROMPTMARKER ", 400)

	_, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users", ColumnData: manyColumns(1), UserPrompt: prompt,
	})
	require.NoError(t, err)
	require.Contains(t, run.logs.all(), "WARN the user prompt of the job is longer than what the model is given: it is cut")
	require.NotContains(t, run.logs.all(), "PROMPTMARKER")
	require.Equal(t, 2000/len("PROMPTMARKER "), strings.Count(endpoint.requests()[0], "PROMPTMARKER"))
}

func valueRequest() *DetectPiiLLMRequest {
	return &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "users",
		ColumnData: []*ColumnData{
			{Column: "id", DataType: "uuid", Profile: &profile.Profile{Rows: 200}},
			{Column: "c17", DataType: "text", Profile: &profile.Profile{Rows: 200}},
			{Column: "note", DataType: "text", Profile: &profile.Profile{Rows: 200}},
			{Column: "photo", DataType: "bytea", Profile: &profile.Profile{Rows: 200}},
		},
		ConnectionId: "connection-1",
		Input:        "values",
	}
}

func valueSource(t *testing.T, classifier *model.Classifier) (*activityRun, *connectiondata.MockConnectionDataService) {
	t.Helper()
	builder, data := source(t)
	return newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, classifier, Config{})), data
}

// sentValues returns the values each column of a request carries, by column name.
func sentValues(request asked) map[string][]string {
	values := map[string][]string{}
	for _, column := range request.columns {
		name, _ := column["name"].(string)
		list, _ := column["values"].([]any)
		for _, value := range list {
			values[name] = append(values[name], value.(string))
		}
	}
	return values
}

// With the values input the activity reads rows by itself and gives each column a few
// values: at most 5, distinct, not null, not blank, cut to 64 characters, never from a
// binary column. Nothing of them is in what the activity returns, logs or records.
func Test_DetectPiiLLM_SendsBoundedValues(t *testing.T) {
	var requests []asked
	var mu sync.Mutex
	_, classifier := newModelEndpoint(t, func(request asked) (int, string) {
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		return everyColumn("personal", 0.9)(request)
	})
	run, data := valueSource(t, classifier)

	long := "LONGMARKER " + strings.Repeat("é", 100)
	rows := []map[string]any{
		{"id": "IDMARKER-1", "c17": nil, "note": "   ", "photo": &husonymtypes.Binary{Bytes: []byte("PHOTOMARKER-1")}},
		{"id": "IDMARKER-1", "c17": "  IBANMARKER-1  ", "note": long, "photo": &husonymtypes.Binary{Bytes: []byte("PHOTOMARKER-2")}},
		{"id": "IDMARKER-2", "c17": "IBANMARKER-1", "note": long + " and a tail that is cut away", "photo": nil},
	}
	for i := 3; i <= 9; i++ {
		rows = append(rows, map[string]any{
			"id": fmt.Sprintf("IDMARKER-%d", i), "c17": int64(1000 + i), "note": "", "photo": []byte{0xff, 0xfe},
		})
	}
	sends(data, rows, nil)

	response, payload, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", valueRequest())
	require.NoError(t, err)
	require.Equal(t, "values", response.Input)
	require.Len(t, response.PiiColumns, 4)

	require.Len(t, requests, 1)
	values := sentValues(requests[0])
	require.Equal(t, []string{"IDMARKER-1", "IDMARKER-2", "IDMARKER-3", "IDMARKER-4", "IDMARKER-5"}, values["id"],
		"the first five distinct values, in the order the rows came")
	require.Equal(t, []string{"IBANMARKER-1", "1003", "1004", "1005", "1006"}, values["c17"], "trimmed, and a number as it prints")
	require.Len(t, values["note"], 1, "two values that are the same once cut are one; a blank one is none")
	require.Equal(t, 64, utf8.RuneCountInString(values["note"][0]))
	require.True(t, strings.HasSuffix(values["note"][0], "…"))
	require.True(t, strings.HasPrefix(values["note"][0], "LONGMARKER ééé"))
	require.NotContains(t, values, "photo", "a binary value is never sent")
	require.NotContains(t, requests[0].body, "PHOTOMARKER")

	for _, marker := range []string{"IDMARKER", "IBANMARKER", "LONGMARKER", "PHOTOMARKER"} {
		require.NotContains(t, payload, marker)
		require.NotContains(t, run.logs.all(), marker)
		for _, heartbeat := range run.heartbeats {
			require.NotContains(t, heartbeat, marker)
		}
	}
}

// Rows are read for the values input only: when the request names it and gives the
// connection.
func Test_DetectPiiLLM_ReadsRowsOnlyForTheValuesInput(t *testing.T) {
	for name, change := range map[string]func(*DetectPiiLLMRequest){
		"ShouldSample, without the input": func(req *DetectPiiLLMRequest) { req.Input, req.ShouldSample = "", true },
		"the input without a connection":  func(req *DetectPiiLLMRequest) { req.ConnectionId = "" },
		"another input":                   func(req *DetectPiiLLMRequest) { req.Input = "profiles" },
	} {
		t.Run(name, func(t *testing.T) {
			_, classifier := newModelEndpoint(t, everyColumn("none", 1))
			builder := connectiondata.NewMockConnectionDataBuilder(t) // never asked
			run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, classifier, Config{}))
			request := valueRequest()
			change(request)

			response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", request)
			require.NoError(t, err)
			require.Equal(t, "profiles", response.Input)
		})
	}
}

// When no value can be shown the model is asked with what the request carries, and the
// answer says what it was given.
func Test_DetectPiiLLM_FallsBackWhenNoValueCanBeSent(t *testing.T) {
	for name, tt := range map[string]struct {
		rows    []map[string]any
		readErr error
		logged  string
	}{
		"an empty table": {nil, nil, "the table holds no value that may be shown"},
		"a table of nulls and binary values": {
			[]map[string]any{{"id": nil, "c17": "", "note": nil, "photo": &husonymtypes.Binary{Bytes: []byte("PHOTOMARKER")}}},
			nil, "the table holds no value that may be shown",
		},
		"a table that cannot be read": {
			[]map[string]any{{"id": "IDMARKER-1"}}, errors.New("unable to convert row: IDMARKER-2"),
			"the rows of the table could not be read",
		},
	} {
		t.Run(name, func(t *testing.T) {
			endpoint, classifier := newModelEndpoint(t, everyColumn("none", 1))
			run, data := valueSource(t, classifier)
			sends(data, tt.rows, tt.readErr)

			response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", valueRequest())
			require.NoError(t, err)
			require.Equal(t, "profiles", response.Input)
			require.Len(t, endpoint.requests(), 1)
			require.NotContains(t, endpoint.requests()[0], `\"values\"`)
			require.NotContains(t, endpoint.requests()[0], "MARKER")
			require.Contains(t, run.logs.all(), "WARN "+tt.logged+": the model is asked without sample values")
			require.NotContains(t, run.logs.all(), "MARKER")
		})
	}

	t.Run("and without profile either, the model is given names", func(t *testing.T) {
		_, classifier := newModelEndpoint(t, everyColumn("none", 1))
		run, data := valueSource(t, classifier)
		sends(data, nil, nil)
		request := valueRequest()
		for _, column := range request.ColumnData {
			column.Profile = nil
		}
		response, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", request)
		require.NoError(t, err)
		require.Equal(t, "names", response.Input)
	})
}

// An endpoint may quote the request in its error. With values in the request, the
// failure of the activity names the status, the type and the code of the error only.
func Test_DetectPiiLLM_AFailureWithValuesQuotesNoValue(t *testing.T) {
	_, classifier := newModelEndpoint(t, func(request asked) (int, string) {
		body, _ := json.Marshal(map[string]any{"error": map[string]any{
			"type": "invalid_request_error", "code": "context_length_exceeded", "message": "too long: " + request.body,
		}})
		return http.StatusBadRequest, string(body)
	})
	run, data := valueSource(t, classifier)
	sends(data, []map[string]any{{"id": "IDMARKER-1", "c17": "IBANMARKER-1", "note": "NOTEMARKER-1"}}, nil)

	_, _, err := execute[DetectPiiLLMResponse](t, run, "DetectPiiLLM", valueRequest())
	requireNotRetried(t, err, "ModelRejected")
	require.ErrorContains(t, err, "invalid_request_error context_length_exceeded")
	require.NotContains(t, fmt.Sprintf("%v %+v", err, err), "MARKER")
	require.NotContains(t, run.logs.all(), "MARKER")
}
