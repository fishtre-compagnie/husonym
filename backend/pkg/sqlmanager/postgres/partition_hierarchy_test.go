package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The partitions are read of the tables asked for, not of every partitioned table of their
// schemas: the others are not told, and one of them dropped meanwhile would fail the read.
func Test_GetTableInitStatements_ReadsThePartitionsOfTheTablesAskedFor(t *testing.T) {
	querier := pg_queries.NewMockQuerier(t)
	querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, []string{"app.events"}).
		Return([]*pg_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
			SchemaName: "app", TableName: "events", ColumnName: "id", DataType: "integer", IsNullable: "NO",
		}}, nil)
	querier.EXPECT().GetNonForeignKeyTableConstraintsBySchema(mock.Anything, mock.Anything, []string{"app"}).Return(nil, nil)
	querier.EXPECT().GetForeignKeyConstraintsBySchemas(mock.Anything, mock.Anything, []string{"app"}).Return(nil, nil)
	querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, []string{"app.events"}).Return(nil, nil)
	querier.EXPECT().GetPartitionedTablesBySchema(mock.Anything, mock.Anything, []string{"app"}).
		Return([]*pg_queries.GetPartitionedTablesBySchemaRow{
			{SchemaName: "app", TableName: "events", PartitionKey: "RANGE (id)"},
			{SchemaName: "app", TableName: "audit", PartitionKey: "RANGE (id)"},
		}, nil)
	// The only one: the mock fails the test on a read of the partitions of app.audit.
	querier.EXPECT().GetPartitionHierarchyByTable(mock.Anything, mock.Anything, "app.events").
		Return([]*pg_queries.GetPartitionHierarchyByTableRow{
			{SchemaName: "app", TableName: "events"},
			{
				SchemaName: "app", TableName: "events_1",
				ParentSchemaName: sql.NullString{String: "app", Valid: true},
				ParentTableName:  sql.NullString{String: "events", Valid: true},
				PartitionBound:   "FOR VALUES FROM (1) TO (100)",
			},
		}, nil).Once()

	statements, err := NewManager(querier, nil, func() {}).GetTableInitStatements(
		context.Background(), []*sqlmanager_shared.SchemaTable{{Schema: "app", Table: "events"}},
	)

	require.NoError(t, err)
	require.Len(t, statements, 1)
	require.Equal(t,
		`CREATE TABLE IF NOT EXISTS "app"."events" ("id" integer NOT NULL) PARTITION BY RANGE (id);`,
		statements[0].CreateTableStatement)
	require.Equal(t,
		[]string{`CREATE TABLE IF NOT EXISTS "app"."events_1" PARTITION OF "app"."events" FOR VALUES FROM (1) TO (100) ;`},
		statements[0].PartitionStatements)
}
