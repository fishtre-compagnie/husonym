package sync_cmd

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// loggingConfig is a stream that logs an error for each of its three rows, as the stream of a
// table that fails does.
const loggingConfig = `
input:
  generate:
    count: 3
    interval: ""
    mapping: 'root = {"id": counter()}'
pipeline:
  processors:
    - log:
        level: ERROR
        message: 'row ${! this.id } refused'
output:
  counting: {}
logger:
  level: ERROR
`

// terminal runs a function and tells what it wrote to the standard streams of the process. It
// replaces them while it runs: a test that calls it does not run beside the others.
func terminal(t *testing.T, run func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	require.NoError(t, err)
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = write, write
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()

	written := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		_, _ = io.Copy(&out, read)
		written <- out.String()
	}()
	run()
	require.NoError(t, write.Close())
	return <-written
}

func runToItsEnd(t *testing.T, stream *service.Stream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, stream.Run(ctx))
}

// A sync that shows a screen gives its streams no logger. Their errors are not written to the
// terminal for it, over the screen: the sync tells the error of a table that fails itself.
func Test_newStream_WithoutLoggerWritesNothingToTheTerminal(t *testing.T) {
	env, written := countingEnv(t)

	shown := terminal(t, func() {
		stream, err := newStream(env, loggingConfig, nil)
		require.NoError(t, err)
		runToItsEnd(t, stream)
	})

	require.EqualValues(t, 3, written.Load(), "the stream did not run")
	require.Empty(t, shown)
}

// A sync that shows no screen gives its streams its logger, which their errors are told to.
func Test_newStream_LogsToTheLoggerItIsGiven(t *testing.T) {
	env, _ := countingEnv(t)
	logs := &bytes.Buffer{}

	shown := terminal(t, func() {
		stream, err := newStream(env, loggingConfig, slog.New(slog.NewTextHandler(logs, nil)))
		require.NoError(t, err)
		runToItsEnd(t, stream)
	})

	require.Empty(t, shown)
	for _, refused := range []string{"row 1 refused", "row 2 refused", "row 3 refused"} {
		require.Contains(t, logs.String(), refused)
	}
}
