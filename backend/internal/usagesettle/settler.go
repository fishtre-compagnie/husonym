// Package usagesettle closes the usage rows of the runs that never reported their end, a run
// that was terminated or timed out having no one left to say so. It asks the orchestrator what
// became of each of them.
package usagesettle

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
)

// settleAfter is how long a run may stay open before the orchestrator is asked about it.
const settleAfter = 24 * time.Hour

// Runs is the part of the usage store the settler uses.
type Runs interface {
	OpenRunsStartedBefore(ctx context.Context, before time.Time) ([]usagestore.OpenRun, error)
	Settle(ctx context.Context, runId string, status usagestore.Status, endedAt *time.Time) error
}

// Fate tells what became of a run: found is false when Temporal no longer knows it. A status
// of running means the run is not over, and is left as it is.
type Fate func(ctx context.Context, accountId, runId string) (status usagestore.Status, endedAt *time.Time, found bool, err error)

// DescribeFunc asks Temporal about the workflow of a run, as
// clientmanager.ClientManager.DescribeWorklowExecution does.
type DescribeFunc func(ctx context.Context, accountId, workflowId string, logger *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error)

// Settler settles the runs that stayed open.
type Settler struct {
	runs   Runs
	fate   Fate
	logger *slog.Logger
}

func New(runs Runs, fate Fate, logger *slog.Logger) *Settler {
	return &Settler{runs: runs, fate: fate, logger: logger}
}

// SettleOnce settles the runs still open that started more than a day before now. A run whose
// fate cannot be told is left for the next pass, and the others are still settled.
func (s *Settler) SettleOnce(ctx context.Context, now time.Time) error {
	open, err := s.runs.OpenRunsStartedBefore(ctx, now.Add(-settleAfter))
	if err != nil {
		return err
	}
	for _, run := range open {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status, endedAt, found, err := s.fate(ctx, run.AccountId, run.RunId)
		if err != nil {
			s.logger.Warn("could not tell what became of a run", "run_id", run.RunId, "error", err)
			continue
		}
		if !found {
			status, endedAt = usagestore.StatusTerminated, nil
		}
		if status == usagestore.StatusRunning {
			continue
		}
		if err := s.runs.Settle(ctx, run.RunId, status, endedAt); err != nil {
			s.logger.Warn("could not settle a run", "run_id", run.RunId, "error", err)
		}
	}
	return nil
}

// Every settles at the given interval until ctx is done, the first pass being at the first
// tick. A pass that fails is logged and the next one tries again.
func (s *Settler) Every(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SettleOnce(ctx, time.Now()); err != nil && ctx.Err() == nil {
				s.logger.Warn("could not settle the runs left open", "error", err)
			}
		}
	}
}

// TemporalFate reads the fate of a run from its workflow, whose id is the id of the run.
func TemporalFate(describe DescribeFunc, logger *slog.Logger) Fate {
	return func(ctx context.Context, accountId, runId string) (usagestore.Status, *time.Time, bool, error) {
		resp, err := describe(ctx, accountId, runId, logger)
		if err != nil {
			var notFound *serviceerror.NotFound
			if errors.As(err, &notFound) || husonymerrors.IsNotFound(err) {
				return "", nil, false, nil
			}
			return "", nil, false, err
		}
		info := resp.GetWorkflowExecutionInfo()
		status, ok := translate(info.GetStatus())
		if !ok {
			return usagestore.StatusRunning, nil, true, nil
		}
		var endedAt *time.Time
		if closed := info.GetCloseTime(); closed != nil {
			t := closed.AsTime()
			endedAt = &t
		}
		return status, endedAt, true, nil
	}
}

// translate gives the status of a run that is over; ok is false for one that is not, or that
// goes on in a new workflow.
func translate(status enumspb.WorkflowExecutionStatus) (usagestore.Status, bool) {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return usagestore.StatusCompleted, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return usagestore.StatusFailed, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return usagestore.StatusCanceled, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return usagestore.StatusTerminated, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return usagestore.StatusTimedOut, true
	default:
		return "", false
	}
}
