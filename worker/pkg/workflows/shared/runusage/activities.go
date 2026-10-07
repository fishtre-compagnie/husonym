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

const errorTypeUnknownOutcome = "UnknownRunOutcome"

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
}

// RecordRunStarted tells the API that a run has begun.
func (a *Activities) RecordRunStarted(ctx context.Context, req *RunStartedRequest) error {
	_, err := a.usageclient.RecordRunStarted(ctx, connect.NewRequest(&mgmtv1alpha1.RecordRunStartedRequest{
		JobId:     req.JobId,
		RunId:     req.RunId,
		StartedAt: timestamppb.New(req.StartedAt),
	}))
	if err != nil {
		return fmt.Errorf("unable to report the start of the run: %w", err)
	}
	return nil
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
	}))
	if err != nil {
		return fmt.Errorf("unable to report the end of the run: %w", err)
	}
	return nil
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
