package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// Every reference by name that an object of the database makes: a module to what its text
// names, a table to what its computed columns call, a check or default constraint to what its
// expression calls. A reference the server did not resolve — another database, a name that
// depends on the caller, an object that does not exist — has no id and keeps the names as
// written.
const getDependencies = `-- name: GetDependencies :many
SELECT DISTINCT
    d.referencing_id,
    RTRIM(o.type),
    o.parent_object_id,
    d.referenced_class,
    COALESCE(d.referenced_id, 0),
    COALESCE(RTRIM(ro.type), ''),
    COALESCE(rt.is_table_type, 0),
    COALESCE(rt.is_assembly_type, 0),
    COALESCE(ros.name, rts.name, d.referenced_schema_name, ''),
    COALESCE(ro.name, rt.name, d.referenced_entity_name, '')
FROM sys.sql_expression_dependencies d
JOIN sys.objects o ON o.object_id = d.referencing_id
LEFT JOIN sys.objects ro ON d.referenced_class = 1 AND ro.object_id = d.referenced_id
LEFT JOIN sys.schemas ros ON ros.schema_id = ro.schema_id
LEFT JOIN sys.types rt ON d.referenced_class = 6 AND rt.user_type_id = d.referenced_id
LEFT JOIN sys.schemas rts ON rts.schema_id = rt.schema_id
WHERE d.referencing_class = 1;
`

type GetDependenciesRow struct {
	ReferencingID int64
	// ReferencingType is the type code of the referencing object, without its padding.
	ReferencingType     string
	ReferencingParentID int64

	// ReferencedClass is 1 for an object or a column, 6 for a type.
	ReferencedClass int
	// ReferencedID is 0 for a reference the server did not resolve.
	ReferencedID int64
	// ReferencedType is the type code of a referenced object, empty for anything else.
	ReferencedType           string
	ReferencedIsTableType    bool
	ReferencedIsAssemblyType bool
	ReferencedSchema         string
	ReferencedName           string
}

// GetDependencies gives every reference by name of the database.
func (q *Queries) GetDependencies(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*GetDependenciesRow, error) {
	return queryRows(ctx, db, getDependencies, func(rows *sql.Rows, i *GetDependenciesRow) error {
		return rows.Scan(
			&i.ReferencingID,
			&i.ReferencingType,
			&i.ReferencingParentID,
			&i.ReferencedClass,
			&i.ReferencedID,
			&i.ReferencedType,
			&i.ReferencedIsTableType,
			&i.ReferencedIsAssemblyType,
			&i.ReferencedSchema,
			&i.ReferencedName,
		)
	})
}
