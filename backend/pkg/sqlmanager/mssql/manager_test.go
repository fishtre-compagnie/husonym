package sqlmanager_mssql

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func Test_Manager_UnsupportedOperations(t *testing.T) {
	t.Parallel()
	// A querier with no expectation: an unsupported operation reads nothing.
	manager := NewManager(mssql_queries.NewMockQuerier(t), nil, nil, testutil.GetTestLogger(t))
	ctx := context.Background()
	tables := []*sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "t"}}

	_, err := manager.GetTableConstraintsByTables(ctx, "dbo", []string{"t"})
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: GetTableConstraintsByTables: unsupported operation")

	_, err = manager.GetColumnsByTables(ctx, tables)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: GetColumnsByTables: unsupported operation")

	_, err = manager.GetDataTypesByTables(ctx, tables)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: GetDataTypesByTables: unsupported operation")
}

func Test_Manager_EmptyInput(t *testing.T) {
	t.Parallel()
	// A querier with no expectation and no database: an empty request reads nothing.
	manager := NewManager(mssql_queries.NewMockQuerier(t), nil, nil, testutil.GetTestLogger(t))
	ctx := context.Background()
	none := []*sqlmanager_shared.SchemaTable{}

	t.Run("GetSchemaInitStatements gives the eight blocks, empty", func(t *testing.T) {
		t.Parallel()
		blocks, err := manager.GetSchemaInitStatements(ctx, none)
		require.NoError(t, err)
		labels := []string{}
		for _, block := range blocks {
			labels = append(labels, block.Label)
			require.NotNil(t, block.Statements)
			require.Empty(t, block.Statements)
		}
		require.Equal(t, []string{
			sqlmanager_shared.SchemasLabel, DataTypesLabel, sqlmanager_shared.CreateTablesLabel,
			ViewsFunctionsLabel, NonFkAlterTableLabel, TableIndexLabel, FkAlterTableLabel, TableTriggersLabel,
		}, labels)
	})

	t.Run("GetTableInitStatements", func(t *testing.T) {
		t.Parallel()
		statements, err := manager.GetTableInitStatements(ctx, none)
		require.NoError(t, err)
		require.NotNil(t, statements)
		require.Empty(t, statements)
	})

	t.Run("GetSchemaTableDataTypes", func(t *testing.T) {
		t.Parallel()
		types, err := manager.GetSchemaTableDataTypes(ctx, none)
		require.NoError(t, err)
		require.Equal(t, &sqlmanager_shared.SchemaTableDataTypeResponse{
			Sequences:  []*sqlmanager_shared.DataType{},
			Functions:  []*sqlmanager_shared.DataType{},
			Composites: []*sqlmanager_shared.DataType{},
			Enums:      []*sqlmanager_shared.DataType{},
			Domains:    []*sqlmanager_shared.DataType{},
		}, types)
	})

	t.Run("GetSchemaTableTriggers", func(t *testing.T) {
		t.Parallel()
		triggers, err := manager.GetSchemaTableTriggers(ctx, none)
		require.NoError(t, err)
		require.NotNil(t, triggers)
		require.Empty(t, triggers)
	})

	t.Run("GetSequencesByTables", func(t *testing.T) {
		t.Parallel()
		sequences, err := manager.GetSequencesByTables(ctx, "dbo", []string{})
		require.NoError(t, err)
		require.NotNil(t, sequences)
		require.Empty(t, sequences)
	})

	t.Run("GetDatabaseTableSchemasBySchemasAndTables", func(t *testing.T) {
		t.Parallel()
		rows, err := manager.GetDatabaseTableSchemasBySchemasAndTables(ctx, none)
		require.NoError(t, err)
		require.NotNil(t, rows)
		require.Empty(t, rows)
	})

	t.Run("GetTableConstraintsBySchema", func(t *testing.T) {
		t.Parallel()
		constraints, err := manager.GetTableConstraintsBySchema(ctx, []string{})
		require.NoError(t, err)
		require.Equal(t, &sqlmanager_shared.TableConstraints{}, constraints)
	})
}

func Test_Manager_Exec(t *testing.T) {
	t.Parallel()

	t.Run("BatchExec runs the statements one by one and stops at the first that fails", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		failure := errors.New("syntax error")
		mock.ExpectExec("first").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("second").WillReturnError(failure)
		manager := NewManager(mssql_queries.NewMockQuerier(t), db, nil, testutil.GetTestLogger(t))

		err = manager.BatchExec(t.Context(), 10, []string{"first", "second", "third"}, &sqlmanager_shared.BatchExecOpts{})

		require.ErrorIs(t, err, failure)
		require.ErrorContains(t, err, "failed to execute batch statement 2/3")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetTableRowCount quotes the table and takes the condition as given", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()
		mock.ExpectQuery("SELECT COUNT(*) FROM [sales].[Order ]] Lines]").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
		mock.ExpectQuery("SELECT COUNT(*) FROM [a.b].[it's] WHERE [qty] > 0").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
		manager := NewManager(mssql_queries.NewMockQuerier(t), db, nil, testutil.GetTestLogger(t))

		count, err := manager.GetTableRowCount(t.Context(), "sales", "Order ] Lines", nil)
		require.NoError(t, err)
		require.Equal(t, int64(7), count)

		where := "[qty] > 0"
		count, err = manager.GetTableRowCount(t.Context(), "a.b", "it's", &where)
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Close releases the session once given a closer", func(t *testing.T) {
		t.Parallel()
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		closed := 0
		NewManager(mssql_queries.NewMockQuerier(t), db, func() { closed++ }, testutil.GetTestLogger(t)).Close()
		require.Equal(t, 1, closed)
		NewManager(mssql_queries.NewMockQuerier(t), nil, nil, testutil.GetTestLogger(t)).Close()
	})
}
