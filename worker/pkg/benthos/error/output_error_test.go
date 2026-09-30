package husonym_benthos_error

import (
	"context"
	"testing"

	_ "github.com/redpanda-data/benthos/v4/public/components/pure"

	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

func Test_ErrorOutputEmptyShutdown(t *testing.T) {
	conf := `
error_msg: "test error"
`
	spec := errorOutputSpec()
	env := service.NewEnvironment()

	config, err := spec.ParseYAML(conf, env)
	require.NoError(t, err)
	errorOutput, err := newErrorOutput(config, service.MockResources(), nil)
	require.NoError(t, err)
	require.NoError(t, errorOutput.Close(context.Background()))
}

func Test_ErrorOutputSendSignal(t *testing.T) {
	conf := `
error_msg: "${! meta(\"key\") }"
`
	spec := errorOutputSpec()
	env := service.NewEnvironment()

	config, err := spec.ParseYAML(conf, env)
	require.NoError(t, err)
	stopActivityChan := make(chan error, 1)
	errorOutput, err := newErrorOutput(config, service.MockResources(), stopActivityChan)
	require.NoError(t, err)
	msg := service.NewMessage([]byte("content"))
	msg.MetaSet("key", "duplicate key value violates unique constraint")

	batch := service.MessageBatch{msg}

	ctx := context.Background()
	err = errorOutput.WriteBatch(ctx, batch)
	require.NoError(t, err)
	out := <-stopActivityChan
	require.Error(t, out)
}

func Test_ErrorOutputMaxConnError(t *testing.T) {
	conf := `
error_msg: "${! meta(\"key\") }"
`
	spec := errorOutputSpec()
	env := service.NewEnvironment()

	config, err := spec.ParseYAML(conf, env)
	require.NoError(t, err)
	stopActivityChan := make(chan error, 1)
	errorOutput, err := newErrorOutput(config, service.MockResources(), stopActivityChan)
	require.NoError(t, err)
	msg := service.NewMessage([]byte("content"))
	msg.MetaSet("key", "too many clients already")

	batch := service.MessageBatch{msg}

	ctx := context.Background()
	err = errorOutput.WriteBatch(ctx, batch)
	require.Error(t, err)
}

// A write that fails critically signals the stop before it is acknowledged: when the stream
// ends, the signal is already there for the activity to find, which it relies on to fail.
func Test_ErrorOutput_SignalsBeforeTheStreamEnds(t *testing.T) {
	stopActivityChan := make(chan error, 3)
	env := service.NewEnvironment()
	require.NoError(t, RegisterErrorOutput(env, stopActivityChan))
	builder := env.NewStreamBuilder()
	require.NoError(t, builder.SetYAML(`
input:
  generate:
    count: 1
    interval: ""
    mapping: 'root = {"id": 1}'
output:
  fallback:
    - reject: 'null value in column "obligatoire" violates not-null constraint'
    - error:
        error_msg: ${! meta("fallback_error") }
        batching:
          count: 1
`))
	stream, err := builder.Build()
	require.NoError(t, err)

	require.NoError(t, stream.Run(context.Background()))
	select {
	case stop := <-stopActivityChan:
		require.ErrorContains(t, stop, "violates not-null constraint")
	default:
		t.Fatal("the stream ended before the stop signal")
	}
}
