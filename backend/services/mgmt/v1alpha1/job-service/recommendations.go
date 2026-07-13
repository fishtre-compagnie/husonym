package v1alpha1_jobservice

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	v1alpha1_transformersservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/transformers-service"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	ee_recommendations "github.com/fishtre-compagnie/husonym/internal/ee/recommendations"
	nucleuserrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
)

// confidence assigned to regex (column name) detections; the regex detector
// does not emit a confidence itself, but a name match is a strong signal.
const regexDetectionConfidence float32 = 0.9

// GetJobMappingRecommendations builds transformer recommendations for the
// columns of a connection, based on the most recent pii detection report
// available for that connection (plans/assistant-ia-config-anonymisation.md §4.2).
// When no report exists, it returns an empty recommendations list with
// report_run_id unset so the frontend can offer to launch a scan.
func (s *Service) GetJobMappingRecommendations(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobMappingRecommendationsRequest],
) (*connect.Response[mgmtv1alpha1.GetJobMappingRecommendationsResponse], error) {
	if !s.cfg.IsJobMappingRecommendationsEnabled {
		return nil, nucleuserrors.NewNotImplemented(
			"job mapping recommendations are not enabled: requires a valid license",
		)
	}

	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With(
		"accountId", req.Msg.GetAccountId(),
		"connectionId", req.Msg.GetConnectionId(),
	)

	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceJob(ctx, userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()), rbac.JobAction_View); err != nil {
		return nil, err
	}
	if err := s.verifyConnectionInAccount(ctx, req.Msg.GetConnectionId(), req.Msg.GetAccountId()); err != nil {
		return nil, err
	}

	reportRunId, tableReports, err := s.getLatestPiiReportForConnection(
		ctx,
		req.Msg.GetAccountId(),
		req.Msg.GetConnectionId(),
		logger,
	)
	if err != nil {
		return nil, err
	}
	if reportRunId == nil {
		logger.Debug("no pii detection report found for connection")
		return connect.NewResponse(&mgmtv1alpha1.GetJobMappingRecommendationsResponse{
			Recommendations: []*mgmtv1alpha1.TransformerRecommendation{},
		}), nil
	}

	colInfoMap, err := s.getConnectionSchemaColumnMap(ctx, req.Msg.GetConnectionId(), logger)
	if err != nil {
		return nil, err
	}

	catalog := v1alpha1_transformersservice.GetSystemTransformerCatalog(true)

	recommendations := []*mgmtv1alpha1.TransformerRecommendation{}
	proposalBudget := ee_recommendations.MaxProposalsPerRequest
	for _, tableReport := range tableReports {
		tableCols := colInfoMap[sqlmanager_shared.BuildTable(tableReport.TableSchema, tableReport.TableName)]
		for idx := range tableReport.ColumnReports {
			columnReport := tableReport.ColumnReports[idx]
			recommendation, genericFallback, category, dataType := buildColumnRecommendation(
				tableReport.TableSchema,
				tableReport.TableName,
				&columnReport,
				tableCols[columnReport.ColumnName],
				catalog,
			)
			if genericFallback && proposalBudget > 0 && s.codegenClient != nil {
				// The budget bounds LLM attempts, not successes: a flaky model
				// must not turn one request into unbounded completion calls.
				proposalBudget--
				if proposal, ok := ee_recommendations.GenerateProposal(
					ctx,
					s.codegenClient,
					s.codegenModel,
					v1alpha1_transformersservice.IsUserJavascriptCodeValid,
					ee_recommendations.ProposalRequest{
						ColumnName:       columnReport.ColumnName,
						Category:         category,
						DataType:         dataType,
						GenericRationale: recommendation.Evidence[len(recommendation.Evidence)-1].GetDetail(),
					},
				); ok {
					recommendation.Proposal = &mgmtv1alpha1.NewTransformerProposal{
						Name:           proposal.Name,
						Description:    proposal.Description,
						JavascriptCode: proposal.JavascriptCode,
						Rationale:      proposal.Rationale,
					}
				} else {
					logger.Debug(fmt.Sprintf(
						"dropped AI transformer proposal for %s.%s.%s: LLM call failed or generated code failed validation",
						tableReport.TableSchema, tableReport.TableName, columnReport.ColumnName,
					))
				}
			}
			recommendations = append(recommendations, recommendation)
		}
	}

	logger.Debug(fmt.Sprintf("built %d transformer recommendations", len(recommendations)))

	return connect.NewResponse(&mgmtv1alpha1.GetJobMappingRecommendationsResponse{
		Recommendations: recommendations,
		ReportRunId:     reportRunId,
	}), nil
}

// getLatestPiiReportForConnection locates piidetect jobs whose source is the
// given connection, then walks their runs from newest to oldest and returns
// the first run that has table reports available. Returns a nil run id when
// no report exists.
func (s *Service) getLatestPiiReportForConnection(
	ctx context.Context,
	accountId string,
	connectionId string,
	logger *slog.Logger,
) (*string, []*piidetect_table_activities.TableReport, error) {
	jobsResp, err := s.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{
		AccountId: accountId,
	}))
	if err != nil {
		return nil, nil, err
	}

	piiDetectJobIds := []string{}
	for _, job := range jobsResp.Msg.GetJobs() {
		if job.GetJobType().GetPiiDetect() == nil {
			continue
		}
		sourceConnectionId, err := getJobSourceConnectionId(job.GetSource())
		if err != nil {
			logger.Warn(fmt.Sprintf("unable to resolve source connection for piidetect job %s: %v", job.GetId(), err))
			continue
		}
		if sourceConnectionId != nil && *sourceConnectionId == connectionId {
			piiDetectJobIds = append(piiDetectJobIds, job.GetId())
		}
	}
	if len(piiDetectJobIds) == 0 {
		return nil, nil, nil
	}

	workflows, err := s.temporalmgr.GetWorkflowExecutionsByScheduleIds(ctx, accountId, piiDetectJobIds, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to retrieve piidetect job runs: %w", err)
	}

	runs := make([]*mgmtv1alpha1.JobRun, 0, len(workflows))
	for _, workflow := range workflows {
		run := dtomaps.ToJobRunDtoFromWorkflowExecutionInfo(workflow, logger)
		// completed runs have a full job-level report; running ones may have
		// partial table-level reports which are still useful to stream in.
		if run.GetStatus() == mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE ||
			run.GetStatus() == mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING {
			runs = append(runs, run)
		}
	}
	// newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].GetStartedAt().AsTime().After(runs[j].GetStartedAt().AsTime())
	})

	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return nil, nil, err
	}

	for _, run := range runs {
		reports, err := s.getTablePiiReportsForRun(ctx, run, accountUuid, logger)
		if err != nil {
			return nil, nil, err
		}
		if len(reports) > 0 {
			return gotypeutil.ToPtr(run.GetId()), reports, nil
		}
	}
	return nil, nil, nil
}

// getConnectionSchemaColumnMap returns the column metadata for a SQL
// connection keyed by "schema.table" then column name. Non-SQL connections
// (mongo, dynamodb, s3, ...) return an empty map: recommendations then fall
// back to name/category information only.
func (s *Service) getConnectionSchemaColumnMap(
	ctx context.Context,
	connectionId string,
	logger *slog.Logger,
) (map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow, error) {
	connResp, err := s.connectionService.GetConnection(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{
		Id: connectionId,
	}))
	if err != nil {
		return nil, err
	}
	connection := connResp.Msg.GetConnection()
	if !isConnectionSQLType(connection) {
		return map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{}, nil
	}

	db, err := s.sqlmanager.NewSqlConnection(ctx, connectionmanager.NewUniqueSession(), connection, logger)
	if err != nil {
		return nil, err
	}
	defer db.Db().Close()

	colInfoMap, err := db.Db().GetSchemaColumnMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve schema column map: %w", err)
	}
	return colInfoMap, nil
}

// buildColumnRecommendation returns the recommendation itself, along with
// whether the deterministic mapping fell back to a generic transformer (a
// candidate for an AI transformer proposal, see attachTransformerProposals-
// style logic in GetJobMappingRecommendations), and the category/data type
// used to build it (reused as-is when drafting a proposal).
func buildColumnRecommendation(
	schema, table string,
	columnReport *piidetect_table_activities.ColumnReport,
	columnInfo *sqlmanager_shared.DatabaseSchemaRow,
	catalog []*mgmtv1alpha1.SystemTransformer,
) (recommendation *mgmtv1alpha1.TransformerRecommendation, isGenericFallback bool, category ee_recommendations.Category, dataType mgmtv1alpha1.TransformerDataType) {
	category, confidence, evidence := deriveCategoryAndEvidence(columnReport)

	dataType = mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED
	isGenerated := false
	isIdentity := false
	if columnInfo != nil {
		dataType = ee_recommendations.SqlDataTypeToTransformerDataType(columnInfo.DataType)
		isGenerated = columnInfo.GeneratedType != nil && *columnInfo.GeneratedType != ""
		isIdentity = columnInfo.IdentityGeneration != nil && *columnInfo.IdentityGeneration != ""
	}

	rawRecommendation := ee_recommendations.Recommend(category, columnReport.ColumnName, dataType)
	validatedConfig := ee_recommendations.FilterForValidity(
		catalog,
		rawRecommendation.Config,
		dataType,
		isGenerated,
		isIdentity,
	)

	// surface the mapping rule's rationale as a dedicated evidence entry so
	// the reviewing user understands why this transformer was suggested.
	evidence = append(evidence, &mgmtv1alpha1.DetectionEvidence{
		Kind:       mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_DICTIONARY,
		Detail:     rawRecommendation.Rationale,
		Confidence: confidence,
	})

	// Generated/identity columns are never candidates for an AI transformer
	// proposal: their value is never up to the anonymization pipeline in the
	// first place, so a custom transformer would be moot.
	genericFallback := rawRecommendation.IsGenericFallback && !isGenerated && !isIdentity

	return &mgmtv1alpha1.TransformerRecommendation{
		Schema:            schema,
		Table:             table,
		Column:            columnReport.ColumnName,
		Category:          string(category),
		RecommendedConfig: validatedConfig,
		Confidence:        confidence,
		Evidence:          evidence,
	}, genericFallback, category, dataType
}

// deriveCategoryAndEvidence combines the regex, dictionary and LLM reporters
// of a column report into a single category + confidence (the reporter with
// the highest confidence wins) along with one evidence entry per reporter.
func deriveCategoryAndEvidence(
	columnReport *piidetect_table_activities.ColumnReport,
) (category ee_recommendations.Category, confidence float32, evidence []*mgmtv1alpha1.DetectionEvidence) {
	evidence = []*mgmtv1alpha1.DetectionEvidence{}

	if columnReport.Report.Regex != nil {
		category = columnReport.Report.Regex.Category
		confidence = regexDetectionConfidence
		evidence = append(evidence, &mgmtv1alpha1.DetectionEvidence{
			Kind: mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_REGEX,
			Detail: fmt.Sprintf(
				"regex detector matched the column name and classified it as %q",
				columnReport.Report.Regex.Category,
			),
			Confidence: regexDetectionConfidence,
		})
	}
	if columnReport.Report.Dictionary != nil {
		dict := columnReport.Report.Dictionary
		evidence = append(evidence, &mgmtv1alpha1.DetectionEvidence{
			Kind: mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_DICTIONARY,
			Detail: fmt.Sprintf(
				"dictionary detector matched column-name token %q and classified it as %q",
				dict.Token, dict.Category,
			),
			Confidence: dict.Confidence,
		})
		if dict.Confidence > confidence {
			category = dict.Category
			confidence = dict.Confidence
		}
	}
	if columnReport.Report.LLM != nil {
		llm := columnReport.Report.LLM
		evidence = append(evidence, &mgmtv1alpha1.DetectionEvidence{
			Kind: mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_LLM,
			Detail: fmt.Sprintf(
				"LLM detector classified the column as %q with confidence %.2f",
				llm.Category, llm.Confidence,
			),
			Confidence: llm.Confidence,
		})
		if llm.Confidence > confidence {
			category = llm.Category
			confidence = llm.Confidence
		}
	}
	return category, confidence, evidence
}
