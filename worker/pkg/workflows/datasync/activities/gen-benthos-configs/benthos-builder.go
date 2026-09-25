package genbenthosconfigs_activity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/metrics"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_mssql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql"
	sqlmanager_postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	bb_shared "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/shared"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/worker/pkg/consistencykey"
	selectquerybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/select-query-builder"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"

	"gopkg.in/yaml.v3"
)

type benthosBuilder struct {
	sqlmanagerclient sqlmanager.SqlManagerClient

	jobclient         mgmtv1alpha1connect.JobServiceClient
	connclient        mgmtv1alpha1connect.ConnectionServiceClient
	transformerclient mgmtv1alpha1connect.TransformersServiceClient

	jobId    string
	jobRunId string
	runId    string

	metricsEnabled bool

	pageLimit int

	keys *consistencykey.Resolver

	athanor shared.AthanorPolicy
}

func newBenthosBuilder(
	sqlmanagerclient sqlmanager.SqlManagerClient,

	jobclient mgmtv1alpha1connect.JobServiceClient,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	transformerclient mgmtv1alpha1connect.TransformersServiceClient,

	jobId, jobRunId string, runId string,

	metricsEnabled bool,

	pageLimit int,

	keys *consistencykey.Resolver,
	athanor shared.AthanorPolicy,
) *benthosBuilder {
	return &benthosBuilder{
		sqlmanagerclient:  sqlmanagerclient,
		jobclient:         jobclient,
		connclient:        connclient,
		transformerclient: transformerclient,
		jobId:             jobId,
		jobRunId:          jobRunId,
		runId:             runId,
		metricsEnabled:    metricsEnabled,
		pageLimit:         pageLimit,
		keys:              keys,
		athanor:           athanor,
	}
}

type workflowMetadata struct {
	WorkflowId string
	RunId      string
}

// reconcileJobMappings writes to the job what the run found in its source: the columns it mapped
// because the job did not, the mappings whose column is gone, and the columns with their types.
//
// Sent on every run that read a SQL source, changes or not: the types are what lets the next run
// tell that a column changed type.
//
// A failure fails the run. The configs already follow the source, so the data would be right;
// but the job would not say so, the next run would decide the same columns again, and under
// AutoMap & Review nobody would be asked to review what this one decided.
func (b *benthosBuilder) reconcileJobMappings(
	ctx context.Context,
	job *mgmtv1alpha1.Job,
	added, removed []*mgmtv1alpha1.JobMapping,
	columns []*mgmtv1alpha1.JobSourceColumn,
	recordChanges bool,
) error {
	if len(added) == 0 && len(removed) == 0 && len(columns) == 0 {
		return nil
	}
	removedColumns := make([]*mgmtv1alpha1.JobColumn, 0, len(removed))
	for _, mapping := range removed {
		removedColumns = append(removedColumns, &mgmtv1alpha1.JobColumn{
			Schema: mapping.GetSchema(),
			Table:  mapping.GetTable(),
			Column: mapping.GetColumn(),
		})
	}
	_, err := b.jobclient.ReconcileJobMappings(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.ReconcileJobMappingsRequest{
			JobId:         job.GetId(),
			AccountId:     job.GetAccountId(),
			JobRunId:      b.jobRunId,
			Added:         added,
			Removed:       removedColumns,
			Columns:       columns,
			RecordChanges: recordChanges,
		}),
	)
	if err != nil {
		return fmt.Errorf("unable to bring the job's mappings in step with its source: %w", err)
	}
	return nil
}

func (b *benthosBuilder) GenerateBenthosConfigsNew(
	ctx context.Context,
	req *GenerateBenthosConfigsRequest,
	wfmetadata *workflowMetadata,
	slogger *slog.Logger,
) (*GenerateBenthosConfigsResponse, error) {
	job, err := b.getJobById(ctx, req.JobId)
	if err != nil {
		return nil, fmt.Errorf("unable to get job by id: %w", err)
	}

	// Whether this account has anything to derive from — and, on a deployment where no
	// variable carries a key, what gives it one. Asked here, before AutoMap decides
	// anything: a mapping written to the job is one the runs after this one will keep.
	consistencyKey, err := b.keys.ForAccount(ctx, job.GetAccountId())
	if err != nil {
		return nil, err
	}

	benthosManager, responses, err := b.plan(ctx, job, consistencyKey != "", slogger)
	if err != nil {
		return nil, err
	}

	changes := benthosManager.MappingChanges()
	if err := b.reconcileJobMappings(
		ctx, job, changes.Added, changes.Removed, changes.Columns, changes.RecordChanges,
	); err != nil {
		return nil, err
	}

	err = b.setConnectionIdsRunContext(ctx, responses, job.GetAccountId())
	if err != nil {
		return nil, fmt.Errorf("unable to set connection ids run context: %w", err)
	}

	// TODO move run context logic into benthos builder
	postTableSyncRunCtx := buildPostTableSyncRunCtx(responses, job.Destinations)
	err = b.setPostTableSyncRunCtx(ctx, postTableSyncRunCtx, job.GetAccountId())
	if err != nil {
		return nil, fmt.Errorf(
			"unable to set all run contexts for post table sync configs: %w",
			err,
		)
	}

	outputConfigs, err := b.setRunContexts(ctx, responses, job.GetAccountId())
	if err != nil {
		return nil, fmt.Errorf("unable to set all run contexts for benthos configs: %w", err)
	}
	return &GenerateBenthosConfigsResponse{
		AccountId:      job.AccountId,
		BenthosConfigs: outputConfigs,
		Findings:       benthosManager.Findings(),
	}, nil
}

// plan computes the configs of a run of the job, and what they tell of the run, from the
// schemas of its connections. It writes nothing: what the run keeps, and what it brings the
// job in step with, is up to the caller.
func (b *benthosBuilder) plan(
	ctx context.Context,
	job *mgmtv1alpha1.Job,
	hasConsistencyKey bool,
	slogger *slog.Logger,
) (*benthosbuilder.BenthosConfigManager, []*benthosbuilder.BenthosConfigResponse, error) {
	sourceConnection, err := shared.GetJobSourceConnection(ctx, job.GetSource(), b.connclient)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get connection by id: %w", err)
	}

	destConnections := []*mgmtv1alpha1.Connection{}
	for _, destination := range job.Destinations {
		destinationConnection, err := shared.GetConnectionById(
			ctx,
			b.connclient,
			destination.ConnectionId,
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"unable to get destination connection (%s) by id: %w",
				destination.ConnectionId,
				err,
			)
		}
		destConnections = append(destConnections, destinationConnection)
	}

	benthosManagerConfig := &benthosbuilder.WorkerBenthosConfig{
		Job:                    job,
		SourceConnection:       sourceConnection,
		DestinationConnections: destConnections,
		JobRunId:               b.jobRunId,
		Logger:                 slogger,
		Sqlmanagerclient:       b.sqlmanagerclient,
		Transformerclient:      b.transformerclient,
		Connectionclient:       b.connclient,
		SelectQueryBuilder:     &selectquerybuilder.QueryMapBuilderWrapper{},
		MetricsEnabled:         b.metricsEnabled,
		MetricLabelKeyVals: map[string]string{
			metrics.TemporalWorkflowId: bb_shared.WithEnvInterpolation(
				metrics.TemporalWorkflowIdEnvKey,
			),
			metrics.TemporalRunId: bb_shared.WithEnvInterpolation(metrics.TemporalRunIdEnvKey),
		},
		PageLimit:         &b.pageLimit,
		HasConsistencyKey: hasConsistencyKey,
		UsesAthanor:       b.athanor.UsesAthanor(job),
	}
	benthosManager, err := benthosbuilder.NewWorkerBenthosConfigManager(benthosManagerConfig)
	if err != nil {
		return nil, nil, err
	}
	responses, err := benthosManager.GenerateBenthosConfigs(ctx)
	if err != nil {
		return nil, nil, err
	}
	return benthosManager, responses, nil
}

// PlanPreflight computes the plan of a run of the job as GenerateBenthosConfigsNew does, and
// returns the tables it writes and what it tells of the run. Nothing is written: not the
// job, whose mappings the columns AutoMap would add are not added to, nor the run context,
// nor the key of the account, which is read and never drawn.
func (b *benthosBuilder) PlanPreflight(
	ctx context.Context,
	jobID string,
	slogger *slog.Logger,
) (*PlanPreflightResponse, error) {
	job, err := b.getJobById(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("unable to get job by id: %w", err)
	}
	hasConsistencyKey, err := b.keys.HasKey(ctx, job.GetAccountId())
	if err != nil {
		return nil, err
	}
	benthosManager, responses, err := b.plan(ctx, job, hasConsistencyKey, slogger)
	if err != nil {
		return nil, err
	}
	return &PlanPreflightResponse{
		AccountId: job.GetAccountId(),
		Tables:    preflight_activity.TablesOf(responses),
		Findings:  benthosManager.Findings(),
	}, nil
}

func (b *benthosBuilder) setConnectionIdsRunContext(
	ctx context.Context,
	responses []*benthosbuilder.BenthosConfigResponse,
	accountId string,
) error {
	connectionIds := map[string]struct{}{}
	for _, config := range responses {
		for _, dsn := range config.BenthosDsns {
			connectionIds[dsn.ConnectionId] = struct{}{}
		}
	}
	connectionIdsList := []string{}
	for id := range connectionIds {
		connectionIdsList = append(connectionIdsList, id)
	}
	bits, err := json.Marshal(connectionIdsList)
	if err != nil {
		return fmt.Errorf("failed to marshal connection ids: %w", err)
	}
	_, err = b.jobclient.SetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId:   b.jobRunId,
			ExternalId: shared.GetConnectionIdsExternalId(),
			AccountId:  accountId,
		},
		Value: bits,
	}))
	if err != nil {
		return fmt.Errorf("failed to send connection ids run context: %w", err)
	}
	return nil
}

// this method modifies the input responses by nilling out the benthos config. it returns the same slice for convenience
func (b *benthosBuilder) setRunContexts(
	ctx context.Context,
	responses []*benthosbuilder.BenthosConfigResponse,
	accountId string,
) ([]*benthosbuilder.BenthosConfigResponse, error) {
	rcstream := b.jobclient.SetRunContexts(ctx)

	for _, config := range responses {
		bits, err := yaml.Marshal(config.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal benthos config: %w", err)
		}
		err = rcstream.Send(&mgmtv1alpha1.SetRunContextsRequest{
			Id: &mgmtv1alpha1.RunContextKey{
				JobRunId:   b.jobRunId,
				ExternalId: shared.GetBenthosConfigExternalId(config.Name),
				AccountId:  accountId,
			},
			Value: bits,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to send run context: %w", err)
		}
		// The same work, without any Benthos type, for the Athanor engine.
		if plan := toTablePlan(config); plan != nil {
			planBits, err := plan.Marshal()
			if err != nil {
				return nil, fmt.Errorf("failed to marshal table plan: %w", err)
			}
			err = rcstream.Send(&mgmtv1alpha1.SetRunContextsRequest{
				Id: &mgmtv1alpha1.RunContextKey{
					JobRunId:   b.jobRunId,
					ExternalId: shared.GetTablePlanExternalId(config.Name),
					AccountId:  accountId,
				},
				Value: planBits,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to send table plan run context: %w", err)
			}
		}
		config.Config = nil // nilling this out so that it does not persist in temporal
	}

	_, err := rcstream.CloseAndReceive()
	if err != nil {
		return nil, fmt.Errorf(
			"unable to receive response from benthos runcontext request: %w",
			err,
		)
	}
	return responses, nil
}

func (b *benthosBuilder) setPostTableSyncRunCtx(
	ctx context.Context,
	postSyncConfigs map[string]*shared.PostTableSyncConfig,
	accountId string,
) error {
	rcstream := b.jobclient.SetRunContexts(ctx)

	for name, config := range postSyncConfigs {
		bits, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("failed to marshal post table sync config: %w", err)
		}
		err = rcstream.Send(&mgmtv1alpha1.SetRunContextsRequest{
			Id: &mgmtv1alpha1.RunContextKey{
				JobRunId:   b.jobRunId,
				ExternalId: shared.GetPostTableSyncConfigExternalId(name),
				AccountId:  accountId,
			},
			Value: bits,
		})
		if err != nil {
			return fmt.Errorf("failed to send post table sync run context: %w", err)
		}
	}

	_, err := rcstream.CloseAndReceive()
	if err != nil {
		return fmt.Errorf(
			"unable to receive response from post table sync runcontext request: %w",
			err,
		)
	}
	return nil
}

func (b *benthosBuilder) getJobById(
	ctx context.Context,
	jobId string,
) (*mgmtv1alpha1.Job, error) {
	getjobResp, err := b.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{
		Id: jobId,
	}))
	if err != nil {
		return nil, err
	}

	return getjobResp.Msg.Job, nil
}

func buildPostTableSyncRunCtx(
	benthosConfigs []*benthosbuilder.BenthosConfigResponse,
	destinations []*mgmtv1alpha1.JobDestination,
) map[string]*shared.PostTableSyncConfig {
	postTableSyncRunCtx := map[string]*shared.PostTableSyncConfig{} // benthos_config_name -> config
	for _, bc := range benthosConfigs {
		destConfigs := map[string]*shared.PostTableSyncDestConfig{}
		for _, destination := range destinations {
			var stmts []string
			switch destination.GetOptions().GetConfig().(type) {
			case *mgmtv1alpha1.JobDestinationOptions_PostgresOptions:
				stmts = buildPgPostTableSyncStatement(bc)
			case *mgmtv1alpha1.JobDestinationOptions_MssqlOptions:
				stmts = buildMssqlPostTableSyncStatement(bc)
			}
			if len(stmts) != 0 {
				destConfigs[destination.GetConnectionId()] = &shared.PostTableSyncDestConfig{
					Statements: stmts,
				}
			}
		}
		if len(destConfigs) != 0 {
			postTableSyncRunCtx[bc.Name] = &shared.PostTableSyncConfig{
				DestinationConfigs: destConfigs,
			}
		}
	}
	return postTableSyncRunCtx
}

func buildPgPostTableSyncStatement(bc *benthosbuilder.BenthosConfigResponse) []string {
	statements := []string{}
	if bc.RunType == runconfigs.RunTypeUpdate {
		return statements
	}
	colDefaultProps := bc.ColumnDefaultProperties
	for colName, p := range colDefaultProps {
		if p.NeedsReset && !p.HasDefaultTransformer {
			// resets sequences and identities
			resetSql := sqlmanager_postgres.BuildPgIdentityColumnResetCurrentSql(
				bc.TableSchema,
				bc.TableName,
				colName,
			)
			statements = append(statements, resetSql)
		}
	}
	return statements
}

func buildMssqlPostTableSyncStatement(bc *benthosbuilder.BenthosConfigResponse) []string {
	statements := []string{}
	if bc.RunType == runconfigs.RunTypeUpdate {
		return statements
	}
	colDefaultProps := bc.ColumnDefaultProperties
	for _, p := range colDefaultProps {
		if p.NeedsOverride {
			// reset identity
			resetSql := sqlmanager_mssql.BuildMssqlIdentityColumnResetCurrent(
				bc.TableSchema,
				bc.TableName,
			)
			statements = append(statements, resetSql)
		}
	}
	return statements
}
