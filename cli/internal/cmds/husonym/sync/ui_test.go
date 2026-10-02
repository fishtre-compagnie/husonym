package sync_cmd

import (
	"context"
	"errors"
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

// endlessSync starts a sync of tables that never end, in plain mode, and returns once rows are
// being written: the program, how the sync ends, and the count of rows written.
func endlessSync(t *testing.T) (*model, *tea.Program, <-chan error, *atomic.Int64) {
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
	m.cancel = cancel
	program := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))

	done := make(chan error, 1)
	go func() { done <- runSyncProgram(m, program, logger) }()
	require.Eventually(t, func() bool { return written.Load() > 0 }, 10*time.Second, 10*time.Millisecond)
	return m, program, done, written
}

// endsInTime returns how the sync ended, which it must do soon.
func endsInTime(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the sync is slow to end once it was quit")
		return nil
	}
}

// A sync quit before its last table is told interrupted, soon, and starts no write once it has
// returned: it used to return as if it had synced every table, its streams still writing.
func Test_runSync_QuitBeforeItsLastTable(t *testing.T) {
	for name, quit := range map[string]tea.Msg{
		"q":       tea.KeyPressMsg{Code: 'q', Text: "q"},
		"esc":     tea.KeyPressMsg{Code: tea.KeyEscape},
		"ctrl+c":  tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl},
		"sigint":  tea.InterruptMsg{},
		"sigterm": tea.QuitMsg{},
	} {
		t.Run(name, func(t *testing.T) {
			_, program, done, written := endlessSync(t)

			program.Send(quit)
			err := endsInTime(t, done)

			require.ErrorIs(t, err, errSyncInterrupted)
			before := written.Load()
			time.Sleep(300 * time.Millisecond)
			require.Equal(t, before, written.Load(), "the sync goes on writing after it returned")
		})
	}
}

// The tables are stopped at the key, before the program has closed.
func Test_model_AKeyStopsTheTables(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newModel(ctx, droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	m.cancel = cancel

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	require.NotNil(t, cmd)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func Test_model_outcome(t *testing.T) {
	failure := errors.New("the table failed")
	crash := errors.New("the program crashed")
	for name, tc := range map[string]struct {
		done       bool
		failed     error
		programErr error
		want       error
	}{
		"its last table synced":                {done: true},
		"a key after its last table":           {done: true, programErr: nil},
		"a signal after its last table":        {done: true, programErr: tea.ErrInterrupted},
		"quit before its last table":           {want: errSyncInterrupted},
		"a signal before its last table":       {programErr: tea.ErrInterrupted, want: errSyncInterrupted},
		"a table failed":                       {failed: failure, want: failure},
		"a table failed, then a signal":        {failed: failure, programErr: tea.ErrInterrupted, want: failure},
		"the program failed":                   {programErr: crash, want: crash},
		"the program failed after a table did": {failed: failure, programErr: crash, want: crash},
	} {
		t.Run(name, func(t *testing.T) {
			m := &model{done: tc.done, err: tc.failed}
			err := m.outcome(tc.programErr)
			if tc.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, "unable to finish syncing data")
		})
	}
}

func Test_inFlightGroups(t *testing.T) {
	t.Run("waits for the groups being synced", func(t *testing.T) {
		groups := &inFlightGroups{}
		require.True(t, groups.begin())
		require.False(t, groups.end(50*time.Millisecond))
		groups.done()
		require.True(t, groups.end(time.Second))
	})

	t.Run("a group does not start once the sync has ended", func(t *testing.T) {
		groups := &inFlightGroups{}
		require.True(t, groups.end(time.Second), "nothing to wait for")
		require.False(t, groups.begin())
		require.True(t, groups.end(time.Second), "the group that did not start is not waited for")
	})
}

// The program may end between asking for a group and running it: the group does not start.
func Test_model_AGroupAskedForAfterTheEndDoesNotStart(t *testing.T) {
	env, written := droppingEnvCounted(t)
	m := newModel(context.Background(), env, nil, testutil.GetTestLogger(t), output.PlainOutput)
	syncGroup := m.syncConfigs(context.Background(),
		[]*benthosbuilder.BenthosConfigResponse{tableConfig("public.a.insert", `root = {"id": 1}`)})

	require.True(t, m.inFlight.end(time.Second), "a group asked for and not started is not waited for")
	require.Nil(t, syncGroup())
	require.Zero(t, written.Load())
}

// The sync does not return while a group of tables has yet to end, and does not wait for it
// past its bound.
func Test_runSyncProgram_WaitsForTheTablesBeingSynced(t *testing.T) {
	t.Run("until they have ended", func(t *testing.T) {
		m, program, done, _ := endlessSync(t)
		// A group that takes its time to end.
		require.True(t, m.inFlight.begin())

		program.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
		select {
		case <-done:
			require.FailNow(t, "the sync returned while a group of tables had yet to end")
		case <-time.After(500 * time.Millisecond):
		}
		m.inFlight.done()
		require.ErrorIs(t, endsInTime(t, done), errSyncInterrupted)
	})

	t.Run("for a time", func(t *testing.T) {
		m, program, done, _ := endlessSync(t)
		m.endWait = 100 * time.Millisecond
		// A group that never ends.
		require.True(t, m.inFlight.begin())

		program.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
		require.ErrorIs(t, endsInTime(t, done), errSyncInterrupted)
	})
}
