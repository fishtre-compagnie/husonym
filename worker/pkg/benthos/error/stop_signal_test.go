package husonym_benthos_error

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// stopChannelSize is the room the sync activity gives its stop channel.
const stopChannelSize = 3

// inTime returns what work returns, and fails the test when work is still going after a
// moment: it waits on a channel nobody listens to anymore.
func inTime(t *testing.T, work func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- work() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the stop signal waits for a listener: its sender is held for good")
		return nil
	}
}

// firstSignal returns the signal that waits on the channel, without waiting for one.
func firstSignal(t *testing.T, stop <-chan error) error {
	t.Helper()
	select {
	case err := <-stop:
		return err
	default:
		require.FailNow(t, "no stop signal was sent")
		return nil
	}
}

func failingMessage(reason string) *service.Message {
	msg := service.NewMessage([]byte("content"))
	msg.MetaSet("key", reason)
	return msg
}

// The activity acts on the first stop signal and listens no more. Each failing message of a
// batch sends its own: those past the room of the channel must not hold the processor.
func Test_ErrorProcessor_DoesNotWaitForAListener(t *testing.T) {
	config, err := errorOutputSpec().ParseYAML(`error_msg: "${! meta(\"key\") }"`, service.NewEnvironment())
	require.NoError(t, err)
	stop := make(chan error, stopChannelSize)
	processor, err := newErrorProcessor(config, service.MockResources(), stop)
	require.NoError(t, err)

	batch := service.MessageBatch{}
	for i := range stopChannelSize + 5 {
		batch = append(batch, failingMessage(fmt.Sprintf("failure %d", i)))
	}
	require.NoError(t, inTime(t, func() error {
		_, err := processor.ProcessBatch(context.Background(), batch)
		return err
	}))
	require.EqualError(t, firstSignal(t, stop), "failure 0", "the first signal is the one kept")
}

func Test_ErrorOutput_DoesNotWaitForAListener(t *testing.T) {
	config, err := errorOutputSpec().ParseYAML(`error_msg: "${! meta(\"key\") }"`, service.NewEnvironment())
	require.NoError(t, err)
	stop := make(chan error, stopChannelSize)
	output, err := newErrorOutput(config, service.MockResources(), stop)
	require.NoError(t, err)

	require.NoError(t, inTime(t, func() error {
		for i := range stopChannelSize + 5 {
			reason := fmt.Sprintf("row %d violates not-null constraint", i)
			if err := output.WriteBatch(context.Background(), service.MessageBatch{failingMessage(reason)}); err != nil {
				return err
			}
		}
		return nil
	}))
	require.EqualError(t, firstSignal(t, stop), "row 0 violates not-null constraint")
}

// A stream whose rows fail by the dozen, in its pipeline and at its output, ends all the same
// when nobody listens to the stop channel anymore — as the activity does once it has acted on
// the first signal.
func Test_Stream_EndsWhenManyRowsFail(t *testing.T) {
	stop := make(chan error, stopChannelSize)
	env := service.NewEnvironment()
	require.NoError(t, RegisterErrorOutput(env, stop))
	require.NoError(t, RegisterErrorProcessor(env, stop))
	builder := env.NewStreamBuilder()
	require.NoError(t, builder.SetYAML(`
input:
  generate:
    count: 50
    interval: ""
    mapping: 'root = {"id": counter()}'
pipeline:
  threads: 4
  processors:
    - mapping: 'root = if this.id % 2 == 0 { throw("row %d cannot be mapped".format(this.id)) } else { this }'
    - catch:
        - error:
            error_msg: ${! error() }
output:
  fallback:
    - reject: 'row ${! json("id") } violates not-null constraint'
    - error:
        error_msg: ${! meta("fallback_error") }
        batching:
          count: 1
`))
	stream, err := builder.Build()
	require.NoError(t, err)

	require.NoError(t, inTime(t, func() error { return stream.Run(context.Background()) }))
	require.Len(t, stop, stopChannelSize, "the channel keeps the first signals, and drops the rest")
}
