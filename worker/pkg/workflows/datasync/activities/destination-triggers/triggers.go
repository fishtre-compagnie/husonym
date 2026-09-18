// Package destinationtriggers_activity takes the triggers of the destination out of the
// way of a run, and puts them back when it ends.
//
// A destination trigger fires on what the run writes. When it writes into a table the job
// also copies, the destination ends up holding rows nothing read in the source: the copy
// of the source, plus what the trigger added. MySQL cannot suspend a trigger for a session
// the way it suspends foreign key checks, so the triggers of the tables the job writes are
// dropped before the run and created again after it, from the definition read before.
//
// Only the triggers of the tables of the job are touched: a destination holding triggers
// on other tables keeps them, and a job writing to tables without triggers changes nothing.
// PostgreSQL and SQL Server are left alone: both can disable a trigger without dropping it,
// which is what the one-pass writing will use.
package destinationtriggers_activity

import (
	"context"
	"fmt"
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Trigger is a destination trigger taken out of the way, with what recreates it.
type Trigger struct {
	// Schema is the schema of the trigger itself, which is where it is dropped from.
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// Table is the table it fires on, for the report.
	Table string `json:"table"`
	// Create is the statement putting it back, as the destination describes it.
	Create string `json:"create"`
}

// DropStatement removes the trigger from the destination.
func (t *Trigger) DropStatement() string {
	return fmt.Sprintf("DROP TRIGGER IF EXISTS %s.%s", quoteMysql(t.Schema), quoteMysql(t.Name))
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
	if driver != sqlmanager_shared.MysqlDriver {
		return nil
	}
	triggers := make([]*Trigger, 0, len(found))
	for _, t := range found {
		schema := t.Schema
		if t.TriggerSchema != nil && *t.TriggerSchema != "" {
			schema = *t.TriggerSchema
		}
		triggers = append(triggers, &Trigger{
			Schema: schema,
			Name:   t.TriggerName,
			Table:  t.Table,
			Create: t.Definition,
		})
	}
	return triggers
}

func quoteMysql(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
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
