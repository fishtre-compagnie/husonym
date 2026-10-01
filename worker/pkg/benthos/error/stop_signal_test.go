package husonym_benthos_error

import (
	"context"
	"testing"
	"time"

	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// stopChannelSize is the room the sync activity gives its stop channel.
const stopChannelSize = 3

// returnsInTime fails the test when work is still going after a moment: it waits on a channel
// nobody listens to anymore.
func returnsInTime(t *testing.T, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the stop signal waits for a listener: its sender is held for good")
	}
}

func failingBatch(size int, reason string) service.MessageBatch {
	batch := make(service.MessageBatch, 0, size)
	for range size {
		msg := service.NewMessage([]byte("content"))
		msg.MetaSet("key", reason)
		batch = append(batch, msg)
	}
	return batch
}

// The activity acts on the first stop signal and listens no more. Each failing message of a
// batch sends its own: those past the room of the channel must not hold the processor.
func Test_ErrorProcessor_DoesNotWaitForAListener(t *testing.T) {
	config, err := errorOutputSpec().ParseYAML(`error_msg: "${! meta(\"key\") }"`, service.NewEnvironment())
	require.NoError(t, err)
	stop := make(chan error, stopChannelSize)
	processor, err := newErrorProcessor(config, service.MockResources(), stop)
	require.NoError(t, err)

	returnsInTime(t, func() {
		_, err := processor.ProcessBatch(context.Background(), failingBatch(stopChannelSize+5, "Processor Error"))
		require.NoError(t, err)
	})
	require.EqualError(t, <-stop, "Processor Error", "the first signal is the one kept")
}

func Test_ErrorOutput_DoesNotWaitForAListener(t *testing.T) {
	config, err := errorOutputSpec().ParseYAML(`error_msg: "${! meta(\"key\") }"`, service.NewEnvironment())
	require.NoError(t, err)
	stop := make(chan error, stopChannelSize)
	output, err := newErrorOutput(config, service.MockResources(), stop)
	require.NoError(t, err)

	returnsInTime(t, func() {
		for range stopChannelSize + 5 {
			require.NoError(t, output.WriteBatch(context.Background(), failingBatch(1, "violates not-null constraint")))
		}
	})
	require.EqualError(t, <-stop, "violates not-null constraint")
}
