package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// partitionRow is a partition of a table: its name and its bound, read from the catalog.
type partitionRow struct{ Name, Bound string }

func readPartitions(ctx context.Context, t *testing.T, db *sql.DB, schema, table string) []partitionRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT c.relname, pg_get_expr(c.relpartbound, c.oid)
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		JOIN pg_namespace n ON n.oid = p.relnamespace
		WHERE n.nspname = $1 AND p.relname = $2
		ORDER BY c.relname`, schema, table)
	require.NoError(t, err)
	defer rows.Close()
	out := []partitionRow{}
	for rows.Next() {
		var r partitionRow
		require.NoError(t, rows.Scan(&r.Name, &r.Bound))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// A table partitioned by range is read by its quoted name whatever the schema and the table
// are named, and the statements it gives recreate the table and its partitions on an empty
// database.
func Test_PostgresManager_InitializesAPartitionedTableWhoseNameNeedsQuotes(t *testing.T) {
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

	for _, name := range []string{"MixedCase", "sp ace"} {
		t.Run(name, func(t *testing.T) {
			quoted := `"` + name + `"`
			qualified := quoted + "." + quoted
			part1 := `"` + name + `_1"`
			part2 := `"` + name + `_2"`

			_, err := db.ExecContext(ctx, `
				CREATE SCHEMA `+quoted+`;
				CREATE TABLE `+qualified+` (id int NOT NULL, label text) PARTITION BY RANGE (id);
				CREATE TABLE `+quoted+"."+part1+` PARTITION OF `+qualified+` FOR VALUES FROM (1) TO (100);
				CREATE TABLE `+quoted+"."+part2+` PARTITION OF `+qualified+` FOR VALUES FROM (100) TO (200);`)
			require.NoError(t, err)
			want := readPartitions(ctx, t, db, name, name)
			require.Len(t, want, 2)

			manager := postgres.NewManager(pg_queries.New(), db, func() {})
			statements, err := manager.GetTableInitStatements(ctx, []*sqlmanager_shared.SchemaTable{{Schema: name, Table: name}})
			require.NoError(t, err)
			require.Len(t, statements, 1)
			require.Len(t, statements[0].PartitionStatements, 2)

			// An empty database: the schema is dropped, and the statements recreate what it held.
			_, err = db.ExecContext(ctx, "DROP SCHEMA "+quoted+" CASCADE; CREATE SCHEMA "+quoted+";")
			require.NoError(t, err)
			require.Empty(t, readPartitions(ctx, t, db, name, name))

			_, err = db.ExecContext(ctx, statements[0].CreateTableStatement)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, strings.Join(statements[0].PartitionStatements, "\n"))
			require.NoError(t, err)

			require.Equal(t, want, readPartitions(ctx, t, db, name, name))
		})
	}
}
