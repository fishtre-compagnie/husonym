package sqlmanager_mssql

import (
	"context"
	"strconv"

	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// GetDatabaseSchema gives the columns of every table of a user.
func (m *Manager) GetDatabaseSchema(
	ctx context.Context,
) ([]*sqlmanager_shared.DatabaseSchemaRow, error) {
	rows, err := m.querier.GetColumnsOfUserTables(ctx, m.db)
	if err != nil {
		return nil, err
	}
	return toDatabaseSchemaRows(rows), nil
}

// GetSchemaColumnMap gives the columns of every table of a user, by table then by column.
func (m *Manager) GetSchemaColumnMap(
	ctx context.Context,
) (map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow, error) {
	dbSchemas, err := m.GetDatabaseSchema(ctx)
	if err != nil {
		return nil, err
	}
	return sqlmanager_shared.GetUniqueSchemaColMappings(dbSchemas), nil
}

// GetDatabaseTableSchemasBySchemasAndTables gives the columns of the given tables.
func (m *Manager) GetDatabaseTableSchemasBySchemasAndTables(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) ([]*sqlmanager_shared.DatabaseSchemaRow, error) {
	if len(tables) == 0 {
		return []*sqlmanager_shared.DatabaseSchemaRow{}, nil
	}
	requested := make([]mssql_queries.SchemaTable, len(tables))
	for i, table := range tables {
		requested[i] = mssql_queries.SchemaTable{Schema: table.Schema, Table: table.Table}
	}
	resolved, err := m.querier.ResolveTables(ctx, m.db, requested)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return []*sqlmanager_shared.DatabaseSchemaRow{}, nil
	}
	ids := make([]int64, len(resolved))
	for i, table := range resolved {
		ids[i] = table.ObjectID
	}
	rows, err := m.querier.GetColumns(ctx, m.db, ids)
	if err != nil {
		return nil, err
	}
	return toDatabaseSchemaRows(rows), nil
}

func toDatabaseSchemaRows(rows []*mssql_queries.GetColumnsRow) []*sqlmanager_shared.DatabaseSchemaRow {
	output := make([]*sqlmanager_shared.DatabaseSchemaRow, len(rows))
	for i, row := range rows {
		output[i] = toDatabaseSchemaRow(row)
	}
	return output
}

// generatedAlways names the kinds of columns the server fills by itself.
var generatedAlways = map[int]string{
	1: "GENERATED ALWAYS AS ROW START",
	2: "GENERATED ALWAYS AS ROW END",
	5: "GENERATED ALWAYS AS TRANSACTION_ID_START",
	6: "GENERATED ALWAYS AS TRANSACTION_ID_END",
	7: "GENERATED ALWAYS AS SEQUENCE_NUMBER_START",
	8: "GENERATED ALWAYS AS SEQUENCE_NUMBER_END",
}

func toDatabaseSchemaRow(row *mssql_queries.GetColumnsRow) *sqlmanager_shared.DatabaseSchemaRow {
	column := toColumn(row)

	var generatedType *string
	if column.IsComputed {
		generatedType = &column.ComputedDefinition
	} else if label, ok := generatedAlways[column.GeneratedAlways]; ok {
		generatedType = &label
	}

	var identityGeneration *string
	var identitySeed, identityIncrement *int
	if column.IsIdentity {
		identity := "IDENTITY(" + column.IdentitySeed + "," + column.IdentityIncrement + ")"
		identityGeneration = &identity
		identitySeed = toInt(column.IdentitySeed)
		identityIncrement = toInt(column.IdentityIncrement)
	}

	return &sqlmanager_shared.DatabaseSchemaRow{
		TableSchema:            row.TableSchema,
		TableName:              row.TableName,
		ColumnName:             column.Name,
		DataType:               column.TypeName,
		ColumnDefault:          column.DefaultDefinition,
		IsNullable:             column.IsNullable,
		CharacterMaximumLength: ddl.CharacterLength(column.BaseTypeName, column.MaxLength),
		NumericPrecision:       column.Precision,
		NumericScale:           column.Scale,
		OrdinalPosition:        column.ColumnID,
		GeneratedType:          generatedType,
		IdentityGeneration:     identityGeneration,
		IdentitySeed:           identitySeed,
		IdentityIncrement:      identityIncrement,
		UpdateAllowed:          !column.IsIdentity && !column.IsComputed && generatedType == nil,
	}
}

// toInt reads a number of the catalog, or gives nothing for one an int does not hold: an
// identity may be a decimal of 38 digits.
func toInt(value string) *int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &n
}
