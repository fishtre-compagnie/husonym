package mcp_server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	jobId         = "7f2c1e4a-0000-4000-8000-0000000000a1"
	createdJobId  = "7f2c1e4a-0000-4000-8000-0000000000a2"
	destinationId = "7f2c1e4a-0000-4000-8000-0000000000c2"
	// failedOn is a value a run failed on, quoted by the database in its message.
	failedOn = "jean.dupont@example.com"
)

// fakeJobService holds one job of the shop, its runs, and what the server asked of it. It
// behaves as the API does where the server relies on it: a job created without a schedule has
// the API's placeholder one, paused, and writing a job leaves its updated_at alone.
type fakeJobService struct {
	mgmtv1alpha1connect.UnimplementedJobServiceHandler

	mu        sync.Mutex
	job       *mgmtv1alpha1.Job
	hooks     []*mgmtv1alpha1.JobHook
	status    mgmtv1alpha1.JobStatus
	runs      []*mgmtv1alpha1.JobRun
	events    map[string][]*mgmtv1alpha1.JobRunEvent
	created   []*mgmtv1alpha1.CreateJobRequest
	updated   []*mgmtv1alpha1.UpdateJobSourceConnectionRequest
	triggered []string
}

func newFakeJobService() *fakeJobService {
	return &fakeJobService{job: shopJob(), status: mgmtv1alpha1.JobStatus_JOB_STATUS_PAUSED}
}

// shopJob copies the users of the shop into staging, emptying its tables first.
func shopJob() *mgmtv1alpha1.Job {
	passthrough := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
		PassthroughConfig: &mgmtv1alpha1.Passthrough{},
	}}
	email := &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
		GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
	}}
	mapping := func(column string, config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: "public", Table: "users", Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: config},
		}
	}
	placeholder := "0 0 1 1 *"
	return &mgmtv1alpha1.Job{
		Id:           jobId,
		Name:         "shop-anon",
		CronSchedule: &placeholder,
		AccountId:    accountId,
		UpdatedAt:    updatedAt,
		Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
				ConnectionId: connectionId,
			}},
		}},
		Destinations: []*mgmtv1alpha1.JobDestination{{
			ConnectionId: destinationId,
			Options:      destinationOptions(postgres, true),
		}},
		Mappings: []*mgmtv1alpha1.JobMapping{
			mapping("id", passthrough),
			mapping("email", email),
			mapping("birth_date", passthrough),
		},
	}
}

// failedRun is a run of the shop job whose activity failed on a value and is retried, and says
// so in its messages. The API reports such an activity as started, with its last failure.
func failedRun(id string, startedAt time.Time, status mgmtv1alpha1.JobRunStatus) *mgmtv1alpha1.JobRun {
	return &mgmtv1alpha1.JobRun{
		Id:        id,
		JobId:     jobId,
		Status:    status,
		StartedAt: timestamppb.New(startedAt),
		PendingActivities: []*mgmtv1alpha1.PendingActivity{{
			ActivityName: "RunSqlInitTableStatements",
			Status:       mgmtv1alpha1.ActivityStatus_ACTIVITY_STATUS_STARTED,
			LastFailure: &mgmtv1alpha1.ActivityFailure{
				Message: "Duplicate entry '" + failedOn + "' for key 'email'",
			},
		}},
	}
}

func (f *fakeJobService) GetJob(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobRequest],
) (*connect.Response[mgmtv1alpha1.GetJobResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.job == nil || f.job.GetId() != req.Msg.GetId() {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unable to find job"))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobResponse{Job: proto.CloneOf(f.job)}), nil
}

func (f *fakeJobService) GetJobStatus(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetJobStatusRequest],
) (*connect.Response[mgmtv1alpha1.GetJobStatusResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return connect.NewResponse(&mgmtv1alpha1.GetJobStatusResponse{Status: f.status}), nil
}

func (f *fakeJobService) CreateJob(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.CreateJobRequest],
) (*connect.Response[mgmtv1alpha1.CreateJobResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req.Msg)
	return connect.NewResponse(&mgmtv1alpha1.CreateJobResponse{Job: &mgmtv1alpha1.Job{
		Id:       createdJobId,
		Name:     req.Msg.GetJobName(),
		Mappings: req.Msg.GetMappings(),
	}}), nil
}

func (f *fakeJobService) UpdateJobSourceConnection(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.UpdateJobSourceConnectionRequest],
) (*connect.Response[mgmtv1alpha1.UpdateJobSourceConnectionResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, req.Msg)
	f.job.Mappings = req.Msg.GetMappings()
	return connect.NewResponse(&mgmtv1alpha1.UpdateJobSourceConnectionResponse{Job: proto.CloneOf(f.job)}), nil
}

func (f *fakeJobService) GetJobRuns(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetJobRunsRequest],
) (*connect.Response[mgmtv1alpha1.GetJobRunsResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	runs := make([]*mgmtv1alpha1.JobRun, 0, len(f.runs))
	for _, run := range f.runs {
		runs = append(runs, proto.CloneOf(run))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobRunsResponse{JobRuns: runs}), nil
}

func (f *fakeJobService) GetJobRun(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobRunRequest],
) (*connect.Response[mgmtv1alpha1.GetJobRunResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.runs, func(run *mgmtv1alpha1.JobRun) bool { return run.GetId() == req.Msg.GetJobRunId() })
	if i < 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unable to find job run"))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobRunResponse{JobRun: proto.CloneOf(f.runs[i])}), nil
}

func (f *fakeJobService) GetJobRunEvents(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobRunEventsRequest],
) (*connect.Response[mgmtv1alpha1.GetJobRunEventsResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	events := []*mgmtv1alpha1.JobRunEvent{}
	for _, event := range f.events[req.Msg.GetJobRunId()] {
		events = append(events, proto.CloneOf(event))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobRunEventsResponse{Events: events, IsRunComplete: true}), nil
}

// ValidateJobMappings checks mappings against the shop with the API's own validator, so that
// the tools are held to the rule itself, not to a copy of it.
func (f *fakeJobService) ValidateJobMappings(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.ValidateJobMappingsRequest],
) (*connect.Response[mgmtv1alpha1.ValidateJobMappingsResponse], error) {
	columns := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{}
	for _, column := range shopColumns() {
		table := tableKey(column.GetSchema(), column.GetTable())
		if columns[table] == nil {
			columns[table] = map[string]*sqlmanager_shared.DatabaseSchemaRow{}
		}
		columns[table][column.GetColumn()] = &sqlmanager_shared.DatabaseSchemaRow{
			TableSchema:        column.GetSchema(),
			TableName:          column.GetTable(),
			ColumnName:         column.GetColumn(),
			DataType:           column.GetDataType(),
			IsNullable:         column.GetIsNullable() == "YES",
			ColumnDefault:      column.GetColumnDefault(),
			GeneratedType:      column.GeneratedType,
			IdentityGeneration: column.IdentityGeneration,
		}
	}
	constraints := &sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{"public.users": {"id"}, "public.orders": {"id"}},
		ForeignKeyConstraints: map[string][]*sqlmanager_shared.ForeignConstraint{"public.orders": {{
			Columns:     []string{"user_id"},
			NotNullable: []bool{true},
			ForeignKey:  &sqlmanager_shared.ForeignKey{Table: "public.users", Columns: []string{"id"}},
		}}},
	}
	validator := job_util.NewJobMappingsValidator(
		req.Msg.GetMappings(),
		job_util.WithJobType(job_util.SupportedJobTypeOf(req.Msg.GetJobSource())),
	)
	result, err := validator.Validate(columns, req.Msg.GetVirtualForeignKeys(), constraints)
	if err != nil {
		return nil, err
	}
	res := &mgmtv1alpha1.ValidateJobMappingsResponse{
		DatabaseErrors: &mgmtv1alpha1.DatabaseError{ErrorReports: result.DatabaseErrors},
	}
	for table, reports := range result.TableErrors {
		schema, name := sqlmanager_shared.SplitTableKey(table)
		res.TableErrors = append(res.TableErrors, &mgmtv1alpha1.TableError{Schema: schema, Table: name, ErrorReports: reports})
	}
	for table, byColumn := range result.ColumnErrors {
		schema, name := sqlmanager_shared.SplitTableKey(table)
		for column, reports := range byColumn {
			res.ColumnErrors = append(res.ColumnErrors, &mgmtv1alpha1.ColumnError{
				Schema: schema, Table: name, Column: column, ErrorReports: reports,
			})
		}
	}
	return connect.NewResponse(res), nil
}

func (f *fakeJobService) CreateJobRun(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.CreateJobRunRequest],
) (*connect.Response[mgmtv1alpha1.CreateJobRunResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggered = append(f.triggered, req.Msg.GetJobId())
	return connect.NewResponse(&mgmtv1alpha1.CreateJobRunResponse{}), nil
}

// seen returns what the server created, changed and triggered so far.
func (f *fakeJobService) seen() (
	created []*mgmtv1alpha1.CreateJobRequest,
	updated []*mgmtv1alpha1.UpdateJobSourceConnectionRequest,
	triggered []string,
) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.created), slices.Clone(f.updated), slices.Clone(f.triggered)
}

// start stands for the scheduler starting a run.
func (f *fakeJobService) start(run *mgmtv1alpha1.JobRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, run)
}

// touch stands for someone changing the job behind the agent's back: its mappings change,
// and, as with the API, its updated_at does not.
func (f *fakeJobService) touch() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.job.Mappings[0].Transformer = f.job.Mappings[1].GetTransformer()
}

// hook adds an enabled SQL hook to the job, run before the sync on the destination.
func (f *fakeJobService) hook(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks = append(f.hooks, &mgmtv1alpha1.JobHook{
		Id: name, Name: name, JobId: jobId, Enabled: true,
		Config: &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
			Query:        "DELETE FROM audit",
			ConnectionId: destinationId,
			Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{
				PreSync: &mgmtv1alpha1.JobHookTimingPreSync{},
			}},
		}}},
	})
}

func (f *fakeJobService) GetJobHooks(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetJobHooksRequest],
) (*connect.Response[mgmtv1alpha1.GetJobHooksResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hooks := make([]*mgmtv1alpha1.JobHook, 0, len(f.hooks))
	for _, hook := range f.hooks {
		hooks = append(hooks, proto.CloneOf(hook))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobHooksResponse{Hooks: hooks}), nil
}

// connectJobs stands the fake API up with jobs, for a client of the test's making.
func connectJobs(t *testing.T, jobService *fakeJobService, clientOptions *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	return connectAPI(t, fakeAPI{
		connections: &fakeConnectionService{},
		data:        &fakeDataService{},
		jobs:        jobService,
	}, clientOptions, "")
}
