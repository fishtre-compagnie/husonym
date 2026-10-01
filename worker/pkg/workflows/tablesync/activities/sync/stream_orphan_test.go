package sync_activity

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	benthosstream "github.com/fishtre-compagnie/husonym/internal/benthos-stream"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// countingOutput counts the rows a stream writes.
type countingOutput struct{ written *atomic.Int64 }

func (countingOutput) Connect(context.Context) error { return nil }
func (o countingOutput) Write(context.Context, *service.Message) error {
	o.written.Add(1)
	return nil
}
func (countingOutput) Close(context.Context) error { return nil }

// endlessStream reads rows without end and counts those it writes.
func endlessStream(t *testing.T) (benthosstream.BenthosStreamClient, *atomic.Int64) {
	t.Helper()
	written := &atomic.Int64{}
	env := service.NewEnvironment()
	require.NoError(t, env.RegisterOutput("counting", service.NewConfigSpec(),
		func(*service.ParsedConfig, *service.Resources) (service.Output, int, error) {
			return countingOutput{written: written}, 1, nil
		}))
	builder := env.NewStreamBuilder()
	require.NoError(t, builder.SetYAML(`
input:
  generate:
    count: 0
    interval: 1ms
    mapping: 'root = {"id": counter()}'
output:
  counting: {}
`))
	stream, err := builder.Build()
	require.NoError(t, err)
	client := benthosstream.NewBenthosStreamAdapter(stream)
	t.Cleanup(func() { _ = client.StopWithin(time.Second) })
	return client, written
}

// stillWrites says whether the stream goes on writing rows.
func stillWrites(written *atomic.Int64) bool {
	before := written.Load()
	time.Sleep(300 * time.Millisecond)
	return written.Load() > before
}

// An activity cancelled before its stream starts has no one left to stop it: the monitor acted
// on the cancellation when there was no stream yet. The stream must not start and go on alone,
// reading the source and writing the destination of a run that has failed.
func Test_runStream_CancelledBeforeItStarts(t *testing.T) {
	stream, written := endlessStream(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	streamDone := make(chan error, 1)
	runStream(stream, ctx, streamDone, testutil.GetTestLogger(t))

	require.ErrorIs(t, <-streamDone, context.Canceled)
	require.False(t, stillWrites(written), "the stream goes on alone after the activity ended")
}

// It is not even asked to run: it would connect to the databases.
func Test_runStream_EndedActivityDoesNotRunItsStream(t *testing.T) {
	stream := benthosstream.NewMockBenthosStreamClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	streamDone := make(chan error, 1)
	runStream(stream, ctx, streamDone, testutil.GetTestLogger(t))

	require.ErrorIs(t, <-streamDone, context.Canceled)
}

// The stream is stopped before its end is told: the activity returns on that end, and gives
// back the session the stream writes with.
func Test_runStream_StopsBeforeItTellsTheEnd(t *testing.T) {
	streamDone := make(chan error, 1)
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().Run(mock.Anything).Return(errors.New("the stream failed"))
	stream.EXPECT().StopWithin(streamStopBudget).RunAndReturn(func(time.Duration) error {
		require.Empty(t, streamDone, "the end was told before the stream stopped")
		return nil
	}).Once()

	runStream(stream, context.Background(), streamDone, testutil.GetTestLogger(t))

	require.ErrorContains(t, <-streamDone, "the stream failed")
}

// The monitor runs before the stream is built: a stop signal stops the stream the activity has
// set since, and gives the activity its result.
func Test_monitorStream_StopsTheStreamSetSinceItStarted(t *testing.T) {
	stop := make(chan error, 3)
	result := make(chan error, 1)
	shared := &sharedStream{}
	monitored := make(chan struct{})
	go func() {
		defer close(monitored)
		monitorStream(context.Background(), stop, make(chan error, 1), shared, result, testutil.GetTestLogger(t))
	}()

	// The monitor is given the time to start: it must read the stream when it acts, not then.
	time.Sleep(50 * time.Millisecond)
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().StopWithin(streamStopBudget).RunAndReturn(func(time.Duration) error {
		require.Empty(t, result, "the result was given before the stream stopped")
		return nil
	}).Once()
	shared.set(stream)
	cause := errors.New("violates not-null constraint")
	stop <- cause

	select {
	case <-monitored:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the monitor did not act on the stop signal")
	}
	require.Equal(t, cause, <-result)
}

// The same once it runs: whatever stops the activity, its stream stops with it.
func Test_runStream_CancelledWhileItRuns(t *testing.T) {
	stream, written := endlessStream(t)
	ctx, cancel := context.WithCancel(context.Background())

	streamDone := make(chan error, 1)
	go runStream(stream, ctx, streamDone, testutil.GetTestLogger(t))
	require.Eventually(t, func() bool { return written.Load() > 0 }, 5*time.Second, 10*time.Millisecond)
	cancel()

	require.ErrorIs(t, <-streamDone, context.Canceled)
	require.False(t, stillWrites(written), "the stream goes on alone after the activity ended")
}

// A stream that panics is stopped too, before its end is told, and the panic is what the
// activity reports.
func Test_runStream_PanicStopsTheStream(t *testing.T) {
	streamDone := make(chan error, 1)
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().Run(mock.Anything).RunAndReturn(func(context.Context) error { panic("boom") })
	stream.EXPECT().StopWithin(streamStopBudget).RunAndReturn(func(time.Duration) error {
		require.Empty(t, streamDone, "the end was told before the stream stopped")
		return nil
	}).Once()

	runStream(stream, context.Background(), streamDone, testutil.GetTestLogger(t))

	require.ErrorContains(t, <-streamDone, "panic in benthos stream: boom")
}

// A stop that panics, where the panic of the stream was recovered, does not end the worker.
func Test_runStream_PanicWhileStopping(t *testing.T) {
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().Run(mock.Anything).RunAndReturn(func(context.Context) error { panic("boom") })
	stream.EXPECT().StopWithin(streamStopBudget).RunAndReturn(func(time.Duration) error { panic("again") })

	streamDone := make(chan error, 1)
	runStream(stream, context.Background(), streamDone, testutil.GetTestLogger(t))

	require.ErrorContains(t, <-streamDone, "panic in benthos stream: boom")
}

// A stream that ends well was stopped by its own end: nothing more is asked of it.
func Test_runStream_DoneIsNotStoppedAgain(t *testing.T) {
	stream := benthosstream.NewMockBenthosStreamClient(t)
	stream.EXPECT().Run(mock.Anything).Return(nil)

	streamDone := make(chan error, 1)
	runStream(stream, context.Background(), streamDone, testutil.GetTestLogger(t))

	require.NoError(t, <-streamDone)
}

// The monitor reads the stream while the activity sets it.
func Test_sharedStream(t *testing.T) {
	shared := &sharedStream{}
	require.Nil(t, shared.get())
	stream := benthosstream.NewMockBenthosStreamClient(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		shared.set(stream)
	}()
	_ = shared.get()
	<-done
	require.Equal(t, stream, shared.get())
}
