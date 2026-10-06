package connectiondata

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// rowCountService is a service for a PostgreSQL connection, and the database double it counts
// through. The double expects the connection to be opened once and closed, and nothing else:
// each test states the count it expects.
func rowCountService(t *testing.T) (*SQLConnectionDataService, *sqlmanager.MockSqlDatabase) {
	t.Helper()
	database := sqlmanager.NewMockSqlDatabase(t)
	database.EXPECT().Close().Return().Once()
	manager := sqlmanager.NewMockSqlManagerClient(t)
	manager.EXPECT().
		NewSqlConnection(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(sqlmanager.NewPostgresSqlConnection(database), nil).
		Once()

	connection := &mgmtv1alpha1.Connection{
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	}
	return NewSQLConnectionDataService(
		testutil.GetTestLogger(t), sqlconnect.NewMockSqlConnector(t), manager, connection,
	), database
}

// A row count goes to the database as the request names it: the schema, then the table, with the
// WHERE clause as written. The catalog is not read first, so that a view is counted like a table.
func Test_GetTableRowCount_NamesTheSchemaThenTheTable(t *testing.T) {
	service, database := rowCountService(t)
	where := "id > 3"
	database.EXPECT().GetTableRowCount(mock.Anything, "sch", "tbl", &where).Return(int64(7), nil).Once()

	count, err := service.GetTableRowCount(t.Context(), "sch", "tbl", &where)

	require.NoError(t, err)
	require.Equal(t, int64(7), count)
}
