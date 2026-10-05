package sqlmanager_mssql

import (
	"context"
	"fmt"

	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// GetAllSchemas gives the schemas of a user.
func (m *Manager) GetAllSchemas(
	ctx context.Context,
) ([]*sqlmanager_shared.DatabaseSchemaNameRow, error) {
	rows, err := m.querier.GetAllSchemas(ctx, m.db)
	if err != nil {
		return nil, err
	}
	result := make([]*sqlmanager_shared.DatabaseSchemaNameRow, len(rows))
	for i, row := range rows {
		result[i] = &sqlmanager_shared.DatabaseSchemaNameRow{SchemaName: row}
	}
	return result, nil
}

// GetAllTables gives the tables of a user.
func (m *Manager) GetAllTables(ctx context.Context) ([]*sqlmanager_shared.DatabaseTableRow, error) {
	rows, err := m.querier.GetAllTables(ctx, m.db)
	if err != nil {
		return nil, err
	}
	result := make([]*sqlmanager_shared.DatabaseTableRow, len(rows))
	for i, row := range rows {
		result[i] = &sqlmanager_shared.DatabaseTableRow{SchemaName: row.TableSchema, TableName: row.TableName}
	}
	return result, nil
}

// GetRolePermissionsMap gives, by table, the privileges the connecting principal holds.
func (m *Manager) GetRolePermissionsMap(ctx context.Context) (map[string][]string, error) {
	rows, err := m.querier.GetRolePermissions(ctx, m.db)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve mssql role permissions: %w", err)
	}
	privileges := map[string][]string{}
	for _, permission := range rows {
		key := sqlmanager_shared.BuildTable(permission.TableSchema, permission.TableName)
		privileges[key] = append(privileges[key], permission.PrivilegeType)
	}
	return privileges, nil
}

// GetTableRowCount counts the rows of a table, those a condition keeps when one is given. The
// condition is SQL the caller wrote: it goes into the statement as it is.
func (m *Manager) GetTableRowCount(
	ctx context.Context,
	schema, table string,
	whereClause *string,
) (int64, error) {
	query := "SELECT COUNT(*) FROM " + ddl.QualifiedName(schema, table)
	if whereClause != nil && *whereClause != "" {
		query += " WHERE " + *whereClause
	}
	var count int64
	if err := m.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("unable to query table row count for mssql: %w", err)
	}
	return count, nil
}
