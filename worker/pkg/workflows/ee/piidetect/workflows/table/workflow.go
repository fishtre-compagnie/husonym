package piidetect_table_workflow

import (
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// DefaultConfidenceThreshold is the default stage-1 confidence threshold:
// columns whose stage-1 (regex/dictionary) confidence is at or above it skip the LLM stage.
const DefaultConfidenceThreshold = float32(0.8)

type Config struct {
	// ConfidenceThreshold is the stage-1 confidence threshold; values <= 0 fall back to the default.
	ConfidenceThreshold float32
}

type Workflow struct {
	confidenceThreshold float32
}

func New(config *Config) *Workflow {
	threshold := DefaultConfidenceThreshold
	if config != nil && config.ConfidenceThreshold > 0 {
		threshold = config.ConfidenceThreshold
	}
	return &Workflow{
		confidenceThreshold: threshold,
	}
}

type TablePiiDetectRequest struct {
	AccountId    string
	JobId        string
	ConnectionId string
	TableSchema  string
	TableName    string
	// ShouldSampleData is the legacy sampling toggle; used only when SamplingMode is unspecified.
	ShouldSampleData   bool
	SamplingMode       piidetect_table_activities.SamplingMode
	UserPrompt         string
	PreviousResultsKey *mgmtv1alpha1.RunContextKey // incremental mode to only detect pii for new columns
	ParentExecutionId  *string                     // present if this is running as a child workflow
}

type TablePiiDetectResponse struct {
	PiiColumns map[string]piidetect_table_activities.CombinedPiiDetectReport
	ResultKey  *mgmtv1alpha1.RunContextKey
}

func (w *Workflow) TablePiiDetect(
	ctx workflow.Context,
	req *TablePiiDetectRequest,
) (*TablePiiDetectResponse, error) {
	logger := log.With(
		workflow.GetLogger(ctx),
		"jobId", req.JobId,
		"tableSchema", req.TableSchema,
		"tableName", req.TableName,
	)

	logger.Info("starting PII detection")

	var activities *piidetect_table_activities.Activities

	var columDataResp *piidetect_table_activities.GetColumnDataResponse
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 1 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 3,
			},
		}),
		activities.GetColumnData,
		&piidetect_table_activities.GetColumnDataRequest{
			ConnectionId: req.ConnectionId,
			TableSchema:  req.TableSchema,
			TableName:    req.TableName,
		},
	).Get(ctx, &columDataResp)
	if err != nil {
		return nil, err
	}

	var regexResp *piidetect_table_activities.DetectPiiRegexResponse
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 1 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 3,
			},
		}),
		activities.DetectPiiRegex,
		&piidetect_table_activities.DetectPiiRegexRequest{
			ColumnData: columDataResp.ColumnData,
		},
	).Get(ctx, &regexResp)
	if err != nil {
		return nil, err
	}

	// Version gate: histories recorded before the dictionary stage existed ran
	// regex → LLM (all columns, unconditionally). Replaying them against the new
	// command sequence would be non-deterministic.
	version := workflow.GetVersion(ctx, "stage1-dictionary-cascade", workflow.DefaultVersion, 1)

	dictResp := &piidetect_table_activities.DetectPiiDictionaryResponse{
		PiiColumns: map[string]piidetect_table_activities.DictionaryMatch{},
	}
	if version >= 1 {
		err = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 1 * time.Minute,
				RetryPolicy: &temporal.RetryPolicy{
					MaximumAttempts: 3,
				},
			}),
			activities.DetectPiiDictionary,
			&piidetect_table_activities.DetectPiiDictionaryRequest{
				ColumnData: columDataResp.ColumnData,
			},
		).Get(ctx, &dictResp)
		if err != nil {
			return nil, err
		}
	}

	// Cascade: only columns whose stage-1 confidence is below the threshold reach the LLM.
	llmCandidates := columDataResp.ColumnData
	if version >= 1 {
		llmCandidates = getLLMCandidateColumns(
			columDataResp.ColumnData,
			regexResp,
			dictResp,
			w.confidenceThreshold,
		)
	}

	llmResp := &piidetect_table_activities.DetectPiiLLMResponse{
		PiiColumns: map[string]piidetect_table_activities.LLMPiiDetectReport{},
	}
	if version == workflow.DefaultVersion || len(llmCandidates) > 0 {
		err = workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 1 * time.Minute,
				RetryPolicy: &temporal.RetryPolicy{
					MaximumAttempts: 3,
				},
			}),
			activities.DetectPiiLLM,
			&piidetect_table_activities.DetectPiiLLMRequest{
				TableSchema:  req.TableSchema,
				TableName:    req.TableName,
				ColumnData:   llmCandidates,
				ShouldSample: req.ShouldSampleData,
				SamplingMode: req.SamplingMode,
				ConnectionId: req.ConnectionId,
				UserPrompt:   req.UserPrompt,
			},
		).Get(ctx, &llmResp)
		if err != nil {
			return nil, err
		}
	} else {
		logger.Debug("all columns resolved by stage 1, skipping LLM PII detection")
	}

	logger.Debug("PII detection complete")

	report := buildFinalReport(regexResp, dictResp, llmResp)

	scannedColumns := make([]string, 0, len(columDataResp.ColumnData))
	for _, col := range columDataResp.ColumnData {
		scannedColumns = append(scannedColumns, col.Column)
	}

	var saveResp *piidetect_table_activities.SaveTablePiiDetectReportResponse
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 1 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 3,
			},
		}),
		activities.SaveTablePiiDetectReport,
		&piidetect_table_activities.SaveTablePiiDetectReportRequest{
			ParentRunId:    req.ParentExecutionId,
			AccountId:      req.AccountId,
			TableSchema:    req.TableSchema,
			TableName:      req.TableName,
			Report:         report,
			ScannedColumns: scannedColumns,
		},
	).Get(ctx, &saveResp)
	if err != nil {
		return nil, err
	}

	return &TablePiiDetectResponse{
		PiiColumns: report,
		ResultKey:  saveResp.Key,
	}, nil
}

// getLLMCandidateColumns returns the columns whose stage-1 (regex/dictionary)
// confidence is below the threshold; only those are sent to the LLM stage.
func getLLMCandidateColumns(
	columnData []*piidetect_table_activities.ColumnData,
	regexResp *piidetect_table_activities.DetectPiiRegexResponse,
	dictResp *piidetect_table_activities.DetectPiiDictionaryResponse,
	confidenceThreshold float32,
) []*piidetect_table_activities.ColumnData {
	candidates := []*piidetect_table_activities.ColumnData{}
	for _, col := range columnData {
		confidence := float32(0)
		if _, ok := regexResp.PiiColumns[col.Column]; ok {
			confidence = piidetect_table_activities.RegexMatchConfidence
		}
		if _, ok := dictResp.PiiColumns[col.Column]; ok &&
			piidetect_table_activities.DictionaryMatchConfidence > confidence {
			confidence = piidetect_table_activities.DictionaryMatchConfidence
		}
		if confidence < confidenceThreshold {
			candidates = append(candidates, col)
		}
	}
	return candidates
}

func buildFinalReport(
	regexResp *piidetect_table_activities.DetectPiiRegexResponse,
	dictResp *piidetect_table_activities.DetectPiiDictionaryResponse,
	llmResp *piidetect_table_activities.DetectPiiLLMResponse,
) map[string]piidetect_table_activities.CombinedPiiDetectReport {
	reportByColumn := make(map[string]piidetect_table_activities.CombinedPiiDetectReport)

	for col, category := range regexResp.PiiColumns {
		reportByColumn[col] = piidetect_table_activities.CombinedPiiDetectReport{
			Regex: &piidetect_table_activities.RegexPiiDetectReport{
				Category: category,
			},
		}
	}

	for col, match := range dictResp.PiiColumns {
		existingReport := reportByColumn[col]
		existingReport.Dictionary = &piidetect_table_activities.DictionaryPiiDetectReport{
			Category:   match.Category,
			Token:      match.Token,
			Confidence: piidetect_table_activities.DictionaryMatchConfidence,
		}
		reportByColumn[col] = existingReport
	}

	for col, report := range llmResp.PiiColumns {
		existingReport := reportByColumn[col]
		existingReport.LLM = &report
		reportByColumn[col] = existingReport
	}

	return reportByColumn
}
