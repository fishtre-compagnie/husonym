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

type execution = temporalclient.ScheduleWorkflowExecution

// fakeSchedule is a schedule whose trigger starts a workflow a few looks later, or none.
type fakeSchedule struct {
	temporalclient.ScheduleHandle

	mu        sync.Mutex
	actions   []execution
	running   []execution
	starts    *execution // what the trigger starts; nothing if nil
	lookLater int        // how many looks after the trigger it appears
	failLooks int        // how many looks after the trigger fail
	triggered bool
	looks     int
}

func (f *fakeSchedule) Describe(context.Context) (*temporalclient.ScheduleDescription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.triggered {
		f.looks++
		if f.looks <= f.failLooks {
			return nil, errors.New("unavailable")
		}
		if f.starts != nil && f.looks > f.lookLater {
			f.actions = append(f.actions, *f.starts)
			f.starts = nil
		}
	}
	info := temporalclient.ScheduleInfo{RunningWorkflows: f.running}
	for _, e := range f.actions {
		info.RecentActions = append(info.RecentActions, temporalclient.ScheduleActionResult{StartWorkflowResult: &e})
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
	earlier := execution{WorkflowID: "job-2026-09-30T10:00:00Z", FirstExecutionRunID: "run-1"}

	t.Run("the new action", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []execution{earlier}, lookLater: 3,
			starts: &execution{WorkflowID: "job-2026-09-30T10:00:05Z", FirstExecutionRunID: "run-2"}}
		id, err := startScheduledRun(ctx, schedule, time.Millisecond, time.Second)
		require.NoError(t, err)
		require.Equal(t, "job-2026-09-30T10:00:05Z", id)
	})

	t.Run("a run in the same second as the last one", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []execution{earlier},
			starts: &execution{WorkflowID: earlier.WorkflowID, FirstExecutionRunID: "run-2"}}
		id, err := startScheduledRun(ctx, schedule, time.Millisecond, time.Second)
		require.NoError(t, err)
		require.Equal(t, earlier.WorkflowID, id)
	})

	t.Run("looks that fail for a while", func(t *testing.T) {
		schedule := &fakeSchedule{failLooks: 2,
			starts: &execution{WorkflowID: "job-2026-09-30T10:00:05Z", FirstExecutionRunID: "run-2"}}
		id, err := startScheduledRun(ctx, schedule, time.Millisecond, time.Second)
		require.NoError(t, err)
		require.Equal(t, "job-2026-09-30T10:00:05Z", id)
	})

	t.Run("a run already going", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []execution{earlier}, running: []execution{earlier}}
		_, err := startScheduledRun(ctx, schedule, time.Millisecond, time.Second)
		var inProgress *RunInProgressError
		require.ErrorAs(t, err, &inProgress)
		require.Equal(t, earlier.WorkflowID, inProgress.WorkflowId)
		require.False(t, schedule.triggered, "the schedule would skip it")
	})

	t.Run("no run appears", func(t *testing.T) {
		schedule := &fakeSchedule{actions: []execution{earlier}}
		_, err := startScheduledRun(ctx, schedule, time.Millisecond, 20*time.Millisecond)
		require.ErrorIs(t, err, ErrNoRunStarted)
		require.True(t, schedule.triggered)
	})

	t.Run("the caller gives up", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		schedule := &fakeSchedule{}
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		_, err := startScheduledRun(canceled, schedule, time.Millisecond, time.Minute)
		require.ErrorIs(t, err, context.Canceled)
	})
}
