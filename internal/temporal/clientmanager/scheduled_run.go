package clientmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	temporalclient "go.temporal.io/sdk/client"
)

// RunInProgressError says a run of the schedule is already going: the schedule skips an
// overlapping one, so no run was started.
type RunInProgressError struct {
	WorkflowId string
}

func (e *RunInProgressError) Error() string {
	return fmt.Sprintf("a run of the job is already in progress: %s", e.WorkflowId)
}

// ErrNoRunStarted says the schedule was triggered and no run of it appeared in time.
var ErrNoRunStarted = errors.New("the schedule was triggered but no run started")

const (
	scheduledRunPollEvery = 200 * time.Millisecond
	scheduledRunTimeout   = 10 * time.Second
)

// StartScheduledRun triggers the schedule of a job and returns the id of the workflow it
// started. The run goes through the schedule, as those of its cron do: it carries the
// schedule's attributes, by which the runs of a job are listed, and its overlap policy.
func (m *ClientManager) StartScheduledRun(
	ctx context.Context,
	accountId string,
	scheduleId string,
	logger *slog.Logger,
) (string, error) {
	schedclient, closeClient, err := m.createScheduleClient(ctx, accountId, logger)
	if err != nil {
		return "", err
	}
	defer closeClient()
	return startScheduledRun(ctx, schedclient.GetHandle(ctx, scheduleId), scheduledRunPollEvery, scheduledRunTimeout)
}

// startScheduledRun triggers the schedule, then looks among its recent actions for one it did
// not have before: the schedule gives no id when triggered. A run already going is told
// rather than triggered, since the schedule would skip the new one. Should the cron start one
// between the look and the trigger, that run is the one returned: it started after the ask.
func startScheduledRun(
	ctx context.Context,
	handle temporalclient.ScheduleHandle,
	pollEvery, timeout time.Duration,
) (string, error) {
	before, err := handle.Describe(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to describe the schedule: %w", err)
	}
	if running := before.Info.RunningWorkflows; len(running) > 0 {
		return "", &RunInProgressError{WorkflowId: running[0].WorkflowID}
	}
	known := map[string]bool{}
	for _, action := range before.Info.RecentActions {
		if action.StartWorkflowResult != nil {
			known[action.StartWorkflowResult.WorkflowID] = true
		}
	}

	if err := handle.Trigger(ctx, temporalclient.ScheduleTriggerOptions{}); err != nil {
		return "", fmt.Errorf("unable to trigger the schedule: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		after, err := handle.Describe(ctx)
		if err != nil && ctx.Err() == nil {
			return "", fmt.Errorf("unable to describe the schedule: %w", err)
		}
		if err == nil {
			for _, action := range after.Info.RecentActions {
				if action.StartWorkflowResult != nil && !known[action.StartWorkflowResult.WorkflowID] {
					return action.StartWorkflowResult.WorkflowID, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ErrNoRunStarted
		case <-ticker.C:
		}
	}
}
