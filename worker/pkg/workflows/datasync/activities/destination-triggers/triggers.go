// Package destinationtriggers_activity takes the triggers of the destination out of the
// way of a run, and puts them back when it ends.
//
// A destination trigger fires on what the run writes. When it writes into a table the job
// also copies, the destination ends up holding rows nothing read in the source: the copy
// of the source, plus what the trigger added.
//
// MySQL cannot suspend a trigger for a session the way it suspends foreign key checks, so
// the triggers of the tables the job writes are dropped before the run and created again
// after it, exactly as they were: same definer, same sql_mode and collation, same place
// among the triggers of their event. PostgreSQL disables them instead, and puts each back
// in the state it was in: a trigger can fire always, only in replica sessions or never,
// and one the user had disabled must stay so.
//
// This is not left to the replication role Athanor writes in. That role silences the
// ordinary triggers but wakes the ones set ENABLE REPLICA, and Benthos never takes it: the
// two engines would copy different rows. Suspending the triggers here, for both, is what
// keeps them doing the same work.
//
// What puts them back is recorded for the job before anything is suspended. A run that
// fails puts them back on its way out; a run stopped before it could — terminated, or its
// worker lost for good — leaves the record to the next run of the job, which puts them
// back when it ends.
//
// Only the triggers of the tables of the job are touched: a destination holding triggers
// on other tables keeps them, and a job writing to tables without triggers changes nothing.
// SQL Server is left alone for now.
package destinationtriggers_activity

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	sqlmanager_mysql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mysql"
	sqlmanager_postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Trigger is a destination trigger taken out of the way, with what puts it back.
type Trigger struct {
	// Schema is the schema of the trigger itself, which is where it is dropped from.
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// Table is the table it fires on.
	Table string `json:"table"`
	// Suspend takes the trigger out of the way of the run.
	Suspend string `json:"suspend"`
	// Restore puts it back as it was: statements run in order on one session.
	Restore []string `json:"restore"`
	// Reset, when set, gives the session back the settings Restore changed. It runs after
	// Restore whether it succeeded or not.
	Reset string `json:"reset,omitempty"`
}

// key identifies a trigger on a destination: a PostgreSQL trigger is named per table.
func (t *Trigger) key() string {
	return t.Schema + "\x1f" + t.Table + "\x1f" + t.Name
}

// suspended is what one destination connection held, recorded so that the restoring
// activity puts back exactly what the suspending one took away, even when it runs on
// another worker or in another run.
type suspended struct {
	ConnectionID string     `json:"connectionId"`
	Triggers     []*Trigger `json:"triggers"`
}

// merge adds to what a destination has recorded the triggers found on it now. A trigger
// already recorded keeps its record: it was read before an earlier attempt or an earlier
// run took it out of the way, and what is read now is its suspended state, or nothing.
func merge(recorded []*suspended, connectionID string, found []*Trigger) []*suspended {
	i := slices.IndexFunc(recorded, func(s *suspended) bool { return s.ConnectionID == connectionID })
	if i < 0 {
		if len(found) == 0 {
			return recorded
		}
		return append(recorded, &suspended{ConnectionID: connectionID, Triggers: found})
	}
	destination := recorded[i]
	for _, trigger := range found {
		known := slices.ContainsFunc(destination.Triggers, func(t *Trigger) bool { return t.key() == trigger.key() })
		if !known {
			destination.Triggers = append(destination.Triggers, trigger)
		}
	}
	return recorded
}

// toTriggers keeps, among the triggers of the destination, the ones a run must take out of
// its way, and pairs each of them with what puts it back.
func toTriggers(driver string, found []*sqlmanager_shared.TableTrigger) ([]*Trigger, error) {
	switch driver {
	case sqlmanager_shared.MysqlDriver:
		return mysqlTriggers(found)
	case sqlmanager_shared.PostgresDriver:
		return postgresTriggers(found), nil
	default:
		return nil, nil
	}
}

// mysqlTriggers drops each trigger and creates it again as MySQL describes it. They come
// back in the order they fire in: a trigger created after the others of its event fires
// after them, which is how the place of each is kept.
func mysqlTriggers(found []*sqlmanager_shared.TableTrigger) ([]*Trigger, error) {
	ordered := slices.Clone(found)
	for _, t := range ordered {
		if t.Mysql == nil {
			return nil, fmt.Errorf("trigger %s of %s.%s read without what it was created with", t.TriggerName, t.Schema, t.Table)
		}
	}
	slices.SortStableFunc(ordered, func(a, b *sqlmanager_shared.TableTrigger) int {
		return cmp.Or(
			cmp.Compare(a.Schema, b.Schema),
			cmp.Compare(a.Table, b.Table),
			cmp.Compare(a.Mysql.Event, b.Mysql.Event),
			cmp.Compare(a.Mysql.Timing, b.Mysql.Timing),
			cmp.Compare(a.Mysql.ActionOrder, b.Mysql.ActionOrder),
		)
	})
	triggers := make([]*Trigger, 0, len(ordered))
	for _, t := range ordered {
		schema := t.Schema
		if t.TriggerSchema != nil && *t.TriggerSchema != "" {
			schema = *t.TriggerSchema
		}
		definer, err := mysqlAccount(t.Mysql.Definer)
		if err != nil {
			return nil, fmt.Errorf("trigger %s of %s.%s: %w", t.TriggerName, t.Schema, t.Table, err)
		}
		triggers = append(triggers, &Trigger{
			Schema:  schema,
			Name:    t.TriggerName,
			Table:   t.Table,
			Suspend: fmt.Sprintf("DROP TRIGGER IF EXISTS %s.%s", quoteMysql(schema), quoteMysql(t.TriggerName)),
			Restore: []string{
				// The session keeps its own settings, given back by Reset.
				"SET @husonym_sql_mode = @@SESSION.sql_mode, @husonym_collation = @@SESSION.collation_connection",
				// A trigger keeps the sql_mode and the collation of the session it is created in.
				fmt.Sprintf("SET SESSION sql_mode = %s, collation_connection = %s",
					quoteMysqlString(t.Mysql.SqlMode), quoteMysqlString(t.Mysql.CollationConnection)),
				fmt.Sprintf("CREATE DEFINER = %s TRIGGER IF NOT EXISTS %s.%s %s %s ON %s.%s FOR EACH %s %s",
					definer, quoteMysql(schema), quoteMysql(t.TriggerName), t.Mysql.Timing, t.Mysql.Event,
					quoteMysql(t.Schema), quoteMysql(t.Table), t.Mysql.Orientation, t.Mysql.Statement),
			},
			Reset: "SET SESSION sql_mode = @husonym_sql_mode, collation_connection = @husonym_collation",
		})
	}
	return triggers, nil
}

// mysqlAccount quotes a definer as information_schema gives it, user@host. The host
// cannot hold an @, the user can: the last one separates them. Each part is quoted as an
// identifier, which reads the same whatever the sql_mode, backslashes included.
func mysqlAccount(definer string) (string, error) {
	at := strings.LastIndex(definer, "@")
	if at < 0 {
		return "", fmt.Errorf("definer %q is not an account", definer)
	}
	return quoteMysql(definer[:at]) + "@" + quoteMysql(definer[at+1:]), nil
}

// postgresEnable is the clause putting a trigger back in the state it was read in.
var postgresEnable = map[string]string{
	"O": "ENABLE TRIGGER",
	"R": "ENABLE REPLICA TRIGGER",
	"A": "ENABLE ALWAYS TRIGGER",
}

// postgresTriggers disables each trigger that can fire and restores the state it had. A
// trigger already disabled is left out: the run has nothing to fear from it, and enabling
// it afterwards would undo the user's choice.
func postgresTriggers(found []*sqlmanager_shared.TableTrigger) []*Trigger {
	triggers := make([]*Trigger, 0, len(found))
	for _, t := range found {
		enable, fires := postgresEnable[t.EnabledState]
		if !fires {
			continue
		}
		table := quotePostgres(t.Schema) + "." + quotePostgres(t.Table)
		triggers = append(triggers, &Trigger{
			Schema:  t.Schema,
			Name:    t.TriggerName,
			Table:   t.Table,
			Suspend: fmt.Sprintf("ALTER TABLE %s DISABLE TRIGGER %s", table, quotePostgres(t.TriggerName)),
			Restore: []string{fmt.Sprintf("ALTER TABLE %s %s %s", table, enable, quotePostgres(t.TriggerName))},
		})
	}
	return triggers
}

// Identifiers are quoted the way the sql managers quote them.
var (
	quoteMysql    = sqlmanager_mysql.EscapeMysqlColumn
	quotePostgres = sqlmanager_postgres.EscapePgColumn
)

// quoteMysqlString writes a MySQL string literal for a setting value — mode names, a
// collation — which holds neither a quote nor a backslash; a doubled quote reads the same
// under every sql_mode, a backslash would not.
func quoteMysqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// tablesOf turns the tables of the job into what the sql manager reads triggers for.
func tablesOf(tables []TableRef) []*sqlmanager_shared.SchemaTable {
	schemaTables := make([]*sqlmanager_shared.SchemaTable, 0, len(tables))
	for _, t := range tables {
		schemaTables = append(schemaTables, &sqlmanager_shared.SchemaTable{Schema: t.Schema, Table: t.Table})
	}
	return schemaTables
}

// TableRef names a table of the job.
type TableRef struct {
	Schema string
	Table  string
}

// triggerReader reads the triggers of a destination.
type triggerReader interface {
	GetSchemaTableTriggers(
		ctx context.Context,
		tables []*sqlmanager_shared.SchemaTable,
	) ([]*sqlmanager_shared.TableTrigger, error)
}
