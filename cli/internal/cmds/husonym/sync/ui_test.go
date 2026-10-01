package sync_cmd

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/output"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// tableConfig is the config of one table, which generates rows and drops them; mapping is the
// one of its input, and a mapping that does not parse makes the table fail.
func tableConfig(name, mapping string) *benthosbuilder.BenthosConfigResponse {
	return &benthosbuilder.BenthosConfigResponse{
		Name: name,
		Config: &husonym_benthos.BenthosConfig{StreamConfig: husonym_benthos.StreamConfig{
			Input: &husonym_benthos.InputConfig{Inputs: husonym_benthos.Inputs{
				Generate: &husonym_benthos.Generate{Mapping: mapping, Interval: "1ms", Count: 1},
			}},
			Pipeline: &husonym_benthos.PipelineConfig{Threads: 1, Processors: []husonym_benthos.ProcessorConfig{}},
			Output: &husonym_benthos.OutputConfig{Outputs: husonym_benthos.Outputs{
				Error: &husonym_benthos.ErrorOutputConfig{ErrorMsg: "unused"},
			}},
		}},
	}
}

// droppingEnv returns an environment whose "error" output, the plainest one a table config can
// name, drops the rows it is given.
func droppingEnv(t *testing.T) *service.Environment {
	t.Helper()
	env, _ := droppingEnvCounted(t)
	return env
}

// droppingEnvCounted does the same, and counts the rows dropped.
func droppingEnvCounted(t *testing.T) (*service.Environment, *atomic.Int64) {
	t.Helper()
	written := &atomic.Int64{}
	env := service.NewEnvironment()
	spec := service.NewConfigSpec().
		Field(service.NewStringField("error_msg")).
		Field(service.NewBoolField("is_generate_job"))
	require.NoError(t, env.RegisterOutput("error", spec,
		func(*service.ParsedConfig, *service.Resources) (service.Output, int, error) {
			return countingOutput{written: written}, 1, nil
		}))
	return env, written
}

func runSyncInTime(t *testing.T, groups [][]*benthosbuilder.BenthosConfigResponse) error {
	t.Helper()
	return runSyncInTimeWith(t, droppingEnv(t), groups)
}

func runSyncInTimeWith(t *testing.T, env *service.Environment, groups [][]*benthosbuilder.BenthosConfigResponse) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- runSync(context.Background(), output.PlainOutput, env, groups, testutil.GetTestLogger(t))
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(20 * time.Second):
		require.FailNow(t, "the sync does not end")
		return nil
	}
}

// A sync whose table fails ends, and tells so: it used to go on waiting for good, with no
// table left to sync.
func Test_runSync_AFailedTableEndsTheSync(t *testing.T) {
	failing := tableConfig("public.broken.insert", "root = (((")
	later := tableConfig("public.later.insert", `root = {"id": 1}`)

	err := runSyncInTime(t, [][]*benthosbuilder.BenthosConfigResponse{{failing}, {later}})

	require.ErrorContains(t, err, "unable to finish syncing data")
}

// A sync whose tables all succeed ends without an error, group after group.
func Test_runSync_SyncsEveryGroup(t *testing.T) {
	first := tableConfig("public.first.insert", `root = {"id": 1}`)
	second := tableConfig("public.second.insert", `root = {"id": 2}`)

	require.NoError(t, runSyncInTime(t, [][]*benthosbuilder.BenthosConfigResponse{{first}, {second}}))
}

// Once a table has failed, the tables still queued each get their turn: none builds a stream.
func Test_syncData_EndedSyncBuildsNoStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := syncData(ctx, droppingEnv(t), tableConfig("public.later.insert", `root = {"id": 1}`),
		testutil.GetTestLogger(t), output.PlainOutput)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "the sync ended before the table started")
}

// The table that failed is the one the sync reports, and the group after it is not started.
func Test_runSync_ReportsTheTableThatFailed(t *testing.T) {
	env, written := droppingEnvCounted(t)
	failing := tableConfig("public.broken.insert", "root = (((")
	later := tableConfig("public.later.insert", `root = {"id": 1}`)

	err := runSyncInTimeWith(t, env, [][]*benthosbuilder.BenthosConfigResponse{{failing}, {later}})

	require.ErrorContains(t, err, "unable to convert benthos config")
	require.Zero(t, written.Load(), "the group after the failed one was synced")
}

// The rows of every group are written.
func Test_runSync_WritesEveryGroup(t *testing.T) {
	env, written := droppingEnvCounted(t)
	first := tableConfig("public.first.insert", `root = {"id": 1}`)
	second := tableConfig("public.second.insert", `root = {"id": 2}`)

	require.NoError(t, runSyncInTimeWith(t, env, [][]*benthosbuilder.BenthosConfigResponse{{first}, {second}}))
	require.EqualValues(t, 2, written.Load())
}
