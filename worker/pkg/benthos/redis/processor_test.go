package benthos_redis

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// answering answers the commands of a client in place of a server: it fails the first
// failures of them with failure, and any command whose context has ended.
type answering struct {
	failures int64
	failure  error
	calls    *atomic.Int64
}

func (answering) DialHook(next redis.DialHook) redis.DialHook { return next }
func (a answering) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		call := a.calls.Add(1)
		err := ctx.Err()
		if err == nil && call <= a.failures {
			err = a.failure
		}
		cmd.SetErr(err)
		return err
	}
}
func (answering) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// errDown is how the server of the tests fails a command.
var errDown = errors.New("redis is down")

// newTestProcessor builds a processor from its configuration, whose commands a server that
// fails the first failures of them answers. It tells the commands sent, and what is logged.
func newTestProcessor(t *testing.T, config string, failures int64) (*redisProc, *atomic.Int64, *bytes.Buffer) {
	t.Helper()
	return newTestProcessorFailing(t, config, failures, errDown)
}

func newTestProcessorFailing(
	t *testing.T,
	config string,
	failures int64,
	failure error,
) (*redisProc, *atomic.Int64, *bytes.Buffer) {
	t.Helper()
	calls, logs := &atomic.Int64{}, &bytes.Buffer{}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(answering{failures: failures, failure: failure, calls: calls})

	conf, err := redisProcConfig().ParseYAML(config, nil)
	require.NoError(t, err)
	logger := service.NewLoggerFromSlog(slog.New(slog.NewTextHandler(logs, nil)))
	proc, err := newRedisProcFromConfig(conf, service.MockResources(service.MockResourcesOptUseLogger(logger)), client)
	require.NoError(t, err)
	return proc, calls, logs
}

func rows(count int) service.MessageBatch {
	batch := make(service.MessageBatch, 0, count)
	for range count {
		batch = append(batch, service.NewMessage([]byte(`{"id": 1}`)))
	}
	return batch
}

const (
	rawCommand = `
command: hget
args_mapping: 'root = ["parents", "1"]'
`
	operator = `
operator: scard
key: parents
`
)

// A stream that stops ends the context of its commands: none is tried again, and the rows of
// the batch do not each sit through the waits between tries.
func Test_redisProc_AnEndedContextIsNotTriedAgain(t *testing.T) {
	for name, config := range map[string]string{"command": rawCommand, "operator": operator} {
		t.Run(name, func(t *testing.T) {
			proc, calls, logs := newTestProcessor(t, config, 0)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			start := time.Now()
			out, err := proc.ProcessBatch(ctx, rows(5))

			require.NoError(t, err)
			require.Less(t, time.Since(start), 400*time.Millisecond, "the batch sat through the waits between tries")
			require.EqualValues(t, 5, calls.Load(), "a command whose context has ended was tried again")
			require.Len(t, out, 1)
			for _, row := range out[0] {
				require.ErrorIs(t, row.GetError(), context.Canceled)
			}
			// A stop is no failure of Redis: it is not logged as one, row after row.
			require.NotContains(t, logs.String(), "command failed")
		})
	}
}

// The wait between two tries ends with the context.
func Test_redisProc_TheWaitEndsWithTheContext(t *testing.T) {
	for name, config := range map[string]string{"command": rawCommand, "operator": operator} {
		t.Run(name, func(t *testing.T) {
			proc, calls, _ := newTestProcessor(t, config+"retry_period: 10s\n", 100)
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(50*time.Millisecond, cancel)

			start := time.Now()
			out, err := proc.ProcessBatch(ctx, rows(1))

			require.NoError(t, err)
			require.Less(t, time.Since(start), 5*time.Second, "the wait outlived the context")
			require.EqualValues(t, 1, calls.Load())
			require.Error(t, out[0][0].GetError())
		})
	}
}

// A command that fails is tried again while its context lives, until it succeeds.
func Test_redisProc_AFailedCommandIsTriedAgain(t *testing.T) {
	for name, config := range map[string]string{"command": rawCommand, "operator": operator} {
		t.Run(name, func(t *testing.T) {
			proc, calls, logs := newTestProcessor(t, config+"retry_period: 1ms\n", 2)

			out, err := proc.ProcessBatch(context.Background(), rows(1))

			require.NoError(t, err)
			require.NoError(t, out[0][0].GetError())
			require.EqualValues(t, 3, calls.Load())
			require.Contains(t, logs.String(), "command failed: redis is down")
		})
	}
}

// A command that keeps failing is given up, and its row told in error.
func Test_redisProc_ACommandThatKeepsFailingIsGivenUp(t *testing.T) {
	proc, calls, _ := newTestProcessor(t, rawCommand+"retry_period: 1ms\nretries: 2\n", 100)

	out, err := proc.ProcessBatch(context.Background(), rows(1))

	require.NoError(t, err)
	require.ErrorIs(t, out[0][0].GetError(), errDown)
	// One try, then one more than the retries asked: the count of the processor this one is
	// adapted from.
	require.EqualValues(t, 4, calls.Load())
}
