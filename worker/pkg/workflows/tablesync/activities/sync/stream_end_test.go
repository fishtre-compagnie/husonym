package sync_activity

import (
	"context"
	"errors"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// A write that fails critically signals the stop, then is acknowledged, and the stream may end
// before the stop is heard: the activity has failed all the same. Whichever of the two the
// monitor sees first, it reports the failure.
func Test_monitorActivityHeartbeat_StreamDoneAfterAStop(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	for range 200 {
		stopActivityChan := make(chan error, 3)
		streamDone := make(chan error, 1)
		stopActivityChan <- errors.New(`null value in column "obligatoire" violates not-null constraint`)
		streamDone <- nil

		result := make(chan error, 2)
		monitorActivityHeartbeat(context.Background(), stopActivityChan, streamDone,
			func(_ string, err error) { result <- err },
			func(err error) { result <- err },
			logger)

		require.ErrorContains(t, <-result, "violates not-null constraint")
	}
}

// A stream done with no stop signal succeeds; one that failed reports its own error.
func Test_monitorActivityHeartbeat_StreamDone(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	for _, streamErr := range []error{nil, errors.New("the stream failed")} {
		streamDone := make(chan error, 1)
		streamDone <- streamErr
		result := make(chan error, 1)
		monitorActivityHeartbeat(context.Background(), make(chan error, 3), streamDone,
			func(_ string, err error) { result <- err },
			func(err error) { result <- err },
			logger)
		require.Equal(t, streamErr, <-result)
	}
}

// A stream that failed with a stop signal waiting reports the stop, which names the cause, with
// how the stream ended.
func Test_monitorActivityHeartbeat_FailedStreamKeepsTheStop(t *testing.T) {
	stopActivityChan := make(chan error, 3)
	stopActivityChan <- errors.New(`null value in column "obligatoire" violates not-null constraint`)
	streamDone := make(chan error, 1)
	streamDone <- errors.New("unable to run benthos stream: context canceled")
	result := make(chan error, 2)
	// The stop signal alone ready first would stop the stream: here it waits behind the end.
	monitorActivityHeartbeat(context.Background(), make(chan error, 3), streamDone,
		func(_ string, err error) { result <- err },
		func(err error) { result <- err },
		testutil.GetTestLogger(t))
	require.Equal(t, "unable to run benthos stream: context canceled", (<-result).Error())

	streamDone <- errors.New("unable to run benthos stream: context canceled")
	monitorActivityHeartbeat(context.Background(), stopActivityChan, streamDone,
		func(_ string, err error) { result <- err },
		func(err error) { result <- err },
		testutil.GetTestLogger(t))
	err := <-result
	require.ErrorContains(t, err, "violates not-null constraint")
}

// A monitor that panics still ends the activity, which waits for the end only it gives.
func Test_monitorActivityHeartbeat_PanicEndsTheActivity(t *testing.T) {
	stopActivityChan := make(chan error, 3)
	stopActivityChan <- errors.New("stop")
	result := make(chan error, 2)
	monitorActivityHeartbeat(context.Background(), stopActivityChan, make(chan error, 1),
		func(string, error) { panic("the stream would not stop") },
		func(err error) { result <- err },
		testutil.GetTestLogger(t))
	require.ErrorContains(t, <-result, "panic")
}
