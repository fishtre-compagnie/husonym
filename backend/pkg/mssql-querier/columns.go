package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// One row per column. The numbers of a column come from sys.columns: sys.types holds the
// greatest a type allows. bt is the system type a declared type is stored as; a CLR type has none.
const getColumns = `-- name: GetColumns :many
SELECT
    t.object_id,
    s.name,
    t.name,
    c.column_id,
    c.name,
    ts.name,
    ty.name,
    COALESCE(bt.name, ''),
    ty.is_user_defined,
    ty.is_assembly_type,
    COALESCE(ty.is_nullable, 0),
    ty.rule_object_id,
    ty.default_object_id,
    c.max_length,
    c.precision,
    c.scale,
    COALESCE(c.collation_name, ''),
    COALESCE(c.is_nullable, 0),
    c.is_ansi_padded,
    c.is_rowguidcol,
    c.is_filestream,
    COALESCE(c.is_sparse, 0),
    COALESCE(c.is_column_set, 0),
    COALESCE(c.is_hidden, 0),
    CASE WHEN c.encryption_type IS NULL THEN 0 ELSE 1 END,
    c.xml_collection_id,
    c.rule_object_id,
    c.default_object_id,
    COALESCE(c.generated_always_type, 0),
    COALESCE(dc.name, ''),
    COALESCE(dc.definition, ''),
    c.is_computed,
    COALESCE(cc.definition, ''),
    COALESCE(cc.is_persisted, 0),
    c.is_identity,
    COALESCE(CONVERT(nvarchar(60), ic.seed_value), ''),
    COALESCE(CONVERT(nvarchar(60), ic.increment_value), ''),
    COALESCE(ic.is_not_for_replication, 0),
    COALESCE(c.is_masked, 0),
    COALESCE(mc.masking_function, '')
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.columns c ON c.object_id = t.object_id
JOIN sys.types ty ON ty.user_type_id = c.user_type_id
JOIN sys.schemas ts ON ts.schema_id = ty.schema_id
LEFT JOIN sys.types bt ON bt.user_type_id = ty.system_type_id
LEFT JOIN sys.default_constraints dc ON dc.object_id = c.default_object_id AND dc.parent_object_id = c.object_id
LEFT JOIN sys.computed_columns cc ON cc.object_id = c.object_id AND cc.column_id = c.column_id
LEFT JOIN sys.identity_columns ic ON ic.object_id = c.object_id AND ic.column_id = c.column_id
LEFT JOIN sys.masked_columns mc ON mc.object_id = c.object_id AND mc.column_id = c.column_id
WHERE `

const orderColumns = `
ORDER BY s.name, t.name, c.column_id;
`

type GetColumnsRow struct {
	ObjectID    int64
	TableSchema string
	TableName   string
	ColumnID    int
	Name        string

	TypeSchema        string
	TypeName          string
	BaseTypeName      string
	IsUserDefinedType bool
	IsAssemblyType    bool
	TypeIsNullable    bool
	TypeRuleID        int64
	TypeDefaultID     int64

	MaxLength int
	Precision int
	Scale     int
	Collation string

	IsNullable      bool
	IsAnsiPadded    bool
	IsRowGuidCol    bool
	IsFilestream    bool
	IsSparse        bool
	IsColumnSet     bool
	IsHidden        bool
	IsEncrypted     bool
	XMLCollectionID int64
	RuleID          int64
	DefaultID       int64
	GeneratedAlways int

	// DefaultName is empty for a column whose default, if any, is not a constraint of its own.
	DefaultName       string
	DefaultDefinition string

	IsComputed         bool
	ComputedDefinition string
	IsPersisted        bool

	IsIdentity                bool
	IdentitySeed              string
	IdentityIncrement         string
	IdentityNotForReplication bool

	IsMasked        bool
	MaskingFunction string
}

func scanColumn(rows *sql.Rows, i *GetColumnsRow) error {
	return rows.Scan(
		&i.ObjectID,
		&i.TableSchema,
		&i.TableName,
		&i.ColumnID,
		&i.Name,
		&i.TypeSchema,
		&i.TypeName,
		&i.BaseTypeName,
		&i.IsUserDefinedType,
		&i.IsAssemblyType,
		&i.TypeIsNullable,
		&i.TypeRuleID,
		&i.TypeDefaultID,
		&i.MaxLength,
		&i.Precision,
		&i.Scale,
		&i.Collation,
		&i.IsNullable,
		&i.IsAnsiPadded,
		&i.IsRowGuidCol,
		&i.IsFilestream,
		&i.IsSparse,
		&i.IsColumnSet,
		&i.IsHidden,
		&i.IsEncrypted,
		&i.XMLCollectionID,
		&i.RuleID,
		&i.DefaultID,
		&i.GeneratedAlways,
		&i.DefaultName,
		&i.DefaultDefinition,
		&i.IsComputed,
		&i.ComputedDefinition,
		&i.IsPersisted,
		&i.IsIdentity,
		&i.IdentitySeed,
		&i.IdentityIncrement,
		&i.IdentityNotForReplication,
		&i.IsMasked,
		&i.MaskingFunction,
	)
}

// GetColumns gives the columns of the given tables.
func (q *Queries) GetColumns(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetColumnsRow, error) {
	return queryByIDs(ctx, db, getColumns+inIDs("t.object_id")+orderColumns, ids, scanColumn)
}

// GetColumnsOfUserTables gives the columns of every table of a user.
func (q *Queries) GetColumnsOfUserTables(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*GetColumnsRow, error) {
	return queryRows(ctx, db, getColumns+userSchema+" AND "+userTable+orderColumns, scanColumn)
}
