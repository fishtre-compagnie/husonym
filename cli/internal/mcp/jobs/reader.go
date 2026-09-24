// Package jobs is the only way the MCP server reads and writes jobs, and the only way it runs
// one.
//
// Running a job writes into real databases, so the run is not the model's to decide: before
// each run, this package asks the person behind the agent, through the client, and triggers
// the run only on their yes. The question is asked here rather than left to the tools, so that
// there is no way to trigger a run that skips it, and the answer covers one run of the job as
// it stood when the question was put — mappings, destinations, options and hooks — and is never
// remembered. Changing the mappings of a job that runs on a schedule is running it at the next
// tick, and asks the same way.
//
// What this package reads back is configuration, never a value from a row, with one exception
// it removes: the message of a run's failure can quote the value it failed on. The runs and
// events it returns say that something failed, not why; the why is read through rowvalues,
// which asks first.
package jobs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/ask"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/maskedconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/proto"
)

// jobClient is the part of the job service this package may call. reader_test.go pins the
// list, so that widening it is a decision someone takes: a method that answers with a run's
// failure must have it removed here, and a method that makes a job run must ask first.
type jobClient interface {
	GetJob(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobResponse], error)
	GetJobHooks(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobHooksRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobHooksResponse], error)
	GetJobStatus(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobStatusRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobStatusResponse], error)
	CreateJob(
		context.Context,
		*connect.Request[mgmtv1alpha1.CreateJobRequest],
	) (*connect.Response[mgmtv1alpha1.CreateJobResponse], error)
	UpdateJobSourceConnection(
		context.Context,
		*connect.Request[mgmtv1alpha1.UpdateJobSourceConnectionRequest],
	) (*connect.Response[mgmtv1alpha1.UpdateJobSourceConnectionResponse], error)
	GetJobRuns(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRunsRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobRunsResponse], error)
	GetJobRun(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRunRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobRunResponse], error)
	GetJobRunEvents(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetJobRunEventsRequest],
	) (*connect.Response[mgmtv1alpha1.GetJobRunEventsResponse], error)
	CreateJobRun(
		context.Context,
		*connect.Request[mgmtv1alpha1.CreateJobRunRequest],
	) (*connect.Response[mgmtv1alpha1.CreateJobRunResponse], error)
}

// ErrDeclined is returned when the person declined, or dismissed the question.
var ErrDeclined = errors.New("the person declined: nothing was run or changed")

// ErrCannotAsk is returned when the client cannot put a question to the person.
var ErrCannotAsk = errors.New(
	"this client cannot ask the person for confirmation, and a job is never run without it: " +
		"running a job, or changing one that runs on a schedule, needs a client that supports elicitation",
)

// Reader reads, creates and changes the jobs of an account, and runs them once the person
// has agreed.
type Reader struct {
	client      jobClient
	connections *maskedconn.Reader
	accountId   string

	// now tells the time a trigger is given up on.
	now func() time.Time

	mu        sync.Mutex
	questions *ask.Questions[confirmation]
	// launched holds, for each job triggered from here whose run has not shown yet, the runs
	// the job had before: the run started here is the first one outside them.
	launched map[string]launch
}

// New builds a Reader on its own job client, which is never handed out. It names connections
// to the person through connections.
func New(
	httpClient connect.HTTPClient,
	baseURL string,
	accountId string,
	connections *maskedconn.Reader,
	opts ...connect.ClientOption,
) *Reader {
	return &Reader{
		client:      mgmtv1alpha1connect.NewJobServiceClient(httpClient, baseURL, opts...),
		connections: connections,
		accountId:   accountId,
		now:         time.Now,
		questions:   ask.New[confirmation](),
		launched:    map[string]launch{},
	}
}

// Get returns one job.
func (r *Reader) Get(ctx context.Context, jobId string) (*mgmtv1alpha1.Job, error) {
	res, err := r.client.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobId}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetJob(), nil
}

// Create creates a job that runs only when asked to: without a schedule, and without the run
// the API can start on creation.
func (r *Reader) Create(ctx context.Context, job *mgmtv1alpha1.CreateJobRequest) (*mgmtv1alpha1.Job, error) {
	if job.CronSchedule != nil || job.GetInitiateJobRun() {
		return nil, errors.New("a job is created without a schedule and without a run: run it with run_job")
	}
	job = proto.CloneOf(job)
	job.AccountId = r.accountId
	res, err := r.client.CreateJob(ctx, connect.NewRequest(job))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetJob(), nil
}

// SetMappings replaces the mappings of a job, as it was read. It refuses while a run of the
// job is going or starting: the run reads the mappings when it begins, and a change landing
// then would run without the question the person answered. When the job runs on a schedule,
// the change is as good as a run, and the person is asked first: until they answer, it
// changes nothing and returns the question to put to them instead.
func (r *Reader) SetMappings(
	ctx context.Context,
	req *mcp.CallToolRequest,
	job *mgmtv1alpha1.Job,
	mappings []*mgmtv1alpha1.JobMapping,
) (mcp.InputRequestMap, error) {
	if err := r.idle(ctx, job.GetId()); err != nil {
		return nil, err
	}
	scheduled, err := r.scheduled(ctx, job)
	if err != nil {
		return nil, err
	}
	if scheduled {
		change, err := proto.MarshalOptions{Deterministic: true}.Marshal(
			&mgmtv1alpha1.UpdateJobSourceConnectionRequest{Mappings: mappings},
		)
		if err != nil {
			return nil, err
		}
		questions, err := r.confirm(ctx, req, job, string(change), func(snap *snapshot) (string, error) {
			described, err := r.describe(ctx, snap, mappings)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"The agent asks to change the mappings of the job %q, which runs on the schedule %q: "+
					"the change takes effect at its next run. After the change, it %s Apply the change?",
				job.GetName(), job.GetCronSchedule(), described,
			), nil
		})
		if err != nil || questions != nil {
			return questions, err
		}
	}
	_, err = r.client.UpdateJobSourceConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
		Id:                 job.GetId(),
		Source:             job.GetSource(),
		Mappings:           mappings,
		VirtualForeignKeys: job.GetVirtualForeignKeys(),
		JobType:            job.GetJobType(),
		// The job as it was read: a change landing since — in the UI, or by a run mapping a new
		// column — is refused rather than overwritten.
		ExpectedUpdatedAt: job.GetUpdatedAt(),
	}))
	return nil, err
}

// scheduled says whether a job runs on its own: its schedule is not paused. A job created
// without a schedule has one all the same, which the API keeps paused.
func (r *Reader) scheduled(ctx context.Context, job *mgmtv1alpha1.Job) (bool, error) {
	if job.GetCronSchedule() == "" {
		return false, nil
	}
	res, err := r.client.GetJobStatus(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobStatusRequest{JobId: job.GetId()}))
	if err != nil {
		return false, fmt.Errorf("unable to read the status of the job: %w", err)
	}
	return res.Msg.GetStatus() == mgmtv1alpha1.JobStatus_JOB_STATUS_ENABLED, nil
}

// SourceConnectionId returns the connection a job reads from, when it reads from a database
// the MCP server knows how to configure: PostgreSQL or MySQL. It returns "" otherwise.
func SourceConnectionId(job *mgmtv1alpha1.Job) string {
	options := job.GetSource().GetOptions()
	return cmp.Or(options.GetPostgres().GetConnectionId(), options.GetMysql().GetConnectionId())
}

// Truncates says whether a destination empties its tables before writing into them.
func Truncates(options *mgmtv1alpha1.JobDestinationOptions) bool {
	return options.GetPostgresOptions().GetTruncateTable().GetTruncateBeforeInsert() ||
		options.GetMysqlOptions().GetTruncateTable().GetTruncateBeforeInsert() ||
		options.GetMssqlOptions().GetTruncateTable().GetTruncateBeforeInsert()
}

// cascades says whether a destination empties, along with its tables, the tables that
// reference them.
func cascades(options *mgmtv1alpha1.JobDestinationOptions) bool {
	return options.GetPostgresOptions().GetTruncateTable().GetCascade()
}

// initsSchema says whether a destination creates the tables it is missing.
func initsSchema(options *mgmtv1alpha1.JobDestinationOptions) bool {
	return options.GetPostgresOptions().GetInitTableSchema() ||
		options.GetMysqlOptions().GetInitTableSchema() ||
		options.GetMssqlOptions().GetInitTableSchema()
}

// autoMaps says whether a run maps a column that appears in a table of the job by itself.
func autoMaps(job *mgmtv1alpha1.Job) bool {
	options := job.GetSource().GetOptions()
	return options.GetPostgres().GetNewColumnAdditionStrategy().GetAutoMap() != nil ||
		options.GetMysql().GetNewColumnAdditionStrategy().GetAutoMap() != nil ||
		options.GetMssql().GetNewColumnAdditionStrategy().GetAutoMap() != nil
}
