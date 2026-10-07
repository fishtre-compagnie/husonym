package usagesettle

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	var open []usagestore.OpenRun
	for _, run := range f.open {
		// As the store answers: only a run that started before the given time. A run with no
		// start time in a test is old enough.
		if run.StartedAt.IsZero() || run.StartedAt.Before(before) {
			open = append(open, run)
		}
	}
	return open, nil
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

func Test_SettleOnce_AsksOnlyForRunsOlderThanAnHour(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	runs := &fakeRuns{open: []usagestore.OpenRun{
		{RunId: "recent", StartedAt: now.Add(-59 * time.Minute)},
		{RunId: "silent", StartedAt: now.Add(-61 * time.Minute)},
	}}
	answers := map[string]answer{
		"recent": {status: usagestore.StatusTimedOut, found: true},
		"silent": {status: usagestore.StatusTimedOut, found: true},
	}
	settle(t, runs, answers, now)
	require.Equal(t, now.Add(-time.Hour), runs.before)
	require.Len(t, runs.settled, 1)
	require.Equal(t, "silent", runs.settled[0].runId)
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

func Test_TemporalFate_ANotFoundThatIsNotTheWorkflowIsAnError(t *testing.T) {
	for _, notWorkflow := range []error{
		serviceerror.NewNamespaceNotFound("ns"),
		status.Error(codes.NotFound, "something else"),
	} {
		describe := func(context.Context, string, string, *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return nil, notWorkflow
		}
		_, _, found, err := TemporalFate(describe, slog.Default())(t.Context(), "acc", "run")
		require.ErrorIs(t, err, notWorkflow)
		require.False(t, found)
	}
}

func Test_SettleOnce_ANamespaceThatIsMissingLeavesTheRun(t *testing.T) {
	describe := func(context.Context, string, string, *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
		return nil, serviceerror.NewNamespaceNotFound("ns")
	}
	runs := &fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}}}
	require.NoError(t, New(runs, TemporalFate(describe, slog.Default()), slog.Default()).SettleOnce(t.Context(), time.Now()))
	require.Empty(t, runs.settled)
}

func Test_SettleOnce_ACallThatHangsLeavesTheRunAndTheNextIsSettled(t *testing.T) {
	runs := &fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}, {RunId: "b"}}}
	fate := func(ctx context.Context, _, runId string) (usagestore.Status, *time.Time, bool, error) {
		if runId == "a" {
			<-ctx.Done()
			return "", nil, false, ctx.Err()
		}
		return usagestore.StatusCompleted, nil, true, nil
	}
	s := New(runs, fate, slog.Default())
	s.fateTimeout = 20 * time.Millisecond
	require.NoError(t, s.SettleOnce(t.Context(), time.Now()))
	require.Equal(t, []settled{{"b", usagestore.StatusCompleted, nil}}, runs.settled)
}

type failingSettle struct{ fakeRuns }

func (f *failingSettle) Settle(ctx context.Context, runId string, status usagestore.Status, endedAt *time.Time) error {
	if runId == "a" {
		return errors.New("database is down")
	}
	return f.fakeRuns.Settle(ctx, runId, status, endedAt)
}

func Test_SettleOnce_ASettleThatFailsIsLoggedAndTheNextIsSettled(t *testing.T) {
	runs := &failingSettle{fakeRuns{open: []usagestore.OpenRun{{RunId: "a"}, {RunId: "b"}}}}
	answers := map[string]answer{
		"a": {status: usagestore.StatusFailed, found: true},
		"b": {status: usagestore.StatusFailed, found: true},
	}
	require.NoError(t, New(runs, fateOf(answers), slog.Default()).SettleOnce(t.Context(), time.Now()))
	require.Equal(t, []settled{{"b", usagestore.StatusFailed, nil}}, runs.settled)
}

func Test_TemporalFate_OtherErrorsAreReturned(t *testing.T) {
	boom := errors.New("boom")
	describe := func(context.Context, string, string, *slog.Logger) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
		return nil, boom
	}
	_, _, _, err := TemporalFate(describe, slog.Default())(t.Context(), "acc", "run")
	require.ErrorIs(t, err, boom)
}

// panickingRuns panics on its first listing and counts the others.
type panickingRuns struct {
	fakeRuns
	mu    sync.Mutex
	calls int
}

func (p *panickingRuns) OpenRunsStartedBefore(context.Context, time.Time) ([]usagestore.OpenRun, error) {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	if first {
		panic("a value that must not be logged")
	}
	return nil, nil
}

func (p *panickingRuns) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// syncBuffer is a log sink the loop may write to while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func Test_Every_APassThatPanicsDoesNotStopTheLoop(t *testing.T) {
	var logs syncBuffer
	runs := &panickingRuns{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		New(runs, fateOf(nil), slog.New(slog.NewTextHandler(&logs, nil))).Every(ctx, time.Millisecond)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool { return runs.count() >= 2 }, 5*time.Second, time.Millisecond)
	require.Contains(t, logs.String(), "panicked=true")
	require.NotContains(t, logs.String(), "must not be logged")
}
