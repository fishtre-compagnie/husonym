package ddbuilder_mssql

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_mssql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	shared "github.com/fishtre-compagnie/husonym/internal/schema-manager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	manager *MssqlSchemaManager
	source  *sqlmanager.MockSqlDatabase
	dest    *sqlmanager.MockSqlDatabase
}

func newFixture(t *testing.T, destOpts *mgmtv1alpha1.MssqlDestinationConnectionOptions, licensed bool) *fixture {
	t.Helper()
	source := sqlmanager.NewMockSqlDatabase(t)
	dest := sqlmanager.NewMockSqlDatabase(t)
	licenseOpts := []testutil.Option{}
	if licensed {
		licenseOpts = append(licenseOpts, testutil.WithIsValid())
	}
	return &fixture{
		source: source,
		dest:   dest,
		manager: &MssqlSchemaManager{
			logger:    testutil.GetTestLogger(t),
			eelicense: testutil.NewFakeEELicense(licenseOpts...),
			destOpts:  destOpts,
			sourcedb:  sqlmanager.NewMssqlSqlConnection(source),
			destdb:    sqlmanager.NewMssqlSqlConnection(dest),
		},
	}
}

func initSchema() *mgmtv1alpha1.MssqlDestinationConnectionOptions {
	return &mgmtv1alpha1.MssqlDestinationConnectionOptions{InitTableSchema: true}
}

func tables(pairs ...string) []*sqlmanager_shared.DatabaseTableRow {
	rows := []*sqlmanager_shared.DatabaseTableRow{}
	for i := 0; i+1 < len(pairs); i += 2 {
		rows = append(rows, &sqlmanager_shared.DatabaseTableRow{SchemaName: pairs[i], TableName: pairs[i+1]})
	}
	return rows
}

func Test_InitializeSchema_License(t *testing.T) {
	t.Parallel()

	t.Run("without a valid license, nothing is read and nothing is run", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, initSchema(), false)

		_, err := f.manager.InitializeSchema(t.Context(), map[string]struct{}{"dbo.users": {}})

		require.EqualError(
			t,
			err,
			"invalid or non-existent Husonym License. SQL Server schema init requires valid Enterprise license",
		)
	})

	t.Run("when schema init is off, the license is not asked for", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, &mgmtv1alpha1.MssqlDestinationConnectionOptions{}, false)

		initErrors, err := f.manager.InitializeSchema(t.Context(), map[string]struct{}{"dbo.users": {}})

		require.NoError(t, err)
		require.Empty(t, initErrors)
	})
}

func Test_InitializeSchema_Tables(t *testing.T) {
	t.Parallel()

	requested := func(t *testing.T, source []*sqlmanager_shared.DatabaseTableRow, keys ...string) []*sqlmanager_shared.SchemaTable {
		t.Helper()
		f := newFixture(t, initSchema(), true)
		f.source.EXPECT().GetAllTables(mock.Anything).Return(source, nil)
		var asked []*sqlmanager_shared.SchemaTable
		f.source.EXPECT().GetSchemaInitStatements(mock.Anything, mock.Anything).
			Run(func(_ context.Context, tables []*sqlmanager_shared.SchemaTable) {
				asked = tables
			}).
			Return([]*sqlmanager_shared.InitSchemaStatements{}, nil)
		unique := map[string]struct{}{}
		for _, key := range keys {
			unique[key] = struct{}{}
		}
		_, err := f.manager.InitializeSchema(t.Context(), unique)
		require.NoError(t, err)
		return asked
	}

	t.Run("a key is the table of the source that builds it, dots included", func(t *testing.T) {
		t.Parallel()
		asked := requested(t,
			tables("dbo", "users", "a.b", "c", "sales", "order.lines"),
			"sales.order.lines", "a.b.c", "dbo.users",
		)
		require.Equal(t, []*sqlmanager_shared.SchemaTable{
			{Schema: "a.b", Table: "c"},
			{Schema: "dbo", Table: "users"},
			{Schema: "sales", Table: "order.lines"},
		}, asked)
	})

	t.Run("a key no table of the source builds is asked for as it splits", func(t *testing.T) {
		t.Parallel()
		asked := requested(t, tables("dbo", "users"), "dbo.Users")
		require.Equal(t, []*sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "Users"}}, asked)
	})

	t.Run("a key two tables build is refused, both named", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, initSchema(), true)
		f.source.EXPECT().GetAllTables(mock.Anything).Return(tables("a.b", "c", "a", "b.c"), nil)

		_, err := f.manager.InitializeSchema(t.Context(), map[string]struct{}{"a.b.c": {}})

		require.EqualError(t, err, `table key "a.b.c" names two tables of the source: [a.b].[c] and [a].[b.c]`)
	})
}

func Test_InitializeSchema_Blocks(t *testing.T) {
	t.Parallel()
	unique := map[string]struct{}{"dbo.users": {}}

	t.Run("statements run in order; what a block leaves out is recorded", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, initSchema(), true)
		f.source.EXPECT().GetAllTables(mock.Anything).Return(tables("dbo", "users"), nil)
		f.source.EXPECT().GetSchemaInitStatements(mock.Anything, mock.Anything).Return(
			[]*sqlmanager_shared.InitSchemaStatements{
				{Label: sqlmanager_shared.SchemasLabel, Statements: []string{"schema"}},
				{
					Label:      sqlmanager_shared.CreateTablesLabel,
					Statements: []string{"table"},
					Skipped: []*sqlmanager_shared.SkippedObject{
						{Object: "[dbo].[Gone]", Reason: "not found in the source database"},
					},
				},
				{Label: sqlmanager_mssql.TableIndexLabel, Statements: []string{}},
			}, nil)
		var run []string
		f.dest.EXPECT().Exec(mock.Anything, mock.Anything).
			Run(func(_ context.Context, statement string) { run = append(run, statement) }).
			Return(nil)

		initErrors, err := f.manager.InitializeSchema(t.Context(), unique)

		require.NoError(t, err)
		require.Equal(t, []string{"schema", "table"}, run)
		require.Equal(t, []*shared.InitSchemaError{
			{Statement: "[dbo].[Gone]", Error: "skipped: not found in the source database"},
		}, initErrors)
	})

	t.Run("a view or function that fails is recorded, and the run goes on", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, initSchema(), true)
		f.source.EXPECT().GetAllTables(mock.Anything).Return(tables("dbo", "users"), nil)
		f.source.EXPECT().GetSchemaInitStatements(mock.Anything, mock.Anything).Return(
			[]*sqlmanager_shared.InitSchemaStatements{
				{Label: sqlmanager_mssql.ViewsFunctionsLabel, Statements: []string{"view", "function"}},
				{Label: sqlmanager_mssql.FkAlterTableLabel, Statements: []string{"fk"}},
			}, nil)
		f.dest.EXPECT().Exec(mock.Anything, "view").Return(errors.New("invalid object name"))
		f.dest.EXPECT().Exec(mock.Anything, "function").Return(nil)
		f.dest.EXPECT().Exec(mock.Anything, "fk").Return(nil)

		initErrors, err := f.manager.InitializeSchema(t.Context(), unique)

		require.NoError(t, err)
		require.Equal(t, []*shared.InitSchemaError{{Statement: "view", Error: "invalid object name"}}, initErrors)
	})

	for _, label := range []string{
		sqlmanager_shared.SchemasLabel, sqlmanager_mssql.DataTypesLabel, sqlmanager_shared.CreateTablesLabel,
		sqlmanager_mssql.NonFkAlterTableLabel, sqlmanager_mssql.TableIndexLabel,
		sqlmanager_mssql.FkAlterTableLabel, sqlmanager_mssql.TableTriggersLabel,
	} {
		t.Run("a statement of the block "+label+" that fails stops the run", func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, initSchema(), true)
			f.source.EXPECT().GetAllTables(mock.Anything).Return(tables("dbo", "users"), nil)
			f.source.EXPECT().GetSchemaInitStatements(mock.Anything, mock.Anything).Return(
				[]*sqlmanager_shared.InitSchemaStatements{{Label: label, Statements: []string{"first", "second"}}}, nil)
			failure := errors.New("there is already an object named that")
			f.dest.EXPECT().Exec(mock.Anything, "first").Return(failure)

			_, err := f.manager.InitializeSchema(t.Context(), unique)

			require.ErrorIs(t, err, failure)
			require.ErrorContains(t, err, "unable to exec mssql "+label+" statements")
		})
	}

	t.Run("a plan the source refuses stops the run", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, initSchema(), true)
		f.source.EXPECT().GetAllTables(mock.Anything).Return(tables("dbo", "users"), nil)
		refused := errors.New("[dbo].[users]: memory-optimized table")
		f.source.EXPECT().GetSchemaInitStatements(mock.Anything, mock.Anything).Return(nil, refused)

		_, err := f.manager.InitializeSchema(t.Context(), unique)

		require.ErrorIs(t, err, refused)
	})
}

func Test_TruncateData(t *testing.T) {
	t.Parallel()
	identity := "IDENTITY(5,2)"
	seed, increment := 5, 2
	f := newFixture(t, &mgmtv1alpha1.MssqlDestinationConnectionOptions{
		TruncateTable: &mgmtv1alpha1.MssqlTruncateTableConfig{TruncateBeforeInsert: true},
	}, false)
	f.source.EXPECT().GetTableConstraintsBySchema(mock.Anything, []string{"dbo"}).
		Return(&sqlmanager_shared.TableConstraints{}, nil)
	f.source.EXPECT().GetSchemaColumnMap(mock.Anything).Return(
		map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"dbo.order.lines": {
				"id": {
					TableSchema: "dbo", TableName: "order.lines", ColumnName: "id",
					IdentityGeneration: &identity, IdentitySeed: &seed, IdentityIncrement: &increment,
				},
				"qty": {TableSchema: "dbo", TableName: "order.lines", ColumnName: "qty"},
			},
			"dbo.other": {
				"id": {TableSchema: "dbo", TableName: "other", ColumnName: "id", IdentityGeneration: &identity},
			},
		}, nil)
	var batches [][]string
	f.dest.EXPECT().BatchExec(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, _ int, statements []string, _ *sqlmanager_shared.BatchExecOpts) {
			batches = append(batches, statements)
		}).
		Return(nil)

	err := f.manager.TruncateData(t.Context(), map[string]struct{}{"dbo.order.lines": {}}, []string{"dbo"})

	require.NoError(t, err)
	require.Len(t, batches, 2)
	require.Equal(t, []string{
		sqlmanager_mssql.BuildMssqlIdentityColumnResetStatement("dbo", "order.lines", &seed, &increment),
	}, batches[1], "the table of an identity column is named by its row, not by a split of its key")
}

func Test_UnsupportedOperations(t *testing.T) {
	t.Parallel()
	f := newFixture(t, initSchema(), true)

	_, err := f.manager.CalculateSchemaDiff(t.Context(), nil)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: CalculateSchemaDiff: unsupported operation")

	_, err = f.manager.BuildSchemaDiffStatements(t.Context(), nil)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: BuildSchemaDiffStatements: unsupported operation")

	_, err = f.manager.ReconcileDestinationSchema(t.Context(), nil, nil)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: ReconcileDestinationSchema: unsupported operation")

	err = f.manager.TruncateTables(t.Context(), nil)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.EqualError(t, err, "sql server: TruncateTables: unsupported operation")
}
