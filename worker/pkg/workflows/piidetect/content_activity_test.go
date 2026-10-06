package piidetect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func textColumn(name string, words float64) *ColumnData {
	return &ColumnData{Column: name, DataType: "text", Profile: &profile.Profile{Rows: 200, Kind: profile.KindText, Words: words}}
}

// The columns the analyzer is asked about are the text columns of several words that the
// rules found nothing in.
func Test_doubtfulColumns(t *testing.T) {
	nothing := &DetectPiiRegexResponse{PiiColumns: map[string]report.Category{}}
	for name, tt := range map[string]struct {
		columns []*ColumnData
		byRules *DetectPiiRegexResponse
		want    []string
	}{
		"a text of five words the rules found nothing in": {
			columns: []*ColumnData{textColumn("note", 5)}, byRules: nothing, want: []string{"note"},
		},
		"the same column with a finding of the rules": {
			columns: []*ColumnData{textColumn("note", 5)},
			byRules: &DetectPiiRegexResponse{PiiColumns: map[string]report.Category{"note": report.Personal}},
		},
		"a text of 1.2 words": {columns: []*ColumnData{textColumn("city", 1.2)}, byRules: nothing},
		"a text of exactly three words": {
			columns: []*ColumnData{textColumn("label", 3)}, byRules: nothing, want: []string{"label"},
		},
		"a text of just under three words": {columns: []*ColumnData{textColumn("label", 2.99)}, byRules: nothing},
		"a column without profile, as a job that samples nothing reads it": {
			columns: []*ColumnData{{Column: "note", DataType: "text"}}, byRules: nothing,
		},
		"a column of JSON documents": {
			columns: []*ColumnData{{Column: "payload", DataType: "jsonb", Profile: &profile.Profile{Rows: 200, Kind: profile.KindJSON, Words: 5}}},
			byRules: nothing,
		},
		"several columns, named in order": {
			columns: []*ColumnData{
				textColumn("remarks", 4), nil, textColumn("email", 6), textColumn("comment", 9), textColumn("code", 1),
			},
			byRules: &DetectPiiRegexResponse{PiiColumns: map[string]report.Category{"email": report.Contact}},
			want:    []string{"comment", "remarks"},
		},
		"no answer of the rules": {columns: []*ColumnData{textColumn("note", 5)}, want: []string{"note"}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, doubtfulColumns(tt.columns, tt.byRules))
		})
	}
}

func columnNames(count int) []string {
	names := make([]string, 0, count)
	for i := range count {
		names = append(names, fmt.Sprintf("column_%02d", i))
	}
	return names
}

// analyzed answers a request with a verdict per column and nothing found.
func analyzed(request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) *mgmtv1alpha1.DetectPiiInConnectionDataResponse {
	response := &mgmtv1alpha1.DetectPiiInConnectionDataResponse{}
	for _, column := range request.GetColumns() {
		response.Verdicts = append(response.Verdicts, &mgmtv1alpha1.ColumnPiiVerdict{
			Schema: request.GetSchema(), Table: request.GetTable(), Column: column,
		})
	}
	return response
}

func freeText(column, entity string, matches, sampled uint32) *mgmtv1alpha1.ColumnPiiDetection {
	return &mgmtv1alpha1.ColumnPiiDetection{
		Column: column, EntityType: entity, MatchCount: matches, SampledCount: sampled,
		DataCategory: "free_text_pii", IsSensitive: true, Score: 0.8,
		PiiConfidence:      mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
		PiiDetectionMethod: mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
	}
}

func contentRun(t *testing.T, api *fakeContent) *activityRun {
	t.Helper()
	return newActivityRun(t, NewActivities(nil, nil, api, nil, nil, nil, &Config{}))
}

func notesRequest(columns ...string) *DetectPiiContentRequest {
	return &DetectPiiContentRequest{ConnectionId: "connection-1", TableSchema: "public", TableName: "notes", Columns: columns}
}

// The API is asked about the named columns of the table, 50 values of each, with its own
// language and threshold; what it found in free text is kept, by column.
func Test_DetectPiiContent_KeepsWhatWasFoundInFreeText(t *testing.T) {
	api := &fakeContent{answer: func(_ int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		response := analyzed(request)
		response.Detections = []*mgmtv1alpha1.ColumnPiiDetection{
			freeText("comment", "PERSON", 7, 50),
			// A detection of another kind is not a finding in free text.
			{Column: "label", EntityType: "LOCATION", MatchCount: 30, SampledCount: 50, DataCategory: "city"},
			freeText("remarks", "", 2, 12),
		}
		return response, nil
	}}
	run := contentRun(t, api)

	response, payload, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest("comment", "label", "remarks", "body"))
	require.NoError(t, err)
	require.Equal(t, &DetectPiiContentResponse{
		PiiColumns: map[string]report.AnalyzerFinding{
			"comment": {Category: "free_text_pii", Entity: "PERSON", Matches: 7, Sampled: 50},
			"remarks": {Category: "free_text_pii", Matches: 2, Sampled: 12},
		},
		Status: report.AnalyzerAnswered,
	}, response)
	require.JSONEq(t, `{
		"PiiColumns": {
			"comment": {"category": "free_text_pii", "entity": "PERSON", "matches": 7, "sampled": 50},
			"remarks": {"category": "free_text_pii", "matches": 2, "sampled": 12}
		},
		"Status": "answered"
	}`, payload)

	sent := api.sent()
	require.Len(t, sent, 1)
	require.Equal(t, "connection-1", sent[0].GetConnectionId())
	require.Equal(t, "public", sent[0].GetSchema())
	require.Equal(t, "notes", sent[0].GetTable())
	require.Equal(t, []string{"comment", "label", "remarks", "body"}, sent[0].GetColumns())
	require.Equal(t, uint32(50), sent[0].GetSampleSize())
	require.Zero(t, sent[0].GetScoreThreshold(), "the threshold is the one of the API")
	require.Empty(t, sent[0].GetLanguage(), "the language is the one of the API")
}

// Nothing found is an empty map, as for the other detections.
func Test_DetectPiiContent_NothingFound(t *testing.T) {
	api := &fakeContent{answer: func(_ int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		return analyzed(request), nil
	}}
	run := contentRun(t, api)

	_, payload, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest("comment"))
	require.NoError(t, err)
	require.JSONEq(t, `{"PiiColumns":{},"Status":"answered"}`, payload)
}

func Test_DetectPiiContent_WithoutColumnAsksNothing(t *testing.T) {
	api := &fakeContent{answer: func(int, *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		return nil, errors.New("not to be asked")
	}}
	run := contentRun(t, api)

	_, payload, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest())
	require.NoError(t, err)
	require.JSONEq(t, `{"PiiColumns":{},"Status":"answered"}`, payload)
	require.Empty(t, api.sent())
}

// An API without analyzer answers that the scan is not configured: the activity says that
// there is none, and asks nothing more.
func Test_DetectPiiContent_WithoutAnAnalyzer(t *testing.T) {
	api := &fakeContent{answer: func(int, *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the content scan is not configured"))
	}}
	run := contentRun(t, api)

	_, payload, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(columnNames(45)...))
	require.NoError(t, err)
	require.JSONEq(t, `{"PiiColumns":{},"Status":"none"}`, payload)
	require.Len(t, api.sent(), 1, "the other columns are not asked")
}

// Columns the analyzer could not analyze are named, in order, beside what was found in
// the others.
func Test_DetectPiiContent_ColumnsNotAnalyzed(t *testing.T) {
	api := &fakeContent{answer: func(call int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		response := analyzed(request)
		if call == 1 {
			response.Detections = []*mgmtv1alpha1.ColumnPiiDetection{freeText("column_03", "PERSON", 4, 50)}
			response.Verdicts[17].ContentNotAnalyzed = true
			response.Verdicts[5].ContentNotAnalyzed = true
		} else {
			response.Verdicts[1].ContentNotAnalyzed = true
		}
		return response, nil
	}}
	run := contentRun(t, api)

	response, payload, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(columnNames(25)...))
	require.NoError(t, err)
	require.Equal(t, report.AnalyzerPartial, response.Status)
	require.Equal(t, []string{"column_05", "column_17", "column_21"}, response.NotAnalyzed)
	require.JSONEq(t, `{
		"PiiColumns": {"column_03": {"category": "free_text_pii", "entity": "PERSON", "matches": 4, "sampled": 50}},
		"NotAnalyzed": ["column_05", "column_17", "column_21"],
		"Status": "partial"
	}`, payload)
	require.Contains(t, run.logs.all(), "WARN the content of some columns was not analyzed")
}

// A call that fails is an error of the activity, which its retry policy attempts again:
// the API may answer the next time.
func Test_DetectPiiContent_AFailedCallIsAttemptedAgain(t *testing.T) {
	for name, failure := range map[string]error{
		"the API cannot answer now": connect.NewError(connect.CodeUnavailable, errors.New("upstream connect error")),
		"the call took too long":    connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded),
		// The columns of the table changed since they were read.
		"a column the table no longer has": connect.NewError(connect.CodeNotFound, errors.New(`no column "comment"`)),
		"an error that is not of the API":  errors.New("connection reset by peer"),
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakeContent{answer: func(call int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
				if call == 2 {
					return nil, failure
				}
				return analyzed(request), nil
			}}
			run := contentRun(t, api)

			_, _, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(columnNames(45)...))
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, err, &appErr)
			require.False(t, appErr.NonRetryable())
			require.ErrorContains(t, err, failure.Error())
			require.Len(t, api.sent(), 2, "the columns that follow are not asked")
			require.Contains(t, run.logs.all(), "WARN a request to analyze the content of columns failed")
		})
	}
}

// A call the API refuses to the worker is not attempted again, and the error does not
// repeat what the API said.
func Test_DetectPiiContent_ARefusedCallIsNotRetried(t *testing.T) {
	for name, code := range map[string]connect.Code{
		"permission denied": connect.CodePermissionDenied,
		"unauthenticated":   connect.CodeUnauthenticated,
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakeContent{answer: func(int, *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
				return nil, connect.NewError(code, errors.New("REFUSALMARKER at api.internal:8080"))
			}}
			run := contentRun(t, api)

			_, _, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(columnNames(45)...))
			appErr := requireNotRetried(t, err, "AnalyzerRefused")
			require.Equal(t, "the API refused to analyze the content of the columns", appErr.Message())
			require.Len(t, api.sent(), 1)
			// The activity's own log line gives the code, not what the API said. (The
			// error the SDK logs afterwards carries its cause, as for any activity.)
			logged := false
			for _, line := range strings.Split(run.logs.all(), "\n") {
				if strings.HasPrefix(line, "WARN a request to analyze the content of columns failed") {
					logged = true
					require.Contains(t, line, "code"+code.String())
					require.NotContains(t, line, "REFUSALMARKER")
				}
			}
			require.True(t, logged)
		})
	}
}

// The columns are asked 20 per call, in their order; a heartbeat follows each answer and
// counts what was asked and found so far.
func Test_DetectPiiContent_AsksInCallsOfTwentyColumns(t *testing.T) {
	api := &fakeContent{answer: func(call int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		response := analyzed(request)
		response.Detections = []*mgmtv1alpha1.ColumnPiiDetection{freeText(request.GetColumns()[0], "PERSON", 3, 50)}
		if call == 2 {
			response.Verdicts[4].ContentNotAnalyzed = true
		}
		return response, nil
	}}
	activities := NewActivities(nil, nil, api, nil, nil, nil, &Config{})
	var mu sync.Mutex
	var beats []contentProgress
	activities.heartbeat = func(_ context.Context, details ...any) {
		mu.Lock()
		defer mu.Unlock()
		beats = append(beats, details[0].(contentProgress))
	}
	run := newActivityRun(t, activities)
	names := columnNames(45)

	response, _, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(names...))
	require.NoError(t, err)
	require.Len(t, response.PiiColumns, 3)
	require.Contains(t, response.PiiColumns, "column_00")
	require.Contains(t, response.PiiColumns, "column_20")
	require.Contains(t, response.PiiColumns, "column_40")
	require.Equal(t, []string{"column_24"}, response.NotAnalyzed)

	sent := api.sent()
	require.Len(t, sent, 3)
	require.Equal(t, names[:20], sent[0].GetColumns())
	require.Equal(t, names[20:40], sent[1].GetColumns())
	require.Equal(t, names[40:], sent[2].GetColumns())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []contentProgress{
		{Calls: 1, Asked: 20, Found: 1},
		{Calls: 2, Asked: 40, Found: 2, NotAnalyzed: 1},
		{Calls: 3, Asked: 45, Found: 3, NotAnalyzed: 1},
	}, beats)
}

// While a call is in flight the activity keeps saying that it is alive: an analysis that
// takes long is not a worker that is gone.
func Test_DetectPiiContent_HeartbeatsWhileTheAPIAnswers(t *testing.T) {
	api := &fakeContent{answer: func(_ int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		time.Sleep(300 * time.Millisecond)
		return analyzed(request), nil
	}}
	activities := NewActivities(nil, nil, api, nil, nil, nil, &Config{})
	require.Less(t, activities.heartbeatEvery, contentOptions().HeartbeatTimeout/2,
		"several heartbeats fit in the time the workflow gives one")
	activities.heartbeatEvery = 20 * time.Millisecond
	var mu sync.Mutex
	var beats []contentProgress
	activities.heartbeat = func(_ context.Context, details ...any) {
		mu.Lock()
		defer mu.Unlock()
		beats = append(beats, details[0].(contentProgress))
	}
	run := newActivityRun(t, activities)

	_, _, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(columnNames(30)...))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	before, between := 0, 0
	for _, beat := range beats {
		switch beat.Calls {
		case 0:
			before++
		case 1:
			between++
			require.Equal(t, 20, beat.Asked)
		}
	}
	require.GreaterOrEqual(t, before, 5, "while the first call is answered")
	require.GreaterOrEqual(t, between, 5, "while the second call is answered")
}

// The activity is the long one of the analyzer: half an hour, a heartbeat every three
// minutes at the latest, three attempts.
func Test_contentOptions(t *testing.T) {
	options := contentOptions()
	require.Equal(t, 30*time.Minute, options.StartToCloseTimeout)
	require.Equal(t, 3*time.Minute, options.HeartbeatTimeout)
	require.Equal(t, int32(3), options.RetryPolicy.MaximumAttempts)
	require.NotEmpty(t, options.Summary)
}

// Of an answer of the API the activity keeps column names, a category, an entity type and
// counts. Every other text of the answer stays out of what is recorded of the activity:
// its result, the details of its heartbeats and its log lines.
func Test_DetectPiiContent_RecordsNoOtherTextOfTheAnswer(t *testing.T) {
	const marker = "TEXTMARKER"
	api := &fakeContent{answer: func(call int, request *mgmtv1alpha1.DetectPiiInConnectionDataRequest) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
		response := &mgmtv1alpha1.DetectPiiInConnectionDataResponse{}
		for i, column := range request.GetColumns() {
			evidence := fmt.Sprintf("%s-%d-%d", marker, call, i)
			response.Verdicts = append(response.Verdicts, &mgmtv1alpha1.ColumnPiiVerdict{
				Column: column, IsSensitive: true, DataCategory: "free_text_pii", PiiEvidence: evidence,
				ContentNotAnalyzed: i%7 == 3,
			})
			if i%2 == 0 {
				detection := freeText(column, "PERSON", 5, 50)
				detection.PiiEvidence = evidence
				response.Detections = append(response.Detections, detection)
			}
		}
		return response, nil
	}}
	run := contentRun(t, api)

	response, payload, err := execute[DetectPiiContentResponse](t, run, "DetectPiiContent", notesRequest(columnNames(45)...))
	require.NoError(t, err)
	require.Len(t, response.PiiColumns, 23)
	require.NotEmpty(t, response.NotAnalyzed)

	run.mu.Lock()
	heartbeats := strings.Join(run.heartbeats, "\n")
	run.mu.Unlock()
	require.NotEmpty(t, heartbeats)
	for name, recorded := range map[string]string{"result": payload, "heartbeats": heartbeats, "logs": run.logs.all()} {
		require.NotContains(t, recorded, marker, name)
	}

	// The result holds the members below and no other: none is a text of the answer
	// other than a column name, the category and the entity type.
	var document struct {
		PiiColumns  map[string]map[string]any
		NotAnalyzed []string
		Status      string
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&document))
	asked := columnNames(45)
	for column, finding := range document.PiiColumns {
		require.Contains(t, asked, column)
		require.Equal(t, map[string]any{"category": "free_text_pii", "entity": "PERSON", "matches": 5.0, "sampled": 50.0}, finding)
	}
	require.Subset(t, asked, document.NotAnalyzed)

	// A heartbeat holds numbers only.
	require.NotRegexp(t, `column_\d\d`, heartbeats)
}
