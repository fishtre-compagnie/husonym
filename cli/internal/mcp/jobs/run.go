package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// launch is a trigger whose run has not shown yet.
type launch struct {
	before map[string]bool
	at     time.Time
}

// launchTimeout is how long a trigger is waited for. A trigger the scheduler skips — it
// skips a firing while a run of the job is going — never shows; past this, it is given up.
const launchTimeout = 2 * time.Minute

// errStarting is returned while a run triggered from here has not shown yet.
var errStarting = errors.New(
	"a run of this job was just triggered and has not started yet: get_run_status follows it",
)

// Run triggers one run of a job, once the person has agreed to it. Until they answer, it runs
// nothing and returns the question to put to them instead. It refuses while a run of the job
// is in progress, and while a run triggered from here has not shown yet: the API does not say
// which run a trigger starts, and the run takes a moment to show among the job's.
func (r *Reader) Run(ctx context.Context, req *mcp.CallToolRequest, jobId string) (mcp.InputRequestMap, error) {
	job, err := r.Get(ctx, jobId)
	if err != nil {
		return nil, fmt.Errorf("unable to read job %s: %w", jobId, err)
	}
	runs, err := r.runsIfIdle(ctx, jobId)
	if err != nil {
		return nil, err
	}
	questions, err := r.confirm(ctx, req, job, "run", func(snap *snapshot) (string, error) {
		described, err := r.describe(ctx, snap, job.GetMappings())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("The agent asks to run the job %q now. It %s Run it?", job.GetName(), described), nil
	})
	if err != nil || questions != nil {
		return questions, err
	}

	// The trigger is held before it is sent, so that two calls answered at once start one run.
	before := map[string]bool{}
	for _, run := range runs {
		before[run.GetId()] = true
	}
	r.mu.Lock()
	if _, ok := r.launched[jobId]; ok {
		r.mu.Unlock()
		return nil, errStarting
	}
	r.launched[jobId] = launch{before: before, at: r.now()}
	r.mu.Unlock()
	if _, err := r.client.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: jobId})); err != nil {
		r.mu.Lock()
		delete(r.launched, jobId)
		r.mu.Unlock()
		return nil, err
	}
	return nil, nil
}

// idle refuses while a run of the job is going, or starting.
func (r *Reader) idle(ctx context.Context, jobId string) error {
	_, err := r.runsIfIdle(ctx, jobId)
	return err
}

// runsIfIdle returns the runs of a job, and refuses while one is going or starting.
func (r *Reader) runsIfIdle(ctx context.Context, jobId string) ([]*mgmtv1alpha1.JobRun, error) {
	runs, err := r.Runs(ctx, jobId)
	if err != nil {
		return nil, err
	}
	if r.Starting(jobId, runs) {
		return nil, errStarting
	}
	if i := slices.IndexFunc(runs, inProgress); i >= 0 {
		return nil, fmt.Errorf(
			"a run of this job is in progress (%s): wait for it to end, get_run_status follows it",
			runs[i].GetId(),
		)
	}
	return runs, nil
}

// Starting says whether a run triggered from here has yet to show among runs, the job's runs
// as just read. It forgets a trigger once a run it did not know shows — a run of the schedule
// showing then is taken for it, and holds the job just the same — or once it is given up.
func (r *Reader) Starting(jobId string, runs []*mgmtv1alpha1.JobRun) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	launched, ok := r.launched[jobId]
	if !ok {
		return false
	}
	shown := slices.ContainsFunc(runs, func(run *mgmtv1alpha1.JobRun) bool { return !launched.before[run.GetId()] })
	if shown || r.now().Sub(launched.at) > launchTimeout {
		delete(r.launched, jobId)
		return false
	}
	return true
}

func inProgress(run *mgmtv1alpha1.JobRun) bool {
	switch run.GetStatus() {
	case mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_PENDING, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING:
		return true
	default:
		return false
	}
}
