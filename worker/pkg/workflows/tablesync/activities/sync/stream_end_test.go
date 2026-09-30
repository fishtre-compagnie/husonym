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
