package piidetect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func usersCatalogue() []*mgmtv1alpha1.DatabaseColumn {
	notNull := column("public", "users", "id", "uuid")
	notNull.IsNullable = "NO"
	return []*mgmtv1alpha1.DatabaseColumn{
		notNull,
		column("public", "users", "c17", "text"),
		column("public", "users", "note", "text"),
		column("public", "users", "photo", "bytea"),
	}
}

func columnRead(t *testing.T) (*activityRun, *connectiondata.MockConnectionDataService) {
	t.Helper()
	builder, data := source(t)
	data.EXPECT().GetTableSchema(mock.Anything, "public", "users").Return(usersCatalogue(), nil).Maybe()
	return newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{})), data
}

func usersRequest(sample bool) *GetColumnDataRequest {
	return &GetColumnDataRequest{ConnectionId: "connection-1", TableSchema: "public", TableName: "users", Sample: sample}
}

// Without sampling the columns come from the catalogue and no row is read.
func Test_GetColumnData_WithoutSampling(t *testing.T) {
	run, _ := columnRead(t) // a sampling would be a call the source does not expect

	response, payload, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(false))
	require.NoError(t, err)
	require.JSONEq(t, `{"ColumnData":[
		{"Column":"id","DataType":"uuid","IsNullable":false,"Comment":null},
		{"Column":"c17","DataType":"text","IsNullable":true,"Comment":null},
		{"Column":"note","DataType":"text","IsNullable":true,"Comment":null},
		{"Column":"photo","DataType":"bytea","IsNullable":true,"Comment":null}
	]}`, payload)
	require.Zero(t, response.SampledRows)
}

// The rows of these tests: every value is a marker, so that it can be looked for in what
// leaves the activity.
func markedRows(count int) (rows []map[string]any, markers []string) {
	for i := range count {
		iban := []string{"FR7630006000011234567890189", "FR1420041010050500013M02606", "FR8810278073000002056360189"}[i%3]
		note := fmt.Sprintf("MARKER%04dQXZ wrote to the support", 1000+i)
		photo := fmt.Sprintf("PHOTOBYTES%04d", 1000+i)
		rows = append(rows, map[string]any{
			"id":    fmt.Sprintf("IDMARKER%04d", 1000+i),
			"c17":   iban,
			"note":  note,
			"photo": &husonymtypes.Binary{Bytes: []byte(photo)},
		})
		markers = append(markers, iban, fmt.Sprintf("MARKER%04dQXZ", 1000+i), photo, fmt.Sprintf("IDMARKER%04d", 1000+i))
	}
	return rows, markers
}

// With sampling, 200 rows are asked for and each column gets the profile of its values.
// Nothing of a value is in what the activity returns or logs.
func Test_GetColumnData_ProfilesTheSampledRows(t *testing.T) {
	run, data := columnRead(t)
	rows, markers := markedRows(200)
	sends(data, rows, nil)

	response, payload, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
	require.NoError(t, err)
	require.Equal(t, 200, response.SampledRows)
	require.Len(t, response.ColumnData, 4)

	c17 := response.ColumnData[1]
	require.Equal(t, "c17", c17.Column)
	require.Equal(t, 200, c17.Profile.Rows)
	require.Equal(t, 3, c17.Profile.Distinct)
	require.Equal(t, []profile.Share{{Name: "iban", Share: 1}}, c17.Profile.Hits)
	require.Equal(t, profile.KindBinary, response.ColumnData[3].Profile.Kind)
	require.NotEmpty(t, response.ColumnData[2].Profile.Shapes)

	for _, marker := range markers {
		require.NotContains(t, payload, marker)
		require.NotContains(t, run.logs.all(), marker)
	}
	for _, part := range []string{"MARKER", "QXZ", "PHOTOBYTES", "FR76", "support"} {
		require.NotContains(t, payload, part)
		require.NotContains(t, run.logs.all(), part)
	}
}

// An empty table has columns and no profile: it is scanned on their names and types.
func Test_GetColumnData_AnEmptyTable(t *testing.T) {
	run, data := columnRead(t)
	sends(data, nil, nil)

	response, payload, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
	require.NoError(t, err)
	require.Len(t, response.ColumnData, 4)
	require.Zero(t, response.SampledRows)
	require.NotContains(t, payload, "Profile")
}

// A sample that cannot be read is not a table that cannot be scanned. What the failure
// says is not logged: the error of a row may quote the row.
func Test_GetColumnData_ASamplingThatFails(t *testing.T) {
	run, data := columnRead(t)
	rows, _ := markedRows(3)
	sends(data, rows, errors.New("unable to convert row to map: MARKER-IN-THE-ERROR"))

	response, payload, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
	require.NoError(t, err)
	require.Len(t, response.ColumnData, 4)
	require.Zero(t, response.SampledRows)
	require.NotContains(t, payload, "Profile")
	require.Contains(t, run.logs.all(), "WARN the rows of the table could not be sampled")
	require.NotContains(t, run.logs.all(), "MARKER")
}

// The reading of the rows is bounded: past its time the table is scanned without them.
func Test_GetColumnData_ASamplingThatLastsTooLong(t *testing.T) {
	builder, data := source(t)
	data.EXPECT().GetTableSchema(mock.Anything, "public", "users").Return(usersCatalogue(), nil)
	data.EXPECT().SampleData(mock.Anything, mock.Anything, "public", "users", uint(200)).
		RunAndReturn(func(ctx context.Context, _ connectiondata.SampleDataStream, _, _ string, _ uint) error {
			<-ctx.Done()
			return ctx.Err()
		})
	activities := NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{})
	require.Equal(t, 30*time.Second, activities.samplingTimeout)
	activities.samplingTimeout = 20 * time.Millisecond
	run := newActivityRun(t, activities)

	response, _, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
	require.NoError(t, err)
	require.Len(t, response.ColumnData, 4)
	require.Zero(t, response.SampledRows)
	require.Nil(t, response.ColumnData[1].Profile)
	require.Contains(t, run.logs.all(), "timedOuttrue")
}

// A row that cannot be decoded is left out, and named by its position only.
func Test_GetColumnData_ARowThatCannotBeDecoded(t *testing.T) {
	run, data := columnRead(t)
	data.EXPECT().SampleData(mock.Anything, mock.Anything, "public", "users", uint(200)).
		RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _ string, _ uint) error {
			return stream.Send(&mgmtv1alpha1.GetConnectionDataStreamResponse{RowBytes: []byte("MARKER not a row")})
		})

	response, _, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
	require.NoError(t, err)
	require.Zero(t, response.SampledRows)
	require.Contains(t, run.logs.all(), "WARN sampled rows could not be decoded and were left out")
	require.Contains(t, run.logs.all(), "positions[1]")
	require.NotContains(t, run.logs.all(), "MARKER")
}

// A very wide table has profiles without shapes, so that its payload stays small.
func Test_GetColumnData_AVeryWideTable(t *testing.T) {
	for count, withShapes := range map[int]bool{1000: true, 1001: false} {
		catalogue := make([]*mgmtv1alpha1.DatabaseColumn, 0, count)
		row := map[string]any{}
		for i := range count {
			name := fmt.Sprintf("c%d", i)
			catalogue = append(catalogue, column("public", "users", name, "text"))
			row[name] = "some text"
		}
		builder, data := source(t)
		data.EXPECT().GetTableSchema(mock.Anything, "public", "users").Return(catalogue, nil)
		sends(data, []map[string]any{row, row, row}, nil)
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))

		response, _, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
		require.NoError(t, err)
		require.Equal(t, withShapes, len(response.ColumnData[0].Profile.Shapes) > 0, count)
		require.NotNil(t, response.ColumnData[0].Profile.Len, count)
	}
}

func Test_GetColumnData_Failures(t *testing.T) {
	t.Run("a connection whose tables cannot be scanned", func(t *testing.T) {
		builder := connectiondata.NewMockConnectionDataBuilder(t) // never asked
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		request := usersRequest(true)
		request.ConnectionId = "connection-mongo"
		_, _, err := execute[GetColumnDataResponse](t, run, "GetColumnData", request)
		requireNotRetried(t, err, "UnsupportedSource")
	})

	t.Run("a reader that cannot read columns", func(t *testing.T) {
		builder, data := source(t)
		data.EXPECT().GetTableSchema(mock.Anything, "public", "users").Return(nil, errors.ErrUnsupported)
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		_, _, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(false))
		requireNotRetried(t, err, "UnsupportedSource")
	})

	t.Run("columns that cannot be read fail the activity, which is retried", func(t *testing.T) {
		builder, data := source(t)
		data.EXPECT().GetTableSchema(mock.Anything, "public", "users").Return(nil, errors.New("connection refused"))
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		_, _, err := execute[GetColumnDataResponse](t, run, "GetColumnData", usersRequest(true))
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, err, &appErr)
		require.False(t, appErr.NonRetryable())
		require.ErrorContains(t, err, "the columns of the table cannot be read")
	})
}

// The rules read the name, the type and the profile of each column.
func Test_DetectPiiRegex(t *testing.T) {
	run := newActivityRun(t, NewActivities(nil, nil, nil, nil, nil, &Config{}))

	response, payload, err := execute[DetectPiiRegexResponse](t, run, "DetectPiiRegex", &DetectPiiRegexRequest{ColumnData: []*ColumnData{
		{Column: "id", DataType: "uuid"},
		{Column: "email", DataType: "text"},
		{Column: "telephone", DataType: "text"},
		{Column: "c17", DataType: "text", Profile: &profile.Profile{Rows: 200, Hits: []profile.Share{{Name: "iban", Share: 0.97}}}},
		{Column: "c18", DataType: "text", Profile: &profile.Profile{Rows: 200, Hits: []profile.Share{{Name: "iban", Share: 0.2}}}},
		nil,
	}})
	require.NoError(t, err)
	require.Equal(t, map[string]report.Category{
		"email": report.Contact, "telephone": report.Contact, "c17": report.Financial,
	}, response.PiiColumns)
	require.JSONEq(t, `{
		"PiiColumns": {"email": "contact", "telephone": "contact", "c17": "financial"},
		"Evidence": {"email": "name", "telephone": "name", "c17": "values:iban 0.97"}
	}`, payload)

	_, payload, err = execute[DetectPiiRegexResponse](t, run, "DetectPiiRegex", &DetectPiiRegexRequest{ColumnData: []*ColumnData{
		{Column: "id", DataType: "uuid"},
	}})
	require.NoError(t, err)
	require.JSONEq(t, `{"PiiColumns":{}}`, payload, "nothing found is an empty map, as recorded runs hold it")

	// A request that carries no profile, as a workflow that samples nothing sends it.
	response, _, err = execute[DetectPiiRegexResponse](t, run, "DetectPiiRegex", &DetectPiiRegexRequest{})
	require.NoError(t, err)
	require.Empty(t, response.PiiColumns)
}

func Test_SaveTablePiiDetectReport(t *testing.T) {
	jobs := &fakeJobs{}
	run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{}))
	parent := "run-1"

	// With every member.
	response, _, err := execute[SaveTablePiiDetectReportResponse](t, run, "SaveTablePiiDetectReport", &SaveTablePiiDetectReportRequest{
		ParentRunId: &parent, AccountId: "account-1", TableSchema: "public", TableName: "users",
		Report: map[string]report.Combined{
			"note":  {LLM: &report.ModelFinding{Category: report.Personal, Confidence: 0.6}},
			"email": {Regex: &report.RuleFinding{Category: report.Contact, Evidence: "name"}, LLM: &report.ModelFinding{Category: report.Contact, Confidence: 0.98}},
			"c17":   {Regex: &report.RuleFinding{Category: report.Financial, Evidence: "values:iban 0.97"}},
		},
		ScannedColumns: []string{"id", "email", "c17", "note"},
		Scan:           &report.Scan{SampledRows: 200, Input: "profiles", Model: "local-model", ModelStatus: "partial", Unanswered: []string{"id"}},
	})
	require.NoError(t, err)
	require.Equal(t, "account-1", response.Key.GetAccountId())
	require.Equal(t, "run-1", response.Key.GetJobRunId())
	require.Equal(t, "public.users--table-pii-report", response.Key.GetExternalId())
	// The columns are stored in the order of their names.
	require.JSONEq(t, `{
		"table_schema": "public", "table_name": "users",
		"column_reports": [
			{"column_name": "c17", "report": {"regex": {"category": "financial", "evidence": "values:iban 0.97"}, "llm": null}},
			{"column_name": "email", "report": {"regex": {"category": "contact", "evidence": "name"}, "llm": {"category": "contact", "confidence": 0.98}}},
			{"column_name": "note", "report": {"regex": null, "llm": {"category": "personal", "confidence": 0.6}}}
		],
		"scanned_columns": ["id", "email", "c17", "note"],
		"scan": {"sampled_rows": 200, "input": "profiles", "model": "local-model", "model_status": "partial", "unanswered": ["id"]}
	}`, string(jobs.contexts[contextKey(response.Key)]))
	require.True(t, strings.Index(string(jobs.contexts[contextKey(response.Key)]), `"c17"`) < strings.Index(string(jobs.contexts[contextKey(response.Key)]), `"email"`))

	// With none of the members a request may lack, as a workflow that knows none sends
	// it: the stored report holds the members every reader knows.
	response, _, err = execute[SaveTablePiiDetectReportResponse](t, run, "SaveTablePiiDetectReport", &SaveTablePiiDetectReportRequest{
		AccountId: "account-1", TableSchema: "public", TableName: "empty",
		Report: map[string]report.Combined{},
	})
	require.NoError(t, err)
	require.Equal(t, testRunId, response.Key.GetJobRunId(), "without a parent, the report is stored under the run of the table")
	require.JSONEq(t,
		`{"table_schema":"public","table_name":"empty","column_reports":[]}`,
		string(jobs.contexts[contextKey(response.Key)]),
	)
}

func Test_SaveTablePiiDetectReport_FailsWhenTheReportCannotBeStored(t *testing.T) {
	jobs := &fakeJobs{setErr: connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))}
	run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{}))
	_, _, err := execute[SaveTablePiiDetectReportResponse](t, run, "SaveTablePiiDetectReport", &SaveTablePiiDetectReportRequest{
		AccountId: "account-1", TableSchema: "public", TableName: "users",
	})
	require.ErrorContains(t, err, "unable to set run context")
}
