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

// launch is a run started from here that has not shown among the runs of its job yet.
type launch struct {
	// runId is the run the API started. It is empty while the trigger is being sent, and when
	// the API failed without telling whether a run started: the job is then held until the
	// launch is given up.
	runId string
	at    time.Time
}

// launchTimeout is how long a run started from here is waited for among the runs of its job.
// One removed before it showed never does; past this, it is given up.
const launchTimeout = 2 * time.Minute

// Run triggers one run of a job, once the person has agreed to it, and returns the id of the
// run started. Until they answer, it runs nothing and returns the question to put to them
// instead. It refuses while a run of the job is in progress, and while the run it last started
// has not shown among the job's runs: the list of runs sees a new one a moment late.
func (r *Reader) Run(ctx context.Context, req *mcp.CallToolRequest, jobId string) (string, mcp.InputRequestMap, error) {
	job, err := r.Get(ctx, jobId)
	if err != nil {
		return "", nil, fmt.Errorf("unable to read job %s: %w", jobId, err)
	}
	if err := r.idle(ctx, jobId); err != nil {
		return "", nil, err
	}
	questions, err := r.confirm(ctx, req, job, "run", func(snap *snapshot) (string, error) {
		described, err := r.describe(ctx, snap, job.GetMappings())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("The agent asks to run the job %q now. It %s Run it?", job.GetName(), described), nil
	})
	if err != nil || questions != nil {
		return "", questions, err
	}

	// The job is held before the trigger is sent: the API answers once the run has started,
	// and the run reads the mappings meanwhile. It also makes two calls answered at once start
	// one run.
	if !r.hold(jobId) {
		return "", nil, errTriggered
	}
	res, err := r.client.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: jobId}))
	if err != nil {
		if startedNothing(err) {
			r.mu.Lock()
			delete(r.launched, jobId)
			r.mu.Unlock()
		}
		return "", nil, err
	}
	runId := res.Msg.GetJobRun().GetId()
	r.mu.Lock()
	r.launched[jobId] = launch{runId: runId, at: r.now()}
	r.mu.Unlock()
	return runId, nil, nil
}

// hold marks a job as triggered from here, before its run is known. It answers false when the
// job already is: two calls that found it idle at once do not both trigger it.
func (r *Reader) hold(jobId string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.launched[jobId]; ok {
		return false
	}
	r.launched[jobId] = launch{at: r.now()}
	return true
}

// errTriggered is returned while a trigger sent from here has not told its run.
var errTriggered = errors.New(
	"a run of this job was just triggered and may be starting: get_run_status follows it",
)

// startedNothing says whether the API refused a trigger for what the caller may do: it then
// sent none. Any other failure keeps the job held: it may come after the trigger — a run that
// starts and cannot be read yet, an answer that never arrives or cannot be read — or tell of a
// run the API sees going and the list of runs does not show yet.
func startedNothing(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodePermissionDenied, connect.CodeUnauthenticated, connect.CodeNotFound:
		return true
	default:
		return false
	}
}

// idle refuses while a run of the job is going, or has just been started from here and does
// not show among its runs yet.
func (r *Reader) idle(ctx context.Context, jobId string) error {
	runs, err := r.Runs(ctx, jobId)
	if err != nil {
		return err
	}
	if runId, ok := r.starting(jobId, runs); ok {
		if runId == "" {
			return errTriggered
		}
		return fmt.Errorf(
			"the run %s of this job was just started and does not show among its runs yet: "+
				"get_run_status follows it", runId,
		)
	}
	if i := slices.IndexFunc(runs, inProgress); i >= 0 {
		return fmt.Errorf(
			"a run of this job is in progress (%s): wait for it to end, get_run_status follows it",
			runs[i].GetId(),
		)
	}
	return nil
}

// starting names the run started from here that has yet to show among runs, the job's runs as
// just read; it names none while the trigger has not told its run. It forgets the run once it
// shows, or once it is given up.
func (r *Reader) starting(jobId string, runs []*mgmtv1alpha1.JobRun) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	launched, ok := r.launched[jobId]
	if !ok {
		return "", false
	}
	shown := slices.ContainsFunc(runs, func(run *mgmtv1alpha1.JobRun) bool { return run.GetId() == launched.runId })
	if shown || r.now().Sub(launched.at) > launchTimeout {
		delete(r.launched, jobId)
		return "", false
	}
	return launched.runId, true
}

func inProgress(run *mgmtv1alpha1.JobRun) bool {
	switch run.GetStatus() {
	case mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_PENDING, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING:
		return true
	default:
		return false
	}
}
