package connectiondata

import (
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// rowCountService is a service for a PostgreSQL connection whose catalog holds the table `tbl`
// of the schema `sch`, and the database double it reads and counts through. The double has no
// expectation for the count: each test states whether the count is issued.
func rowCountService(t *testing.T) (*SQLConnectionDataService, *sqlmanager.MockSqlDatabase) {
	t.Helper()
	database := sqlmanager.NewMockSqlDatabase(t)
	database.EXPECT().
		GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything).
		Return([]*sqlmanager_shared.DatabaseSchemaRow{
			{TableSchema: "sch", TableName: "tbl", ColumnName: "id", DataType: "integer"},
		}, nil)
	database.EXPECT().Close().Return()
	manager := sqlmanager.NewMockSqlManagerClient(t)
	manager.EXPECT().
		NewSqlConnection(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(sqlmanager.NewPostgresSqlConnection(database), nil)

	connection := &mgmtv1alpha1.Connection{
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	}
	return NewSQLConnectionDataService(
		testutil.GetTestLogger(t), sqlconnect.NewMockSqlConnector(t), manager, connection,
	), database
}

// A row count names a schema and a table of the catalog: one the catalog does not hold is
// refused as a sample of it is, and no count is issued.
func Test_GetTableRowCount_RefusesANameAbsentFromTheCatalog(t *testing.T) {
	for name, names := range map[string]struct{ schema, table string }{
		"schema absent": {"other", "tbl"},
		"table absent":  {"sch", "other"},
	} {
		t.Run(name, func(t *testing.T) {
			service, database := rowCountService(t)

			_, err := service.GetTableRowCount(t.Context(), names.schema, names.table, nil)

			require.EqualError(t, err, "invalid schema or table: invalid_argument: must provide valid schema and table")
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			database.AssertNotCalled(t, "GetTableRowCount", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// A table of the catalog is counted once the check passes: the schema, then the table, with the
// WHERE clause of the request as written.
func Test_GetTableRowCount_CountsATableOfTheCatalog(t *testing.T) {
	service, database := rowCountService(t)
	where := "id > 3"
	database.EXPECT().GetTableRowCount(mock.Anything, "sch", "tbl", &where).Return(int64(7), nil).Once()

	count, err := service.GetTableRowCount(t.Context(), "sch", "tbl", &where)

	require.NoError(t, err)
	require.Equal(t, int64(7), count)
}
