package piidetect

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// TablePiiDetectRequest is the input of the table workflow. The API decodes TableSchema
// and TableName from the history of the job workflow.
type TablePiiDetectRequest struct {
	AccountId        string
	JobId            string
	ConnectionId     string
	TableSchema      string
	TableName        string
	ShouldSampleData bool
	UserPrompt       string
	// PreviousResultsKey is the key of the report of the table in the earlier run of an
	// incremental job. The table is scanned whole: the key is carried, not read.
	PreviousResultsKey *mgmtv1alpha1.RunContextKey
	// ParentExecutionId is the id of the job run the report is stored under. Without
	// it the report is stored under the id of this run.
	ParentExecutionId *string
	// ModelInput is "values" when the job sends sample values to the model.
	ModelInput string `json:",omitempty"`
	// AnalyzerAbsent says that a table of the run already learned that the API has no
	// analyzer: the content of the columns of this one is not asked about.
	AnalyzerAbsent bool `json:",omitempty"`
}

type TablePiiDetectResponse struct {
	// PiiColumns holds, for each column in which personal data was found, what the
	// rules and the model each found.
	PiiColumns map[string]report.Combined
	ResultKey  *mgmtv1alpha1.RunContextKey
	// Model is what became of the model step: one of the statuses of report.Scan, empty
	// when the activity did not say.
	Model string `json:",omitempty"`
	// Analyzer is what became of the analyzer step: one of the statuses of report.Scan,
	// empty when the table was scanned without that step.
	Analyzer string `json:",omitempty"`
}

// TablePiiDetect scans one table: it reads its columns, asks the rules, asks the model,
// has the content of the free-text columns the rules found nothing in analyzed, and
// stores the report of the table. The activities run in that order, one after the other;
// the content activity is run only for a table that has such columns.
//
// Nothing is checked, dropped or weighed here: what the three detections return is stored
// side by side. A step added between them, or a finding filtered here, would change what
// recorded runs replay to.
func TablePiiDetect(ctx workflow.Context, req *TablePiiDetectRequest) (*TablePiiDetectResponse, error) {
	logger := log.With(
		workflow.GetLogger(ctx),
		"jobId", req.JobId,
		"tableSchema", req.TableSchema,
		"tableName", req.TableName,
	)
	logger.Info("starting PII detection")

	var activities *Activities
	var columns *GetColumnDataResponse
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, columnDataOptions()),
		activities.GetColumnData,
		&GetColumnDataRequest{
			ConnectionId: req.ConnectionId,
			TableSchema:  req.TableSchema,
			TableName:    req.TableName,
			Sample:       req.ShouldSampleData,
		},
	).Get(ctx, &columns)
	if err != nil {
		return nil, err
	}

	var byRules *DetectPiiRegexResponse
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, rulesOptions()),
		activities.DetectPiiRegex,
		&DetectPiiRegexRequest{ColumnData: columns.ColumnData},
	).Get(ctx, &byRules)
	if err != nil {
		return nil, err
	}

	// ShouldSample is never set. Values are asked for through Input, and the connection
	// is given with it and not otherwise.
	modelRequest := &DetectPiiLLMRequest{
		TableSchema: req.TableSchema,
		TableName:   req.TableName,
		ColumnData:  columns.ColumnData,
		UserPrompt:  req.UserPrompt,
	}
	sendsValues := req.ShouldSampleData && req.ModelInput == report.InputValues
	if sendsValues {
		modelRequest.Input = report.InputValues
		modelRequest.ConnectionId = req.ConnectionId
	}
	var byModel *DetectPiiLLMResponse
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, modelOptions(sendsValues)),
		activities.DetectPiiLLM,
		modelRequest,
	).Get(ctx, &byModel)
	modelStatus := ""
	if err != nil {
		// An activity that ends canceled did not fail: the run is ending.
		if temporal.IsCanceledError(err) {
			return nil, err
		}
		// What the rules found does not depend on the model. The version is only read
		// once the activity has failed, so that a run whose model answers records
		// nothing of it.
		if workflow.GetVersion(ctx, modelFailureToleratedChangeId, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
			return nil, err
		}
		logger.Error("the model could not be asked about the table", "error", err)
		byModel, modelStatus = &DetectPiiLLMResponse{}, report.ModelFailed
	} else {
		modelStatus = byModel.Status
	}

	// The content of the free-text columns the rules found nothing in is analyzed, unless
	// a table of the run already learned that the API has no analyzer. The version is
	// only read for a table that has such columns and is to be asked about, so that any
	// other run records nothing of it. A table without such columns has no step for the
	// analyzer, and no status of it, whatever its run was told.
	var byAnalyzer *DetectPiiContentResponse
	analyzerStatus := ""
	if doubtful := doubtfulColumns(columns.ColumnData, byRules); len(doubtful) > 0 {
		switch {
		case req.AnalyzerAbsent:
			analyzerStatus = report.AnalyzerNone
		case workflow.GetVersion(ctx, contentAnalysisChangeId, workflow.DefaultVersion, 1) != workflow.DefaultVersion:
			err = workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, contentOptions()),
				activities.DetectPiiContent,
				&DetectPiiContentRequest{
					ConnectionId: req.ConnectionId,
					TableSchema:  req.TableSchema,
					TableName:    req.TableName,
					Columns:      doubtful,
				},
			).Get(ctx, &byAnalyzer)
			if err != nil {
				// An activity that ends canceled did not fail: the run is ending.
				if temporal.IsCanceledError(err) {
					return nil, err
				}
				// What the rules and the model found does not depend on the analyzer.
				logger.Error("the content of the columns of the table could not be analyzed", "error", err)
				byAnalyzer, analyzerStatus = nil, report.AnalyzerFailed
			} else {
				analyzerStatus = byAnalyzer.Status
			}
		}
	}

	found := combine(byRules, byModel, byAnalyzer)
	scannedColumns := make([]string, 0, len(columns.ColumnData))
	for _, column := range columns.ColumnData {
		scannedColumns = append(scannedColumns, column.Column)
	}
	// The report says what it rests on: the rules alone when no model is configured or
	// when it could not be asked, and the analyzer when it analyzed columns.
	sources := []string{report.SourceRules}
	if modelStatus != report.ModelFailed && modelStatus != report.ModelNone {
		sources = append(sources, report.SourceModel)
	}
	if analyzerStatus == report.AnalyzerAnswered || analyzerStatus == report.AnalyzerPartial {
		sources = append(sources, report.SourceAnalyzer)
	}
	scan := &report.Scan{
		Sources:        sources,
		SampledRows:    columns.SampledRows,
		Input:          byModel.Input,
		Model:          byModel.Model,
		ModelStatus:    modelStatus,
		Unanswered:     byModel.Unanswered,
		BelowThreshold: byModel.BelowThreshold,
		AnalyzerStatus: analyzerStatus,
	}
	if byAnalyzer != nil {
		scan.NotAnalyzed = byAnalyzer.NotAnalyzed
	}
	if scan.ModelStatus == "" {
		// An activity that does not say how it went answered for every column it was
		// asked about.
		scan.ModelStatus = report.ModelAnswered
	}

	var saved *SaveTablePiiDetectReportResponse
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, saveTableReportOptions()),
		activities.SaveTablePiiDetectReport,
		&SaveTablePiiDetectReportRequest{
			ParentRunId:    req.ParentExecutionId,
			AccountId:      req.AccountId,
			TableSchema:    req.TableSchema,
			TableName:      req.TableName,
			Report:         found,
			ScannedColumns: scannedColumns,
			Scan:           scan,
		},
	).Get(ctx, &saved)
	if err != nil {
		return nil, err
	}
	return &TablePiiDetectResponse{PiiColumns: found, ResultKey: saved.Key, Model: modelStatus, Analyzer: analyzerStatus}, nil
}

// combine puts what the rules, the model and the analyzer found side by side: a column is
// there as soon as one of the three named it, with each finding as it was returned. The
// analyzer's answer is nil when the table was scanned without it.
func combine(
	byRules *DetectPiiRegexResponse,
	byModel *DetectPiiLLMResponse,
	byAnalyzer *DetectPiiContentResponse,
) map[string]report.Combined {
	found := make(map[string]report.Combined, len(byRules.PiiColumns)+len(byModel.PiiColumns))
	for column, category := range byRules.PiiColumns {
		found[column] = report.Combined{
			Regex: &report.RuleFinding{Category: category, Evidence: byRules.Evidence[column]},
		}
	}
	for column, finding := range byModel.PiiColumns {
		combined := found[column]
		combined.LLM = &report.ModelFinding{Category: finding.Category, Confidence: finding.Confidence}
		found[column] = combined
	}
	if byAnalyzer != nil {
		for column, finding := range byAnalyzer.PiiColumns {
			combined := found[column]
			combined.Analyzer = &finding
			found[column] = combined
		}
	}
	return found
}
