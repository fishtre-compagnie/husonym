// Package destinationtriggers_activity takes the triggers of the destination out of the
// way of a run, and puts them back when it ends.
//
// A destination trigger fires on what the run writes. When it writes into a table the job
// also copies, the destination ends up holding rows nothing read in the source: the copy
// of the source, plus what the trigger added.
//
// MySQL cannot suspend a trigger for a session the way it suspends foreign key checks, so
// the triggers of the tables the job writes are dropped before the run and created again
// after it, from the definition read before. PostgreSQL disables them instead, and puts
// each back in the state it was in: a trigger can fire always, only in replica sessions or
// never, and one the user had disabled must stay so.
//
// This is not left to the replication role Athanor writes in. That role silences the
// ordinary triggers but wakes the ones set ENABLE REPLICA, and Benthos never takes it: the
// two engines would copy different rows. Suspending the triggers here, for both, is what
// keeps them doing the same work.
//
// Only the triggers of the tables of the job are touched: a destination holding triggers
// on other tables keeps them, and a job writing to tables without triggers changes nothing.
// SQL Server is left alone for now.
package destinationtriggers_activity

import (
	"context"
	"fmt"
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Trigger is a destination trigger taken out of the way, with what puts it back.
type Trigger struct {
	// Schema is the schema of the trigger itself, which is where it is dropped from.
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// Table is the table it fires on, for the report.
	Table string `json:"table"`
	// Suspend takes the trigger out of the way of the run.
	Suspend string `json:"suspend"`
	// Restore puts it back as it was. The record keeps the name it had when it could only
	// recreate a dropped trigger, so a run suspended by a worker of that time is restored.
	Restore string `json:"create"`
}

// suspended is what one destination connection held, recorded in the run context so that
// the restoring activity puts back exactly what the suspending one took away, even when it
// runs on another worker.
type suspended struct {
	ConnectionID string     `json:"connectionId"`
	Triggers     []*Trigger `json:"triggers"`
}

// toTriggers keeps, among the triggers of the destination, the ones a run must take out of
// its way, and pairs each of them with what puts it back.
func toTriggers(driver string, found []*sqlmanager_shared.TableTrigger) []*Trigger {
	switch driver {
	case sqlmanager_shared.MysqlDriver:
		return mysqlTriggers(found)
	case sqlmanager_shared.PostgresDriver:
		return postgresTriggers(found)
	default:
		return nil
	}
}

// mysqlTriggers drops each trigger and recreates it from the definition MySQL gives.
func mysqlTriggers(found []*sqlmanager_shared.TableTrigger) []*Trigger {
	triggers := make([]*Trigger, 0, len(found))
	for _, t := range found {
		schema := t.Schema
		if t.TriggerSchema != nil && *t.TriggerSchema != "" {
			schema = *t.TriggerSchema
		}
		triggers = append(triggers, &Trigger{
			Schema:  schema,
			Name:    t.TriggerName,
			Table:   t.Table,
			Suspend: fmt.Sprintf("DROP TRIGGER IF EXISTS %s.%s", quoteMysql(schema), quoteMysql(t.TriggerName)),
			Restore: t.Definition,
		})
	}
	return triggers
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
			Restore: fmt.Sprintf("ALTER TABLE %s %s %s", table, enable, quotePostgres(t.TriggerName)),
		})
	}
	return triggers
}

func quoteMysql(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func quotePostgres(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
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

// triggerReader is what the activity needs from a destination.
type triggerReader interface {
	GetSchemaTableTriggers(
		ctx context.Context,
		tables []*sqlmanager_shared.SchemaTable,
	) ([]*sqlmanager_shared.TableTrigger, error)
	Exec(ctx context.Context, statement string) error
}
