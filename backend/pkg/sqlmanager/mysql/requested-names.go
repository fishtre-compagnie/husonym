package sqlmanager_mysql

import (
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// requestedNames gives back the spelling of the databases and tables a caller asked about.
//
// A server with lower_case_table_names 1 or 2 finds a table asked for in any case, and
// answers with its own spelling, folded to lower case. A caller then compares the answer
// with its request — a job's tables, spelled as its source spells them — and finds nothing:
// the destination looks empty of the tables it holds, its triggers are not taken out of
// the way, its tables are not emptied. On a server comparing names exactly, the answer
// already has the requested spelling, and nothing changes.
type requestedNames struct {
	schemas map[string]string
	tables  map[string]sqlmanager_shared.SchemaTable
}

func newRequestedNames(tables []*sqlmanager_shared.SchemaTable) *requestedNames {
	names := &requestedNames{schemas: map[string]string{}, tables: map[string]sqlmanager_shared.SchemaTable{}}
	for _, t := range tables {
		names.schemas[strings.ToLower(t.Schema)] = t.Schema
		names.tables[strings.ToLower(t.String())] = *t
	}
	return names
}

func newRequestedTables(schema string, tables []string) *requestedNames {
	schemaTables := make([]*sqlmanager_shared.SchemaTable, len(tables))
	for i, table := range tables {
		schemaTables[i] = &sqlmanager_shared.SchemaTable{Schema: schema, Table: table}
	}
	return newRequestedNames(schemaTables)
}

func newRequestedSchemas(schemas []string) *requestedNames {
	names := newRequestedNames(nil)
	for _, schema := range schemas {
		names.schemas[strings.ToLower(schema)] = schema
	}
	return names
}

// schema returns the requested spelling of a database the server answered with.
func (r *requestedNames) schema(schema string) string {
	if requested, ok := r.schemas[strings.ToLower(schema)]; ok {
		return requested
	}
	return schema
}

// table returns the requested spelling of a table the server answered with. A table that
// was not asked about — the parent of a foreign key — keeps its spelling, its database
// taking the requested one.
func (r *requestedNames) table(schema, table string) (requestedSchema, requestedTable string) {
	key := sqlmanager_shared.SchemaTable{Schema: schema, Table: table}.String()
	if requested, ok := r.tables[strings.ToLower(key)]; ok {
		return requested.Schema, requested.Table
	}
	return r.schema(schema), table
}
