package sync_activity

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"go.temporal.io/sdk/temporal"
)

// withRetryability tells Temporal not to attempt a table again when the database refused the
// query itself: an unknown column, a syntax error, a constraint, a privilege. Another attempt
// would fail the same way. Anything else may pass by itself, and is attempted again, within
// the attempts of the activity: a connection lost or refused, a deadlock, a lock waited for
// too long, a page that no longer moved, an API of the worker that did not answer.
//
// A page is written in one transaction, which the database rolls back when its connection is
// lost: the next attempt finds nothing of it.
func withRetryability(err error) error {
	if err == nil || !refusedByTheDatabase(err) {
		return err
	}
	var applicationErr *temporal.ApplicationError
	if errors.As(err, &applicationErr) && applicationErr.NonRetryable() {
		return err
	}
	return temporal.NewNonRetryableApplicationError(err.Error(), "SyncTableFailed", err)
}

// refusedByTheDatabase tells whether the database answered the query with an error of its
// own that does not pass by itself.
func refusedByTheDatabase(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1040, // too many connections
			1053, // server shutdown in progress
			1205, // lock wait timeout exceeded
			1213, // deadlock found
			1317: // query execution was interrupted
			return false
		}
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "40001", // serialization failure
			pgErr.Code == "40P01",                // deadlock detected
			pgErr.Code == "55P03",                // lock not available
			pgErr.Code == "57014",                // query canceled
			strings.HasPrefix(pgErr.Code, "08"),  // connection exception
			strings.HasPrefix(pgErr.Code, "53"),  // insufficient resources
			strings.HasPrefix(pgErr.Code, "57P"): // operator intervention
			return false
		}
		return true
	}
	return false
}
