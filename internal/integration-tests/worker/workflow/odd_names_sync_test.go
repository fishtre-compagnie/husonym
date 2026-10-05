package integrationtest

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// oddSyncNames are names every engine accepts once they are quoted, and that a statement written
// without quoting them, or quoting them without doubling the quote character, reads as something
// else. They are the names of internal/integration-tests/sqlmanager/odd_names_fixture_integration_test.go,
// which a test of another package cannot share.
var oddSyncNames = []string{
	`we"ird`,
	"we`ird",
	"we]ird",
	"o'clock",
	`back\slash`,
	"semi;colon",
	"two$$dollars",
	"sp ace",
	"new\nline",
	"MixedCase",
	"order",
}

// oddSyncUnit is a schema (a database on MySQL and MariaDB) with a parent table and a child table
// linked by a foreign key, every object of which bears one of the odd names.
//
// On PostgreSQL the parent holds the key, a text column, an identity column and a column fed by a
// sequence named like the identity column; the column it feeds is named like the foreign key
// column of the child. On MySQL and MariaDB the identity column is the key of the child: the
// product creates an AUTO_INCREMENT column as the primary key of its table.
type oddSyncUnit struct {
	schema, parent, child, pk, fk, other, index, counter, check, trigger, foreignKey string
}

func oddSyncUnitAt(offset int) oddSyncUnit {
	name := func(slot int) string { return oddSyncNames[(slot+offset)%len(oddSyncNames)] }
	return oddSyncUnit{
		schema: name(0), parent: name(1), child: name(2), pk: name(3), fk: name(4), other: name(5),
		index: name(6), counter: name(7), check: name(8), trigger: name(9), foreignKey: name(10),
	}
}

const (
	oddSyncRotations = 3
	// oddSyncParents and oddSyncChildren are the rows of the two tables of a unit; the job keeps
	// the parents up to oddSyncKept and their children, three for each parent.
	oddSyncParents  = 20
	oddSyncChildren = 60
	oddSyncKept     = 10

	oddSyncTriggerFunction = "touch"
	oddSyncCanary          = "canary"
)

// oddSyncRotation gives every third unit from the rotation's own on: a name is at a different
// place in each unit of a rotation, and in no two rotations at the same.
func oddSyncRotation(rotation int) []oddSyncUnit {
	var units []oddSyncUnit
	for offset := rotation; offset < len(oddSyncNames); offset += oddSyncRotations {
		units = append(units, oddSyncUnitAt(offset))
	}
	return units
}

// oddSyncServers are the source and the destination of one engine.
type oddSyncServers struct {
	mysql          bool
	canarySchema   string
	source, target *sql.DB
	sourceConn     *mgmtv1alpha1.Connection
	destConn       *mgmtv1alpha1.Connection
}

// quote quotes one identifier the way the engine does: the quote character is doubled.
func (s *oddSyncServers) quote(name string) string {
	if s.mysql {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (s *oddSyncServers) table(schema, table string) string {
	return s.quote(schema) + "." + s.quote(table)
}

func oddSyncText(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

// childKey names the key of the child: the identity column on MySQL and MariaDB.
func (s *oddSyncServers) childKey(u oddSyncUnit) string {
	if s.mysql {
		return u.counter
	}
	return u.pk
}

// parentColumns and childColumns name the columns of the two tables, the key first.
func (s *oddSyncServers) parentColumns(u oddSyncUnit) []string {
	if s.mysql {
		return []string{u.pk, u.other}
	}
	return []string{u.pk, u.other, u.counter, u.fk}
}

func (s *oddSyncServers) childColumns(u oddSyncUnit) []string {
	return []string{s.childKey(u), u.fk, "note"}
}

// unitStatements gives the statements that create a unit, written by this test with the quoting
// rule of the engine.
func (s *oddSyncServers) unitStatements(u oddSyncUnit) []string {
	q := s.quote
	schema := q(u.schema)
	parent, child := s.table(u.schema, u.parent), s.table(u.schema, u.child)
	if s.mysql {
		// MySQL gives the expression of a CHECK constraint with escapes and the statement of a
		// trigger that names a column of NEW holding a backtick with the name altered: the
		// constraint and the trigger are over a column whose name holds neither.
		checked, assigned := u.pk, u.other
		if strings.ContainsAny(checked, "\\\n") {
			checked = u.other
		}
		if strings.Contains(assigned, "`") {
			assigned = u.pk
		}
		return []string{
			"CREATE DATABASE " + schema,
			fmt.Sprintf(
				"CREATE TABLE %s (%s INT NOT NULL, %s VARCHAR(40) NOT NULL, "+
					"PRIMARY KEY (%s), KEY %s (%s), CONSTRAINT %s CHECK (%s IS NOT NULL))",
				parent, q(u.pk), q(u.other), q(u.pk), q(u.index), q(u.other), q(u.check), q(checked)),
			fmt.Sprintf(
				"CREATE TRIGGER %s.%s BEFORE INSERT ON %s FOR EACH ROW SET NEW.%s = NEW.%s",
				schema, q(u.trigger), parent, q(assigned), q(assigned)),
			fmt.Sprintf(
				"CREATE TABLE %s (%s INT NOT NULL AUTO_INCREMENT, %s INT NOT NULL, note VARCHAR(40), "+
					"PRIMARY KEY (%s), CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s))",
				child, q(u.counter), q(u.fk), q(u.counter), q(u.foreignKey), q(u.fk), parent, q(u.pk)),
		}
	}
	sequence := s.table(u.schema, u.counter)
	function := schema + "." + oddSyncTriggerFunction
	return []string{
		"CREATE SCHEMA " + schema,
		"CREATE SEQUENCE " + sequence,
		fmt.Sprintf(
			"CREATE TABLE %s (%s integer NOT NULL, %s varchar(40) NOT NULL, "+
				"%s integer GENERATED BY DEFAULT AS IDENTITY, %s integer NOT NULL DEFAULT nextval(%s), "+
				"CONSTRAINT %s PRIMARY KEY (%s), CONSTRAINT %s CHECK (%s > 0))",
			parent, q(u.pk), q(u.other), q(u.counter), q(u.fk), oddSyncText(sequence),
			q(u.parent+" pk"), q(u.pk), q(u.check), q(u.pk)),
		fmt.Sprintf("ALTER SEQUENCE %s OWNED BY %s.%s", sequence, parent, q(u.fk)),
		fmt.Sprintf("CREATE INDEX %s ON %s (%s)", q(u.index), parent, q(u.other)),
		fmt.Sprintf(
			"CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fn$ BEGIN RETURN NEW; END $fn$",
			function),
		fmt.Sprintf(
			"CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()",
			q(u.trigger), parent, function),
		fmt.Sprintf(
			"CREATE TABLE %s (%s integer NOT NULL, %s integer NOT NULL, note varchar(40), "+
				"CONSTRAINT %s PRIMARY KEY (%s), CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s))",
			child, q(u.pk), q(u.fk), q(u.child+" pk"), q(u.pk), q(u.foreignKey), q(u.fk), parent, q(u.pk)),
	}
}

func oddSyncExec(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), statement)
	require.NoError(t, err, statement)
}

// createSource creates a unit in the source and fills it: the parents 1..oddSyncParents and three
// children for each.
func (s *oddSyncServers) createSource(t *testing.T, u oddSyncUnit) {
	t.Helper()
	q := s.quote
	for _, statement := range s.unitStatements(u) {
		oddSyncExec(t, s.source, statement)
	}
	parents := make([]string, 0, oddSyncParents)
	for pk := 1; pk <= oddSyncParents; pk++ {
		parents = append(parents, fmt.Sprintf("(%d, %s)", pk, oddSyncText(fmt.Sprintf("row-%d", pk))))
	}
	oddSyncExec(t, s.source, fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES %s",
		s.table(u.schema, u.parent), q(u.pk), q(u.other), strings.Join(parents, ", ")))
	children := make([]string, 0, oddSyncChildren)
	for pk := 1; pk <= oddSyncChildren; pk++ {
		children = append(children, fmt.Sprintf("(%d, %d, %s)",
			pk, (pk-1)%oddSyncParents+1, oddSyncText(fmt.Sprintf("note-%d", pk))))
	}
	oddSyncExec(t, s.source, fmt.Sprintf("INSERT INTO %s (%s, %s, note) VALUES %s",
		s.table(u.schema, u.child), q(s.childKey(u)), q(u.fk), strings.Join(children, ", ")))
}

func (s *oddSyncServers) drop(db *sql.DB, u oddSyncUnit) error {
	statement := "DROP SCHEMA IF EXISTS " + s.quote(u.schema) + " CASCADE"
	if s.mysql {
		statement = "DROP DATABASE IF EXISTS " + s.quote(u.schema)
	}
	_, err := db.ExecContext(context.Background(), statement)
	return err
}

// oddSyncTexts runs a query and gives its rows, every column as text.
func oddSyncTexts(t *testing.T, db *sql.DB, query string) [][]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query)
	require.NoError(t, err, query)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	out := [][]string{}
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		require.NoError(t, rows.Scan(targets...), query)
		row := make([]string, len(columns))
		for i, value := range values {
			row[i] = value.String
		}
		out = append(out, row)
	}
	require.NoError(t, rows.Err(), query)
	return out
}

// rows reads the given columns of a table in the order of its key, the first column, under an
// optional condition.
func (s *oddSyncServers) rows(t *testing.T, db *sql.DB, schema, table string, columns []string, where string) [][]string {
	t.Helper()
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = s.quote(column)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(quoted, ", "), s.table(schema, table))
	if where != "" {
		query += " WHERE " + where
	}
	return oddSyncTexts(t, db, query+" ORDER BY "+quoted[0])
}

const (
	oddSyncPgUserSchemas    = `n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'`
	oddSyncMysqlSystemNames = `('mysql', 'information_schema', 'performance_schema', 'sys')`
)

// The schemas, tables, sequences, triggers and routines of a database (of a server on MySQL and
// MariaDB), read from the catalog by queries of this test: kind, schema, table of a trigger, name.
var (
	oddSyncPostgresObjects = []string{
		`SELECT 'schema', n.nspname, '', '' FROM pg_namespace n WHERE ` + oddSyncPgUserSchemas,
		`SELECT CASE c.relkind WHEN 'S' THEN 'sequence' ELSE 'table' END, n.nspname, '', c.relname
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S') AND ` + oddSyncPgUserSchemas,
		`SELECT 'trigger', n.nspname, c.relname, t.tgname
			FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE NOT t.tgisinternal`,
		`SELECT 'routine', n.nspname, '', p.proname
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE ` + oddSyncPgUserSchemas,
	}
	oddSyncMysqlObjects = []string{
		`SELECT 'schema', SCHEMA_NAME, '', '' FROM information_schema.SCHEMATA
			WHERE SCHEMA_NAME NOT IN ` + oddSyncMysqlSystemNames,
		`SELECT 'table', TABLE_SCHEMA, '', TABLE_NAME FROM information_schema.TABLES
			WHERE TABLE_SCHEMA NOT IN ` + oddSyncMysqlSystemNames,
		`SELECT 'trigger', TRIGGER_SCHEMA, EVENT_OBJECT_TABLE, TRIGGER_NAME FROM information_schema.TRIGGERS
			WHERE TRIGGER_SCHEMA NOT IN ` + oddSyncMysqlSystemNames,
		`SELECT 'routine', ROUTINE_SCHEMA, '', ROUTINE_NAME FROM information_schema.ROUTINES
			WHERE ROUTINE_SCHEMA NOT IN ` + oddSyncMysqlSystemNames,
	}
)

func oddSyncObject(kind, schema, table, name string) string {
	return fmt.Sprintf("%s %q %q %q", kind, schema, table, name)
}

func (s *oddSyncServers) objects(t *testing.T, db *sql.DB) []string {
	t.Helper()
	queries := oddSyncPostgresObjects
	if s.mysql {
		queries = oddSyncMysqlObjects
	}
	lines := []string{}
	for _, query := range queries {
		for _, row := range oddSyncTexts(t, db, query) {
			lines = append(lines, oddSyncObject(row[0], row[1], row[2], row[3]))
		}
	}
	sort.Strings(lines)
	return lines
}

// unitObjects lists what a unit is made of, the way objects reads it.
func (s *oddSyncServers) unitObjects(u oddSyncUnit) []string {
	lines := []string{
		oddSyncObject("schema", u.schema, "", ""),
		oddSyncObject("table", u.schema, "", u.parent),
		oddSyncObject("table", u.schema, "", u.child),
		oddSyncObject("trigger", u.schema, u.parent, u.trigger),
	}
	if !s.mysql {
		lines = append(lines,
			oddSyncObject("sequence", u.schema, "", u.counter),
			// The sequence of an identity column is named after its table and its column.
			oddSyncObject("sequence", u.schema, "", u.parent+"_"+u.counter+"_seq"),
			oddSyncObject("routine", u.schema, "", oddSyncTriggerFunction),
		)
	}
	return lines
}

func (s *oddSyncServers) requireCanaryIntact(t *testing.T, db *sql.DB) {
	t.Helper()
	require.Equal(t, [][]string{{"1", "one"}, {"2", "two"}, {"3", "three"}},
		s.rows(t, db, s.canarySchema, oddSyncCanary, []string{"id", "label"}, ""),
		"the canary table does not hold its three rows")
}

// createJob creates the sync job of the units, run by the product's own engine: the destination
// is initialized and truncated, every column is passed through but the text column of each
// parent, which is given a generated first name, and the parents are kept up to oddSyncKept, with
// the children that refer to them.
//
// The engine writes its rows by a statement of its own, or, under a conflict policy, by the
// statement of the query builder: skipConflicts asks for the policy that changes nothing in a
// destination just emptied.
func (s *oddSyncServers) createJob(
	t *testing.T,
	ctx context.Context,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	accountId, name string,
	units []oddSyncUnit,
	skipConflicts bool,
) *mgmtv1alpha1.Job {
	t.Helper()
	passthrough := &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	}}
	firstName := &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{}},
	}}
	mappings := []*mgmtv1alpha1.JobMapping{}
	for _, u := range units {
		for _, column := range s.parentColumns(u) {
			transformer := passthrough
			if column == u.other {
				transformer = firstName
			}
			mappings = append(mappings, &mgmtv1alpha1.JobMapping{
				Schema: u.schema, Table: u.parent, Column: column, Transformer: transformer,
			})
		}
		for _, column := range s.childColumns(u) {
			mappings = append(mappings, &mgmtv1alpha1.JobMapping{
				Schema: u.schema, Table: u.child, Column: column, Transformer: passthrough,
			})
		}
	}

	source := &mgmtv1alpha1.JobSourceOptions{}
	destination := &mgmtv1alpha1.JobDestinationOptions{}
	if s.mysql {
		schemas := []*mgmtv1alpha1.MysqlSourceSchemaOption{}
		for _, u := range units {
			where := s.keptParents(u)
			schemas = append(schemas, &mgmtv1alpha1.MysqlSourceSchemaOption{
				Schema: u.schema,
				Tables: []*mgmtv1alpha1.MysqlSourceTableOption{{Table: u.parent, WhereClause: &where}},
			})
		}
		source.Config = &mgmtv1alpha1.JobSourceOptions_Mysql{Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
			ConnectionId:                  s.sourceConn.GetId(),
			Schemas:                       schemas,
			SubsetByForeignKeyConstraints: true,
		}}
		onConflict := &mgmtv1alpha1.MysqlOnConflictConfig{}
		if skipConflicts {
			onConflict.Strategy = &mgmtv1alpha1.MysqlOnConflictConfig_Nothing{}
		}
		destination.Config = &mgmtv1alpha1.JobDestinationOptions_MysqlOptions{
			MysqlOptions: &mgmtv1alpha1.MysqlDestinationConnectionOptions{
				InitTableSchema: true,
				TruncateTable:   &mgmtv1alpha1.MysqlTruncateTableConfig{TruncateBeforeInsert: true},
				OnConflict:      onConflict,
			},
		}
	} else {
		schemas := []*mgmtv1alpha1.PostgresSourceSchemaOption{}
		for _, u := range units {
			where := s.keptParents(u)
			schemas = append(schemas, &mgmtv1alpha1.PostgresSourceSchemaOption{
				Schema: u.schema,
				Tables: []*mgmtv1alpha1.PostgresSourceTableOption{{Table: u.parent, WhereClause: &where}},
			})
		}
		source.Config = &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			ConnectionId:                  s.sourceConn.GetId(),
			Schemas:                       schemas,
			SubsetByForeignKeyConstraints: true,
			NewColumnAdditionStrategy:     &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy{},
		}}
		onConflict := &mgmtv1alpha1.PostgresOnConflictConfig{}
		if skipConflicts {
			onConflict.Strategy = &mgmtv1alpha1.PostgresOnConflictConfig_Nothing{}
		}
		destination.Config = &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
			PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{
				InitTableSchema: true,
				TruncateTable: &mgmtv1alpha1.PostgresTruncateTableConfig{
					TruncateBeforeInsert: true,
					Cascade:              true,
				},
				OnConflict: onConflict,
			},
		}
	}

	husonymApi.MockTemporalForCreateJob("test-odd-names-sync")
	job, err := husonymApi.OSSUnauthenticatedLicensedClients.Jobs().CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   name,
		Source:    &mgmtv1alpha1.JobSource{Options: source},
		Destinations: []*mgmtv1alpha1.CreateJobDestination{
			{ConnectionId: s.destConn.GetId(), Options: destination},
		},
		Mappings: mappings,
		// The engine the job names runs it, whatever the default of the deployment.
		WorkflowOptions: &mgmtv1alpha1.WorkflowOptions{Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR},
	}))
	require.NoError(t, err)
	return job.Msg.GetJob()
}

// keptParents is the condition of the job on a parent table: it names the key column, quoted the
// way the engine does.
func (s *oddSyncServers) keptParents(u oddSyncUnit) string {
	return fmt.Sprintf("%s <= %d", s.quote(u.pk), oddSyncKept)
}

// requireSynced fails unless the destination holds the kept parents and their children, and they
// alone, with the values of the source in every column but the transformed one, which differs.
func (s *oddSyncServers) requireSynced(t *testing.T, u oddSyncUnit) {
	t.Helper()
	columns := s.parentColumns(u)
	want := s.rows(t, s.source, u.schema, u.parent, columns, s.keptParents(u))
	require.Len(t, want, oddSyncKept)
	got := s.rows(t, s.target, u.schema, u.parent, columns, "")
	require.Len(t, got, oddSyncKept, "parents of %q.%q", u.schema, u.parent)
	for i := range want {
		for c, column := range columns {
			if column == u.other {
				require.NotEmpty(t, got[i][c], "column %q of %q.%q", column, u.schema, u.parent)
				require.NotEqual(t, want[i][c], got[i][c], "column %q of %q.%q was not transformed", column, u.schema, u.parent)
				continue
			}
			require.Equal(t, want[i][c], got[i][c], "column %q of %q.%q", column, u.schema, u.parent)
		}
	}

	children := fmt.Sprintf("%s <= %d", s.quote(u.fk), oddSyncKept)
	wantChildren := s.rows(t, s.source, u.schema, u.child, s.childColumns(u), children)
	require.Len(t, wantChildren, oddSyncKept*oddSyncChildren/oddSyncParents)
	require.Equal(t, wantChildren, s.rows(t, s.target, u.schema, u.child, s.childColumns(u), ""),
		"children of %q.%q", u.schema, u.child)
}

// A sync run of the product's own engine copies tables whose schema, name, columns, index,
// sequence, constraints and trigger hold quote characters: it creates them in the destination,
// empties them, reads the source under a condition that names such a column and under the foreign
// key, and writes the rows, a second time as well. The destination then holds the tables of the
// job and its canary, and nothing else.
//
// The engine copies PostgreSQL and MySQL sources: SQL Server is not run here.
func Test_OddNames_Sync(t *testing.T) {
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

	run := func(t *testing.T, engine string, servers *oddSyncServers) {
		for _, db := range []*sql.DB{servers.source, servers.target} {
			oddSyncExec(t, db, fmt.Sprintf("CREATE TABLE %s (id INT NOT NULL PRIMARY KEY, label VARCHAR(20) NOT NULL)",
				servers.table(servers.canarySchema, oddSyncCanary)))
			oddSyncExec(t, db, fmt.Sprintf("INSERT INTO %s (id, label) VALUES (1, 'one'), (2, 'two'), (3, 'three')",
				servers.table(servers.canarySchema, oddSyncCanary)))
		}
		for rotation := range oddSyncRotations {
			t.Run(fmt.Sprintf("rotation %d", rotation+1), func(t *testing.T) {
				units := oddSyncRotation(rotation)
				atFirst := servers.objects(t, servers.target)
				t.Cleanup(func() {
					for _, u := range units {
						require.NoError(t, servers.drop(servers.source, u))
						require.NoError(t, servers.drop(servers.target, u))
					}
				})
				for _, u := range units {
					servers.createSource(t, u)
				}
				name := fmt.Sprintf("odd-names-%s-%d", engine, rotation+1)
				plain := servers.createJob(t, ctx, husonymApi, accountId, name, units, false)
				skipping := servers.createJob(t, ctx, husonymApi, accountId, name+"-skip-conflicts", units, true)

				want := append([]string{}, atFirst...)
				for _, u := range units {
					want = append(want, servers.unitObjects(u)...)
				}
				sort.Strings(want)

				// The second run finds the tables of the first, with their rows, and so does the
				// run of the job that writes under a conflict policy.
				for attempt, job := range []*mgmtv1alpha1.Job{plain, plain, skipping} {
					testworkflow := NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
					testworkflow.RequireActivitiesCompletedSuccessfully(t)
					testworkflow.ExecuteTestDataSyncWorkflow(job.GetId())
					require.True(t, testworkflow.TestEnv.IsWorkflowCompleted(), "run %d", attempt+1)
					require.NoError(t, testworkflow.TestEnv.GetWorkflowError(), "run %d", attempt+1)

					for _, u := range units {
						servers.requireSynced(t, u)
					}
					require.Equal(t, want, servers.objects(t, servers.target),
						"the schemas, tables, sequences, triggers and routines of the destination")
					servers.requireCanaryIntact(t, servers.target)
					servers.requireCanaryIntact(t, servers.source)
				}
			})
		}
	}

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		containers, err := tcpostgres.NewPostgresTestSyncContainer(ctx, []tcpostgres.Option{}, []tcpostgres.Option{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, containers.TearDown(context.Background())) })
		source, err := sql.Open(sqlmanager_shared.PostgresDriver, containers.Source.URL)
		require.NoError(t, err)
		t.Cleanup(func() { _ = source.Close() })
		target, err := sql.Open(sqlmanager_shared.PostgresDriver, containers.Target.URL)
		require.NoError(t, err)
		t.Cleanup(func() { _ = target.Close() })
		run(t, "postgres", &oddSyncServers{
			canarySchema: "public",
			source:       source,
			target:       target,
			sourceConn: tchusonymapi.CreatePostgresConnection(
				ctx, t, connclient, accountId, "odd-names-postgres-source", containers.Source.URL),
			destConn: tchusonymapi.CreatePostgresConnection(
				ctx, t, connclient, accountId, "odd-names-postgres-dest", containers.Target.URL),
		})
	})

	for engine, options := range map[string][]tcmysql.Option{
		"mysql":   nil,
		"mariadb": {tcmysql.WithImage("mariadb:11.4.13")},
	} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			containers, err := tcmysql.NewMysqlTestSyncContainer(ctx, options, options)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, containers.TearDown(context.Background())) })
			run(t, engine, &oddSyncServers{
				mysql:        true,
				canarySchema: "testdb",
				source:       containers.Source.DB,
				target:       containers.Target.DB,
				sourceConn: tchusonymapi.CreateMysqlConnection(
					ctx, t, connclient, accountId, "odd-names-"+engine+"-source", containers.Source.URL),
				destConn: tchusonymapi.CreateMysqlConnection(
					ctx, t, connclient, accountId, "odd-names-"+engine+"-dest", containers.Target.URL),
			})
		})
	}
}
