package piidetect

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

// toldAbsent says, by table, whether the run of the table was told that the API has no
// analyzer.
func (r *jobRun) toldAbsent() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	told := map[string]bool{}
	for _, table := range r.tables {
		told[table.TableName] = table.AnalyzerAbsent
	}
	return told
}

// A table that learns that the API has no analyzer spares the tables started after it the
// question. Nothing is missing from the reports of such a run: it ends well, and reads no
// version of its own for it.
func Test_JobPiiDetect_ATableWithoutAnalyzerTellsTheTablesStartedAfterIt(t *testing.T) {
	run := newJobRun(t, 1)
	versions := watchVersions(run.env)
	run.withDetails(plainDetails())
	run.withTables("t1", "t2", "t3")
	run.scanTablesTo(func(*TablePiiDetectRequest) (steps, error) {
		return steps{model: report.ModelAnswered, analyzer: report.AnalyzerNone}, nil
	})
	saved := run.savesReport()

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	require.NoError(t, run.env.GetWorkflowError())
	require.Equal(t, map[string]bool{"t1": false, "t2": true, "t3": true}, run.toldAbsent())
	requireReport(t, &report.JobReport{
		SuccessfulTableReports: []*report.TableEntry{entry("t1"), entry("t2"), entry("t3")},
	}, *saved)
	require.Equal(t, []string{createdKind, succeededKind}, run.started())
	require.Equal(t, []string{"license-read-recorded-1"}, versions.all())
}

// The tables that are being scanned when another learns that there is no analyzer were
// started without knowing it; those started later are told.
func Test_JobPiiDetect_OnlyTheTablesStartedLaterAreToldThatTheAnalyzerIsAbsent(t *testing.T) {
	run := newJobRun(t, 2)
	run.withDetails(plainDetails())
	run.withTables("t1", "t2", "t3", "t4")
	run.scanTablesTo(func(*TablePiiDetectRequest) (steps, error) {
		return steps{analyzer: report.AnalyzerNone}, nil
	})
	run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.Equal(t, 2, run.maxRunning)
	require.Equal(t, map[string]bool{"t1": false, "t2": false, "t3": true, "t4": true}, run.toldAbsent())
}

// A run in which no table says that the analyzer is absent tells no table so.
func Test_JobPiiDetect_TablesWhoseAnalyzerAnswersAreToldNothing(t *testing.T) {
	run := newJobRun(t, 1)
	run.withDetails(plainDetails())
	run.withTables("t1", "t2")
	run.scanTablesTo(func(*TablePiiDetectRequest) (steps, error) {
		return steps{analyzer: report.AnalyzerAnswered}, nil
	})
	saved := run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.Equal(t, map[string]bool{"t1": false, "t2": false}, run.toldAbsent())
	requireReport(t, &report.JobReport{SuccessfulTableReports: []*report.TableEntry{entry("t1"), entry("t2")}}, *saved)
}

// A table whose analyzer could not be asked, or did not analyze every column, is marked
// in the index. The run saves the index, then ends failed on a message that names the
// table and what did not answer.
func Test_JobPiiDetect_ATableTheAnalyzerDidNotAnswerForFailsTheRunOnceTheIndexIsSaved(t *testing.T) {
	run := newJobRun(t, 3)
	versions := watchVersions(run.env)
	run.withDetails(plainDetails())
	run.withTables("t1", "silent_analyzer", "partly_analyzed", "silent_both", "silent_model")
	run.scanTablesTo(func(req *TablePiiDetectRequest) (steps, error) {
		switch req.TableName {
		case "silent_analyzer":
			return steps{model: report.ModelAnswered, analyzer: report.AnalyzerFailed}, nil
		case "partly_analyzed":
			return steps{model: report.ModelNone, analyzer: report.AnalyzerPartial}, nil
		case "silent_both":
			return steps{model: report.ModelFailed, analyzer: report.AnalyzerFailed}, nil
		case "silent_model":
			return steps{model: report.ModelFailed, analyzer: report.AnalyzerAnswered}, nil
		}
		return steps{model: report.ModelAnswered, analyzer: report.AnalyzerAnswered}, nil
	})
	saved := run.savesReport()

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, run.env.GetWorkflowError(), &appErr)
	require.Equal(t, "ScanIncomplete", appErr.Type())
	require.Equal(t,
		"4 of 5 tables were not fully scanned: public.partly_analyzed (the analyzer did not answer), "+
			"public.silent_analyzer (the analyzer did not answer), "+
			"public.silent_both (the model and the analyzer did not answer), "+
			"public.silent_model (the model did not answer)",
		appErr.Message(),
	)

	incomplete := func(table string) *report.TableEntry {
		marked := entry(table)
		marked.Incomplete = true
		return marked
	}
	requireReport(t, &report.JobReport{SuccessfulTableReports: []*report.TableEntry{
		incomplete("partly_analyzed"), incomplete("silent_analyzer"), incomplete("silent_both"), incomplete("silent_model"),
		entry("t1"),
	}}, *saved)
	require.Equal(t, []string{createdKind, failedKind}, run.started())
	require.Equal(t, []string{"license-read-recorded-1", "pii-detect-incomplete-run-fails-1"}, versions.all())
}
