package sqlmanager_mssql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v7"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fastRetryOptions tries as often as the manager does, without its waits.
func fastRetryOptions() []backoff.RetryOption {
	return []backoff.RetryOption{
		backoff.WithBackOff(&backoff.ConstantBackOff{Interval: time.Millisecond}),
		backoff.WithMaxTries(sqlmanager_shared.CatalogReadAttempts),
	}
}

func newTestManager(t *testing.T) (*Manager, *mssql_queries.MockQuerier) {
	t.Helper()
	querier := mssql_queries.NewMockQuerier(t)
	manager := NewManager(querier, nil, nil, testutil.GetTestLogger(t))
	manager.retryOpts = fastRetryOptions
	return manager, querier
}

func versions(modified time.Time, ids ...int64) []*mssql_queries.GetObjectVersionsRow {
	rows := make([]*mssql_queries.GetObjectVersionsRow, len(ids))
	for i, id := range ids {
		rows[i] = &mssql_queries.GetObjectVersionsRow{ObjectID: id, ModifyDate: modified}
	}
	return rows
}

var (
	requestedUsers = []*sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "users"}}
	created        = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

// expectDatabase answers the first query of a read: a database recent enough.
func expectDatabase(querier *mssql_queries.MockQuerier) {
	querier.EXPECT().GetDatabaseInfo(mock.Anything, mock.Anything).
		Return(&mssql_queries.GetDatabaseInfoRow{CompatibilityLevel: 160, Collation: "Latin1_General_100_CI_AS", MajorVersion: 16}, nil).
		Once()
}

// expectUsers answers every read of the catalog but the versions, for one table of one column.
func expectUsers(querier *mssql_queries.MockQuerier, reads int) {
	querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, []mssql_queries.SchemaTable{{Schema: "dbo", Table: "users"}}).
		Return([]*mssql_queries.ResolveTablesRow{{Position: 0, ObjectID: 1, Schema: "dbo", Name: "users"}}, nil).Times(reads)
	querier.EXPECT().GetModuleHeaders(mock.Anything, mock.Anything).
		Return([]*mssql_queries.GetModuleHeadersRow{}, nil).Times(reads)
	querier.EXPECT().GetDependencies(mock.Anything, mock.Anything).
		Return([]*mssql_queries.GetDependenciesRow{}, nil).Times(reads)
	querier.EXPECT().GetColumns(mock.Anything, mock.Anything, []int64{1}).
		Return([]*mssql_queries.GetColumnsRow{{
			ObjectID: 1, TableSchema: "dbo", TableName: "users", ColumnID: 1, Name: "id",
			TypeSchema: "sys", TypeName: "int", BaseTypeName: "int", MaxLength: 4, Precision: 10,
		}}, nil).Times(reads)
	querier.EXPECT().GetIndexes(mock.Anything, mock.Anything, []int64{1}).
		Return([]*mssql_queries.GetIndexesRow{}, nil).Times(reads)
	querier.EXPECT().GetForeignKeys(mock.Anything, mock.Anything, []int64{1}).
		Return([]*mssql_queries.GetForeignKeysRow{}, nil).Times(reads)
	querier.EXPECT().GetCheckConstraints(mock.Anything, mock.Anything, []int64{1}).
		Return([]*mssql_queries.GetCheckConstraintsRow{}, nil).Times(reads)
	querier.EXPECT().GetTableNotices(mock.Anything, mock.Anything, []int64{1}, 16).
		Return([]*mssql_queries.GetTableNoticesRow{}, nil).Times(reads)
}

func Test_Manager_snapshot_ReadsTheCatalogAsOneState(t *testing.T) {
	t.Parallel()

	t.Run("versions that did not move: one read", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		expectUsers(querier, 1)
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(versions(created, 1, 2), nil).Times(2)

		snapshot, err := manager.snapshot(t.Context(), requestedUsers)

		require.NoError(t, err)
		require.Len(t, snapshot.Tables, 1)
		require.Equal(t, "users", snapshot.Tables[0].Name)
		require.Len(t, snapshot.Tables[0].Columns, 1)
		require.Equal(t, 160, snapshot.Database.CompatibilityLevel)
	})

	t.Run("a child modified during the first read: two reads", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		expectUsers(querier, 2)
		// First read: the constraint 2 is modified between the two looks.
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(versions(created, 1, 2), nil).Once()
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(append(versions(created, 1), versions(created.Add(time.Second), 2)...), nil).Once()
		// Second read: nothing moves.
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(append(versions(created, 1), versions(created.Add(time.Second), 2)...), nil).Times(2)

		snapshot, err := manager.snapshot(t.Context(), requestedUsers)

		require.NoError(t, err)
		require.Len(t, snapshot.Tables, 1)
	})

	t.Run("a child dropped during the first read: two reads", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		expectUsers(querier, 2)
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(versions(created, 1, 2), nil).Once()
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(versions(created, 1), nil).Times(3)

		_, err := manager.snapshot(t.Context(), requestedUsers)

		require.NoError(t, err)
	})

	t.Run("a catalog that keeps changing: an error after four reads", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		expectUsers(querier, sqlmanager_shared.CatalogReadAttempts)
		modified := created
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			RunAndReturn(func(context.Context, mysql_queries.DBTX, []int64) ([]*mssql_queries.GetObjectVersionsRow, error) {
				modified = modified.Add(time.Second)
				return versions(modified, 1), nil
			}).Times(2 * sqlmanager_shared.CatalogReadAttempts)

		_, err := manager.snapshot(t.Context(), requestedUsers)

		require.EqualError(t, err, "the catalog kept changing while it was read: gave up after 4 reads")
	})

	t.Run("a read the server gave up to let a change through: two reads", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		// The first read loses a deadlock against the statement that changes the table.
		deadlock := mssqldb.Error{Number: 1205, Message: "Transaction was deadlocked and has been chosen as the deadlock victim."}
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).Return(nil, deadlock).Once()
		expectUsers(querier, 1)
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
			Return(versions(created, 1), nil).Times(2)

		snapshot, err := manager.snapshot(t.Context(), requestedUsers)

		require.NoError(t, err)
		require.Len(t, snapshot.Tables, 1)
	})

	t.Run("a read the server gives up every time: its error after four reads", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		deadlock := mssqldb.Error{Number: 1205, Message: "Transaction was deadlocked and has been chosen as the deadlock victim."}
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).
			Return(nil, deadlock).Times(sqlmanager_shared.CatalogReadAttempts)

		_, err := manager.snapshot(t.Context(), requestedUsers)

		var failure mssqldb.Error
		require.ErrorAs(t, err, &failure)
		require.Equal(t, int32(1205), failure.Number)
	})

	t.Run("another error is not read again", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		denied := errors.New("the SELECT permission was denied")
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).Return(nil, denied).Once()

		_, err := manager.snapshot(t.Context(), requestedUsers)

		require.ErrorIs(t, err, denied)
	})
}

func Test_Manager_snapshot_CompatibilityLevel(t *testing.T) {
	t.Parallel()
	manager, querier := newTestManager(t)
	querier.EXPECT().GetDatabaseInfo(mock.Anything, mock.Anything).
		Return(&mssql_queries.GetDatabaseInfoRow{CompatibilityLevel: 120, MajorVersion: 16}, nil).Once()

	_, err := manager.snapshot(t.Context(), requestedUsers)

	require.EqualError(t, err, "compatibility level 120: 130 or more is required")
}

func Test_Manager_snapshot_Tables(t *testing.T) {
	t.Parallel()

	t.Run("no requested table is found: nothing else is read", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mssql_queries.ResolveTablesRow{}, nil).Once()

		snapshot, err := manager.snapshot(t.Context(), []*sqlmanager_shared.SchemaTable{
			{Schema: "dbo", Table: "gone"}, {Schema: "dbo", Table: "gone"}, {Schema: "dbo", Table: "too"},
		})

		require.NoError(t, err)
		require.Empty(t, snapshot.Tables)
		require.Equal(t, []sqlmanager_shared.SchemaTable{
			{Schema: "dbo", Table: "gone"}, {Schema: "dbo", Table: "too"},
		}, snapshot.Missing)
	})

	t.Run("two requests for one table give one table; a versioned table brings its history", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		expectDatabase(querier)
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mssql_queries.ResolveTablesRow{
				{
					Position: 0, ObjectID: 1, Schema: "hr", Name: "Staff", TemporalType: ddl.TemporalSystemVersioned,
					HistoryID: 2, HistorySchema: "hr", HistoryName: "staff_history",
					RetentionPeriod: 6, RetentionUnit: "MONTH", PeriodStartColumn: "from", PeriodEndColumn: "to",
				},
				{
					Position: 1, ObjectID: 1, Schema: "hr", Name: "Staff", TemporalType: ddl.TemporalSystemVersioned,
					HistoryID: 2, HistorySchema: "hr", HistoryName: "staff_history",
					RetentionPeriod: 6, RetentionUnit: "MONTH", PeriodStartColumn: "from", PeriodEndColumn: "to",
				},
			}, nil).Once()
		ids := []int64{1, 2}
		querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, ids).Return(versions(created, 1, 2), nil).Times(2)
		querier.EXPECT().GetModuleHeaders(mock.Anything, mock.Anything).Return(nil, nil).Once()
		querier.EXPECT().GetDependencies(mock.Anything, mock.Anything).Return(nil, nil).Once()
		querier.EXPECT().GetColumns(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
		querier.EXPECT().GetIndexes(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
		querier.EXPECT().GetForeignKeys(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
		querier.EXPECT().GetCheckConstraints(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
		querier.EXPECT().GetTableNotices(mock.Anything, mock.Anything, ids, 16).Return(nil, nil).Once()

		snapshot, err := manager.snapshot(t.Context(), []*sqlmanager_shared.SchemaTable{
			{Schema: "hr", Table: "staff"}, {Schema: "HR", Table: "STAFF"},
		})

		require.NoError(t, err)
		require.Empty(t, snapshot.Missing)
		require.Len(t, snapshot.Tables, 2)
		require.Equal(t, "Staff", snapshot.Tables[0].Name, "the name is spelled as the catalog spells it")
		require.Equal(t, "to", snapshot.Tables[0].PeriodEndColumn)
		require.Equal(t, &ddl.Table{ObjectID: 2, Schema: "hr", Name: "staff_history", TemporalType: ddl.TemporalHistory},
			snapshot.Tables[1])
	})
}

func Test_Manager_snapshot_Selection(t *testing.T) {
	t.Parallel()
	manager, querier := newTestManager(t)
	expectDatabase(querier)
	querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mssql_queries.ResolveTablesRow{{Position: 0, ObjectID: 1, Schema: "dbo", Name: "users"}}, nil).Once()
	ids := []int64{1}
	querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, ids).Return(versions(created, 1), nil).Times(2)
	querier.EXPECT().GetModuleHeaders(mock.Anything, mock.Anything).
		Return([]*mssql_queries.GetModuleHeadersRow{
			{ObjectID: 10, Schema: "dbo", Name: "v_users", Type: "V", HasDefinition: true},
			{ObjectID: 11, Schema: "dbo", Name: "v_dropped", Type: "V", HasDefinition: true},
			{ObjectID: 12, Schema: "other", Name: "v_elsewhere", Type: "V", HasDefinition: true},
		}, nil).Once()
	querier.EXPECT().GetDependencies(mock.Anything, mock.Anything).
		Return([]*mssql_queries.GetDependenciesRow{{
			ReferencingID: 5, ReferencingType: "D", ReferencingParentID: 1,
			ReferencedClass: 1, ReferencedID: 30, ReferencedType: "SO", ReferencedSchema: "dbo", ReferencedName: "seq",
		}}, nil).Once()
	querier.EXPECT().GetColumns(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
	querier.EXPECT().GetIndexes(mock.Anything, mock.Anything, ids).
		Return([]*mssql_queries.GetIndexesRow{
			{ObjectID: 1, IndexID: 1, Name: "PK", Type: 1, IsPrimaryKey: true, IndexColumnID: 1, ColumnName: "b", KeyOrdinal: 2},
			{ObjectID: 1, IndexID: 1, Name: "PK", Type: 1, IsPrimaryKey: true, IndexColumnID: 2, ColumnName: "a", KeyOrdinal: 1},
			{ObjectID: 1, IndexID: 5, Name: "CCI", Type: 5},
		}, nil).Once()
	querier.EXPECT().GetForeignKeys(mock.Anything, mock.Anything, ids).
		Return([]*mssql_queries.GetForeignKeysRow{
			{ObjectID: 1, ConstraintID: 7, Name: "FK", ReferencedID: 1, ColumnName: "a", ReferencedColumn: "x"},
			{ObjectID: 1, ConstraintID: 7, Name: "FK", ReferencedID: 1, ColumnName: "b", ReferencedColumn: "y"},
			{ObjectID: 1, ConstraintID: 8, Name: "FK2", ReferencedID: 1, ColumnName: "a", ReferencedColumn: "x"},
		}, nil).Once()
	querier.EXPECT().GetCheckConstraints(mock.Anything, mock.Anything, ids).
		Return([]*mssql_queries.GetCheckConstraintsRow{{ObjectID: 1, Name: "CK", Definition: "([a]>(0))"}}, nil).Once()
	querier.EXPECT().GetTableNotices(mock.Anything, mock.Anything, ids, 16).
		Return([]*mssql_queries.GetTableNoticesRow{
			{ObjectID: 1, Kind: mssql_queries.NoticeCompression, Detail: "PAGE"},
		}, nil).Once()
	// Only the modules and the sequences the selection keeps are read.
	querier.EXPECT().GetModuleDefinitions(mock.Anything, mock.Anything, []int64{10, 11}).
		Return([]*mssql_queries.GetModuleDefinitionsRow{{ObjectID: 10, Definition: "CREATE VIEW dbo.v_users AS SELECT 1 AS a"}}, nil).Once()
	querier.EXPECT().GetSequences(mock.Anything, mock.Anything, []int64{30}).
		Return([]*mssql_queries.GetSequencesRow{{ObjectID: 30, Schema: "dbo", Name: "seq", TypeName: "int"}}, nil).Once()

	snapshot, err := manager.snapshot(t.Context(), requestedUsers)

	require.NoError(t, err)
	table := snapshot.Tables[0]
	require.Len(t, table.Indexes, 2)
	require.Len(t, table.Indexes[0].Columns, 2)
	require.Empty(t, table.Indexes[1].Columns, "an index that lists no column has none")
	require.Len(t, table.ForeignKeys, 2)
	require.Len(t, table.ForeignKeys[0].Columns, 2)
	require.Len(t, table.Checks, 1)
	require.Equal(t, []*ddl.Notice{{ObjectID: 1, Kind: ddl.NoticeCompression, Detail: "PAGE"}}, snapshot.Notices)
	require.Len(t, snapshot.Sequences, 1)

	require.Equal(t, "CREATE VIEW dbo.v_users AS SELECT 1 AS a", snapshot.Modules[0].Definition)
	require.True(t, snapshot.Modules[0].HasDefinition)
	require.False(t, snapshot.Modules[1].HasDefinition, "a wanted module whose text did not come back cannot be read")
	require.True(t, snapshot.Modules[2].HasDefinition, "a module outside the selection is left as its header tells")
	require.Empty(t, snapshot.Modules[2].Definition)
}
