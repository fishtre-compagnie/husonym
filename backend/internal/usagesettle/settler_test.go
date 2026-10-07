package usagesettle

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type settled struct {
	runId   string
	status  usagestore.Status
	endedAt *time.Time
}

type fakeRuns struct {
	open    []usagestore.OpenRun
	before  time.Time
	settled []settled
}

func (f *fakeRuns) OpenRunsStartedBefore(_ context.Context, before time.Time) ([]usagestore.OpenRun, error) {
	f.before = before
	return f.open, nil
}

func (f *fakeRuns) Settle(_ context.Context, runId string, status usagestore.Status, endedAt *time.Time) error {
	f.settled = append(f.settled, settled{runId, status, endedAt})
	return nil
}

type answer struct {
	status  usagestore.Status
	endedAt *time.Time
	found   bool
	err     error
}

func fateOf(answers map[string]answer) Fate {
	return func(_ context.Context, _, runId string) (usagestore.Status, *time.Time, bool, error) {
		a := answers[runId]
		return a.status, a.endedAt, a.found, a.err
	}
}

func settle(t *testing.T, runs *fakeRuns, answers map[string]answer, now time.Time) {
	t.Helper()
	require.NoError(t, New(runs, fateOf(answers), slog.Default()).SettleOnce(t.Context(), now))
}

func Test_SettleOnce_AsksOnlyForRunsOlderThanADay(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	runs := &fakeRuns{}
	settle(t, runs, nil, now)
	require.Equal(t, now.Add(-24*time.Hour), runs.before)
}

func Test_SettleOnce_LeavesARunThatIsStillGoing(t *testing.T) {
	runs := &fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}}}
	settle(t, runs, map[string]answer{"a": {status: usagestore.StatusRunning, found: true}}, time.Now())
	require.Empty(t, runs.settled)
}

func Test_SettleOnce_SettlesAClosedRunWithItsEnd(t *testing.T) {
	end := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
	runs := &fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}}}
	settle(t, runs, map[string]answer{"a": {status: usagestore.StatusTimedOut, endedAt: &end, found: true}}, time.Now())
	require.Equal(t, []settled{{"a", usagestore.StatusTimedOut, &end}}, runs.settled)
}

func Test_SettleOnce_TerminatesARunTemporalDoesNotKnow(t *testing.T) {
	runs := &fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}}}
	settle(t, runs, map[string]answer{"a": {found: false}}, time.Now())
	require.Equal(t, []settled{{"a", usagestore.StatusTerminated, nil}}, runs.settled)
}

func Test_SettleOnce_AnErrorLeavesTheRunAndTheNextIsSettled(t *testing.T) {
	runs := &fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}, {RunId: "b"}}}
	settle(t, runs, map[string]answer{
		"a": {err: errors.New("temporal is down")},
		"b": {status: usagestore.StatusFailed, found: true},
	}, time.Now())
	require.Equal(t, []settled{{"b", usagestore.StatusFailed, nil}}, runs.settled)
}

func describing(status enumspb.WorkflowExecutionStatus, closed *time.Time) DescribeFunc {
	return func(context.Context, string, string, *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
		info := &workflowpb.WorkflowExecutionInfo{Status: status}
		if closed != nil {
			info.CloseTime = timestamppb.New(*closed)
		}
		return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: info}, nil
	}
}

func Test_TemporalFate_TranslatesEachStatus(t *testing.T) {
	closed := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
	cases := map[enumspb.WorkflowExecutionStatus]usagestore.Status{
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:  usagestore.StatusCompleted,
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:     usagestore.StatusFailed,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:   usagestore.StatusCanceled,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED: usagestore.StatusTerminated,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:  usagestore.StatusTimedOut,
	}
	for temporal, want := range cases {
		status, endedAt, found, err := TemporalFate(describing(temporal, &closed), slog.Default())(t.Context(), "acc", "run")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, want, status, temporal.String())
		require.NotNil(t, endedAt)
		require.True(t, closed.Equal(*endedAt))
	}
}

func Test_TemporalFate_LeavesARunThatIsNotOver(t *testing.T) {
	for _, temporal := range []enumspb.WorkflowExecutionStatus{
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
		enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED,
	} {
		status, _, found, err := TemporalFate(describing(temporal, nil), slog.Default())(t.Context(), "acc", "run")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, usagestore.StatusRunning, status, temporal.String())
	}
}

func Test_TemporalFate_UnknownWorkflowIsNotFound(t *testing.T) {
	for _, notFound := range []error{serviceerror.NewNotFound("gone"), husonymerrors.NewNotFound("gone")} {
		describe := func(context.Context, string, string, *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return nil, notFound
		}
		_, _, found, err := TemporalFate(describe, slog.Default())(t.Context(), "acc", "run")
		require.NoError(t, err)
		require.False(t, found)
	}
}

func Test_TemporalFate_OtherErrorsAreReturned(t *testing.T) {
	boom := errors.New("boom")
	describe := func(context.Context, string, string, *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
		return nil, boom
	}
	_, _, _, err := TemporalFate(describe, slog.Default())(t.Context(), "acc", "run")
	require.ErrorIs(t, err, boom)
}
