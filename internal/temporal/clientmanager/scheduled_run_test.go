package clientmanager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	temporalclient "go.temporal.io/sdk/client"
)

// fakeSchedule is a schedule whose trigger starts a workflow a few looks later, or none.
type fakeSchedule struct {
	temporalclient.ScheduleHandle

	mu        sync.Mutex
	actions   []string
	running   []string
	starts    string // the workflow the trigger starts; none if empty
	lookLater int    // how many looks after the trigger it appears
	triggered bool
	looks     int
}

func (f *fakeSchedule) Describe(context.Context) (*temporalclient.ScheduleDescription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.triggered && f.starts != "" {
		f.looks++
		if f.looks > f.lookLater {
			f.actions = append(f.actions, f.starts)
			f.starts = ""
		}
	}
	info := temporalclient.ScheduleInfo{}
	for _, id := range f.actions {
		info.RecentActions = append(info.RecentActions, temporalclient.ScheduleActionResult{
			StartWorkflowResult: &temporalclient.ScheduleWorkflowExecution{WorkflowID: id},
		})
	}
	for _, id := range f.running {
		info.RunningWorkflows = append(info.RunningWorkflows, temporalclient.ScheduleWorkflowExecution{WorkflowID: id})
	}
	return &temporalclient.ScheduleDescription{Info: info}, nil
}

func (f *fakeSchedule) Trigger(context.Context, temporalclient.ScheduleTriggerOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggered = true
	return nil
}

// The schedule gives no id when triggered: the run is the action that was not there before,
// however long it takes to appear.
func Test_startScheduledRun(t *testing.T) {
	ctx := context.Background()

	t.Run("the new action", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []string{"job-2026-09-29T00:00:00Z"}, starts: "job-2026-09-30T10:00:00Z", lookLater: 3}
		id, err := startScheduledRun(ctx, schedule, time.Millisecond, time.Second)
		require.NoError(t, err)
		require.Equal(t, "job-2026-09-30T10:00:00Z", id)
	})

	t.Run("a run already going", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []string{"job-running"}, running: []string{"job-running"}}
		_, err := startScheduledRun(ctx, schedule, time.Millisecond, time.Second)
		var inProgress *RunInProgressError
		require.ErrorAs(t, err, &inProgress)
		require.Equal(t, "job-running", inProgress.WorkflowId)
		require.False(t, schedule.triggered, "the schedule would skip it")
	})

	t.Run("no run appears", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []string{"job-before"}}
		_, err := startScheduledRun(ctx, schedule, time.Millisecond, 20*time.Millisecond)
		require.True(t, errors.Is(err, ErrNoRunStarted), "%v", err)
		require.True(t, schedule.triggered)
	})
}
