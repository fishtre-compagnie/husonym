package v1alpha1_connectionservice

import (
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
	"github.com/stretchr/testify/require"
)

// A MySQL probe that names a generated column is refused whatever the account holds: such
// columns are left out, as a run leaves them out of what it writes, and a table given
// without columns takes the others.
func Test_setMysqlColumns(t *testing.T) {
	stored := "STORED GENERATED"
	given := &connectionchecks.Table{Schema: "shop", Table: "people", Columns: []string{"id", "email", "EMAIL_LOWER"}}
	bare := &connectionchecks.Table{Schema: "shop", Table: "orders"}
	tables := []*connectionchecks.Table{given, bare}
	filled := map[*connectionchecks.Table]bool{given: false, bare: true}

	setMysqlColumns(tables, filled, []*sqlmanager_shared.DatabaseSchemaRow{
		{TableSchema: "shop", TableName: "people", ColumnName: "id"},
		{TableSchema: "shop", TableName: "people", ColumnName: "email"},
		{TableSchema: "shop", TableName: "people", ColumnName: "email_lower", GeneratedType: &stored},
		// a column the job does not write stays out of a table given with its columns
		{TableSchema: "shop", TableName: "people", ColumnName: "nickname"},
		{TableSchema: "Shop", TableName: "Orders", ColumnName: "id"},
		{TableSchema: "shop", TableName: "orders", ColumnName: "total", GeneratedType: &stored},
		{TableSchema: "shop", TableName: "orders", ColumnName: "note"},
		// a table not asked about is ignored
		{TableSchema: "shop", TableName: "stock", ColumnName: "id"},
	})

	require.Equal(t, []string{"id", "email"}, given.Columns)
	require.Equal(t, []string{"id", "note"}, bare.Columns)
}
