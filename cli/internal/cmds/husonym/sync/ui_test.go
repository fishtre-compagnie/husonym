package sync_cmd

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/output"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/redpanda-data/benthos/v4/public/service"

	tea "charm.land/bubbletea/v2"
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

// A table that fails among others of its group ends the sync all the same: the others are cut
// short or never start, and the failure is the one reported, not their being cut short.
func Test_runSync_AFailedTableAmongOthersEndsTheSync(t *testing.T) {
	group := []*benthosbuilder.BenthosConfigResponse{tableConfig("public.broken.insert", "root = (((")}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		group = append(group, tableConfig("public."+name+".insert", `root = {"id": 1}`))
	}

	err := runSyncInTime(t, [][]*benthosbuilder.BenthosConfigResponse{group})

	require.ErrorContains(t, err, "unable to finish syncing data")
	require.ErrorContains(t, err, "unable to convert benthos config")
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

// A sync whose table fails ends, and tells so: it used to go on waiting for good, with no
// table left to sync. The group after the failed one is not started.
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

// endlessTable is the config of a table whose rows never end.
func endlessTable(name string) *benthosbuilder.BenthosConfigResponse {
	cfg := tableConfig(name, `root = {"id": counter()}`)
	cfg.Config.Input.Generate.Count = 0
	return cfg
}

// interruptedSync starts a sync of tables that never end, in plain mode, and presses key once
// rows are being written. It returns how the sync ended, and the count of rows written.
func interruptedSync(t *testing.T, key tea.KeyPressMsg) (error, *atomic.Int64) {
	t.Helper()
	env, written := droppingEnvCounted(t)
	groups := [][]*benthosbuilder.BenthosConfigResponse{
		{endlessTable("public.a.insert"), endlessTable("public.b.insert")},
		{endlessTable("public.later.insert")},
	}
	logger := testutil.GetTestLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newModel(ctx, env, groups, logger, output.PlainOutput)
	program := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))

	done := make(chan error, 1)
	go func() { done <- runSyncProgram(m, cancel, program, logger) }()
	require.Eventually(t, func() bool { return written.Load() > 0 }, 10*time.Second, 10*time.Millisecond)
	program.Send(key)
	asked := time.Now()

	select {
	case err := <-done:
		require.Less(t, time.Since(asked), 2*time.Second, "the sync is slow to end once interrupted")
		return err, written
	case <-time.After(20 * time.Second):
		require.FailNow(t, "the sync does not end once interrupted")
		return nil, nil
	}
}

// A sync quit from the keyboard is told interrupted, and writes nothing once it has returned:
// it used to return at once as if it had synced every table, its streams still writing.
func Test_runSync_InterruptedFromTheKeyboard(t *testing.T) {
	for name, key := range map[string]tea.KeyPressMsg{
		"q":      {Code: 'q', Text: "q"},
		"esc":    {Code: tea.KeyEscape},
		"ctrl+c": {Code: 'c', Mod: tea.ModCtrl},
	} {
		t.Run(name, func(t *testing.T) {
			err, written := interruptedSync(t, key)

			require.ErrorIs(t, err, errSyncInterrupted)
			before := written.Load()
			time.Sleep(300 * time.Millisecond)
			require.Equal(t, before, written.Load(), "the sync goes on writing after it returned")
		})
	}
}

// A key pressed once the last table is synced quits a sync that is done: it was not interrupted.
func Test_model_AKeyAfterTheLastTableIsNoInterruption(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	m.done = true

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	require.NotNil(t, cmd)
	require.NoError(t, m.err)
}

// A table that failed is what the sync reports, though a key quits it afterwards.
func Test_model_AKeyAfterAFailureKeepsTheFailure(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	failure := errors.New("the table failed")
	m.Update(syncFailedMsg{err: failure})

	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	require.ErrorIs(t, m.err, failure)
}

func Test_waitFor(t *testing.T) {
	var group sync.WaitGroup
	require.True(t, waitFor(&group, time.Second), "a group with nothing to wait for is done")

	group.Add(1)
	require.False(t, waitFor(&group, 50*time.Millisecond))
	group.Done()
	require.True(t, waitFor(&group, time.Second))
}

// A group of tables counts as being synced from when the program is asked to sync it, until it
// has ended: the sync that ends meanwhile waits for it.
func Test_model_CountsTheGroupBeingSynced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := newModel(ctx, droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)

	syncGroup := m.syncConfigs(ctx, []*benthosbuilder.BenthosConfigResponse{endlessTable("public.a.insert")})
	require.False(t, waitFor(&m.inFlight, 50*time.Millisecond), "the group asked for is not waited for")

	require.IsType(t, syncFailedMsg{}, syncGroup())
	require.True(t, waitFor(&m.inFlight, time.Second), "the group that ended is still waited for")
}

// The sync does not return while a group of tables has yet to end.
func Test_runSyncProgram_WaitsForTheTablesBeingSynced(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	groups := [][]*benthosbuilder.BenthosConfigResponse{{endlessTable("public.a.insert")}}
	m := newModel(ctx, droppingEnv(t), groups, logger, output.PlainOutput)
	program := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))
	// A group that takes its time to end.
	m.inFlight.Add(1)

	done := make(chan error, 1)
	go func() { done <- runSyncProgram(m, cancel, program, logger) }()
	program.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})

	select {
	case <-done:
		require.FailNow(t, "the sync returned while a group of tables had yet to end")
	case <-time.After(500 * time.Millisecond):
	}
	m.inFlight.Done()
	select {
	case err := <-done:
		require.ErrorIs(t, err, errSyncInterrupted)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the sync does not return once its tables have ended")
	}
}
