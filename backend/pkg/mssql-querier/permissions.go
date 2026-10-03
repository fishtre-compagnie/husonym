package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// For every table and view of a user: what the connecting principal may do with it, by the
// answer of the server for each of the four privileges, and by the grants it holds by name.
const getRolePermissions = `-- name: GetRolePermissions :many
WITH object_list AS (
    SELECT
        s.name COLLATE database_default AS table_schema,
        o.name COLLATE database_default AS table_name
    FROM sys.objects o
    JOIN sys.schemas s ON o.schema_id = s.schema_id
    WHERE o.type IN ('U', 'V') AND ` + userSchema + `
),
effective_permissions AS (
    SELECT
        ol.table_schema,
        ol.table_name,
        p.privilege_type,
        HAS_PERMS_BY_NAME(
            QUOTENAME(ol.table_schema) + '.' + QUOTENAME(ol.table_name), 'OBJECT', p.privilege_type
        ) AS perm_state
    FROM object_list ol
    CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE')) AS p (privilege_type)
),
explicit_permissions AS (
    SELECT
        s.name COLLATE database_default AS table_schema,
        o.name COLLATE database_default AS table_name,
        dp.permission_name COLLATE database_default AS privilege_type
    FROM sys.database_permissions dp
    JOIN sys.objects o ON dp.major_id = o.object_id
    JOIN sys.schemas s ON o.schema_id = s.schema_id
    WHERE dp.grantee_principal_id = DATABASE_PRINCIPAL_ID()
        AND o.type IN ('U', 'V') AND ` + userSchema + `
)
SELECT table_schema, table_name, privilege_type FROM effective_permissions WHERE perm_state = 1
UNION
SELECT table_schema, table_name, privilege_type FROM explicit_permissions
ORDER BY table_schema, table_name, privilege_type;
`

type GetRolePermissionsRow struct {
	TableSchema   string
	TableName     string
	PrivilegeType string
}

// GetRolePermissions gives the privileges the connecting principal holds on each table and view.
func (q *Queries) GetRolePermissions(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*GetRolePermissionsRow, error) {
	return queryRows(ctx, db, getRolePermissions, func(rows *sql.Rows, i *GetRolePermissionsRow) error {
		return rows.Scan(&i.TableSchema, &i.TableName, &i.PrivilegeType)
	})
}
