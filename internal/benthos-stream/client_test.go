package benthosstream

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/redpanda-data/benthos/v4/public/components/pure"
	"github.com/redpanda-data/benthos/v4/public/service"
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
func endlessStream(t *testing.T) (*service.Stream, *atomic.Int64) {
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
logger:
  level: OFF
`))
	stream, err := builder.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.StopWithin(time.Second) })
	return stream, written
}

// stillWrites says whether the stream goes on writing rows.
func stillWrites(written *atomic.Int64) bool {
	before := written.Load()
	time.Sleep(300 * time.Millisecond)
	return written.Load() > before
}

// A stream asked to stop before it ran never starts: whoever asked is gone by the time it
// would, and nobody would stop it.
func Test_Adapter_StoppedBeforeRunDoesNotStart(t *testing.T) {
	stream, written := endlessStream(t)
	adapter := NewBenthosStreamAdapter(stream)

	require.NoError(t, adapter.StopWithin(time.Millisecond), "a stream that never ran has nothing to stop")
	// Bounded: a stream that started would run until its context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.ErrorIs(t, adapter.Run(ctx), ErrStoppedBeforeRun)
	require.Zero(t, written.Load())
	require.False(t, stillWrites(written))
}

// A stop may come once Run was let through and before the stream exists: it is not heard. The
// stream is not taken for stopped then, and the stop asked again reaches it.
func Test_Adapter_StopTooEarlyIsHeardTheNextTime(t *testing.T) {
	stream, written := endlessStream(t)
	adapter := NewBenthosStreamAdapter(stream)

	// Run was let through, and the stream is not built yet.
	adapter.running = true
	require.Error(t, adapter.StopWithin(time.Millisecond), "the stream has nothing to hear a stop with yet")

	// The stream starts, as the rest of Run does, on a context already canceled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, stream.Run(ctx), context.Canceled)
	require.True(t, stillWrites(written), "the stream runs on after its Run returned")

	_ = adapter.StopWithin(time.Second)
	require.False(t, stillWrites(written), "the stream goes on alone: the second stop was lost")
}

// A stream that runs stops when asked, and a stop asked again asks nothing more of it.
func Test_Adapter_StopsARunningStream(t *testing.T) {
	stream, written := endlessStream(t)
	adapter := NewBenthosStreamAdapter(stream)

	done := make(chan error, 1)
	go func() { done <- adapter.Run(context.Background()) }()
	require.Eventually(t, func() bool { return written.Load() > 0 }, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, adapter.StopWithin(5*time.Second))
	require.NoError(t, <-done)
	require.False(t, stillWrites(written))
	require.NoError(t, adapter.Stop(context.Background()))
}
