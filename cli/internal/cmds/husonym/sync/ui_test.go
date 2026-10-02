package sync_cmd

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/output"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	husonym_benthos_error "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/error"
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
		done <- runSync(context.Background(), output.PlainOutput, env, groups, nil, testutil.GetTestLogger(t))
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
	m := newModel(context.Background(), env, groups, logger, output.PlainOutput)
	t.Cleanup(m.cancel)
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
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	require.NoError(t, m.ctx.Err())

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	require.NotNil(t, cmd)
	require.ErrorIs(t, m.ctx.Err(), context.Canceled)
}

// A key stops the tables being synced, which end cut short: the sync was interrupted, it is
// not one of its tables that failed.
func Test_model_TablesCutShortByTheStopDidNotFail(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	m.cancel()

	_, cmd := m.Update(syncFailedMsg{err: fmt.Errorf("unable to run benthos stream: %w", context.Canceled)})

	require.NotNil(t, cmd, "the sync ends all the same")
	require.NoError(t, m.err)
	require.ErrorIs(t, m.outcome(nil), errSyncInterrupted)
}

// A table that ends on a canceled context while the sync was not stopped did fail.
func Test_model_ATableCanceledOnItsOwnFailed(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	failure := fmt.Errorf("unable to run benthos stream: %w", context.Canceled)

	m.Update(syncFailedMsg{err: failure})

	require.ErrorIs(t, m.outcome(nil), failure)
	require.NotErrorIs(t, m.outcome(nil), errSyncInterrupted)
}

// A table that failed is what the sync reports.
func Test_model_ATableThatFailedIsReported(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	failure := errors.New("the table failed")

	_, cmd := m.Update(syncFailedMsg{err: failure})

	require.NotNil(t, cmd)
	require.ErrorIs(t, m.outcome(nil), failure)
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
		"its last table synced":                   {done: true},
		"a key after its last table":              {done: true, programErr: nil},
		"a signal after its last table":           {done: true, programErr: tea.ErrInterrupted},
		"quit before its last table":              {want: errSyncInterrupted},
		"a signal before its last table":          {programErr: tea.ErrInterrupted, want: errSyncInterrupted},
		"a table failed":                          {failed: failure, want: failure},
		"a table failed, then a signal":           {failed: failure, programErr: tea.ErrInterrupted, want: failure},
		"the program failed":                      {programErr: crash, want: crash},
		"the program failed after a table did":    {failed: failure, programErr: crash, want: crash},
		"the program failed after its last table": {done: true, programErr: crash, want: crash},
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

	t.Run("no group starts while the sync waits for those being synced", func(t *testing.T) {
		groups := &inFlightGroups{}
		require.True(t, groups.begin())
		ended := make(chan bool, 1)
		go func() { ended <- groups.end(5 * time.Second) }()
		require.Eventually(t, func() bool {
			groups.mu.Lock()
			defer groups.mu.Unlock()
			return groups.ended
		}, time.Second, time.Millisecond, "the sync that ends closes the door before it waits")
		require.False(t, groups.begin())
		groups.done()
		require.True(t, <-ended)
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

// A row the destination refuses for good — a duplicate key, a constraint — stops the sync, and
// the sync tells why: it used to end the process on the spot, with "Sync Failed." for all
// explanation, its session not given back and the terminal left as it was.
func Test_runSync_ACriticalErrorEndsTheSyncAndIsTold(t *testing.T) {
	stop := make(chan error, 3)
	env := service.NewEnvironment()
	require.NoError(t, husonym_benthos_error.RegisterErrorOutput(env, stop))
	refused := tableConfig("public.refused.insert", `root = {"id": 1}`)
	refused.Config.Output.Error.ErrorMsg = `duplicate key value violates unique constraint "users_pkey"`
	later := tableConfig("public.later.insert", `root = {"id": 1}`)
	later.Config.Output.Error.ErrorMsg = `duplicate key value violates unique constraint "later_pkey"`

	done := make(chan error, 1)
	go func() {
		done <- runSync(context.Background(), output.PlainOutput, env,
			[][]*benthosbuilder.BenthosConfigResponse{{refused}, {later}}, stop, testutil.GetTestLogger(t))
	}()

	select {
	case err := <-done:
		require.ErrorContains(t, err, "unable to finish syncing data")
		require.ErrorContains(t, err, `duplicate key value violates unique constraint "users_pkey"`)
	case <-time.After(20 * time.Second):
		require.FailNow(t, "the sync does not end on a critical error")
	}
	// The group after it was not synced: its own refused row would have signaled too.
	for range cap(stop) {
		select {
		case signal := <-stop:
			require.NotContains(t, signal.Error(), "later_pkey", "the group after the refused row was synced")
		default:
		}
	}
}

// The stream acknowledges the row it was refused, and its table ends as if it had been
// written: on the last table of a sync, the critical error is told all the same.
func Test_runSync_ACriticalErrorOnTheLastTableIsTold(t *testing.T) {
	for range 20 {
		stop := make(chan error, 3)
		env := service.NewEnvironment()
		require.NoError(t, husonym_benthos_error.RegisterErrorOutput(env, stop))
		refused := tableConfig("public.refused.insert", `root = {"id": 1}`)
		refused.Config.Output.Error.ErrorMsg = `null value in column "name" violates not-null constraint`

		done := make(chan error, 1)
		go func() {
			done <- runSync(context.Background(), output.PlainOutput, env,
				[][]*benthosbuilder.BenthosConfigResponse{{refused}}, stop, testutil.GetTestLogger(t))
		}()

		select {
		case err := <-done:
			require.ErrorContains(t, err, "violates not-null constraint")
		case <-time.After(20 * time.Second):
			require.FailNow(t, "the sync does not end on a critical error")
		}
	}
}

// A critical error stops the tables being synced, and is the cause the sync reports: what
// follows it — tables cut short, a table that fails in turn — is its consequence.
func Test_model_ACriticalErrorStopsTheSync(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	critical := errors.New("duplicate key value violates unique constraint")

	_, cmd := m.Update(syncStoppedMsg{err: critical})
	require.IsType(t, tea.QuitMsg{}, cmd(), "the sync ends on a critical error")
	require.ErrorIs(t, m.ctx.Err(), context.Canceled)

	m.Update(syncFailedMsg{err: errors.New("unable to run benthos stream: the sink is closed")})
	require.ErrorIs(t, m.outcome(nil), critical)
}

// A table that failed first stays the cause, though a critical error follows.
func Test_model_TheFirstFailureIsTheCause(t *testing.T) {
	m := newModel(context.Background(), droppingEnv(t), nil, testutil.GetTestLogger(t), output.PlainOutput)
	failure := errors.New("the table failed")

	m.Update(syncFailedMsg{err: failure})
	m.Update(syncStoppedMsg{err: errors.New("duplicate key value violates unique constraint")})

	require.ErrorIs(t, m.outcome(nil), failure)
}

// The watch tells the program of a critical error, and returns it: a program that has ended
// is told nothing. It ends with the sync.
func Test_model_watchStop(t *testing.T) {
	logger := testutil.GetTestLogger(t)
	critical := errors.New("violates not-null constraint")

	t.Run("a critical error is told and returned", func(t *testing.T) {
		stop := make(chan error, 1)
		m := newModel(context.Background(), droppingEnv(t), nil, logger, output.PlainOutput)
		m.stop = stop
		told := make(chan tea.Msg, 1)

		stop <- critical
		heard := m.watchStop(func(msg tea.Msg) { told <- msg })

		require.Equal(t, critical, <-heard)
		require.Equal(t, syncStoppedMsg{err: critical}, <-told)
	})

	t.Run("a program that ended before it was told has failed all the same", func(t *testing.T) {
		stop := make(chan error, 1)
		m := newModel(context.Background(), droppingEnv(t), nil, logger, output.PlainOutput)
		m.stop = stop
		m.done = true

		stop <- critical
		heard := m.watchStop(func(tea.Msg) {})
		// The watch has taken the error off the channel: it is the only one to hold it.
		require.Eventually(t, func() bool { return len(stop) == 0 }, time.Second, time.Millisecond)
		m.finish(heard, logger)

		require.ErrorIs(t, m.outcome(nil), critical)
	})

	t.Run("the watch ends with the sync", func(t *testing.T) {
		m := newModel(context.Background(), droppingEnv(t), nil, logger, output.PlainOutput)
		m.stop = make(chan error, 1)
		m.done = true

		heard := m.watchStop(func(tea.Msg) { require.Fail(t, "nothing to tell") })
		m.finish(heard, logger)

		_, open := <-heard
		require.False(t, open)
		require.NoError(t, m.outcome(nil))
	})
}

// What failed first stays the cause, though a critical error is heard late.
func Test_model_outcome_ALateCriticalErrorDoesNotHideTheCause(t *testing.T) {
	stop := make(chan error, 1)
	stop <- errors.New("violates not-null constraint")
	failure := errors.New("the table failed")
	m := &model{err: failure, stop: stop}

	require.ErrorIs(t, m.outcome(nil), failure)
}

// A critical error the program did not hear before its last table ended is told all the same.
func Test_model_outcome_ACriticalErrorHeardLate(t *testing.T) {
	stop := make(chan error, 1)
	critical := errors.New("violates not-null constraint")
	stop <- critical
	m := &model{done: true, stop: stop}

	require.ErrorIs(t, m.outcome(nil), critical)
}

// The stream goes on past the row it was refused: a table that never ends does not keep the
// sync from stopping on the critical error.
func Test_runSync_ACriticalErrorStopsATableThatGoesOn(t *testing.T) {
	stop := make(chan error, 3)
	env := service.NewEnvironment()
	require.NoError(t, husonym_benthos_error.RegisterErrorOutput(env, stop))
	refused := endlessTable("public.refused.insert")
	refused.Config.Output.Error.ErrorMsg = `duplicate key value violates unique constraint "users_pkey"`

	done := make(chan error, 1)
	go func() {
		done <- runSync(context.Background(), output.PlainOutput, env,
			[][]*benthosbuilder.BenthosConfigResponse{{refused}}, stop, testutil.GetTestLogger(t))
	}()

	select {
	case err := <-done:
		require.ErrorContains(t, err, `duplicate key value violates unique constraint "users_pkey"`)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the sync does not stop on a critical error while its table goes on")
	}
}
