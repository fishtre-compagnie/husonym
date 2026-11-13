package sync_activity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
)

// A page has no limit of time of its own: it runs as long as it moves, and the time limit of
// the run is what bounds it. It is given up when nothing moves:
//
//   - while it writes a batch to the destination, after stallAfter without a row written;
//   - while it waits for the source, after stallAfter without a row read, unless the source
//     says it still works on the query of the page. A source that computes before it sends
//     its first row (a sort, a join on a large table) is asked again every stallAfter.
const (
	stallAfter     = 10 * time.Minute
	watchEvery     = 10 * time.Second
	probeTimeout   = 30 * time.Second
	stopQueryAfter = 10 * time.Second
)

// errPageStalled says that a page was given up because it no longer moved. Another attempt
// may go through: a lock released, a connection open again.
var errPageStalled = errors.New("the table made no progress")

// queryState is what the source says of the query reading a page.
type queryState int

const (
	// queryUnknown: the source could not be asked, or does not tell.
	queryUnknown queryState = iota
	// queryWorking: the source runs the query.
	queryWorking
	// queryWaitingForLock: the source runs the query, which waits for a lock.
	queryWaitingForLock
	// queryGone: the source no longer runs the query, though the page waits for its rows.
	queryGone
)

// sourceProbe asks the source about the query reading a page, found by its marker.
type sourceProbe interface {
	state(ctx context.Context) (queryState, string)
	// stop asks the source to stop the query: a page given up must not leave it running.
	stop(ctx context.Context) error
}

// newQueryTag gives the tag the query of a page carries, by which the probe finds it.
func newQueryTag() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newSourceProbe returns the probe of the source for its dialect, nil when the dialect has
// none: the page is then given up only by the time limit of the run while it waits for the
// source.
func newSourceProbe(src runner.Querier, dialect sqlio.Dialect, tag string) sourceProbe {
	marker := "%" + runner.QueryMarker(tag) + "%"
	switch dialect.(type) {
	case sqlio.MySQLDialect:
		return &mysqlProbe{src: src, marker: marker}
	case sqlio.PostgresDialect:
		return &postgresProbe{src: src, marker: marker}
	default:
		return nil
	}
}

// mysqlProbe reads the processlist. A user sees its own threads without any privilege, and
// the page reads with the user of the probe.
type mysqlProbe struct {
	src    runner.Querier
	marker string
}

func (p *mysqlProbe) find(ctx context.Context) (id int64, state string, found bool, err error) {
	rows, err := p.src.QueryContext(ctx,
		"SELECT ID, COALESCE(STATE, '') FROM information_schema.PROCESSLIST "+
			"WHERE INFO LIKE ? AND ID <> CONNECTION_ID() ORDER BY TIME DESC LIMIT 1", p.marker)
	if err != nil {
		return 0, "", false, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&id, &state); err != nil {
			return 0, "", false, err
		}
		found = true
	}
	return id, state, found, rows.Err()
}

func (p *mysqlProbe) state(ctx context.Context) (answer queryState, detail string) {
	_, state, found, err := p.find(ctx)
	switch {
	case err != nil:
		return queryUnknown, err.Error()
	case !found:
		return queryGone, ""
	case strings.HasPrefix(state, "Waiting for") && strings.Contains(state, "lock"):
		return queryWaitingForLock, state
	default:
		return queryWorking, state
	}
}

func (p *mysqlProbe) stop(ctx context.Context) error {
	id, _, found, err := p.find(ctx)
	if err != nil || !found {
		return err
	}
	rows, err := p.src.QueryContext(ctx, fmt.Sprintf("KILL QUERY %d", id))
	if err != nil {
		return err
	}
	return rows.Close()
}

// postgresProbe reads pg_stat_activity, where a role sees the queries of its own sessions.
type postgresProbe struct {
	src    runner.Querier
	marker string
}

func (p *postgresProbe) find(ctx context.Context) (pid int64, state, waitType string, found bool, err error) {
	rows, err := p.src.QueryContext(ctx,
		"SELECT pid, COALESCE(state, ''), COALESCE(wait_event_type, '') FROM pg_stat_activity "+
			"WHERE query LIKE $1 AND pid <> pg_backend_pid() ORDER BY query_start LIMIT 1", p.marker)
	if err != nil {
		return 0, "", "", false, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&pid, &state, &waitType); err != nil {
			return 0, "", "", false, err
		}
		found = true
	}
	return pid, state, waitType, found, rows.Err()
}

func (p *postgresProbe) state(ctx context.Context) (answer queryState, detail string) {
	_, state, waitType, found, err := p.find(ctx)
	switch {
	case err != nil:
		return queryUnknown, err.Error()
	// A session keeps its last query once it is done with it: only an active one runs it.
	case !found || state != "active":
		return queryGone, state
	case waitType == "Lock":
		return queryWaitingForLock, waitType
	default:
		return queryWorking, waitType
	}
}

func (p *postgresProbe) stop(ctx context.Context) error {
	pid, state, _, found, err := p.find(ctx)
	if err != nil || !found || state != "active" {
		return err
	}
	rows, err := p.src.QueryContext(ctx, "SELECT pg_cancel_backend($1)", pid)
	if err != nil {
		return err
	}
	return rows.Close()
}

// pageWatch watches a page as it runs: it heartbeats with what the page counted, and gives
// the page up when it no longer moves.
type pageWatch struct {
	progress   *runner.Progress
	probe      sourceProbe // nil: the source is not asked
	heartbeat  func(details ...any)
	stallAfter time.Duration
	every      time.Duration
	now        func() time.Time
	logger     *slog.Logger
}

// run watches until ctx is done, and calls giveUp, once, with the reason a page that no
// longer moves is given up for.
func (w *pageWatch) run(ctx context.Context, giveUp func(reason error)) {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()

	last := w.progress.Snapshot()
	lastMove := w.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		current := w.progress.Snapshot()
		w.heartbeat(current)
		if current.RowsRead != last.RowsRead || current.RowsWritten != last.RowsWritten {
			last, lastMove = current, w.now()
			continue
		}
		still := w.now().Sub(lastMove)
		if still < w.stallAfter {
			continue
		}
		if reason := w.stalled(ctx, current, still); reason != nil {
			giveUp(reason)
			return
		}
		// The source works on the query: it is asked again after as long.
		lastMove = w.now()
	}
}

// stalled tells why a page that has not moved for still is given up, nil when it is not.
func (w *pageWatch) stalled(ctx context.Context, current runner.ProgressSnapshot, still time.Duration) error {
	counted := fmt.Sprintf("%d rows read, %d written", current.RowsRead, current.RowsWritten)
	if current.Writing {
		return fmt.Errorf("%w: writing a batch to the destination has not ended for %s (%s)",
			errPageStalled, still.Round(time.Second), counted)
	}
	if w.probe == nil {
		w.logger.Info("the table waits for the source, which cannot be asked about its query: it goes on",
			"still", still.Round(time.Second).String(), "rowsRead", current.RowsRead)
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	state, detail := w.probe.state(probeCtx)
	switch state {
	case queryWorking:
		w.logger.Info("the table waits for the source, which still works on its query",
			"still", still.Round(time.Second).String(), "rowsRead", current.RowsRead, "sourceState", detail)
		return nil
	case queryWaitingForLock:
		return fmt.Errorf("%w for %s: its query on the source waits for a lock (%s; %s)",
			errPageStalled, still.Round(time.Second), detail, counted)
	case queryGone:
		return fmt.Errorf("%w for %s: the source no longer runs its query (%s)",
			errPageStalled, still.Round(time.Second), counted)
	default:
		w.logger.Warn("the table waits for the source, which could not be asked about its query: it goes on",
			"still", still.Round(time.Second).String(), "rowsRead", current.RowsRead, "error", detail)
		return nil
	}
}
