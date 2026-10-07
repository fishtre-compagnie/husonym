// Package runusage holds the two activities through which a run tells the API that it has
// begun and that it has ended. The run workflows call them through
// workflow_shared.TrackRunUsage.
package runusage

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// What became of a run, as RunEndedRequest names it.
const (
	OutcomeCompleted = "completed"
	OutcomeFailed    = "failed"
	OutcomeCanceled  = "canceled"
)

const (
	errorTypeUnknownOutcome = "UnknownRunOutcome"
	errorTypeRefused        = "UsageReportRefused"
)

type Activities struct {
	usageclient mgmtv1alpha1connect.UsageServiceClient
}

func New(client mgmtv1alpha1connect.UsageServiceClient) *Activities {
	return &Activities{usageclient: client}
}

// Registry is what Register needs of a worker or of a test environment.
type Registry interface {
	RegisterActivityWithOptions(a any, options activity.RegisterOptions)
}

// Register registers the two activities under the names of their functions. Every kind of run
// registers them with its own workflow, and one worker serves several kinds: registering them
// again there replaces them with the same.
func Register(r Registry, activities *Activities) {
	options := activity.RegisterOptions{DisableAlreadyRegisteredCheck: true}
	r.RegisterActivityWithOptions(activities.RecordRunStarted, options)
	r.RegisterActivityWithOptions(activities.RecordRunEnded, options)
}

type RunStartedRequest struct {
	JobId     string
	RunId     string
	StartedAt time.Time
}

// RunEndedRequest carries the start as well: the API keeps a run whose start it was never
// told.
type RunEndedRequest struct {
	JobId     string
	RunId     string
	StartedAt time.Time
	EndedAt   time.Time
	// Outcome is one of OutcomeCompleted, OutcomeFailed and OutcomeCanceled.
	Outcome       string
	RowsRead      int64 `json:",omitempty"`
	RowsDiscarded int64 `json:",omitempty"`
	Retries       int64 `json:",omitempty"`
	// TablesUncounted is the number of tables whose rows were not counted.
	TablesUncounted int64 `json:",omitempty"`
	// SourceVersionMajor is the major version of the database the run read, as "16" or "8.0".
	SourceVersionMajor string `json:",omitempty"`
}

// RecordRunStarted tells the API that a run has begun.
func (a *Activities) RecordRunStarted(ctx context.Context, req *RunStartedRequest) error {
	_, err := a.usageclient.RecordRunStarted(ctx, connect.NewRequest(&mgmtv1alpha1.RecordRunStartedRequest{
		JobId:     req.JobId,
		RunId:     req.RunId,
		StartedAt: timestamppb.New(req.StartedAt),
	}))
	return reportFailure("the start", err)
}

// RecordRunEnded tells the API that a run has ended, and what it counted.
func (a *Activities) RecordRunEnded(ctx context.Context, req *RunEndedRequest) error {
	outcome, ok := outcomeOf(req.Outcome)
	if !ok {
		// Asking again would not make the outcome one the API knows.
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("%q is not the outcome of a run", req.Outcome), errorTypeUnknownOutcome, nil,
		)
	}
	_, err := a.usageclient.RecordRunEnded(ctx, connect.NewRequest(&mgmtv1alpha1.RecordRunEndedRequest{
		JobId:         req.JobId,
		RunId:         req.RunId,
		StartedAt:     timestamppb.New(req.StartedAt),
		EndedAt:       timestamppb.New(req.EndedAt),
		Outcome:       outcome,
		RowsRead:      req.RowsRead,
		RowsDiscarded: req.RowsDiscarded,
		Retries:       req.Retries,

		TablesUncounted:    req.TablesUncounted,
		SourceVersionMajor: req.SourceVersionMajor,
	}))
	return reportFailure("the end", err)
}

// reportFailure gives the failure of a call to the API as the activity fails with it. An answer
// that asking again cannot change is not retried: the API does not have the procedure (a worker
// newer than the API, in a rolling upgrade), or it refuses the request or the caller.
func reportFailure(what string, err error) error {
	if err == nil {
		return nil
	}
	failure := fmt.Errorf("unable to report %s of the run: %w", what, err)
	switch connect.CodeOf(err) {
	case connect.CodeUnimplemented, connect.CodeInvalidArgument, connect.CodePermissionDenied:
		return temporal.NewNonRetryableApplicationError(failure.Error(), errorTypeRefused, err)
	default:
		return failure
	}
}

func outcomeOf(outcome string) (mgmtv1alpha1.RunOutcome, bool) {
	switch outcome {
	case OutcomeCompleted:
		return mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED, true
	case OutcomeFailed:
		return mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED, true
	case OutcomeCanceled:
		return mgmtv1alpha1.RunOutcome_RUN_OUTCOME_CANCELED, true
	default:
		return mgmtv1alpha1.RunOutcome_RUN_OUTCOME_UNSPECIFIED, false
	}
}
