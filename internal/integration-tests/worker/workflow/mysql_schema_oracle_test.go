package integrationtest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	posttablesync_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/post-table-sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
)

// Oracle test of the Init Table Schema option on MySQL.
//
// A database modelled on the legacy 5.7 production database (testdata
// mysql/schema-oracle/allopneus.sql) is loaded on a source server. Each scenario empties the
// destination server (DROP DATABASE), recreates the database there, then compares the
// destination with the source:
//
//   - control_mysqldump: schema and data by `mysqldump --routines --triggers --hex-blob`. It
//     involves no product code: it proves the fixture and the assertions themselves (a test
//     that fails here is wrong, not the product).
//   - control_mysqldump_schema: schema by `mysqldump --no-data --skip-triggers --routines`,
//     rows by a product sync job WITHOUT Init Table Schema, triggers by mysqldump after the
//     rows (the order of a mysqldump restore). It isolates the product's data copy from its
//     DDL.
//   - init_schema: the oracle itself. A product sync job with Init Table Schema and truncate,
//     on an empty destination (no database).
//
// Assertions, each a subtest of the scenario (see schemaOracle.compare); a job scenario
// first reports the errors of the run in 0_job_run, and the comparison runs anyway:
//
//	1_show_create_table      SHOW CREATE TABLE, per table, source == destination
//	2_show_create_database   SHOW CREATE DATABASE (default charset/collation)
//	3a_triggers_information_schema  information_schema.TRIGGERS (with ACTION_ORDER)
//	3b_show_create_trigger          SHOW CREATE TRIGGER (statement text kept by the server)
//	3c_routines_information_schema  information_schema.ROUTINES
//	3d_show_create_function         SHOW CREATE FUNCTION
//	4_information_schema     TABLES, COLUMNS, STATISTICS, REFERENTIAL_CONSTRAINTS,
//	                         KEY_COLUMN_USAGE (foreign keys)
//	5_data_hex               every row of every table, every column in hexadecimal
//	6_behaviour              ON UPDATE CURRENT_TIMESTAMP, implicit TIMESTAMP defaults, trigger
//	                         order and effects, on rows written to the destination
//
// Normalisations, the only ones (anything else is a difference):
//
//   - SHOW CREATE TABLE: the AUTO_INCREMENT=n table option is removed. It is the next value of
//     the counter, a state of the table, not a part of its definition: it depends on deleted
//     rows and rolled back inserts (the behaviour assertion rolls one back on the source), and
//     InnoDB 5.7 recomputes it as MAX(id)+1 at every restart.
//   - Names of the database: if the destination database had another name, the destination
//     name would be replaced by the source one in SHOW CREATE DATABASE and in the
//     REFERENCED/UNIQUE_CONSTRAINT schema columns. The job keeps the name, so this is a no-op
//     here; it is written for completeness.
//   - SHOW CREATE TABLE on MySQL 8 only: " CHARACTER SET cs COLLATE cs_xxx" of a column is
//     read as " COLLATE cs_xxx". MySQL 8 prints the character set of a column whose collation
//     was written in the DDL and omits it when the collation came from the table: two texts for
//     one column (a collation names its character set; information_schema.COLUMNS, compared
//     strictly, is identical). Without it even the mysqldump control fails on 8.4. See
//     oracleShowCreateTable.
//   - SHOW CREATE TABLE: a " ROW_FORMAT=X" option of the destination table is removed when the
//     source table shows none and has X as its row format (information_schema.TABLES); so is
//     row_format=X from its information_schema.TABLES.CREATE_OPTIONS. An
//     InnoDB table created under an older default (COMPACT) keeps it without the clause; the
//     product writes the clause so that the destination table has that row format, which
//     information_schema.TABLES, compared strictly, checks. mysqldump loses it (the dump has
//     no clause): the controls give it back by ALTER TABLE, as one would after a restore (see
//     restoreImplicitRowFormats).
//   - SHOW CREATE TRIGGER: the "Created" column (time of creation) is left out.
//   - Data: rows are compared as sorted lists (tables without a primary key have no order).
//     Columns are read as HEX(col) for string and binary types (raw bytes in the column's
//     character set, no conversion), HEX(CAST(col AS CHAR)) for numbers and dates, and
//     HEX(CAST(col + 0E0 AS CHAR)) for FLOAT/DOUBLE (the value widened to DOUBLE, printed with
//     its round-trip digits; HEX(col) of a number would round it to an integer).
//   - Definers are NOT normalised: the source fixture and the job both connect as root@'%'.
//
// The servers: MySQL 5.7.42 as the production ones (tcmysql.WithMysql57: sql_mode='',
// explicit_defaults_for_timestamp=OFF, character_set_server=utf8); MySQL 8.4 and MariaDB 11.4
// set (SET GLOBAL) to the same sql_mode and explicit_defaults_for_timestamp so that the
// fixture means the same there (zero dates, implicit TIMESTAMP defaults).
// lower_case_table_names is 0 on all.
//
// Run: WORKER_INTEGRATION_TESTS_ENABLED=1 go test ./internal/integration-tests/worker/workflow/ \
//
//	-run Test_MysqlInitSchemaOracle -count=1 -timeout 2400s -v
//
// Optionally restrict to a variant: -run 'Test_MysqlInitSchemaOracle/mysql57'.

const (
	oracleSchema     = "allopneus"
	oracleFixtureDir = "schema-oracle"
	oracleFixture    = "allopneus.sql"
)

func Test_MysqlInitSchemaOracle(t *testing.T) {
	t.Parallel()
	if !testutil.ShouldRunWorkerIntegrationTest() {
		return
	}
	ctx := context.Background()

	husonymApi, err := tchusonymapi.NewHusonymApiTestClient(
		ctx,
		t,
		tchusonymapi.WithMigrationsDirectory(husonymDbMigrationsPath),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, husonymApi.TearDown(context.Background())) })

	connclient := husonymApi.OSSUnauthenticatedLicensedClients.Connections()
	accountId := tchusonymapi.CreatePersonalAccount(ctx, t, husonymApi.OSSUnauthenticatedLicensedClients.Users())
	dbManagers := NewTestDatabaseManagers(t)

	// MySQL 8.4 and MariaDB keep their defaults but for the two settings the fixture relies on,
	// set globally once the servers run (setLegacyModes): tcmysql starts a server given options
	// as "mysqld ...", a command the MariaDB image does not have.
	// Many servers run side by side on a test host: InnoDB then may not get its Linux native
	// AIO contexts (io_setup EAGAIN) and abort at start. Without native AIO it starts anyway;
	// nothing the oracle compares depends on it. (Not for MariaDB: see above.)
	noNativeAio := tcmysql.WithServerArgs("--innodb-use-native-aio=0")
	variants := []struct {
		name        string
		opts        []tcmysql.Option
		legacyModes bool
	}{
		{name: "mysql57", opts: []tcmysql.Option{tcmysql.WithMysql57(), noNativeAio}},
		{name: "mysql", opts: []tcmysql.Option{noNativeAio}, legacyModes: true},
		{name: "mariadb", opts: []tcmysql.Option{tcmysql.WithImage("mariadb:11.4.13")}, legacyModes: true},
	}

	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			servers, err := tcmysql.NewMysqlTestSyncContainer(ctx, variant.opts, variant.opts)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, servers.TearDown(context.Background())) })
			if variant.legacyModes {
				setLegacyModes(t, ctx, servers.Source.DB)
				setLegacyModes(t, ctx, servers.Target.DB)
			}

			o := &schemaOracle{
				ctx:        ctx,
				variant:    variant.name,
				servers:    servers,
				husonymApi: husonymApi,
				dbManagers: dbManagers,
				accountId:  accountId,
				sourceConn: tchusonymapi.CreateMysqlConnection(
					ctx, t, connclient, accountId, "oracle-"+variant.name+"-source", servers.Source.URL),
				destConn: tchusonymapi.CreateMysqlConnection(
					ctx, t, connclient, accountId, "oracle-"+variant.name+"-dest", servers.Target.URL),
			}
			o.loadFixture(t)

			// The scenarios share the destination server: they run one after the other, each
			// on a destination emptied first.
			t.Run("control_mysqldump", func(t *testing.T) {
				o.resetDestination(t)
				o.copyWithMysqldump(t, "full", []string{
					"--databases", oracleSchema, "--routines", "--triggers", "--hex-blob",
					"--single-transaction", "--skip-dump-date",
				}, "")
				o.restoreImplicitRowFormats(t)
				o.compare(t)
			})

			t.Run("control_mysqldump_schema", func(t *testing.T) {
				o.resetDestination(t)
				o.copyWithMysqldump(t, "schema", []string{
					"--databases", oracleSchema, "--no-data", "--skip-triggers", "--routines",
					"--skip-dump-date",
				}, "")
				o.restoreImplicitRowFormats(t)
				o.runJob(t, false)
				o.copyWithMysqldump(t, "triggers", []string{
					oracleSchema, "--no-data", "--no-create-info", "--skip-routines", "--triggers",
					"--skip-dump-date",
				}, oracleSchema)
				o.compare(t)
			})

			t.Run("init_schema", func(t *testing.T) {
				o.resetDestination(t)
				o.runJob(t, true)
				o.compare(t)
			})
		})
	}
}

// setLegacyModes gives the server the sql_mode and explicit_defaults_for_timestamp of the 5.7
// production servers (see tcmysql.WithMysql57), for every connection opened from now on: the
// idle connections of the pool, opened under the former values, are closed.
func setLegacyModes(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, "SET GLOBAL sql_mode = ''; SET GLOBAL explicit_defaults_for_timestamp = OFF")
	require.NoError(t, err)
	db.SetMaxIdleConns(0)
	db.SetMaxIdleConns(2)
	var mode string
	var explicit int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode, @@SESSION.explicit_defaults_for_timestamp").
		Scan(&mode, &explicit))
	require.Equal(t, "", mode)
	require.Equal(t, 0, explicit)
}

type schemaOracle struct {
	ctx        context.Context
	variant    string
	servers    *tcmysql.MysqlTestSyncContainer
	husonymApi *tchusonymapi.HusonymApiTestClient
	dbManagers *TestDatabaseManagers
	accountId  string
	sourceConn *mgmtv1alpha1.Connection
	destConn   *mgmtv1alpha1.Connection
}

// loadFixture runs the fixture on the source server, or its twin for the variant
// (allopneus.<variant>.sql) when there is one.
func (o *schemaOracle) loadFixture(t *testing.T) {
	t.Helper()
	path := filepath.Join(mysqlTestdataFolder, oracleFixtureDir, oracleFixture)
	twin := filepath.Join(mysqlTestdataFolder, oracleFixtureDir,
		strings.TrimSuffix(oracleFixture, ".sql")+"."+o.variant+".sql")
	if _, err := os.Stat(twin); err == nil {
		path = twin
	}
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	// Every later query names the database: the connection left in it by USE does no harm.
	_, err = o.servers.Source.DB.ExecContext(o.ctx, string(content))
	require.NoError(t, err, "loading the fixture %s", path)
	if testing.Verbose() {
		for _, table := range oracleTables(t, o.ctx, o.servers.Source.DB, oracleSchema) {
			t.Logf("source %s:\n%s", o.variant, oracleQuery(t, o.ctx, o.servers.Source.DB,
				fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", oracleSchema, table))[0][1].String)
		}
	}
}

func (o *schemaOracle) resetDestination(t *testing.T) {
	t.Helper()
	_, err := o.servers.Target.DB.ExecContext(o.ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", oracleSchema))
	require.NoError(t, err)
}

// copyWithMysqldump dumps the source database with the dump tool of the source server's image
// and loads the dump on the destination with the client of the destination's image. intoDb is
// the database the client starts in ("" when the dump creates and selects it).
func (o *schemaOracle) copyWithMysqldump(t *testing.T, name string, args []string, intoDb string) {
	t.Helper()
	file := "/tmp/oracle-" + name + ".sql"
	dump := `D=$(command -v mysqldump || command -v mariadb-dump) && "$D" -uroot -p"$MYSQL_ROOT_PASSWORD" ` +
		`--default-character-set=utf8mb4 ` + strings.Join(args, " ") + " > " + file
	o.execIn(t, o.servers.Source, dump, nil)

	reader, err := o.servers.Source.TestContainer.CopyFileFromContainer(o.ctx, file)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.NoError(t, o.servers.Target.TestContainer.CopyToContainer(o.ctx, content, file, 0o644))

	// --comments: the client keeps the comments of the trigger bodies (it strips them by
	// default, which changes the definition and trips its parser on a quote in a comment).
	load := `C=$(command -v mysql || command -v mariadb) && "$C" -uroot -p"$MYSQL_ROOT_PASSWORD" ` +
		`--default-character-set=utf8mb4 --comments ` + intoDb + " < " + file
	o.execIn(t, o.servers.Target, load, content)
}

// execIn runs a shell command in the container of the server. On a failure of the client
// that names a line of its input, the lines around it are shown.
func (o *schemaOracle) execIn(t *testing.T, server *tcmysql.MysqlTestContainer, command string, input []byte) {
	t.Helper()
	code, out, err := server.TestContainer.Exec(o.ctx, []string{"sh", "-c", command}, tcexec.Multiplexed())
	require.NoError(t, err)
	output, err := io.ReadAll(out)
	require.NoError(t, err)
	if code != 0 && input != nil {
		if m := regexp.MustCompile(`at line (\d+)`).FindSubmatch(output); m != nil {
			var line int
			_, _ = fmt.Sscanf(string(m[1]), "%d", &line)
			lines := strings.Split(string(input), "\n")
			from, to := max(line-15, 0), min(line+5, len(lines))
			output = append(output, []byte("\ninput lines "+fmt.Sprint(from+1)+"-"+fmt.Sprint(to)+":\n"+
				strings.Join(lines[from:to], "\n"))...)
		}
	}
	require.Equalf(t, 0, code, "%s\n%s", command, output)
}

// runJob syncs the source database to the destination with a passthrough job (every column of
// every table), truncate on, Init Table Schema as asked. An activity or workflow error is
// reported as a failure, and the comparison still runs: it tells what the run left behind.
func (o *schemaOracle) runJob(t *testing.T, initSchema bool) {
	t.Helper()
	jobclient := o.husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	o.husonymApi.MockTemporalForCreateJob("test-mysql-sync")
	job := createMysqlSyncJob(t, o.ctx, jobclient, &createJobConfig{
		AccountId:   o.accountId,
		SourceConn:  o.sourceConn,
		DestConn:    o.destConn,
		JobName:     "mysql_schema_oracle",
		JobMappings: o.passthroughMappings(t),
		JobOptions: &TestJobOptions{
			Truncate:   true,
			InitSchema: initSchema,
		},
	})

	var mu sync.Mutex
	var problems []string
	testworkflow := NewTestDataSyncWorkflowEnv(t, o.husonymApi, o.dbManagers)
	testworkflow.TestEnv.SetOnActivityCompletedListener(
		func(info *activity.Info, result converter.EncodedValue, err error) {
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				problems = append(problems, fmt.Sprintf("activity %s: %v", info.ActivityType.Name, err))
				return
			}
			if info.ActivityType.Name == "RunPostTableSync" && result.HasValue() {
				var resp posttablesync_activity.RunPostTableSyncResponse
				if decodeErr := result.Get(&resp); decodeErr == nil && len(resp.Errors) > 0 {
					problems = append(problems, fmt.Sprintf("activity RunPostTableSync: %v",
						formatPostTableSyncErrors(resp.Errors)))
				}
			}
		},
	)
	testworkflow.ExecuteTestDataSyncWorkflow(job.GetId())
	if !testworkflow.TestEnv.IsWorkflowCompleted() {
		problems = append(problems, "the workflow did not complete")
	}
	if err := testworkflow.TestEnv.GetWorkflowError(); err != nil {
		problems = append(problems, fmt.Sprintf("workflow error: %v", err))
	}
	t.Run("0_job_run", func(t *testing.T) {
		assert.Empty(t, problems, "errors of the sync run (init schema = %v)", initSchema)
	})
}

func (o *schemaOracle) passthroughMappings(t *testing.T) []*mgmtv1alpha1.JobMapping {
	t.Helper()
	rows := oracleQuery(t, o.ctx, o.servers.Source.DB,
		"SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? "+
			"ORDER BY TABLE_NAME, ORDINAL_POSITION", oracleSchema)
	mappings := make([]*mgmtv1alpha1.JobMapping, 0, len(rows))
	for _, row := range rows {
		mappings = append(mappings, &mgmtv1alpha1.JobMapping{
			Schema: oracleSchema,
			Table:  row[0].String,
			Column: row[1].String,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{
				Config: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
						PassthroughConfig: &mgmtv1alpha1.Passthrough{},
					},
				},
			},
		})
	}
	require.NotEmpty(t, mappings)
	return mappings
}

// compare runs every assertion of the oracle, source against destination. The destination
// database keeps the source's name.
func (o *schemaOracle) compare(t *testing.T) {
	t.Helper()
	src, dst := o.servers.Source.DB, o.servers.Target.DB
	srcSchema, dstSchema := oracleSchema, oracleSchema
	ctx := o.ctx

	var dstExists bool
	{
		rows := oracleQuery(t, ctx, dst,
			"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", dstSchema)
		dstExists = len(rows) == 1
	}
	if !assert.Truef(t, dstExists, "the destination has no database %q", dstSchema) {
		return
	}
	// The destination name in the source's terms (no-op while both are the same).
	norm := func(s string) string {
		if srcSchema == dstSchema {
			return s
		}
		return strings.ReplaceAll(s, dstSchema, srcSchema)
	}

	srcTables := oracleTables(t, ctx, src, srcSchema)
	dstTables := oracleTables(t, ctx, dst, dstSchema)

	t.Run("1_show_create_table", func(t *testing.T) {
		assert.Equal(t, srcTables, dstTables, "the tables of the database")
		mysql8 := oracleIsMysql8(t, ctx, src)
		for _, table := range srcTables {
			if !slices.Contains(dstTables, table) {
				continue
			}
			want := oracleShowCreateTable(t, ctx, src, srcSchema, table, mysql8)
			got := norm(oracleShowCreateTable(t, ctx, dst, dstSchema, table, mysql8))
			if !strings.Contains(want, " ROW_FORMAT=") {
				format := strings.ToUpper(oracleRowFormat(t, ctx, src, srcSchema, table))
				got = strings.Replace(got, " ROW_FORMAT="+format, "", 1)
			}
			assert.Equalf(t, want, got, "SHOW CREATE TABLE `%s` (AUTO_INCREMENT=n removed)", table)
		}
	})

	t.Run("2_show_create_database", func(t *testing.T) {
		want := oracleQuery(t, ctx, src, fmt.Sprintf("SHOW CREATE DATABASE `%s`", srcSchema))[0][1].String
		got := norm(oracleQuery(t, ctx, dst, fmt.Sprintf("SHOW CREATE DATABASE `%s`", dstSchema))[0][1].String)
		assert.Equal(t, want, got, "SHOW CREATE DATABASE")
		q := "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?"
		assert.Equal(t, oracleLines(t, ctx, src, q, srcSchema), oracleLines(t, ctx, dst, q, dstSchema),
			"information_schema.SCHEMATA")
	})

	// 3a/3c compare what the server makes of the triggers and routines (body, firing order,
	// sql_mode, definer, session character sets, data access); 3b/3d compare the statement
	// text the server keeps (SHOW CREATE), which mysqldump writes back as is.
	t.Run("3a_triggers_information_schema", func(t *testing.T) {
		q := "SELECT EVENT_OBJECT_TABLE, ACTION_TIMING, EVENT_MANIPULATION, ACTION_ORDER, TRIGGER_NAME, " +
			"ACTION_ORIENTATION, ACTION_STATEMENT, SQL_MODE, DEFINER, CHARACTER_SET_CLIENT, " +
			"COLLATION_CONNECTION, DATABASE_COLLATION FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ? " +
			"ORDER BY EVENT_OBJECT_TABLE, ACTION_TIMING, EVENT_MANIPULATION, ACTION_ORDER"
		assert.Equal(t, oracleLines(t, ctx, src, q, srcSchema), oracleLines(t, ctx, dst, q, dstSchema),
			"information_schema.TRIGGERS (ACTION_ORDER is the firing order)")
	})

	t.Run("3b_show_create_trigger", func(t *testing.T) {
		for _, row := range oracleQuery(t, ctx, src,
			"SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ? ORDER BY TRIGGER_NAME", srcSchema) {
			name := row[0].String
			stmt := fmt.Sprintf("SHOW CREATE TRIGGER `%%s`.`%s`", name)
			want := oracleShowCreate(t, ctx, src, fmt.Sprintf(stmt, srcSchema), "Created")
			got, err := oracleShowCreateErr(ctx, dst, fmt.Sprintf(stmt, dstSchema), "Created")
			if assert.NoErrorf(t, err, "SHOW CREATE TRIGGER `%s` on the destination", name) {
				assert.Equalf(t, want, norm(got), "SHOW CREATE TRIGGER `%s` (column Created left out)", name)
			}
		}
	})

	t.Run("3c_routines_information_schema", func(t *testing.T) {
		q := "SELECT ROUTINE_TYPE, ROUTINE_NAME, DTD_IDENTIFIER, ROUTINE_DEFINITION, IS_DETERMINISTIC, " +
			"SQL_DATA_ACCESS, SECURITY_TYPE, SQL_MODE, ROUTINE_COMMENT, DEFINER, CHARACTER_SET_CLIENT, " +
			"COLLATION_CONNECTION, DATABASE_COLLATION FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? " +
			"ORDER BY ROUTINE_TYPE, ROUTINE_NAME"
		assert.Equal(t, oracleLines(t, ctx, src, q, srcSchema), oracleLines(t, ctx, dst, q, dstSchema),
			"information_schema.ROUTINES")
	})

	t.Run("3d_show_create_function", func(t *testing.T) {
		for _, row := range oracleQuery(t, ctx, src,
			"SELECT ROUTINE_TYPE, ROUTINE_NAME FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? "+
				"ORDER BY ROUTINE_TYPE, ROUTINE_NAME", srcSchema) {
			kind, name := row[0].String, row[1].String
			stmt := fmt.Sprintf("SHOW CREATE %s `%%s`.`%s`", kind, name)
			want := oracleShowCreate(t, ctx, src, fmt.Sprintf(stmt, srcSchema))
			got, err := oracleShowCreateErr(ctx, dst, fmt.Sprintf(stmt, dstSchema))
			if assert.NoErrorf(t, err, "SHOW CREATE %s `%s` on the destination", kind, name) {
				assert.Equalf(t, want, norm(got), "SHOW CREATE %s `%s`", kind, name)
			}
		}
	})

	t.Run("4_information_schema", func(t *testing.T) {
		for _, c := range []struct{ what, query string }{
			{"TABLES", "SELECT TABLE_NAME, TABLE_TYPE, ENGINE, ROW_FORMAT, TABLE_COLLATION, CREATE_OPTIONS, " +
				"TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME"},
			{"COLUMNS", "SELECT TABLE_NAME, ORDINAL_POSITION, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, " +
				"EXTRA, CHARACTER_SET_NAME, COLLATION_NAME, COLUMN_COMMENT, COLUMN_KEY, GENERATION_EXPRESSION " +
				"FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION"},
			{"STATISTICS", "SELECT TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE, SUB_PART, PACKED, " +
				"NULLABLE, INDEX_TYPE, COLLATION, COMMENT, INDEX_COMMENT FROM information_schema.STATISTICS " +
				"WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX"},
			{"REFERENTIAL_CONSTRAINTS", "SELECT TABLE_NAME, CONSTRAINT_NAME, UNIQUE_CONSTRAINT_SCHEMA, " +
				"UNIQUE_CONSTRAINT_NAME, REFERENCED_TABLE_NAME, MATCH_OPTION, UPDATE_RULE, DELETE_RULE " +
				"FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = ? " +
				"ORDER BY TABLE_NAME, CONSTRAINT_NAME"},
			{"KEY_COLUMN_USAGE (foreign keys)", "SELECT TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION, COLUMN_NAME, " +
				"POSITION_IN_UNIQUE_CONSTRAINT, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME " +
				"FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL " +
				"ORDER BY TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION"},
		} {
			want := oracleLines(t, ctx, src, c.query, srcSchema)
			got := oracleLines(t, ctx, dst, c.query, dstSchema)
			for i := range got {
				got[i] = norm(got[i])
			}
			if c.what == "TABLES" {
				got = withoutWrittenRowFormats(want, got)
			}
			assert.Equalf(t, want, got, "information_schema.%s", c.what)
		}
	})

	t.Run("5_data_hex", func(t *testing.T) {
		for _, table := range srcTables {
			if !slices.Contains(dstTables, table) {
				t.Errorf("table `%s` missing on the destination: rows not compared", table)
				continue
			}
			query := oracleHexSelect(t, ctx, src, srcSchema, table)
			want := oracleSortedLines(t, ctx, src, fmt.Sprintf(query, srcSchema))
			got, err := oracleSortedLinesErr(ctx, dst, fmt.Sprintf(query, dstSchema))
			if assert.NoErrorf(t, err, "reading `%s` on the destination", table) {
				assert.Equalf(t, want, got, "rows of `%s`, columns in hexadecimal, sorted", table)
			}
		}
	})

	t.Run("6_behaviour", func(t *testing.T) {
		o.compareBehaviour(t, srcSchema, dstSchema)
	})
}

// compareBehaviour writes rows on the destination and checks what the server makes of them:
// the ON UPDATE CURRENT_TIMESTAMP columns are refreshed, the triggers fire in their order, the
// defaults and the implicit TIMESTAMP defaults are those of the source. The InnoDB statements
// also run on the source, in a transaction rolled back, and both results must be equal; the
// MyISAM table (no transaction) is checked on the destination against fixed expectations.
func (o *schemaOracle) compareBehaviour(t *testing.T, srcSchema, dstSchema string) {
	ctx := o.ctx
	statements := []string{
		// BEFORE UPDATE trigger + ON UPDATE CURRENT_TIMESTAMP on Commande.Date_Maj
		"UPDATE `%[1]s`.`Commande` SET `Montant_HT` = `Montant_HT` + 1 WHERE `id` = 1",
		// ON UPDATE CURRENT_TIMESTAMP on Client.Date_Maj (no trigger on update)
		"UPDATE `%[1]s`.`Client` SET `Newsletter` = 1 - `Newsletter` WHERE `id` = 2",
		// both BEFORE INSERT triggers, in their order, and the column defaults
		"INSERT INTO `%[1]s`.`Commande` (`id`, `Client_id`, `Reference`) VALUES (100, 2, 'cmd-new')",
		"INSERT INTO `%[1]s`.`Client` (`id`, `Email`) VALUES (100, '  Nouveau@Example.COM')",
	}
	// One line per row, prefixed by its table; string columns of mixed character sets are
	// converted to utf8mb4 so that CONCAT_WS can join them.
	u := func(col string) string { return "CONVERT(`" + col + "` USING utf8mb4)" }
	probe := "SELECT CONCAT_WS(' | ', 'Commande', `id`, " + u("Reference") + ", `Nb_Insert`, `Nb_Modifs`, " +
		u("Note_Interne") + ", `Statut_id`, `Montant_HT`, CAST(`Date_Commande` AS CHAR), CAST(`Date_Creation` AS CHAR), " +
		"IFNULL(CAST(`Date_Expedition` AS CHAR), 'NULL'), " +
		"IF(`Date_Maj` >= NOW() - INTERVAL 1 HOUR, 'Date_Maj=now', CONCAT('Date_Maj=', CAST(`Date_Maj` AS CHAR)))) " +
		"FROM `%[1]s`.`Commande` WHERE `id` IN (1, 100)"
	// A query of its own (a UNION of both may refuse their different collations).
	probeClient := "SELECT CONCAT_WS(' | ', 'Client', `id`, " + u("Email") + ", " + u("Nom") + ", " + u("Prenom") +
		", `Newsletter`, IFNULL(" + u("Civilite") + ", 'NULL'), " + u("Preferences") + ", " +
		"CAST(`Date_Naissance` AS CHAR), CAST(`Date_Inscription` AS CHAR), " +
		"IF(`Date_Maj` >= NOW() - INTERVAL 1 HOUR, 'Date_Maj=now', CONCAT('Date_Maj=', CAST(`Date_Maj` AS CHAR)))) " +
		"FROM `%[1]s`.`Client` WHERE `id` IN (2, 100)"

	run := func(db *sql.DB, schema string, rollback bool) ([]string, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback() //nolint:errcheck
		for _, s := range statements {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(s, schema)); err != nil {
				return nil, fmt.Errorf("%s: %w", fmt.Sprintf(s, schema), err)
			}
		}
		var out []string
		for _, q := range []string{probe, probeClient} {
			rows, err := tx.QueryContext(ctx, fmt.Sprintf(q, schema))
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					rows.Close()
					return nil, err
				}
				out = append(out, line)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
		sort.Strings(out)
		if rollback {
			return out, tx.Rollback()
		}
		return out, tx.Commit()
	}
	want, err := run(o.servers.Source.DB, srcSchema, true)
	require.NoError(t, err, "the behaviour statements on the source")
	// What the source itself must give: the oracle's expectations are checked on it too, so
	// that a wrong expectation fails the controls as well.
	require.Len(t, want, 4, "source: rows of the behaviour probe")
	require.Regexp(t, `(?m)^Client \| 0*100 \| nouveau@example.com \|`, strings.Join(want, "\n"),
		"source: trigger on Client")
	require.Regexp(t, `(?m)^Client \| 0*2 \| .*Date_Maj=now$`, strings.Join(want, "\n"),
		"source: ON UPDATE on Client")
	require.Regexp(t, `(?m)^Commande \| 1 \| CMD-0001 \| 1 \| 1 \| .*Date_Maj=now$`, strings.Join(want, "\n"),
		"source: BEFORE UPDATE trigger and ON UPDATE on Commande")
	require.Regexp(t, `(?m)^Commande \| 100 \| CMD-NEW \| 1 \| 0 \| créée : CMD-NEW \| 1 \| 0\.00 \| `+
		`0000-00-00 00:00:00 \| 0000-00-00 00:00:00 \| NULL \| Date_Maj=now$`, strings.Join(want, "\n"),
		"source: BEFORE INSERT triggers on Commande in their order, and the defaults")
	got, err := run(o.servers.Target.DB, dstSchema, false)
	if assert.NoError(t, err, "the behaviour statements on the destination") {
		assert.Equal(t, want, got,
			"rows after UPDATE/INSERT (ON UPDATE CURRENT_TIMESTAMP, triggers in order, defaults)")
	}

	// MyISAM, implicit TIMESTAMP defaults of explicit_defaults_for_timestamp=OFF: the first
	// TIMESTAMP column takes CURRENT_TIMESTAMP on insert and on update, the second keeps
	// '0000-00-00 00:00:00'.
	journal := func(db *sql.DB, schema string) ([]string, error) {
		for _, s := range []string{
			"INSERT INTO `%[1]s`.`Journal_MyISAM` (`Commande_id`, `Message`) VALUES (100, 'oracle')",
			"UPDATE `%[1]s`.`Journal_MyISAM` SET `Detail` = 'oracle' WHERE `Commande_id` = 1",
		} {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(s, schema)); err != nil {
				return nil, err
			}
		}
		return oracleSortedLinesErr(ctx, db, fmt.Sprintf(
			"SELECT CONCAT_WS(' | ', `Commande_id`, `Message`, IFNULL(`Detail`, 'NULL'), "+
				"IF(`Horodatage` >= NOW() - INTERVAL 1 HOUR, 'now', CAST(`Horodatage` AS CHAR)), "+
				"CAST(`Horodatage_Fin` AS CHAR)) FROM `%s`.`Journal_MyISAM` WHERE `Commande_id` IN (1, 100)", schema))
	}
	gotJournal, err := journal(o.servers.Target.DB, dstSchema)
	if assert.NoError(t, err, "the MyISAM statements on the destination") {
		assert.Equal(t, []string{
			"1 | commande créée | oracle | now | 0000-00-00 00:00:00",
			"100 | oracle | NULL | now | 0000-00-00 00:00:00",
		}, gotJournal, "Journal_MyISAM: implicit TIMESTAMP defaults and ON UPDATE")
	}
}

var (
	oracleAutoIncrement = regexp.MustCompile(` AUTO_INCREMENT=\d+`)
	oracleColumnCharset = regexp.MustCompile(` CHARACTER SET (\w+) COLLATE (\w+)`)
)

// oracleShowCreateTable gives SHOW CREATE TABLE without its AUTO_INCREMENT=n option. On MySQL 8
// (dropRedundantCharset), " CHARACTER SET cs COLLATE coll" of a column becomes " COLLATE coll"
// when coll is a collation of cs: MySQL 8 prints the character set of a column whose collation
// was written in its DDL and leaves it out when the collation came from the table, for the same
// column (a collation belongs to one character set; information_schema.COLUMNS, compared in
// full, is the same). Even a mysqldump reload does not keep that text on MySQL 8: the dump
// writes every collation out. 5.7 and MariaDB print the same text either way: not normalised.
func oracleShowCreateTable(
	t *testing.T, ctx context.Context, db *sql.DB, schema, table string, dropRedundantCharset bool,
) string {
	t.Helper()
	rows := oracleQuery(t, ctx, db, fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", schema, table))
	require.Len(t, rows, 1)
	out := oracleAutoIncrement.ReplaceAllString(rows[0][1].String, "")
	if dropRedundantCharset {
		out = oracleColumnCharset.ReplaceAllStringFunc(out, func(m string) string {
			parts := oracleColumnCharset.FindStringSubmatch(m)
			if strings.HasPrefix(parts[2], parts[1]+"_") {
				return " COLLATE " + parts[2]
			}
			return m
		})
	}
	return out
}

// withoutWrittenRowFormats removes, from the CREATE_OPTIONS of a destination line of
// information_schema.TABLES, the row_format=X the source line does not have, when X is the
// ROW_FORMAT of the source table: the clause written for a row format the source table has
// without it (see the normalisations of SHOW CREATE TABLE). ROW_FORMAT is compared as it is.
func withoutWrittenRowFormats(want, got []string) []string {
	format := map[string]string{}
	for _, line := range want {
		if strings.Contains(line, "CREATE_OPTIONS=row_format=") || strings.Contains(line, " row_format=") {
			continue
		}
		if m := oracleTablesLine.FindStringSubmatch(line); m != nil {
			format[m[1]] = strings.ToUpper(m[2])
		}
	}
	out := slices.Clone(got)
	for i, line := range out {
		m := oracleTablesLine.FindStringSubmatch(line)
		if m == nil || format[m[1]] == "" {
			continue
		}
		clause := "row_format=" + format[m[1]]
		switch {
		case strings.Contains(line, "CREATE_OPTIONS="+clause+" |"):
			line = strings.Replace(line, "CREATE_OPTIONS="+clause+" |", "CREATE_OPTIONS= |", 1)
		case strings.Contains(line, "CREATE_OPTIONS="+clause+" "):
			line = strings.Replace(line, "CREATE_OPTIONS="+clause+" ", "CREATE_OPTIONS=", 1)
		default:
			line = strings.Replace(line, " "+clause+" ", " ", 1)
		}
		out[i] = line
	}
	return out
}

// oracleTablesLine reads the name and the row format of a line of information_schema.TABLES.
var oracleTablesLine = regexp.MustCompile(`^TABLE_NAME=(.*?) \| .* \| ROW_FORMAT=(\w*) \|`)

// oracleRowFormat gives information_schema.TABLES.ROW_FORMAT of a table.
func oracleRowFormat(t *testing.T, ctx context.Context, db *sql.DB, schema, table string) string {
	t.Helper()
	rows := oracleQuery(t, ctx, db,
		"SELECT ROW_FORMAT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", schema, table)
	require.Len(t, rows, 1)
	return rows[0][0].String
}

// restoreImplicitRowFormats gives the destination tables loaded from a dump the row format the
// source InnoDB tables have without a ROW_FORMAT clause, when it is not the one of the
// destination's default: the dump writes no clause for them, so they took that default.
func (o *schemaOracle) restoreImplicitRowFormats(t *testing.T) {
	t.Helper()
	rows := oracleQuery(t, o.ctx, o.servers.Source.DB,
		"SELECT TABLE_NAME, ROW_FORMAT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? "+
			"AND ENGINE = 'InnoDB' AND CREATE_OPTIONS NOT LIKE '%row_format=%' "+
			"AND LOWER(ROW_FORMAT) <> LOWER(@@GLOBAL.innodb_default_row_format)", oracleSchema)
	for _, row := range rows {
		_, err := o.servers.Target.DB.ExecContext(o.ctx, fmt.Sprintf(
			"ALTER TABLE `%s`.`%s` ROW_FORMAT=%s", oracleSchema, row[0].String, strings.ToUpper(row[1].String)))
		require.NoError(t, err)
	}
}

// oracleIsMysql8 tells whether the server is MySQL 8 or later (not MariaDB).
func oracleIsMysql8(t *testing.T, ctx context.Context, db *sql.DB) bool {
	t.Helper()
	var version string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version))
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return false
	}
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	require.NoError(t, err, "server version %q", version)
	return n >= 8
}

func oracleTables(t *testing.T, ctx context.Context, db *sql.DB, schema string) []string {
	t.Helper()
	var out []string
	for _, row := range oracleQuery(t, ctx, db,
		"SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", schema) {
		out = append(out, row[0].String)
	}
	return out
}

// oracleHexSelect builds the SELECT giving one line per row of the table, every column as
// name=HEX(value) (see the normalisations above), with %s in place of the schema.
func oracleHexSelect(t *testing.T, ctx context.Context, db *sql.DB, schema, table string) string {
	t.Helper()
	var parts []string
	for _, row := range oracleQuery(t, ctx, db,
		"SELECT COLUMN_NAME, DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? "+
			"ORDER BY ORDINAL_POSITION", schema, table) {
		col := "`" + row[0].String + "`"
		var expr string
		switch strings.ToLower(row[1].String) {
		case "char", "varchar", "tinytext", "text", "mediumtext", "longtext",
			"binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob", "enum", "set":
			expr = "HEX(" + col + ")"
		case "float", "double", "real":
			expr = "HEX(CAST(" + col + " + 0E0 AS CHAR))"
		default: // integers, decimal, dates, times, year, json
			expr = "HEX(CAST(" + col + " AS CHAR))"
		}
		parts = append(parts, fmt.Sprintf("CONCAT('%s=', IFNULL(%s, 'NULL'))", row[0].String, expr))
	}
	require.NotEmpty(t, parts, "columns of %s", table)
	return fmt.Sprintf("SELECT CONCAT_WS(' ', %s) FROM `%%s`.`%s`", strings.Join(parts, ", "), table)
}

func oracleShowCreate(t *testing.T, ctx context.Context, db *sql.DB, query string, skip ...string) string {
	t.Helper()
	out, err := oracleShowCreateErr(ctx, db, query, skip...)
	require.NoError(t, err, query)
	return out
}

// oracleShowCreateErr gives the columns of a SHOW CREATE result as "name: value" lines, but the
// skipped ones.
func oracleShowCreateErr(ctx context.Context, db *sql.DB, query string, skip ...string) (string, error) {
	cols, rows, err := oracleQueryErr(ctx, db, query)
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("%s: %d rows", query, len(rows))
	}
	var b strings.Builder
	for i, col := range cols {
		if slices.Contains(skip, col) {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", col, oracleValue(rows[0][i]))
	}
	return b.String(), nil
}

// oracleLines gives the rows of a query as "COLUMN=value" lines, in the order of the query.
func oracleLines(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	cols, rows, err := oracleQueryErr(ctx, db, query, args...)
	require.NoError(t, err, query)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		fields := make([]string, len(cols))
		for i, col := range cols {
			fields[i] = col + "=" + oracleValue(row[i])
		}
		out = append(out, strings.Join(fields, " | "))
	}
	return out
}

func oracleSortedLines(t *testing.T, ctx context.Context, db *sql.DB, query string) []string {
	t.Helper()
	out, err := oracleSortedLinesErr(ctx, db, query)
	require.NoError(t, err, query)
	return out
}

func oracleSortedLinesErr(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	_, rows, err := oracleQueryErr(ctx, db, query)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, oracleValue(row[0]))
	}
	sort.Strings(out)
	return out, nil
}

func oracleValue(v sql.NullString) string {
	if !v.Valid {
		return "<NULL>"
	}
	return v.String
}

func oracleQuery(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) [][]sql.NullString {
	t.Helper()
	_, rows, err := oracleQueryErr(ctx, db, query, args...)
	require.NoError(t, err, query)
	return rows
}

func oracleQueryErr(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, [][]sql.NullString, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]sql.NullString
	for rows.Next() {
		row := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", query, err)
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}
