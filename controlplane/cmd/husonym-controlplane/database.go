package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// waitingForDatabase is the line logged, once, by a server that starts before its database can
// be reached. It is made of these words alone: the address of the database holds its password.
const waitingForDatabase = "the database is not reachable yet: waiting for it"

// errStartInterrupted is the error of a start that was told to stop while it waited.
var errStartInterrupted = errors.New("the start was interrupted while it waited for the database")

// cannotConnectNow is what PostgreSQL answers while it starts up or shuts down: it is there, and
// not ready.
const cannotConnectNow = "57P03"

// databaseWait is how a server waits, when it starts, for a database it cannot reach yet. A
// server that starts beside its database, or before the network lets it through, would otherwise
// give up at its first attempt and be started again by whatever runs it.
type databaseWait struct {
	// every is how long it waits between two attempts, and atMost how long it tries in all.
	every, atMost time.Duration
	// attempt bounds one attempt: a connection that nothing answers does not use up the wait.
	attempt time.Duration
}

// startWait is the wait of `serve public` and of `serve backoffice`. It is kept well under the
// time whatever runs the server gives it to start: a wait as long as that time would leave none
// for the migration that follows, and a migration cut short leaves a version to repair by hand.
var startWait = databaseWait{every: 2 * time.Second, atMost: 20 * time.Second, attempt: 5 * time.Second}

// awaitDatabase waits for the database at databaseURL to be reachable, as startWait says.
func awaitDatabase(ctx context.Context, databaseURL string, logger *slog.Logger) error {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		// An address that cannot be read is not mended by waiting: the step that needs the
		// database says so in its own words, which do not quote the address.
		return nil //nolint:nilerr // told by the next step
	}
	return startWait.until(ctx, func(ctx context.Context) error {
		conn, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			return err
		}
		return conn.Close(ctx)
	}, logger)
}

// until tries connect every w.every until it succeeds, for w.atMost at most, and logs one line
// when it had to wait. It returns nil once the database was reached, and also once it gives up:
// when the time is up, or when connect fails in a way waiting does not mend. The caller goes on
// either way, and the step that needs the database tells what is wrong as it does without a wait.
// The error of connect is never logged nor returned: it may quote the address.
//
// The only error is the one of a context that ended, which ends the wait at once.
func (w databaseWait) until(ctx context.Context, connect func(context.Context) error, logger *slog.Logger) error {
	deadline := time.Now().Add(w.atMost)
	waited := false
	for {
		attemptDeadline := time.Now().Add(w.attempt)
		if attemptDeadline.After(deadline) {
			attemptDeadline = deadline
		}
		attemptCtx, cancel := context.WithDeadline(ctx, attemptDeadline)
		err := connect(attemptCtx)
		cancel()
		if ctx.Err() != nil {
			return errStartInterrupted
		}
		if err == nil || !unreachable(err) {
			return nil
		}
		// The next attempt would start after the time is up.
		if !time.Now().Add(w.every).Before(deadline) {
			return nil
		}
		if !waited {
			logger.InfoContext(ctx, waitingForDatabase)
			waited = true
		}
		timer := time.NewTimer(w.every)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errStartInterrupted
		case <-timer.C:
		}
	}
}

// unreachable tells whether a connection failed because the database could not be reached, which
// is what a wait mends: nothing listened, nothing answered in time, the name did not resolve, or
// PostgreSQL said it is starting up. A database that answered and refused, for the password, the
// role or the name of the database, is reached: it refuses the next attempt the same way.
func unreachable(err error) bool {
	var answered *pgconn.PgError
	if errors.As(err, &answered) {
		return answered.Code == cannotConnectNow
	}
	return true
}
