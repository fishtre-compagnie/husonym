package piidetect

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

const contentVersion = "pii-detect-content-analysis-1"

// freeTextColumns are the columns of a table that holds two columns of free text, "note"
// and "comment", next to columns that are not.
func freeTextColumns() *GetColumnDataResponse {
	text := func(words float64) *profile.Profile {
		return &profile.Profile{Rows: 200, Kind: profile.KindText, Words: words}
	}
	return &GetColumnDataResponse{
		ColumnData: []*ColumnData{
			{Column: "id", DataType: "uuid"},
			{Column: "email", DataType: "text", IsNullable: true, Profile: text(1)},
			{Column: "note", DataType: "text", IsNullable: true, Profile: text(12)},
			{Column: "comment", DataType: "text", IsNullable: true, Profile: text(8.5)},
			{Column: "address", DataType: "text", IsNullable: true, Profile: text(6)},
		},
		SampledRows: 200,
	}
}

// contentScan is a run of a table whose rules find "email" by its name and "address" by
// its name, and whose model finds "email": "note" and "comment" are the free-text columns
// the rules found nothing in.
type contentScan struct {
	env        *testsuite.TestWorkflowEnvironment
	activities *Activities
	versions   *versionReads
	// byModel is what the model activity answers.
	byModel *DetectPiiLLMResponse

	mu       sync.Mutex
	order    []string
	started  map[string]activity.Info
	asked    atomic.Int32
	requests []*DetectPiiContentRequest
	saved    *SaveTablePiiDetectReportRequest
}

func newContentScan(t *testing.T, columns *GetColumnDataResponse) *contentScan {
	t.Helper()
	scan := &contentScan{started: map[string]activity.Info{}}
	scan.env, scan.activities = newTableEnv(t)
	scan.versions = watchVersions(scan.env)

	scan.env.OnActivity(scan.activities.GetColumnData, mock.Anything, mock.Anything).Return(columns, nil).Once()
	scan.env.OnActivity(scan.activities.DetectPiiRegex, mock.Anything, mock.Anything).
		Return(&DetectPiiRegexResponse{
			PiiColumns: map[string]report.Category{"email": report.Contact, "address": report.Location},
			Evidence:   map[string]string{"email": "name", "address": "name"},
		}, nil).Once()
	scan.byModel = &DetectPiiLLMResponse{
		PiiColumns: map[string]report.ModelFinding{"email": {Category: report.Contact, Confidence: 0.9}},
		Input:      report.InputProfiles, Status: report.ModelAnswered, Model: "local-model",
	}
	scan.env.OnActivity(scan.activities.DetectPiiLLM, mock.Anything, mock.Anything).
		Return(func(context.Context, *DetectPiiLLMRequest) (*DetectPiiLLMResponse, error) {
			return scan.byModel, nil
		}).Once()
	scan.env.OnActivity(scan.activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *SaveTablePiiDetectReportRequest) (*SaveTablePiiDetectReportResponse, error) {
			scan.mu.Lock()
			defer scan.mu.Unlock()
			scan.saved = req
			return &SaveTablePiiDetectReportResponse{Key: tableKey}, nil
		})
	scan.env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		scan.mu.Lock()
		defer scan.mu.Unlock()
		if info.Attempt == 1 {
			scan.order = append(scan.order, info.ActivityType.Name)
		}
		scan.started[info.ActivityType.Name] = *info
	})
	return scan
}

// analyzerAnswers says what the content activity returns each time it is attempted.
func (s *contentScan) analyzerAnswers(answer func() (*DetectPiiContentResponse, error)) {
	s.env.OnActivity(s.activities.DetectPiiContent, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *DetectPiiContentRequest) (*DetectPiiContentResponse, error) {
			s.asked.Add(1)
			s.mu.Lock()
			s.requests = append(s.requests, req)
			s.mu.Unlock()
			return answer()
		})
}

func (s *contentScan) run(t *testing.T, request *TablePiiDetectRequest) *TablePiiDetectResponse {
	t.Helper()
	s.env.ExecuteWorkflow(TablePiiDetect, request)
	require.True(t, s.env.IsWorkflowCompleted())
	require.NoError(t, s.env.GetWorkflowError())
	var response *TablePiiDetectResponse
	require.NoError(t, s.env.GetWorkflowResult(&response))
	return response
}

var (
	rulesAndModel = map[string]report.Combined{
		"email": {
			Regex: &report.RuleFinding{Category: report.Contact, Evidence: "name"},
			LLM:   &report.ModelFinding{Category: report.Contact, Confidence: 0.9},
		},
		"address": {Regex: &report.RuleFinding{Category: report.Location, Evidence: "name"}},
	}
	noteFinding = report.AnalyzerFinding{Category: "free_text_pii", Entity: "PERSON", Matches: 7, Sampled: 50}
)

// A table with free-text columns the rules found nothing in: the content activity is run
// once, between the model and the save, for those columns alone, and what it found is
// stored next to what the rules and the model found.
func Test_TablePiiDetect_AnalyzesTheFreeTextColumns(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{
			PiiColumns: map[string]report.AnalyzerFinding{"note": noteFinding},
			Status:     report.AnalyzerAnswered,
		}, nil
	})

	response := scan.run(t, tableRequest())

	require.Equal(t,
		[]string{"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "DetectPiiContent", "SaveTablePiiDetectReport"},
		scan.order,
	)
	require.EqualValues(t, 1, scan.asked.Load())
	require.Equal(t, []*DetectPiiContentRequest{{
		ConnectionId: "connection-1", TableSchema: "public", TableName: "customers",
		Columns: []string{"comment", "note"},
	}}, scan.requests)
	require.Equal(t, 30*time.Minute, scan.started["DetectPiiContent"].StartToCloseTimeout)
	require.Equal(t, 3*time.Minute, scan.started["DetectPiiContent"].HeartbeatTimeout)

	want := map[string]report.Combined{
		"email":   rulesAndModel["email"],
		"address": rulesAndModel["address"],
		"note":    {Analyzer: &noteFinding},
	}
	require.Equal(t, want, scan.saved.Report)
	require.Equal(t, &report.Scan{
		SampledRows: 200, Input: report.InputProfiles, Model: "local-model", ModelStatus: report.ModelAnswered,
		Sources:        []string{report.SourceRules, report.SourceModel, report.SourceAnalyzer},
		AnalyzerStatus: report.AnalyzerAnswered,
	}, scan.saved.Scan)
	require.Equal(t, want, response.PiiColumns)
	require.Equal(t, report.AnalyzerAnswered, response.Analyzer)
	require.Equal(t, []string{contentVersion}, scan.versions.all())
}

// A column that the model named and in which the analyzer found something holds both
// findings.
func Test_TablePiiDetect_AColumnFoundByTheModelAndByTheAnalyzer(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.byModel = &DetectPiiLLMResponse{
		PiiColumns: map[string]report.ModelFinding{"note": {Category: report.Personal, Confidence: 0.7}},
		Status:     report.ModelAnswered,
	}
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{
			PiiColumns: map[string]report.AnalyzerFinding{"note": noteFinding},
			Status:     report.AnalyzerAnswered,
		}, nil
	})

	scan.run(t, tableRequest())

	require.Equal(t, report.Combined{
		LLM:      &report.ModelFinding{Category: report.Personal, Confidence: 0.7},
		Analyzer: &noteFinding,
	}, scan.saved.Report["note"])
}

// Columns the analyzer could not analyze are named in the report, which still rests on
// the analyzer for the others.
func Test_TablePiiDetect_SomeColumnsAreNotAnalyzed(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{
			PiiColumns:  map[string]report.AnalyzerFinding{"note": noteFinding},
			NotAnalyzed: []string{"comment"},
			Status:      report.AnalyzerPartial,
		}, nil
	})

	response := scan.run(t, tableRequest())

	require.Equal(t, report.AnalyzerPartial, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, []string{"comment"}, scan.saved.Scan.NotAnalyzed)
	require.Equal(t, []string{report.SourceRules, report.SourceModel, report.SourceAnalyzer}, scan.saved.Scan.Sources)
	require.Equal(t, &noteFinding, scan.saved.Report["note"].Analyzer)
	require.Equal(t, report.AnalyzerPartial, response.Analyzer)
}

// An API without analyzer: the report says so, and does not rest on the analyzer.
func Test_TablePiiDetect_TheAPIHasNoAnalyzer(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{PiiColumns: map[string]report.AnalyzerFinding{}, Status: report.AnalyzerNone}, nil
	})

	response := scan.run(t, tableRequest())

	require.Equal(t, rulesAndModel, scan.saved.Report)
	require.Equal(t, report.AnalyzerNone, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, []string{report.SourceRules, report.SourceModel}, scan.saved.Scan.Sources)
	require.Equal(t, report.AnalyzerNone, response.Analyzer)
	require.Equal(t, []string{contentVersion}, scan.versions.all())
}

// A table without a free-text column the rules found nothing in has no step for the
// analyzer: no activity, no version, and a report and a result that say nothing of it.
func Test_TablePiiDetect_WithoutAFreeTextColumn(t *testing.T) {
	columns := freeTextColumns()
	columns.ColumnData = columns.ColumnData[:2] // "id" and "email"
	scan := newContentScan(t, columns)
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{Status: report.AnalyzerAnswered}, nil
	})

	scan.run(t, tableRequest())

	require.Zero(t, scan.asked.Load())
	require.Equal(t, []string{"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport"}, scan.order)
	require.Empty(t, scan.versions.all())
	require.Empty(t, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, []string{report.SourceRules, report.SourceModel}, scan.saved.Scan.Sources)
	require.NotContains(t, resultJSON(t, scan.env), "Analyzer")
}

// A table of a run that already learned that the API has no analyzer: no activity and no
// version, and a report that says there is no analyzer.
func Test_TablePiiDetect_ToldThatTheAnalyzerIsAbsent(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{Status: report.AnalyzerAnswered}, nil
	})
	request := tableRequest()
	request.AnalyzerAbsent = true

	response := scan.run(t, request)

	require.Zero(t, scan.asked.Load())
	require.Equal(t, []string{"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport"}, scan.order)
	require.Empty(t, scan.versions.all())
	require.Equal(t, rulesAndModel, scan.saved.Report)
	require.Equal(t, report.AnalyzerNone, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, []string{report.SourceRules, report.SourceModel}, scan.saved.Scan.Sources)
	require.Equal(t, report.AnalyzerNone, response.Analyzer)
}

// The status of the analyzer step is a property of the table: one that has no free-text
// column has no such step, whatever its run was told of the analyzer.
func Test_TablePiiDetect_ToldThatTheAnalyzerIsAbsentWithoutAFreeTextColumn(t *testing.T) {
	columns := freeTextColumns()
	columns.ColumnData = columns.ColumnData[:2] // "id" and "email"
	scan := newContentScan(t, columns)
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{Status: report.AnalyzerAnswered}, nil
	})
	request := tableRequest()
	request.AnalyzerAbsent = true

	response := scan.run(t, request)

	require.Zero(t, scan.asked.Load())
	require.Equal(t, []string{"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport"}, scan.order)
	require.Empty(t, scan.versions.all())
	require.Empty(t, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, []string{report.SourceRules, report.SourceModel}, scan.saved.Scan.Sources)
	require.Empty(t, response.Analyzer)
	require.NotContains(t, resultJSON(t, scan.env), "Analyzer")
}

// An analyzer that cannot be asked does not cost the table what the rules and the model
// found: the report is saved and says so.
func Test_TablePiiDetect_AFailedAnalyzerIsTolerated(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return nil, errors.New("the content of the columns could not be analyzed")
	})

	response := scan.run(t, tableRequest())

	require.EqualValues(t, 3, scan.asked.Load(), "the analyzer is attempted three times")
	require.Equal(t, rulesAndModel, scan.saved.Report)
	require.Equal(t, report.AnalyzerFailed, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, report.ModelAnswered, scan.saved.Scan.ModelStatus)
	require.Equal(t, []string{report.SourceRules, report.SourceModel}, scan.saved.Scan.Sources)
	require.Empty(t, scan.saved.Scan.NotAnalyzed)
	require.Equal(t, rulesAndModel, response.PiiColumns)
	require.Equal(t, report.AnalyzerFailed, response.Analyzer)
	require.Equal(t, report.ModelAnswered, response.Model)
	require.Equal(t, []string{contentVersion}, scan.versions.all())
}

// A run canceled while the content is analyzed ends canceled, and saves nothing.
func Test_TablePiiDetect_CanceledWhileTheContentIsAnalyzed(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		scan.env.CancelWorkflow()
		return nil, errors.New("interrupted")
	})

	scan.env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, scan.env.IsWorkflowCompleted())
	require.True(t, temporal.IsCanceledError(scan.env.GetWorkflowError()), "%v", scan.env.GetWorkflowError())
	require.Nil(t, scan.saved)
}

// A content activity that ends canceled is not a failure of the analyzer to tolerate.
func Test_TablePiiDetect_AContentActivityThatEndsCanceled(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return nil, temporal.NewCanceledError("the worker is stopping")
	})

	scan.env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, scan.env.IsWorkflowCompleted())
	require.True(t, temporal.IsCanceledError(scan.env.GetWorkflowError()), "%v", scan.env.GetWorkflowError())
	require.Nil(t, scan.saved)
}

// Runs that passed the model before the content was analyzed replay as they ran: the
// report is saved right after the model, and says nothing of the analyzer.
func Test_TablePiiDetect_ARunThatStartedBeforeTheContentWasAnalyzed(t *testing.T) {
	scan := newContentScan(t, freeTextColumns())
	scan.env.OnGetVersion("pii-detect-content-analysis", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	scan.analyzerAnswers(func() (*DetectPiiContentResponse, error) {
		return &DetectPiiContentResponse{Status: report.AnalyzerAnswered}, nil
	})

	scan.run(t, tableRequest())

	require.Zero(t, scan.asked.Load())
	require.Equal(t, []string{"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport"}, scan.order)
	require.Equal(t, rulesAndModel, scan.saved.Report)
	require.Empty(t, scan.saved.Scan.AnalyzerStatus)
	require.Equal(t, []string{report.SourceRules, report.SourceModel}, scan.saved.Scan.Sources)
	require.NotContains(t, resultJSON(t, scan.env), "Analyzer")
}
