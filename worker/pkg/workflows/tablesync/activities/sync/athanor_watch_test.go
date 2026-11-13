package sync_activity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

// fakeProbe answers the states it is given, one per question, the last one over and over.
type fakeProbe struct {
	mu      sync.Mutex
	states  []queryState
	asked   int
	stopped int
}

func (p *fakeProbe) state(context.Context) (queryState, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := min(p.asked, len(p.states)-1)
	p.asked++
	return p.states[i], "fake"
}

func (p *fakeProbe) stop(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped++
	return nil
}

func (p *fakeProbe) questions() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.asked
}

// watchFor runs a watch on progress until it gives up or for runFor, and returns its reason.
func watchFor(t *testing.T, progress *runner.Progress, probe sourceProbe, runFor time.Duration, during func()) (error, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runFor)
	defer cancel()
	var mu sync.Mutex
	heartbeats := 0
	w := &pageWatch{
		progress:   progress,
		probe:      probe,
		heartbeat:  func(...any) { mu.Lock(); heartbeats++; mu.Unlock() },
		stallAfter: 30 * time.Millisecond,
		every:      2 * time.Millisecond,
		now:        time.Now,
		logger:     slog.New(slog.DiscardHandler),
	}
	reasons := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx, func(reason error) { reasons <- reason })
	}()
	if during != nil {
		during()
	}
	<-done
	mu.Lock()
	defer mu.Unlock()
	select {
	case reason := <-reasons:
		return reason, heartbeats
	default:
		return nil, heartbeats
	}
}

func TestPageWatch_MovingPageGoesOn(t *testing.T) {
	progress := &runner.Progress{}
	probe := &fakeProbe{states: []queryState{queryGone}}
	// The rows come in as the page goes.
	reason, heartbeats := watchFor(t, progress, probe, 150*time.Millisecond, func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(140 * time.Millisecond)
		for {
			select {
			case <-deadline:
				return
			case <-ticker.C:
				readRows(t, progress, 1)
			}
		}
	})
	require.NoError(t, reason)
	require.Zero(t, probe.questions(), "a page that moves is not asked about")
	require.Positive(t, heartbeats)
}

func TestPageWatch_SourceStillWorking(t *testing.T) {
	probe := &fakeProbe{states: []queryState{queryWorking}}
	reason, _ := watchFor(t, &runner.Progress{}, probe, 150*time.Millisecond, nil)
	require.NoError(t, reason, "a source that works on the query keeps the page going")
	require.GreaterOrEqual(t, probe.questions(), 2, "the source is asked again after as long")
}

func TestPageWatch_SourceGivesUp(t *testing.T) {
	for name, state := range map[string]queryState{
		"query gone":       queryGone,
		"waiting for lock": queryWaitingForLock,
	} {
		t.Run(name, func(t *testing.T) {
			probe := &fakeProbe{states: []queryState{queryWorking, state}}
			reason, _ := watchFor(t, &runner.Progress{}, probe, time.Second, nil)
			require.ErrorIs(t, reason, errPageStalled)
			require.Equal(t, 2, probe.questions())
		})
	}
}

func TestPageWatch_UnknownOrNoProbeGoesOn(t *testing.T) {
	reason, _ := watchFor(t, &runner.Progress{}, &fakeProbe{states: []queryState{queryUnknown}}, 120*time.Millisecond, nil)
	require.NoError(t, reason)
	reason, _ = watchFor(t, &runner.Progress{}, nil, 120*time.Millisecond, nil)
	require.NoError(t, reason)
}

func TestPageWatch_StuckWrite(t *testing.T) {
	progress := &runner.Progress{}
	probe := &fakeProbe{states: []queryState{queryWorking}}
	release := make(chan struct{})
	writing := make(chan struct{})
	go func() {
		w := writerOf(progress, func() { close(writing); <-release })
		_ = w.WriteBatch([]string{"id"}, [][]any{{1}})
	}()
	<-writing
	reason, _ := watchFor(t, progress, probe, time.Second, nil)
	close(release)
	require.ErrorIs(t, reason, errPageStalled)
	require.ErrorContains(t, reason, "writing a batch to the destination")
	require.Zero(t, probe.questions(), "a write that does not end is not the source's")
}

func TestNewSourceProbe(t *testing.T) {
	require.IsType(t, &mysqlProbe{}, newSourceProbe(nil, sqlio.MySQLDialect{}, "t"))
	require.IsType(t, &postgresProbe{}, newSourceProbe(nil, sqlio.PostgresDialect{}, "t"))
	require.Nil(t, newSourceProbe(nil, sqlio.MSSQLDialect{}, "t"))
}

func TestMysqlProbe(t *testing.T) {
	src, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer src.Close()
	probe := newSourceProbe(src, sqlio.MySQLDialect{}, "abc")
	find := regexp.QuoteMeta("FROM information_schema.PROCESSLIST WHERE INFO LIKE ? AND ID <> CONNECTION_ID()")

	mock.ExpectQuery(find).WithArgs("%husonym-page:abc%").
		WillReturnRows(sqlmock.NewRows([]string{"ID", "STATE"}).AddRow(42, "Sending data"))
	state, _ := probe.state(context.Background())
	require.Equal(t, queryWorking, state)

	mock.ExpectQuery(find).WillReturnRows(sqlmock.NewRows([]string{"ID", "STATE"}).AddRow(42, "Waiting for table metadata lock"))
	state, _ = probe.state(context.Background())
	require.Equal(t, queryWaitingForLock, state)

	mock.ExpectQuery(find).WillReturnRows(sqlmock.NewRows([]string{"ID", "STATE"}))
	state, _ = probe.state(context.Background())
	require.Equal(t, queryGone, state)

	mock.ExpectQuery(find).WillReturnError(errors.New("no access"))
	state, _ = probe.state(context.Background())
	require.Equal(t, queryUnknown, state)

	mock.ExpectQuery(find).WillReturnRows(sqlmock.NewRows([]string{"ID", "STATE"}).AddRow(42, "Sending data"))
	mock.ExpectQuery(regexp.QuoteMeta("KILL QUERY 42")).WillReturnRows(sqlmock.NewRows(nil))
	require.NoError(t, probe.stop(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresProbe(t *testing.T) {
	src, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer src.Close()
	probe := newSourceProbe(src, sqlio.PostgresDialect{}, "abc")
	find := regexp.QuoteMeta("FROM pg_stat_activity WHERE query LIKE $1 AND pid <> pg_backend_pid()")
	cols := []string{"pid", "state", "wait_event_type"}

	mock.ExpectQuery(find).WithArgs("%husonym-page:abc%").WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "active", "IO"))
	state, _ := probe.state(context.Background())
	require.Equal(t, queryWorking, state)

	mock.ExpectQuery(find).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "active", "Lock"))
	state, _ = probe.state(context.Background())
	require.Equal(t, queryWaitingForLock, state)

	// A session keeps its last query once done with it.
	mock.ExpectQuery(find).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "idle", ""))
	state, _ = probe.state(context.Background())
	require.Equal(t, queryGone, state)

	mock.ExpectQuery(find).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "active", "IO"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_cancel_backend($1)")).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"pg_cancel_backend"}).AddRow(true))
	require.NoError(t, probe.stop(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithRetryability(t *testing.T) {
	retryable := map[string]error{
		"stalled":           fmt.Errorf("web.users: %w: the source no longer runs its query", errPageStalled),
		"mysql deadlock":    fmt.Errorf("write: %w", &mysql.MySQLError{Number: 1213}),
		"mysql lock wait":   &mysql.MySQLError{Number: 1205},
		"postgres deadlock": &pgconn.PgError{Code: "40P01"},
		"postgres conn":     &pgconn.PgError{Code: "08006"},
		"connection lost":   fmt.Errorf("read: %w", mysql.ErrInvalidConn),
		"api unavailable":   errors.New("unable to retrieve benthosconfig runcontext: unavailable"),
		"activity canceled": context.Canceled,
	}
	for name, err := range retryable {
		t.Run(name, func(t *testing.T) {
			require.Same(t, err, withRetryability(err))
		})
	}
	final := map[string]error{
		"unknown column":  fmt.Errorf("runner: lecture de allopneus.tb_relance: %w", &mysql.MySQLError{Number: 1054, Message: "Unknown column"}),
		"postgres syntax": &pgconn.PgError{Code: "42601"},
	}
	for name, err := range final {
		t.Run(name, func(t *testing.T) {
			got := withRetryability(err)
			var applicationErr *temporal.ApplicationError
			require.ErrorAs(t, got, &applicationErr)
			require.True(t, applicationErr.NonRetryable())
			require.ErrorIs(t, got, err, "the cause is kept, for its category")
		})
	}
	require.NoError(t, withRetryability(nil))
}

func readRows(t *testing.T, progress *runner.Progress, n int) {
	t.Helper()
	// The runner's own tests cover the rows read: here the counter moves through a writer.
	w := writerOf(progress, nil)
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{i}
	}
	require.NoError(t, w.WriteBatch([]string{"id"}, rows))
}

// writerOf returns a writer that counts on progress as the runner's do, calling during while
// it writes.
func writerOf(progress *runner.Progress, during func()) sqlio.RowWriter {
	return progress.Wrap(writeFunc(func([]string, [][]any) error {
		if during != nil {
			during()
		}
		return nil
	}))
}

type writeFunc func(columns []string, rows [][]any) error

func (f writeFunc) WriteBatch(columns []string, rows [][]any) error { return f(columns, rows) }
