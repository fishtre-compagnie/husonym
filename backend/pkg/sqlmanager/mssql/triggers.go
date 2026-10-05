package sqlmanager_mssql

import (
	"context"
	"fmt"
	"strings"

	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// GetSchemaTableTriggers gives the triggers of the given tables, each with the statement that
// creates it. A disabled trigger has the state D.
//
// It is a read of the triggers, not a plan: one query, which needs neither a compatibility
// level nor the right to read definitions, and which no table can make fail. It is asked of
// destinations, of which nothing else is known. A trigger whose text cannot be read — encrypted,
// held by an assembly, or hidden from the login — is left out and logged.
func (m *Manager) GetSchemaTableTriggers(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) ([]*sqlmanager_shared.TableTrigger, error) {
	if len(tables) == 0 {
		return []*sqlmanager_shared.TableTrigger{}, nil
	}
	rows, err := m.querier.GetTableTriggers(ctx, m.db)
	if err != nil {
		return nil, fmt.Errorf("unable to read the triggers: %w", err)
	}

	// The names are compared here, whatever their case: a trigger of a table whose name
	// differs by its case alone is given as well.
	requested := func(schema, table string) bool {
		for _, t := range tables {
			if strings.EqualFold(t.Schema, schema) && strings.EqualFold(t.Table, table) {
				return true
			}
		}
		return false
	}
	snapshot := &ddl.Snapshot{}
	parents := map[int64]bool{}
	for _, row := range rows {
		if !requested(row.TableSchema, row.TableName) {
			continue
		}
		if !parents[row.ParentID] {
			parents[row.ParentID] = true
			snapshot.Tables = append(snapshot.Tables, &ddl.Table{
				ObjectID: row.ParentID, Schema: row.TableSchema, Name: row.TableName,
			})
		}
		snapshot.Modules = append(snapshot.Modules, toTrigger(row))
	}

	// The tables carry no column and no index: nothing of them can be refused.
	plan, err := ddl.Build(snapshot)
	if err != nil {
		return nil, err
	}
	for _, skipped := range plan.Skipped {
		m.logger.Warn("a trigger is left out", "trigger", skipped.Object, "reason", skipped.Reason)
	}
	return plan.Triggers, nil
}
