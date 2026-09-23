package mcp_server

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type introspectSchemaInput struct {
	ConnectionId string   `json:"connection_id"    jsonschema:"the id of the connection, as list_connections gives it"`
	Tables       []string `json:"tables,omitempty" jsonschema:"tables to describe, as schema.table, at most 20 per call; leave out to list the tables"`
}

type introspectSchemaOutput struct {
	Tables []tableSchema `json:"tables"`
}

type tableSchema struct {
	Table        string         `json:"table"                   jsonschema:"schema.table"`
	Columns      []columnSchema `json:"columns,omitempty"`
	PrimaryKey   []string       `json:"primary_key,omitempty"`
	Unique       [][]string     `json:"unique,omitempty"        jsonschema:"each entry is a set of columns whose values are unique together"`
	ForeignKeys  []foreignKey   `json:"foreign_keys,omitempty"  jsonschema:"the tables this one points at"`
	ReferencedBy []reference    `json:"referenced_by,omitempty" jsonschema:"the tables that point at this one"`
}

type columnSchema struct {
	Name               string  `json:"name"`
	DataType           string  `json:"data_type"`
	Nullable           bool    `json:"nullable"`
	Default            *string `json:"default,omitempty"`
	Generated          *string `json:"generated,omitempty"            jsonschema:"set when the database computes the column; a job must not write it"`
	Identity           *string `json:"identity,omitempty"             jsonschema:"set when the column is an identity"`
	CharacterMaxLength *int32  `json:"character_max_length,omitempty" jsonschema:"the most characters the column accepts"`
}

type foreignKey struct {
	Columns           []string `json:"columns"`
	References        string   `json:"references"         jsonschema:"schema.table"`
	ReferencedColumns []string `json:"referenced_columns"`
	Nullable          bool     `json:"nullable"           jsonschema:"true when a row may leave the reference empty"`
}

type reference struct {
	Table             string   `json:"table"              jsonschema:"schema.table of the referencing table"`
	Columns           []string `json:"columns"            jsonschema:"in the referencing table"`
	ReferencedColumns []string `json:"referenced_columns" jsonschema:"in this table"`
}

func addIntrospectSchema(server *mcp.Server, reader *novalues.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "introspect_schema",
		Description: "Read the structure of a SQL connection. Without tables, list its tables. With tables, " +
			"give their columns, types, keys and the foreign keys in both directions. Never reads a row.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, introspectSchema(reader))
}

func introspectSchema(reader *novalues.Reader) mcp.ToolHandlerFor[introspectSchemaInput, introspectSchemaOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input introspectSchemaInput,
	) (*mcp.CallToolResult, introspectSchemaOutput, error) {
		if len(input.Tables) == 0 {
			tables, err := listTables(ctx, reader, input.ConnectionId)
			if err != nil {
				return nil, introspectSchemaOutput{}, err
			}
			return nil, introspectSchemaOutput{Tables: tables}, nil
		}
		tables, err := describeTables(ctx, reader, input.ConnectionId, input.Tables)
		if err != nil {
			return nil, introspectSchemaOutput{}, err
		}
		return nil, introspectSchemaOutput{Tables: tables}, nil
	}
}

func listTables(ctx context.Context, reader *novalues.Reader, connectionId string) ([]tableSchema, error) {
	tables, err := reader.Tables(ctx, connectionId)
	if err != nil {
		return nil, fmt.Errorf("unable to list the tables of connection %s: %w", connectionId, err)
	}
	out := make([]tableSchema, 0, len(tables))
	for _, table := range tables {
		out = append(out, tableSchema{Table: tableKey(table.GetSchemaName(), table.GetTableName())})
	}
	slices.SortFunc(out, func(a, b tableSchema) int { return cmp.Compare(a.Table, b.Table) })
	return out, nil
}

func describeTables(
	ctx context.Context,
	reader *novalues.Reader,
	connectionId string,
	asked []string,
) ([]tableSchema, error) {
	columns, err := reader.Columns(ctx, connectionId)
	if err != nil {
		return nil, fmt.Errorf("unable to read the schema of connection %s: %w", connectionId, err)
	}
	columnsByTable := map[string][]*mgmtv1alpha1.DatabaseColumn{}
	known := map[string]bool{}
	for _, column := range columns {
		key := tableKey(column.GetSchema(), column.GetTable())
		columnsByTable[key] = append(columnsByTable[key], column)
		known[key] = true
	}
	selected, err := selectTables(asked, known)
	if err != nil {
		return nil, err
	}

	constraints, err := reader.Constraints(ctx, connectionId)
	if err != nil {
		return nil, fmt.Errorf("unable to read the keys of connection %s: %w", connectionId, err)
	}
	referencedBy := referencesByTable(constraints.GetForeignKeyConstraints())

	out := make([]tableSchema, 0, len(selected))
	for _, table := range selected {
		out = append(out, tableSchema{
			Table:        table,
			Columns:      describeColumns(columnsByTable[table]),
			PrimaryKey:   constraints.GetPrimaryKeyConstraints()[table].GetColumns(),
			Unique:       uniqueSets(constraints, table),
			ForeignKeys:  foreignKeys(constraints.GetForeignKeyConstraints()[table]),
			ReferencedBy: referencedBy[table],
		})
	}
	return out, nil
}

// describeColumns keeps the order the database gives, which is the order of the table.
func describeColumns(columns []*mgmtv1alpha1.DatabaseColumn) []columnSchema {
	out := make([]columnSchema, 0, len(columns))
	for _, column := range columns {
		out = append(out, columnSchema{
			Name:     column.GetColumn(),
			DataType: column.GetDataType(),
			// The SQL builders write YES or NO here (sqlmanager_shared.NullableString).
			Nullable:           column.GetIsNullable() == "YES",
			Default:            column.ColumnDefault,
			Generated:          column.GeneratedType,
			Identity:           column.IdentityGeneration,
			CharacterMaxLength: column.CharacterMaximumLength,
		})
	}
	return out
}

// uniqueSets merges the unique constraints and the unique indexes of a table: both forbid two
// rows from sharing values, which is all a caller choosing a transformer needs to know.
func uniqueSets(constraints *mgmtv1alpha1.GetConnectionTableConstraintsResponse, table string) [][]string {
	var sets [][]string
	for _, constraint := range constraints.GetUniqueConstraints()[table].GetConstraints() {
		sets = append(sets, constraint.GetColumns())
	}
	for _, index := range constraints.GetUniqueIndexes()[table].GetIndexes() {
		sets = append(sets, index.GetColumns())
	}
	slices.SortFunc(sets, func(a, b []string) int {
		return cmp.Compare(strings.Join(a, ","), strings.Join(b, ","))
	})
	return slices.CompactFunc(sets, slices.Equal)
}

func foreignKeys(tables *mgmtv1alpha1.ForeignConstraintTables) []foreignKey {
	out := make([]foreignKey, 0, len(tables.GetConstraints()))
	for _, constraint := range tables.GetConstraints() {
		out = append(out, foreignKey{
			Columns:           constraint.GetColumns(),
			References:        constraint.GetForeignKey().GetTable(),
			ReferencedColumns: constraint.GetForeignKey().GetColumns(),
			Nullable:          slices.Contains(constraint.GetNotNullable(), false),
		})
	}
	return out
}

// referencesByTable turns the foreign keys around: for each table, who points at it.
func referencesByTable(foreign map[string]*mgmtv1alpha1.ForeignConstraintTables) map[string][]reference {
	out := map[string][]reference{}
	for _, from := range slices.Sorted(maps.Keys(foreign)) {
		for _, constraint := range foreign[from].GetConstraints() {
			to := constraint.GetForeignKey().GetTable()
			out[to] = append(out[to], reference{
				Table:             from,
				Columns:           constraint.GetColumns(),
				ReferencedColumns: constraint.GetForeignKey().GetColumns(),
			})
		}
	}
	return out
}
