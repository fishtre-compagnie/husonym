package sqlmanager_mssql

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
)

// GetMssqlColumnOverrideAndResetProperties tells what a sync has to do for a column: an
// identity column is written under IDENTITY_INSERT and reseeded afterwards; a column whose
// default draws from a sequence needs the reset alone.
func GetMssqlColumnOverrideAndResetProperties(
	columnInfo *sqlmanager_shared.DatabaseSchemaRow,
) (needsOverride, needsReset bool) {
	if columnInfo.IdentityGeneration != nil && *columnInfo.IdentityGeneration != "" {
		return true, true
	}
	if gotypeutil.CaseInsensitiveContains(columnInfo.ColumnDefault, "NEXT VALUE") {
		return false, true
	}
	return false, false
}

// BuildMssqlDeleteStatement returns DELETE FROM [schema].[table];
func BuildMssqlDeleteStatement(schema, table string) string {
	return "DELETE FROM " + ddl.QualifiedName(schema, table) + ";"
}

// tableLiteral names a table the way DBCC CHECKIDENT and OBJECT_ID take it: as a literal that
// holds the quoted name.
func tableLiteral(schema, table string) string {
	return ddl.QuoteLiteral(ddl.QualifiedName(schema, table))
}

// BuildMssqlIdentityColumnResetStatement makes the next identity value the seed, whatever the
// table held. After a DELETE the next value is the reseed value plus the increment: the table is
// reseeded one increment below its seed. A table that never generated a value starts at its
// seed by itself, and is left alone.
func BuildMssqlIdentityColumnResetStatement(
	schema, table string, identitySeed, identityIncrement *int,
) string {
	if identitySeed == nil || identityIncrement == nil {
		return BuildMssqlIdentityColumnResetCurrent(schema, table)
	}
	name := tableLiteral(schema, table)
	return fmt.Sprintf(
		"IF EXISTS (SELECT 1 FROM sys.identity_columns WHERE object_id = OBJECT_ID(%s, N'U') AND last_value IS NOT NULL)\n"+
			"DBCC CHECKIDENT (%s, RESEED, %d)",
		name, name, *identitySeed-*identityIncrement,
	)
}

// BuildMssqlIdentityColumnResetCurrent raises the identity value to the column's maximum.
func BuildMssqlIdentityColumnResetCurrent(schema, table string) string {
	return "DBCC CHECKIDENT (" + tableLiteral(schema, table) + ", RESEED)"
}

// BuildMssqlSetIdentityInsertStatement returns SET IDENTITY_INSERT [schema].[table] ON|OFF;
func BuildMssqlSetIdentityInsertStatement(schema, table string, enable bool) string {
	state := "OFF"
	if enable {
		state = "ON"
	}
	return "SET IDENTITY_INSERT " + ddl.QualifiedName(schema, table) + " " + state + ";"
}
