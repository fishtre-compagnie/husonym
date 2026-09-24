// Package connectionchecks tells whether a connection can do what its role in a job needs.
//
// What a connection must be able to do depends on how the job uses it: a source is read,
// and may be a read-only replica; a destination is written, on a server that accepts
// writes, emptied first when the job says so, and has the triggers of its tables taken out
// of the way of the run and put back. The run asks this at its start, and stops on what it
// finds; the API asks it when a connection is tested in its role, so that the person finds
// out before the run does. Both call this package: they cannot tell different stories.
//
// Each check answers with findings rather than sentences: what was checked, how serious it
// is, on which table, what is missing, and the statement that grants it when there is one.
package connectionchecks

import (
	"context"
	"database/sql"
	"slices"
	"strings"
)

// Db is what the checks need from a connection.
type Db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Dialect is the database a connection speaks. SQL Server is not checked yet.
type Dialect int

const (
	MySQL Dialect = iota + 1
	Postgres
)

// Check names what a finding is about.
type Check string

const (
	// CheckTableExists: the table, or a column the run writes, is there.
	CheckTableExists Check = "table_exists"
	// CheckReadable: the source can read the table.
	CheckReadable Check = "readable"
	// CheckServerWritable: the destination server accepts writes at all.
	CheckServerWritable Check = "server_writable"
	// CheckWritable: the destination can read, insert, update and delete rows of the table.
	CheckWritable Check = "writable"
	// CheckTruncate: the destination can empty the table before writing it.
	CheckTruncate Check = "truncate"
	// CheckTriggers: the destination can take the triggers of the table out of the way.
	CheckTriggers Check = "triggers"
	// CheckTriggerDefiner: the destination can put a trigger back as its definer.
	CheckTriggerDefiner Check = "trigger_definer"
	// CheckForeignKeySuspension: the destination can suspend foreign keys, as Athanor does.
	CheckForeignKeySuspension Check = "foreign_key_suspension"
)

// Level says what a finding does to a run.
type Level int

const (
	// Blocking: the run stops on it.
	Blocking Level = iota + 1
	// Warning: the run may stop on it, depending on what the caller cannot see, such as the
	// engine a deployment runs by default.
	Warning
)

// Finding is one thing a connection cannot do that its role needs.
type Finding struct {
	Check Check
	Level Level
	// Table is the table concerned, as schema.table; empty for the server as a whole.
	Table string
	// Missing names what the account lacks: privileges, or the column names absent.
	Missing []string
	// Message says it in a sentence naming the connection, as the run reports it.
	Message string
	// Remedy is the statement that grants what is missing, for someone allowed to run it;
	// empty when no statement does.
	Remedy string
}

// Table is a table of a job, and the columns a run writes into it, generated ones left out.
type Table struct {
	Schema  string
	Table   string
	Columns []string
}

func (t *Table) String() string { return t.Schema + "." + t.Table }

// DestinationOptions are what a job does to a destination beyond writing rows, each of which
// needs more of the account.
type DestinationOptions struct {
	// CreatesTables: the run creates the tables and columns the destination lacks.
	CreatesTables bool
	// Truncates: the run empties each table before writing it.
	Truncates bool
	// SuspendsForeignKeys: the run suspends foreign keys while it writes, as Athanor does on
	// PostgreSQL.
	SuspendsForeignKeys bool
}

// Source checks that a connection can be read as the source of a job, on its tables.
func Source(ctx context.Context, db Db, dialect Dialect, name string, tables []*Table) ([]*Finding, error) {
	if dialect == MySQL {
		return checkMysqlSource(ctx, db, name, tables)
	}
	return checkPostgresSource(ctx, db, name, tables)
}

// Destination checks that a connection can be written as a destination of a job, on its
// tables, with what the job does to them.
func Destination(
	ctx context.Context,
	db Db,
	dialect Dialect,
	name string,
	tables []*Table,
	options DestinationOptions,
) ([]*Finding, error) {
	if dialect == MySQL {
		return checkMysqlDestination(ctx, db, name, tables, options.CreatesTables, options.Truncates)
	}
	return checkPostgresDestination(ctx, db, name, tables, options)
}

// Messages gives the sentence of each finding, as the run reports them.
func Messages(findings []*Finding) []string {
	messages := make([]string, 0, len(findings))
	for _, finding := range findings {
		messages = append(messages, finding.Message)
	}
	return messages
}

// sortFindings orders findings by message, so that the same connection reads the same way.
func sortFindings(findings []*Finding) {
	slices.SortFunc(findings, func(a, b *Finding) int { return strings.Compare(a.Message, b.Message) })
}

// blocking builds a finding that stops a run.
func blocking(check Check, table string, missing []string, remedy, message string) *Finding {
	return &Finding{Check: check, Level: Blocking, Table: table, Missing: missing, Message: message, Remedy: remedy}
}
