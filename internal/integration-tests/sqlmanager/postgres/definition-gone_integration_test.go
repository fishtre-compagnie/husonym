package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// overtaken answers one query as a read that a drop overtakes, and every other one from the
// database: the query is under way, a cursor holding its view of the catalog, when the drop is
// made, and its rows are fetched after.
type overtaken struct {
	*sql.DB
	t      *testing.T
	query  string
	drop   string
	served atomic.Bool
}

func (o *overtaken) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if !strings.Contains(query, "-- name: "+o.query+" :many") || !o.served.CompareAndSwap(false, true) {
		return o.DB.QueryContext(ctx, query, args...)
	}
	tx, err := o.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	o.t.Cleanup(func() { _ = tx.Rollback() })
	// A cursor by query: the driver remembers the columns a statement answers.
	cursor := "overtaken_" + o.query
	if _, err := tx.ExecContext(ctx, "DECLARE "+cursor+" CURSOR FOR "+query, args...); err != nil {
		return nil, err
	}
	if _, err := o.DB.ExecContext(ctx, o.drop); err != nil {
		return nil, err
	}
	return tx.QueryContext(ctx, "FETCH ALL FROM "+cursor)
}

// The functions that tell the definition of an index, a constraint, a trigger or a function
// answer NULL once it is dropped, though the query that calls them still lists it: they look
// the catalog up anew, and the query does not. A bare query fails on that NULL, or tells a constraint
// without definition. The manager reads again, and tells the catalog as it now is.
func Test_PostgresManager_ReadsAgainWhenADefinitionIsGone(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	ctx := context.Background()

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	db, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE DOMAIN app.amount AS int;
		CREATE TABLE app.t (id int PRIMARY KEY, v int, a app.amount);`)
	require.NoError(t, err)

	bare := pg_queries.New()
	tables := []*sqlmanager_shared.SchemaTable{{Schema: "app", Table: "t"}}
	schemaTables := []string{"app.t"}
	nullText := "converting NULL to string is unsupported"

	cases := []struct {
		query        string
		create, drop string
		// What the bare query answers when the drop overtook it.
		bare func(t *testing.T, db pg_queries.DBTX)
		read func(t *testing.T, manager *postgres.PostgresManager) error
	}{
		{
			query:  "GetIndicesBySchemasAndTables",
			create: `CREATE INDEX t_v_idx ON app.t (v)`,
			drop:   `DROP INDEX app.t_v_idx`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetIndicesBySchemasAndTables(ctx, db, schemaTables)
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetTableInitStatements(ctx, tables)
				return err
			},
		},
		{
			query:  "GetNonForeignKeyTableConstraintsBySchema",
			create: `ALTER TABLE app.t ADD CONSTRAINT t_v_check CHECK (v > 0)`,
			drop:   `ALTER TABLE app.t DROP CONSTRAINT t_v_check`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetNonForeignKeyTableConstraintsBySchema(ctx, db, []string{"app"})
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetTableConstraintsBySchema(ctx, []string{"app"})
				return err
			},
		},
		{
			query:  "GetNonForeignKeyTableConstraintsBySchemaAndTables",
			create: `ALTER TABLE app.t ADD CONSTRAINT t_v_check CHECK (v > 0)`,
			drop:   `ALTER TABLE app.t DROP CONSTRAINT t_v_check`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetNonForeignKeyTableConstraintsBySchemaAndTables(ctx, db,
					&pg_queries.GetNonForeignKeyTableConstraintsBySchemaAndTablesParams{Schema: "app", Tables: []string{"t"}})
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetTableConstraintsByTables(ctx, "app", []string{"t"})
				return err
			},
		},
		{
			query: "GetCustomTriggersBySchemaAndTables",
			create: `CREATE FUNCTION app.touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
				CREATE TRIGGER t_touch BEFORE INSERT ON app.t FOR EACH ROW EXECUTE FUNCTION app.touch()`,
			drop: `DROP TRIGGER t_touch ON app.t; DROP FUNCTION app.touch()`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetCustomTriggersBySchemaAndTables(ctx, db, schemaTables)
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetSchemaTableTriggers(ctx, tables)
				return err
			},
		},
		{
			query: "GetCustomFunctionsBySchemaAndTables",
			create: `CREATE FUNCTION app.touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
				CREATE TRIGGER t_touch BEFORE INSERT ON app.t FOR EACH ROW EXECUTE FUNCTION app.touch()`,
			drop: `DROP TRIGGER t_touch ON app.t; DROP FUNCTION app.touch()`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetCustomFunctionsBySchemaAndTables(ctx, db,
					&pg_queries.GetCustomFunctionsBySchemaAndTablesParams{Schema: "app", Tables: []string{"t"}})
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetSchemaTableDataTypes(ctx, tables)
				return err
			},
		},
		{
			query:  "GetPartitionedTablesBySchema",
			create: `CREATE TABLE app.p (id int) PARTITION BY RANGE (id)`,
			drop:   `DROP TABLE app.p`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetPartitionedTablesBySchema(ctx, db, []string{"app"})
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetTableInitStatements(ctx, tables)
				return err
			},
		},
		{
			// Without the NULL, the domain would be told without its constraint, as if it had none.
			query:  "GetDataTypesBySchemaAndTables",
			create: `ALTER DOMAIN app.amount ADD CONSTRAINT amount_positive CHECK (VALUE > 0)`,
			drop:   `ALTER DOMAIN app.amount DROP CONSTRAINT amount_positive`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				_, err := bare.GetDataTypesBySchemaAndTables(ctx, db,
					&pg_queries.GetDataTypesBySchemaAndTablesParams{Schema: "app", Tables: []string{"t"}})
				require.ErrorContains(t, err, nullText)
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				_, err := manager.GetSchemaTableDataTypes(ctx, tables)
				return err
			},
		},
		{
			// The constraints of a domain are a JSON document: the NULL does not fail the read.
			query:  "GetDomainsByTables",
			create: `ALTER DOMAIN app.amount ADD CONSTRAINT amount_positive CHECK (VALUE > 0)`,
			drop:   `ALTER DOMAIN app.amount DROP CONSTRAINT amount_positive`,
			bare: func(t *testing.T, db pg_queries.DBTX) {
				domains, err := bare.GetDomainsByTables(ctx, db, schemaTables)
				require.NoError(t, err)
				require.Len(t, domains, 1)
				require.JSONEq(t, `[{"name": "amount_positive", "definition": null}]`, string(domains[0].Constraints))
			},
			read: func(t *testing.T, manager *postgres.PostgresManager) error {
				datatypes, err := manager.GetDataTypesByTables(ctx, tables)
				if err != nil {
					return err
				}
				require.Len(t, datatypes.Domains, 1)
				// Read again, the domain has no constraint left, and is told so.
				require.Empty(t, datatypes.Domains[0].Constraints)
				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			_, err := db.ExecContext(ctx, tc.create)
			require.NoError(t, err)
			tc.bare(t, &overtaken{DB: db, t: t, query: tc.query, drop: tc.drop})

			_, err = db.ExecContext(ctx, tc.create)
			require.NoError(t, err)
			read := &overtaken{DB: db, t: t, query: tc.query, drop: tc.drop}
			manager := postgres.NewManager(pg_queries.New(), read, func() {})
			require.NoError(t, tc.read(t, manager), "the manager failed on a definition gone under its read")
			require.True(t, read.served.Load(), "the manager did not read %s", tc.query)
		})
	}
}
