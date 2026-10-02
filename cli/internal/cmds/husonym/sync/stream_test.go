package sync_cmd

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	benthosstream "github.com/fishtre-compagnie/husonym/internal/benthos-stream"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	_ "github.com/redpanda-data/benthos/v4/public/components/pure"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const endlessConfig = `
input:
  generate:
    count: 0
    interval: 1ms
    mapping: 'root = {"id": counter()}'
output:
  counting: {}
logger:
  level: OFF
`

// countingOutput counts the rows a stream writes.
type countingOutput struct{ written *atomic.Int64 }

func (countingOutput) Connect(context.Context) error { return nil }
func (o countingOutput) Write(context.Context, *service.Message) error {
	o.written.Add(1)
	return nil
}
func (countingOutput) Close(context.Context) error { return nil }

// countingEnv returns an environment whose streams count the rows they write.
func countingEnv(t *testing.T) (*service.Environment, *atomic.Int64) {
	t.Helper()
	written := &atomic.Int64{}
	env := service.NewEnvironment()
	require.NoError(t, env.RegisterOutput("counting", service.NewConfigSpec(),
		func(*service.ParsedConfig, *service.Resources) (service.Output, int, error) {
			return countingOutput{written: written}, 1, nil
		}))
	return env, written
}

// endlessStream reads rows without end and counts those it writes.
func endlessStream(t *testing.T) (benthosstream.BenthosStreamClient, *atomic.Int64) {
	t.Helper()
	env, written := countingEnv(t)
	stream, err := newStream(env, endlessConfig, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.StopWithin(time.Second) })
	return benthosstream.NewBenthosStreamAdapter(stream), written
}

// stillWrites says whether the stream goes on writing rows.
func stillWrites(written *atomic.Int64) bool {
	before := written.Load()
	time.Sleep(300 * time.Millisecond)
	return written.Load() > before
}

// A table whose config is refused does not hold the tables that come after it: streams are
// built one at a time, under a lock.
func Test_newStream_RefusedConfigDoesNotHoldTheNext(t *testing.T) {
	env, _ := countingEnv(t)

	built := make(chan error, 1)
	go func() {
		_, refused := newStream(env, "input: [", nil)
		if refused == nil {
			built <- errors.New("the config was not refused")
			return
		}
		_, err := newStream(env, endlessConfig, nil)
		built <- err
	}()
	select {
	case err := <-built:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the next stream waits for a lock the refused one kept")
	}
}

func Test_newStream_NoEnvironment(t *testing.T) {
	// A config any environment would build.
	_, err := newStream(nil, "input:\n  generate:\n    mapping: 'root = {}'\noutput:\n  drop: {}\n", nil)
	require.ErrorContains(t, err, "benthos env is nil")
}

// Once a table has failed, the sync is canceled and the tables still queued are each given
// their turn: none of them may start a stream, which nobody would stop.
func Test_runStream_EndedSyncDoesNotStartAStream(t *testing.T) {
	stream, written := endlessStream(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, runStream(ctx, stream, testutil.GetTestLogger(t)), context.Canceled)
	require.False(t, stillWrites(written), "the stream goes on alone after the sync ended")
}

// It is not even asked to run.
func Test_runStream_EndedSyncDoesNotRunItsStream(t *testing.T) {
	stream := benthosstream.NewMockBenthosStreamClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, runStream(ctx, stream, testutil.GetTestLogger(t)), context.Canceled)
}

// A sync canceled while a table is being written stops its stream.
func Test_runStream_CanceledWhileItRuns(t *testing.T) {
	stream, written := endlessStream(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runStream(ctx, stream, testutil.GetTestLogger(t)) }()
	require.Eventually(t, func() bool { return written.Load() > 0 }, 5*time.Second, 10*time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the stream did not end with the sync")
	}
	require.False(t, stillWrites(written), "the stream goes on alone after the sync ended")
}

// A stream whose run fails is stopped: Run may return and leave it going.
func Test_runStream_FailedRunIsStopped(t *testing.T) {
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().Run(mock.Anything).Return(errors.New("the stream failed"))
	stream.EXPECT().StopWithin(benthosstream.CloseBudget).Return(nil).Once()

	err := runStream(context.Background(), stream, testutil.GetTestLogger(t))
	require.ErrorContains(t, err, "unable to run benthos stream: the stream failed")
}

// A stream that ends well is asked nothing more.
func Test_runStream_DoneIsNotStopped(t *testing.T) {
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().Run(mock.Anything).Return(nil)

	require.NoError(t, runStream(context.Background(), stream, testutil.GetTestLogger(t)))
}
