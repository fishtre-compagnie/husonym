package sqlmanager

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	tcmssql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/sqlserver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const (
	familyPostgres = "postgres"
	familyMysql    = "mysql"
	familyMssql    = "mssql"
)

// sampleEngine is a database server a sample is drawn from. MariaDB shares the MySQL family:
// the same tests run against both.
type sampleEngine struct {
	name   string
	family string
	start  func(ctx context.Context) (*sampleFixture, error)
}

// sampleFixture is one running server, with the tables the tests create on demand.
type sampleFixture struct {
	engine     sampleEngine
	db         *sql.DB
	connection *mgmtv1alpha1.Connection
	schema     string
	teardown   func(ctx context.Context) error

	// sqlmanager is shared by every sample of the server and closes a connection when its
	// session is released, the way the API server builds it.
	sqlmanager sqlmanager.SqlManagerClient

	// datasets names the tables built so far. The tests run one after the other.
	datasets map[string]bool
}

var sampleEngines = []sampleEngine{
	{name: "postgres", family: familyPostgres, start: startPostgresFixture},
	{name: "mysql", family: familyMysql, start: func(ctx context.Context) (*sampleFixture, error) {
		return startMysqlFixture(ctx)
	}},
	{name: "mariadb", family: familyMysql, start: func(ctx context.Context) (*sampleFixture, error) {
		return startMysqlFixture(ctx, tcmysql.WithImage("mariadb:11.4"))
	}},
	{name: "mssql", family: familyMssql, start: startMssqlFixture},
}

var (
	fixturesMu sync.Mutex
	fixtures   = map[string]*sampleFixture{}
)

// TestMain tears down the servers the sample tests started. A server is started the first time a
// test needs it, so running a single engine (-run 'Test_SampleData.*/postgres') starts one.
func TestMain(m *testing.M) {
	code := m.Run()
	fixturesMu.Lock()
	for _, f := range fixtures {
		_ = f.db.Close()
		_ = f.teardown(context.Background())
	}
	fixturesMu.Unlock()
	os.Exit(code)
}

// forEachEngine runs fn as a subtest for each engine of the given families.
func forEachEngine(t *testing.T, families []string, fn func(t *testing.T, f *sampleFixture)) {
	t.Helper()
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	for _, engine := range sampleEngines {
		if !contains(families, engine.family) {
			continue
		}
		t.Run(engine.name, func(t *testing.T) {
			fn(t, fixtureFor(t, engine))
		})
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func fixtureFor(t *testing.T, engine sampleEngine) *sampleFixture {
	t.Helper()
	fixturesMu.Lock()
	defer fixturesMu.Unlock()
	if f, ok := fixtures[engine.name]; ok {
		return f
	}
	f, err := engine.start(context.Background())
	require.NoError(t, err)
	f.engine = engine
	f.datasets = map[string]bool{}
	f.sqlmanager = tchusonymapi.NewTestSqlManagerClient()
	fixtures[engine.name] = f
	return f
}

func startPostgresFixture(ctx context.Context) (*sampleFixture, error) {
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	if err != nil {
		return nil, err
	}
	return &sampleFixture{
		db:         db,
		connection: pgConnection(container.URL),
		schema:     "public",
		teardown:   container.TearDown,
	}, nil
}

func pgConnection(url string) *mgmtv1alpha1.Connection {
	return &mgmtv1alpha1.Connection{
		Id: uuid.NewString(),
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{
			PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{
				ConnectionConfig: &mgmtv1alpha1.PostgresConnectionConfig_Url{Url: url},
			},
		}},
	}
}

func startMysqlFixture(ctx context.Context, opts ...tcmysql.Option) (*sampleFixture, error) {
	container, err := tcmysql.NewMysqlTestContainer(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &sampleFixture{
		db:     container.DB,
		schema: "testdb",
		connection: &mgmtv1alpha1.Connection{
			Id: uuid.NewString(),
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{
				MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.MysqlConnectionConfig_Url{Url: container.URL},
				},
			}},
		},
		teardown: container.TearDown,
	}, nil
}

func startMssqlFixture(ctx context.Context) (*sampleFixture, error) {
	container, err := tcmssql.NewMssqlTestContainer(ctx)
	if err != nil {
		return nil, err
	}
	return &sampleFixture{
		db:     container.DB,
		schema: "dbo",
		connection: &mgmtv1alpha1.Connection{
			Id: uuid.NewString(),
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_MssqlConfig{
				MssqlConfig: &mgmtv1alpha1.MssqlConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.MssqlConnectionConfig_Url{Url: container.URL},
				},
			}},
		},
		teardown: container.TearDown,
	}, nil
}

// quote quotes one identifier the way the engine does.
func (f *sampleFixture) quote(name string) string {
	switch f.engine.family {
	case familyMysql:
		return "`" + name + "`"
	case familyMssql:
		return "[" + name + "]"
	default:
		return `"` + name + `"`
	}
}

func (f *sampleFixture) qualified(table string) string {
	return f.quote(f.schema) + "." + f.quote(table)
}

func (f *sampleFixture) exec(t *testing.T, statement string) {
	t.Helper()
	_, err := f.db.ExecContext(context.Background(), statement)
	require.NoError(t, err, statement)
}

// dataset builds a table once per server and gives its name. Tests that share a table share the
// build; a build may use another dataset.
func (f *sampleFixture) dataset(t *testing.T, name string, build func()) string {
	t.Helper()
	if !f.datasets[name] {
		build()
		f.datasets[name] = true
	}
	return name
}

// createTable creates a table with a 64-bit integer key id, an integer column rank and a label.
func (f *sampleFixture) createTable(t *testing.T, table string) {
	t.Helper()
	f.exec(t, fmt.Sprintf(
		"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL)",
		f.qualified(table), f.quote("rank"),
	))
}

// load inserts the rows i = from..to, with id = i * idStep and rank = i. The
// numbers come from a cross join of a table of digits, which every engine runs the same way. The
// rows are inserted in rank order, so the first rows of a heap are the lowest ranks.
func (f *sampleFixture) load(t *testing.T, table string, from, to, idStep int) {
	t.Helper()
	digits := f.qualified("digits")
	f.dataset(t, "digits", func() {
		f.exec(t, fmt.Sprintf("CREATE TABLE %s (n INT NOT NULL)", digits))
		f.exec(t, fmt.Sprintf("INSERT INTO %s (n) VALUES (0),(1),(2),(3),(4),(5),(6),(7),(8),(9)", digits))
	})
	places := len(strconv.Itoa(to))
	terms := make([]string, places)
	joins := make([]string, places)
	for p := range places {
		alias := fmt.Sprintf("d%d", p)
		terms[p] = fmt.Sprintf("%s.n * %d", alias, pow10(p))
		joins[p] = fmt.Sprintf("%s %s", digits, alias)
	}
	f.exec(t, fmt.Sprintf(
		"INSERT INTO %[1]s (id, %[2]s, label) SELECT s.i * %[3]d, s.i, CONCAT('row-', i) FROM "+
			"(SELECT %[4]s + 1 AS i FROM %[5]s) s WHERE i BETWEEN %[6]d AND %[7]d ORDER BY s.i",
		f.qualified(table), f.quote("rank"), idStep,
		strings.Join(terms, " + "), strings.Join(joins, " CROSS JOIN "), from, to,
	))
}

func pow10(p int) int {
	n := 1
	for range p {
		n *= 10
	}
	return n
}

// analyze brings the statistics of a table up to date.
func (f *sampleFixture) analyze(t *testing.T, table string) {
	t.Helper()
	switch f.engine.family {
	case familyMysql:
		f.exec(t, "ANALYZE TABLE "+f.qualified(table))
	case familyMssql:
		f.exec(t, "UPDATE STATISTICS "+f.qualified(table))
	default:
		f.exec(t, "ANALYZE "+f.qualified(table))
	}
}

// filledTable builds, once, a table of count rows (rank 1..count, id = rank) and analyzes it.
func (f *sampleFixture) filledTable(t *testing.T, table string, count int) string {
	t.Helper()
	return f.dataset(t, table, func() {
		f.createTable(t, table)
		if count > 0 {
			f.load(t, table, 1, count, 1)
		}
		f.analyze(t, table)
	})
}

// bigTable is the table of 200 000 rows most tests draw from.
func (f *sampleFixture) bigTable(t *testing.T) string {
	t.Helper()
	return f.filledTable(t, "big", bigRows)
}

const bigRows = 200_000

// sampleRows draws a sample of numRows rows from a table through the service.
func (f *sampleFixture) sampleRows(t *testing.T, table string, numRows uint) ([]map[string]any, error) {
	t.Helper()
	return f.sampleRowsAs(t, f.connection, table, numRows)
}

// sampleRowsAs draws a sample through a connection of its own, which may log in as another user
// of the server.
func (f *sampleFixture) sampleRowsAs(
	t *testing.T,
	connection *mgmtv1alpha1.Connection,
	table string,
	numRows uint,
) ([]map[string]any, error) {
	t.Helper()
	service := connectiondata.NewSQLConnectionDataService(
		testutil.GetTestLogger(t),
		&sqlconnect.SqlOpenConnector{},
		f.sqlmanager,
		connection,
	)
	stream := &collectingStream{}
	err := service.SampleData(context.Background(), stream, f.schema, table, numRows)
	if err != nil {
		return nil, err
	}
	return stream.rows(t), nil
}

func (f *sampleFixture) mustSample(t *testing.T, table string, numRows uint) []map[string]any {
	t.Helper()
	rows, err := f.sampleRows(t, table, numRows)
	require.NoError(t, err)
	return rows
}

// collectingStream keeps the rows the service sends.
type collectingStream struct {
	payloads [][]byte
}

func (s *collectingStream) Send(resp *mgmtv1alpha1.GetConnectionDataStreamResponse) error {
	s.payloads = append(s.payloads, resp.GetRowBytes())
	return nil
}

func (s *collectingStream) rows(t *testing.T) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(s.payloads))
	for _, payload := range s.payloads {
		var row map[string]any
		require.NoError(t, gob.NewDecoder(bytes.NewReader(payload)).Decode(&row))
		out = append(out, row)
	}
	return out
}

// intColumn reads an integer column of a sampled row, whatever integer type the driver gave.
func intColumn(t *testing.T, row map[string]any, column string) int64 {
	t.Helper()
	value, ok := row[column]
	require.True(t, ok, "column %s is missing", column)
	v := reflect.ValueOf(value)
	switch v.Kind() { //nolint:exhaustive // only integers and their text form are columns here
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()) //nolint:gosec // test values are small
	case reflect.String:
		n, err := strconv.ParseInt(v.String(), 10, 64)
		require.NoError(t, err)
		return n
	case reflect.Slice:
		n, err := strconv.ParseInt(string(v.Bytes()), 10, 64)
		require.NoError(t, err)
		return n
	default:
		require.FailNow(t, "unexpected type for column "+column, "%T", value)
		return 0
	}
}

// requireDistinct fails when two rows of a sample hold the same value in a column.
func requireDistinct(t *testing.T, rows []map[string]any, column string) {
	t.Helper()
	seen := make(map[int64]bool, len(rows))
	for _, row := range rows {
		v := intColumn(t, row, column)
		require.False(t, seen[v], "a row came twice in the sample")
		seen[v] = true
	}
}

// sessionCountQuery counts the sessions open on the server.
func (f *sampleFixture) sessionCountQuery() string {
	switch f.engine.family {
	case familyMysql:
		return "SELECT COUNT(*) FROM information_schema.PROCESSLIST"
	case familyMssql:
		return "SELECT COUNT(*) FROM sys.dm_exec_sessions WHERE is_user_process = 1"
	default:
		return "SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database()"
	}
}

// queryRanks runs a query that returns the rows of a table built by createTable, and gives
// their ranks.
func (f *sampleFixture) queryRanks(t *testing.T, query string) []int64 {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	var ranks []int64
	for rows.Next() {
		var id, rank int64
		var label string
		require.NoError(t, rows.Scan(&id, &rank, &label))
		ranks = append(ranks, rank)
	}
	require.NoError(t, rows.Err())
	return ranks
}

// pgConnectionAs gives the connection of the fixture with another login.
func (f *sampleFixture) pgConnectionAs(t *testing.T, login, password string) *mgmtv1alpha1.Connection {
	t.Helper()
	u, err := url.Parse(f.connection.GetConnectionConfig().GetPgConfig().GetUrl())
	require.NoError(t, err)
	u.User = url.UserPassword(login, password)
	return pgConnection(u.String())
}

// mssqlConnectionAs gives the connection of the fixture with another login.
func (f *sampleFixture) mssqlConnectionAs(t *testing.T, login, password string) *mgmtv1alpha1.Connection {
	t.Helper()
	u, err := url.Parse(f.connection.GetConnectionConfig().GetMssqlConfig().GetUrl())
	require.NoError(t, err)
	u.User = url.UserPassword(login, password)
	return &mgmtv1alpha1.Connection{
		Id: uuid.NewString(),
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_MssqlConfig{
			MssqlConfig: &mgmtv1alpha1.MssqlConnectionConfig{
				ConnectionConfig: &mgmtv1alpha1.MssqlConnectionConfig_Url{Url: u.String()},
			},
		}},
	}
}
