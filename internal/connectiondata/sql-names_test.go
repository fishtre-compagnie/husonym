package connectiondata

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// namesService is a service for a PostgreSQL connection whose table `tbl` of the schema `sch`
// exists, and whose own connection is the sqlmock database returned with it. The statements
// are matched as exact text, in the order they are expected.
func namesService(t *testing.T) (*SQLConnectionDataService, sqlmock.Sqlmock) {
	t.Helper()
	db, dbMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, dbMock.ExpectationsWereMet())
		// Closing the double is not under test; the service leaves its first result set open.
		_ = db.Close()
	})

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

	container := sqlconnect.NewMockSqlDbContainer(t)
	container.EXPECT().Open().Return(db, nil)
	container.EXPECT().Close().Return(nil)
	connector := sqlconnect.NewMockSqlConnector(t)
	connector.EXPECT().
		NewDbFromConnectionConfig(mock.Anything, mock.Anything, mock.Anything).
		Return(container, nil)

	connection := &mgmtv1alpha1.Connection{
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	}
	return NewSQLConnectionDataService(testutil.GetTestLogger(t), connector, manager, connection), dbMock
}

// The stream reads the table named by its schema and its table, in that order: the schema
// and the table of a test differ, so a statement naming them the other way round is refused.
func Test_StreamData_NamesTheSchemaThenTheTable(t *testing.T) {
	service, dbMock := namesService(t)
	dbMock.ExpectQuery(`SELECT * FROM "sch"."tbl"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	dbMock.ExpectQuery(`SELECT "id" FROM "sch"."tbl";`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	err := service.StreamData(t.Context(), nil, nil, "sch", "tbl")

	require.NoError(t, err)
}

// The window of a sample is the statement the service builds for the table: the schema, then
// the table.
func Test_SampleData_ReadsTheWindowOfTheSchemaThenTheTable(t *testing.T) {
	service, dbMock := namesService(t)
	dbMock.ExpectQuery(`SELECT * FROM (SELECT * FROM "sch"."tbl" LIMIT 1000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 5`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	err := service.SampleData(t.Context(), nil, "sch", "tbl", 5)

	require.NoError(t, err)
}
