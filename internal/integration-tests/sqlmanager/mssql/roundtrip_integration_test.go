package sqlmanager_mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	mssql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcmssql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/sqlserver"
	"github.com/stretchr/testify/require"
)

// Test_MssqlSchemaInit runs the schema initialization of SQL Server against a server: one
// container, a database or two per case.
func Test_MssqlSchemaInit(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	ctx := context.Background()

	started := time.Now()
	container, err := tcmssql.NewMssqlTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.TearDown(ctx))
	})
	server := &testServer{container: container}
	t.Logf("the server started in %s", time.Since(started).Round(time.Millisecond))

	cases := []struct {
		name string
		run  func(t *testing.T, server *testServer)
	}{
		{"round trip", testRoundTrip},
		{"refusals", testRefusals},
		{"skips", testSkips},
		{"quoting", testQuoting},
		{"a catalog that changes under its read", testChangingCatalog},
		{"session options", testSessionOptions},
		{"identity reset", testIdentityReset},
		{"a login that cannot read definitions", testRestrictedLogin},
		{"listing", testListing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Now()
			tc.run(t, server)
			t.Logf("%s took %s", tc.name, time.Since(started).Round(time.Millisecond))
		})
	}
}

// testServer is a SQL Server that gives each case databases of its own.
type testServer struct {
	container *tcmssql.MssqlTestContainer
}

// database creates a database and opens it. options goes after its name in CREATE DATABASE.
func (s *testServer) database(t *testing.T, name, options string) *sql.DB {
	t.Helper()
	_, err := s.container.DB.ExecContext(t.Context(), "CREATE DATABASE ["+name+"] "+options)
	require.NoError(t, err)
	return s.open(t, name, s.container.URL)
}

// open opens a database of the server through the connection string given.
func (s *testServer) open(t *testing.T, name, connectionString string) *sql.DB {
	t.Helper()
	address, err := url.Parse(connectionString)
	require.NoError(t, err)
	query := address.Query()
	query.Set("database", name)
	address.RawQuery = query.Encode()
	db, err := sql.Open(sqlmanager_shared.MssqlDriver, address.String())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func newManager(t *testing.T, db *sql.DB) *mssql.Manager {
	t.Helper()
	return mssql.NewManager(mssql_queries.New(), db, func() {}, testutil.GetTestLogger(t))
}

// splitBatches cuts a script into its batches: they are separated by lines that hold GO alone.
func splitBatches(script string) []string {
	batches := []string{}
	lines := []string{}
	flush := func() {
		if batch := strings.TrimSpace(strings.Join(lines, "\n")); batch != "" {
			batches = append(batches, batch)
		}
		lines = lines[:0]
	}
	for _, line := range strings.Split(script, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "GO") {
			flush()
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return batches
}

func readBatches(t *testing.T, path ...string) []string {
	t.Helper()
	script, err := os.ReadFile(filepath.Join(path...))
	require.NoError(t, err)
	return splitBatches(string(script))
}

// runScript runs the batches of a script on one connection, so that a SET of one batch holds for
// the next ones.
func runScript(t *testing.T, db *sql.DB, path ...string) {
	t.Helper()
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	for i, batch := range readBatches(t, path...) {
		_, err := conn.ExecContext(t.Context(), batch)
		require.NoErrorf(t, err, "batch %d of %s:\n%s", i+1, filepath.Join(path...), batch)
	}
}

// dumpCatalog renders the catalog of a database as lines of text, by the queries of
// testdata/roundtrip/dump.sql.
func dumpCatalog(t *testing.T, db *sql.DB) []string {
	t.Helper()
	lines := []string{}
	for _, query := range readBatches(t, "testdata", "roundtrip", "dump.sql") {
		rows, err := db.QueryContext(t.Context(), query)
		require.NoErrorf(t, err, "dump query:\n%s", query)
		columns, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			require.NoError(t, rows.Scan(pointers...))
			fields := make([]string, len(columns))
			for i, value := range values {
				switch v := value.(type) {
				case nil:
					fields[i] = columns[i] + "=NULL"
				case []byte:
					fields[i] = columns[i] + "=" + string(v)
				default:
					fields[i] = fmt.Sprintf("%s=%v", columns[i], v)
				}
			}
			lines = append(lines, strings.Join(fields, " | "))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	return lines
}

// allTables asks for every table the source lists.
func allTables(t *testing.T, manager *mssql.Manager) []*sqlmanager_shared.SchemaTable {
	t.Helper()
	listed, err := manager.GetAllTables(t.Context())
	require.NoError(t, err)
	tables := make([]*sqlmanager_shared.SchemaTable, len(listed))
	for i, table := range listed {
		tables[i] = &sqlmanager_shared.SchemaTable{Schema: table.SchemaName, Table: table.TableName}
	}
	return tables
}

// apply runs every statement of every block, in order, one call each.
func apply(t *testing.T, db *sql.DB, blocks []*sqlmanager_shared.InitSchemaStatements) {
	t.Helper()
	for _, block := range blocks {
		for _, statement := range block.Statements {
			_, err := db.ExecContext(t.Context(), statement)
			require.NoErrorf(t, err, "block %q:\n%s", block.Label, statement)
		}
	}
}

func skippedOf(blocks []*sqlmanager_shared.InitSchemaStatements) map[string][]string {
	skipped := map[string][]string{}
	for _, block := range blocks {
		for _, s := range block.Skipped {
			skipped[block.Label] = append(skipped[block.Label], s.Object+": "+s.Reason)
		}
	}
	return skipped
}

// testRoundTrip creates on an empty database what the plan of a source database says, and
// requires the two catalogs to be equal.
//
// The dump that compares them reads the system views with queries of its own. It leaves out
// what the plan does not reproduce, and nothing else:
//   - object ids, creation and modification dates: they belong to a database;
//   - is_system_named: a constraint is created under the name the source gave it;
//   - the owner of a schema, permissions, extended properties: administration;
//   - data spaces, partitions, compression, statistics: storage;
//   - the columns the server adds by itself to the indexes of a partitioned table;
//   - the current value of a sequence and of an identity: the destination starts empty;
//   - the collation of an alias type and of the columns typed by one: an alias type takes the
//     default collation of the database it is created in, and its columns take no COLLATE.
func testRoundTrip(t *testing.T, server *testServer) {
	// The source is case-sensitive, so that case matters. The destination has another default
	// collation, so that a collation left to the default would show; it is case-sensitive too:
	// the source holds names that differ by their case alone.
	source := server.database(t, "rt_source", "COLLATE Latin1_General_100_CS_AS")
	dest := server.database(t, "rt_dest", "COLLATE SQL_Latin1_General_CP1_CS_AS")
	runScript(t, source, "testdata", "roundtrip", "source.sql")

	manager := newManager(t, source)
	tables := allTables(t, manager)
	require.NotEmpty(t, tables)

	// 1. The plan: nothing refused, and what is left out is what is expected.
	blocks, err := manager.GetSchemaInitStatements(t.Context(), tables)
	require.NoError(t, err)
	require.Len(t, blocks, 8)
	for _, block := range blocks {
		require.NotEmptyf(t, block.Statements, "the source fills the block %q", block.Label)
	}
	skipped := skippedOf(blocks)
	require.ElementsMatch(t, []string{
		"[a.b].[used.seq]: created at its declared start 10; the source is at 30",
		"[it's].[seq tiny]: created at its declared start 5; the source is at 15",
		"[sales].[seq_alias]: created at its declared start 7; the source is at 21",
		"[sales].[seq_big]: created at its declared start 1000; the source is at 1002",
		"[sales].[seq_decimal]: created at its declared start 1; the source is at 3",
		"[sales].[seq_int]: created at its declared start 1000; the source is at 994",
		"[sales].[seq_small]: created at its declared start 100; the source is at 102",
	}, skipped[mssql.DataTypesLabel])
	require.ElementsMatch(t, []string{
		"[dbo].[all_types]: collation of 1 alias-typed column(s) follows the default of the destination database",
		"[it's].[staff history]: compression not reproduced: PAGE",
		"[sales].[partitioned]: partitioning not reproduced: ps_region",
		"[sales].[compressed]: compression not reproduced: PAGE",
		"[sales].[described]: extended properties not reproduced: 2",
		"[sales].[described]: permissions not reproduced: 1",
	}, skipped[sqlmanager_shared.CreateTablesLabel])
	delete(skipped, mssql.DataTypesLabel)
	delete(skipped, sqlmanager_shared.CreateTablesLabel)
	require.Empty(t, skipped, "no other block leaves anything out")

	// 2. Every statement of every block, in order, on the empty destination.
	apply(t, dest, blocks)

	// 3. The two catalogs are equal.
	expected := dumpCatalog(t, source)
	require.Greater(t, len(expected), 300, "the dump of the source tells its objects")
	require.Equal(t, expected, dumpCatalog(t, dest))

	// 4. The whole plan again: no error, and nothing changes.
	apply(t, dest, blocks)
	require.Equal(t, expected, dumpCatalog(t, dest))

	// 5. One object of each kind is dropped: the plan creates each again, which shows that each
	// is guarded by itself and not by its table or by another of its kind.
	for _, statement := range []string{
		"ALTER TABLE sales.busy DROP CONSTRAINT FK_busy_two",
		"ALTER TABLE sales.busy DROP CONSTRAINT CK_busy_price",
		"DROP INDEX IX_indexed_included ON sales.indexed",
		"DROP VIEW sales.a_view_on_view",
	} {
		_, err := dest.ExecContext(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	require.NotEqual(t, expected, dumpCatalog(t, dest))
	apply(t, dest, blocks)
	require.Equal(t, expected, dumpCatalog(t, dest))

	// The other operations are served from the same plan.
	statements, err := manager.GetTableInitStatements(t.Context(), tables)
	require.NoError(t, err)
	require.Len(t, statements, len(tables)+1, "one per table, and one for the history table")
	types, err := manager.GetSchemaTableDataTypes(t.Context(), tables)
	require.NoError(t, err)
	require.Len(t, types.Sequences, 7)
	require.Len(t, types.Domains, 4)
	require.Len(t, types.Functions, 1)
	require.Empty(t, types.Composites)
	require.Empty(t, types.Enums)
	triggers, err := manager.GetSchemaTableTriggers(t.Context(), tables)
	require.NoError(t, err)
	states := map[string]string{}
	for _, trigger := range triggers {
		states[trigger.TriggerName] = trigger.EnabledState
	}
	require.Equal(t, map[string]string{
		"trg_busy_insert": "", "trg busy ] off": "D", "trg_view_insert": "",
	}, states)
	sequences, err := manager.GetSequencesByTables(t.Context(), "sales", []string{"defaults", "busy"})
	require.NoError(t, err)
	require.Len(t, sequences, 7)
	sequences, err = manager.GetSequencesByTables(t.Context(), "sales", []string{"busy"})
	require.NoError(t, err)
	require.Empty(t, sequences, "a table whose defaults draw from no sequence brings none")
}
