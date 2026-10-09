package runerror

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"os"
	"syscall"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// Classify gives the category of an error of the worker, from its Go types and from the
// code of the database, the first of these that is found anywhere in the error:
//
//  1. the mark of a refusal of the license (License);
//  2. a cancellation;
//  3. a deadline;
//  4. an error of PostgreSQL, by its SQLSTATE;
//  5. an error of MySQL, by its number;
//  6. a network error that timed out;
//  7. a connection that could not be opened, or that broke;
//  8. a disk or a memory that is full.
//
// Anything else is "other", nil included, as is a database error whose code is not listed.
// The answer is never "unspecified".
func Classify(err error) mgmtv1alpha1.RunErrorCategory {
	if err == nil {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
	}
	if isLicense(err) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE
	}
	if errors.Is(err, context.Canceled) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return postgresCategory(pgErr.Code)
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlCategory(mysqlErr.Number)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
	}
	if isConnectionError(err) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.ENOMEM) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED
	}
	return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
}

// isConnectionError tells whether a connection could not be opened, or was lost.
func isConnectionError(err error) bool {
	var (
		connectErr *pgconn.ConnectError
		opErr      *net.OpError
		dnsErr     *net.DNSError
	)
	return errors.As(err, &connectErr) ||
		errors.As(err, &opErr) ||
		errors.As(err, &dnsErr) ||
		errors.Is(err, mysql.ErrInvalidConn) ||
		errors.Is(err, driver.ErrBadConn)
}
