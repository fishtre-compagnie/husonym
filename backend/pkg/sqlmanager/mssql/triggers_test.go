package sqlmanager_mssql

import (
	"context"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v7"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Reading the triggers of tables is one query. It asks for nothing a plan needs: neither the
// database, nor the tables by name, nor their columns — the mock has no other expectation.
func Test_Manager_GetSchemaTableTriggers(t *testing.T) {
	t.Parallel()
	manager, querier := newTestManager(t)
	trigger := func(table, name string, change func(*mssql_queries.GetTableTriggersRow)) *mssql_queries.GetTableTriggersRow {
		row := &mssql_queries.GetTableTriggersRow{
			ObjectID: int64(len(name)), ParentID: int64(len(table)), TableSchema: "sales", TableName: table,
			Name: name, Type: "TR", UsesAnsiNulls: true, UsesQuotedIdentifier: true,
			HasDefinition: true, Definition: "CREATE TRIGGER sales." + name + " ON sales." + table + " AFTER INSERT AS RETURN",
		}
		change(row)
		return row
	}
	none := func(*mssql_queries.GetTableTriggersRow) {}
	querier.EXPECT().GetTableTriggers(mock.Anything, mock.Anything).
		Return([]*mssql_queries.GetTableTriggersRow{
			trigger("Orders", "trg_enabled", none),
			trigger("Orders", "trg_disabled", func(r *mssql_queries.GetTableTriggersRow) { r.IsDisabled = true }),
			trigger("Orders", "trg_unreadable", func(r *mssql_queries.GetTableTriggersRow) {
				r.HasDefinition, r.Definition = false, ""
			}),
			trigger("Orders", "trg_clr", func(r *mssql_queries.GetTableTriggersRow) {
				r.Type, r.HasDefinition, r.Definition = "TA", false, ""
			}),
			trigger("not requested", "trg_elsewhere", none),
		}, nil).Once()

	triggers, err := manager.GetSchemaTableTriggers(t.Context(), []*sqlmanager_shared.SchemaTable{
		{Schema: "sales", Table: "orders"}, {Schema: "sales", Table: "gone"},
	})

	require.NoError(t, err)
	type found struct{ schema, table, name, state string }
	actual := []found{}
	for _, tr := range triggers {
		require.Equal(t, "sales", *tr.TriggerSchema)
		require.Contains(t, tr.Definition, "CREATE TRIGGER sales."+tr.TriggerName)
		actual = append(actual, found{tr.Schema, tr.Table, tr.TriggerName, tr.EnabledState})
	}
	require.Equal(t, []found{
		{"sales", "Orders", "trg_disabled", "D"},
		{"sales", "Orders", "trg_enabled", ""},
	}, actual)
}

func Test_Manager_snapshot_RequestedNames(t *testing.T) {
	t.Parallel()
	manager, querier := newTestManager(t)
	expectDatabase(querier)
	querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mssql_queries.ResolveTablesRow{
			{Position: 0, ObjectID: 1, Schema: "dbo", Name: "legacy", AnsiNullsOff: true},
			{Position: 1, ObjectID: 5, Schema: "dbo", Name: "v_report", IsView: true},
			{
				Position: 2, ObjectID: 2, Schema: "dbo", Name: "staff", TemporalType: ddl.TemporalSystemVersioned,
				HistoryID: 3, HistorySchema: "dbo", HistoryName: "staff_history", HistoryAnsiNullsOff: true,
			},
		}, nil).Once()
	ids := []int64{1, 2, 3}
	querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, ids).Return(versions(created, 1, 2, 3), nil).Times(2)
	querier.EXPECT().GetModuleHeaders(mock.Anything, mock.Anything).
		Return([]*mssql_queries.GetModuleHeadersRow{
			{ObjectID: 5, Schema: "dbo", Name: "v_report", Type: "V", HasDefinition: true, HasIndex: true},
		}, nil).Once()
	querier.EXPECT().GetDependencies(mock.Anything, mock.Anything).Return(nil, nil).Once()
	querier.EXPECT().GetColumns(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
	querier.EXPECT().GetIndexes(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
	querier.EXPECT().GetForeignKeys(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
	querier.EXPECT().GetCheckConstraints(mock.Anything, mock.Anything, ids).Return(nil, nil).Once()
	querier.EXPECT().GetTableNotices(mock.Anything, mock.Anything, ids, 16).Return(nil, nil).Once()
	querier.EXPECT().GetModuleDefinitions(mock.Anything, mock.Anything, []int64{5}).
		Return([]*mssql_queries.GetModuleDefinitionsRow{{ObjectID: 5, Definition: "CREATE VIEW dbo.v_report AS SELECT 1 AS a"}}, nil).Once()

	snapshot, err := manager.snapshot(t.Context(), []*sqlmanager_shared.SchemaTable{
		{Schema: "dbo", Table: "legacy"}, {Schema: "dbo", Table: "v_report"}, {Schema: "dbo", Table: "staff"},
	})

	require.NoError(t, err)
	require.Empty(t, snapshot.Missing)
	require.Equal(t, []sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "v_report"}}, snapshot.Views)
	require.Len(t, snapshot.Tables, 3)
	require.True(t, snapshot.Tables[0].AnsiNullsOff)
	require.False(t, snapshot.Tables[1].AnsiNullsOff)
	require.True(t, snapshot.Tables[2].AnsiNullsOff, "the history table tells its own setting")
	require.True(t, snapshot.Modules[0].HasIndex)
}

// A caller who gives up while the manager waits before reading again is told so: the error is
// the one of its context, not that the catalog kept changing.
func Test_Manager_snapshot_CallerGivesUp(t *testing.T) {
	t.Parallel()
	manager, querier := newTestManager(t)
	manager.retryOpts = func() []backoff.RetryOption {
		return []backoff.RetryOption{
			backoff.WithBackOff(&backoff.ConstantBackOff{Interval: time.Minute}),
			backoff.WithMaxTries(sqlmanager_shared.CatalogReadAttempts),
		}
	}
	expectDatabase(querier)
	expectUsers(querier, 1)
	modified := created
	querier.EXPECT().GetObjectVersions(mock.Anything, mock.Anything, []int64{1}).
		RunAndReturn(func(context.Context, mysql_queries.DBTX, []int64) ([]*mssql_queries.GetObjectVersionsRow, error) {
			modified = modified.Add(time.Second)
			return versions(modified, 1), nil
		}).Times(2)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := manager.snapshot(ctx, requestedUsers)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "gave up reading the catalog")
	require.NotContains(t, err.Error(), "kept changing")
}
