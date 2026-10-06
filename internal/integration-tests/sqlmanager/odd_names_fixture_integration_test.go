package sqlmanager

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// oddNames are names every engine accepts once they are quoted, and that a statement written
// without quoting them, or quoting them without doubling the quote character, reads as something
// else: the quote character of each engine, an apostrophe, a backslash, a semicolon, two dollar
// signs, a space, a line break, capitals and a reserved word.
var oddNames = []string{
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

// oddUnit is a schema (a database on MySQL and MariaDB) with a parent table and a child table
// linked by a foreign key. Every object of it bears one of the odd names, and no two objects of a
// unit bear the same one.
//
// The parent holds the key and a text column. On PostgreSQL and SQL Server it also holds an
// identity column and a column fed by a sequence; the sequence is named like the identity column
// and the column it feeds like the foreign key column of the child: a column and a sequence, or
// two columns of two tables, do not share a namespace. MySQL has no sequence and the product
// does not recreate those of MariaDB: the identity column is the key of the child there.
type oddUnit struct {
	offset     int
	schema     string
	parent     string
	child      string
	pk         string
	fk         string
	other      string
	index      string
	counter    string
	check      string
	trigger    string
	foreignKey string
}

// oddUnitAt gives the unit whose objects take the names from the given offset on: the eleven
// units give every name to every kind of object once.
func oddUnitAt(offset int) oddUnit {
	name := func(slot int) string { return oddNames[(slot+offset)%len(oddNames)] }
	return oddUnit{
		offset:     offset,
		schema:     name(0),
		parent:     name(1),
		child:      name(2),
		pk:         name(3),
		fk:         name(4),
		other:      name(5),
		index:      name(6),
		counter:    name(7),
		check:      name(8),
		trigger:    name(9),
		foreignKey: name(10),
	}
}

// oddRotations is the number of groups the units are run in.
const oddRotations = 3

// oddRotation gives the units of a rotation: every third unit from the rotation's own on, so a
// name is at a different place in each unit of a rotation, and in no two rotations at the same.
func oddRotation(rotation int) []oddUnit {
	var units []oddUnit
	for offset := rotation; offset < len(oddNames); offset += oddRotations {
		units = append(units, oddUnitAt(offset))
	}
	return units
}

func allOddUnits() []oddUnit {
	var units []oddUnit
	for rotation := range oddRotations {
		units = append(units, oddRotation(rotation)...)
	}
	return units
}

// forEachOddRotation runs fn as a subtest for each rotation.
func forEachOddRotation(t *testing.T, fn func(t *testing.T, units []oddUnit)) {
	t.Helper()
	for rotation := range oddRotations {
		t.Run(fmt.Sprintf("rotation %d", rotation+1), func(t *testing.T) {
			fn(t, oddRotation(rotation))
		})
	}
}

const (
	// oddSourceParentRows is the number of rows of the parent table of the first unit of each
	// rotation in the source, and oddSourceFewParentRows of the others: every row of a table is
	// streamed.
	oddSourceParentRows    = 3000
	oddSourceFewParentRows = 300
	// oddSmallParentRows is the number of rows of a parent table a test fills by itself.
	oddSmallParentRows = 20
	// oddChildRows is the number of rows of each child table: three for each of the first twenty
	// parents.
	oddChildRows = 60
)

// oddDatabase is a database the odd-named units live in: the source every test reads, or a
// destination a test writes to. On MySQL and MariaDB it is a whole server, since a unit is a
// database there.
type oddDatabase struct {
	f          *sampleFixture
	db         *sql.DB
	connection *mgmtv1alpha1.Connection
}

func (d *oddDatabase) exec(t *testing.T, statement string) {
	t.Helper()
	_, err := d.db.ExecContext(context.Background(), statement)
	require.NoError(t, err, statement)
}

// quoteText writes a text literal: the apostrophe is doubled. None of the texts of these tests
// holds a backslash.
func quoteText(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

func (d *oddDatabase) table(schema, table string) string {
	return d.f.quote(schema) + "." + d.f.quote(table)
}

// sourceParentRows is the number of rows of the parent of the unit in the source.
func (u oddUnit) sourceParentRows() int {
	if u.offset < oddRotations {
		return oddSourceParentRows
	}
	return oddSourceFewParentRows
}

// ticket names the column of the parent a sequence feeds, on the engines that have one.
func (u oddUnit) ticket() string { return u.fk }

// childKey names the key of the child. On MySQL and MariaDB it is the identity column of the
// unit: the product creates an AUTO_INCREMENT column as the primary key of its table, so the
// identity column is one there, and the parent has none.
func (u oddUnit) childKey(family string) string {
	if family == familyMysql {
		return u.counter
	}
	return u.pk
}

// mysqlCheckedColumn names the column the CHECK constraint of a MySQL or MariaDB unit is over:
// the key, or the text column when the name of the key holds a backslash or a line break. A
// constraint over a column so named has a test of its own,
// Test_OddNames_SchemaInit_CheckOverAColumn.
func (u oddUnit) mysqlCheckedColumn() string {
	if strings.ContainsAny(u.pk, "\\\n") {
		return u.other
	}
	return u.pk
}

// mysqlTriggerColumn names the column the trigger of a MySQL or MariaDB unit assigns to itself:
// the text column, or the key when the name of the text column holds a backtick. MySQL 8.4
// itself reports the statement of a trigger that names such a column of NEW with the name
// altered (the column we-backtick-ird, written with its backtick doubled, is read back from
// information_schema.TRIGGERS as wwe-backtick-irdd), so no copy of that trigger can be compared
// with it; the trigger keeps its odd name.
func (u oddUnit) mysqlTriggerColumn() string {
	if strings.Contains(u.other, "`") {
		return u.pk
	}
	return u.other
}

// parentHasCounter tells whether the parent of a unit holds the identity column.
func (d *oddDatabase) parentHasCounter() bool { return d.f.engine.family != familyMysql }

// primaryKeyName names the primary key constraint of a table.
func primaryKeyName(table string) string { return table + " pk" }

// defaultName names the default constraint of the sequence-fed column on SQL Server.
func defaultName(table string) string { return table + " df" }

// unitStatements gives the statements that create a unit, written by this test with the quoting
// rule of the engine.
func (d *oddDatabase) unitStatements(u oddUnit) []string {
	q := d.f.quote
	schema := q(u.schema)
	parent, child := d.table(u.schema, u.parent), d.table(u.schema, u.child)
	switch d.f.engine.family {
	case familyMysql:
		return []string{
			"CREATE DATABASE " + schema,
			fmt.Sprintf(
				"CREATE TABLE %s (%s INT NOT NULL, %s VARCHAR(40) NOT NULL, "+
					"PRIMARY KEY (%s), KEY %s (%s), CONSTRAINT %s CHECK (%s IS NOT NULL))",
				parent, q(u.pk), q(u.other),
				q(u.pk), q(u.index), q(u.other), q(u.check), q(u.mysqlCheckedColumn())),
			fmt.Sprintf(
				"CREATE TRIGGER %s.%s BEFORE INSERT ON %s FOR EACH ROW SET NEW.%s = NEW.%s",
				schema, q(u.trigger), parent, q(u.mysqlTriggerColumn()), q(u.mysqlTriggerColumn())),
			fmt.Sprintf(
				"CREATE TABLE %s (%s INT NOT NULL AUTO_INCREMENT, %s INT NOT NULL, note VARCHAR(40), "+
					"PRIMARY KEY (%s), CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s))",
				child, q(u.childKey(familyMysql)), q(u.fk),
				q(u.childKey(familyMysql)), q(u.foreignKey), q(u.fk), parent, q(u.pk)),
		}
	case familyMssql:
		sequence := d.table(u.schema, u.counter)
		return []string{
			"CREATE SCHEMA " + schema,
			fmt.Sprintf("CREATE SEQUENCE %s AS int START WITH 1 INCREMENT BY 1", sequence),
			fmt.Sprintf(
				"CREATE TABLE %s (%s int NOT NULL, %s nvarchar(40) NOT NULL, %s int IDENTITY(1,1) NOT NULL, "+
					"%s int NOT NULL CONSTRAINT %s DEFAULT (NEXT VALUE FOR %s), "+
					"CONSTRAINT %s PRIMARY KEY (%s), CONSTRAINT %s CHECK (%s > 0))",
				parent, q(u.pk), q(u.other), q(u.counter),
				q(u.ticket()), q(defaultName(u.parent)), sequence,
				q(primaryKeyName(u.parent)), q(u.pk), q(u.check), q(u.pk)),
			fmt.Sprintf("CREATE INDEX %s ON %s (%s)", q(u.index), parent, q(u.other)),
			fmt.Sprintf(
				"CREATE TRIGGER %s.%s ON %s AFTER INSERT AS BEGIN SET NOCOUNT ON; END",
				schema, q(u.trigger), parent),
			fmt.Sprintf(
				"CREATE TABLE %s (%s int NOT NULL, %s int NOT NULL, note nvarchar(40), "+
					"CONSTRAINT %s PRIMARY KEY (%s), CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s))",
				child, q(u.pk), q(u.fk),
				q(primaryKeyName(u.child)), q(u.pk), q(u.foreignKey), q(u.fk), parent, q(u.pk)),
		}
	default:
		sequence := d.table(u.schema, u.counter)
		function := schema + "." + oddTriggerFunction
		return []string{
			"CREATE SCHEMA " + schema,
			"CREATE SEQUENCE " + sequence,
			fmt.Sprintf(
				"CREATE TABLE %s (%s integer NOT NULL, %s varchar(40) NOT NULL, "+
					"%s integer GENERATED BY DEFAULT AS IDENTITY, %s integer NOT NULL DEFAULT nextval(%s), "+
					"CONSTRAINT %s PRIMARY KEY (%s), CONSTRAINT %s CHECK (%s > 0))",
				parent, q(u.pk), q(u.other),
				q(u.counter), q(u.ticket()), quoteText(sequence),
				q(primaryKeyName(u.parent)), q(u.pk), q(u.check), q(u.pk)),
			fmt.Sprintf("ALTER SEQUENCE %s OWNED BY %s.%s", sequence, parent, q(u.ticket())),
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
				child, q(u.pk), q(u.fk),
				q(primaryKeyName(u.child)), q(u.pk), q(u.foreignKey), q(u.fk), parent, q(u.pk)),
		}
	}
}

// oddTriggerFunction is the function the trigger of a PostgreSQL unit calls.
const oddTriggerFunction = "touch"

// create creates the units, empty.
func (d *oddDatabase) create(t *testing.T, units []oddUnit) {
	t.Helper()
	for _, u := range units {
		for _, statement := range d.unitStatements(u) {
			d.exec(t, statement)
		}
	}
}

// oddLabel is the text of the parent row whose key is pk.
func oddLabel(pk int) string { return fmt.Sprintf("row-%d", pk) }

// oddParentOf is the parent a child row refers to: one of the first twenty.
func oddParentOf(childPk int) int { return (childPk-1)%oddSmallParentRows + 1 }

// load fills a unit: the parent rows 1..parents, whose identity column and sequence-fed column
// take the values the server gives, and the sixty child rows, whose key is given: an identity
// column takes a value that is written to it on MySQL and MariaDB.
func (d *oddDatabase) load(t *testing.T, u oddUnit, parents int) {
	t.Helper()
	q := d.f.quote
	const chunk = 500
	for from := 1; from <= parents; from += chunk {
		values := make([]string, 0, chunk)
		for pk := from; pk < from+chunk && pk <= parents; pk++ {
			values = append(values, fmt.Sprintf("(%d, %s)", pk, quoteText(oddLabel(pk))))
		}
		d.exec(t, fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES %s",
			d.table(u.schema, u.parent), q(u.pk), q(u.other), strings.Join(values, ", ")))
	}
	values := make([]string, 0, oddChildRows)
	for pk := 1; pk <= oddChildRows; pk++ {
		values = append(values, fmt.Sprintf("(%d, %d, %s)", pk, oddParentOf(pk), quoteText(fmt.Sprintf("note-%d", pk))))
	}
	d.exec(t, fmt.Sprintf("INSERT INTO %s (%s, %s, note) VALUES %s",
		d.table(u.schema, u.child), q(u.childKey(d.f.engine.family)), q(u.fk), strings.Join(values, ", ")))
}

// rowCount counts the rows of a table.
func (d *oddDatabase) rowCount(t *testing.T, schema, table string) int {
	t.Helper()
	var n int
	require.NoError(t, d.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+d.table(schema, table)).Scan(&n))
	return n
}

// The canary is a table of three rows that no test touches: it holds the same rows after a test
// as before it.
const canaryTable = "canary"

var canaryRows = []string{"1 one", "2 two", "3 three"}

func (d *oddDatabase) createCanary(t *testing.T) {
	t.Helper()
	d.exec(t, fmt.Sprintf("CREATE TABLE %s (id INT NOT NULL PRIMARY KEY, label VARCHAR(20) NOT NULL)",
		d.table(d.f.schema, canaryTable)))
	d.exec(t, fmt.Sprintf("INSERT INTO %s (id, label) VALUES (1, 'one'), (2, 'two'), (3, 'three')",
		d.table(d.f.schema, canaryTable)))
}

// requireCanaryIntact fails when the canary does not hold its three rows, and they alone.
func (d *oddDatabase) requireCanaryIntact(t *testing.T) {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(),
		fmt.Sprintf("SELECT id, label FROM %s ORDER BY id", d.table(d.f.schema, canaryTable)))
	require.NoError(t, err)
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var id int
		var label string
		require.NoError(t, rows.Scan(&id, &label))
		got = append(got, fmt.Sprintf("%d %s", id, label))
	}
	require.NoError(t, rows.Err())
	require.Equal(t, canaryRows, got, "the canary table does not hold its three rows")
}

// oddSourceDatabase names the database of the source on the engines where a unit is a schema.
const oddSourceDatabase = "odd_source"

// oddSource gives the source: every unit, filled, and the canary. It is built once per server
// and only read afterwards.
func (f *sampleFixture) oddSource(t *testing.T) *oddDatabase {
	t.Helper()
	source := &oddDatabase{f: f, db: f.db, connection: f.connection}
	if f.engine.family != familyMysql {
		f.dataset(t, "odd source database", func() {
			f.exec(t, "CREATE DATABASE "+f.quote(oddSourceDatabase))
		})
		source = f.openDatabase(t, oddSourceDatabase)
	}
	f.dataset(t, "odd source", func() {
		source.createCanary(t)
		for _, u := range allOddUnits() {
			source.create(t, []oddUnit{u})
			source.load(t, u, u.sourceParentRows())
		}
	})
	return source
}

// openDatabase opens another database of the server of the fixture, for the length of the test.
func (f *sampleFixture) openDatabase(t *testing.T, database string) *oddDatabase {
	t.Helper()
	var driver string
	var connection *mgmtv1alpha1.Connection
	var address string
	switch f.engine.family {
	case familyMssql:
		u, err := url.Parse(f.connection.GetConnectionConfig().GetMssqlConfig().GetUrl())
		require.NoError(t, err)
		query := u.Query()
		query.Set("database", database)
		u.RawQuery = query.Encode()
		address, driver = u.String(), sqlmanager_shared.MssqlDriver
		connection = &mgmtv1alpha1.Connection{
			Id: uuid.NewString(),
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_MssqlConfig{
				MssqlConfig: &mgmtv1alpha1.MssqlConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.MssqlConnectionConfig_Url{Url: address},
				},
			}},
		}
	case familyPostgres:
		u, err := url.Parse(f.connection.GetConnectionConfig().GetPgConfig().GetUrl())
		require.NoError(t, err)
		u.Path = "/" + database
		address, driver = u.String(), sqlmanager_shared.PostgresDriver
		connection = pgConnection(address)
	default:
		require.FailNow(t, "a unit is a database on this engine: the server is the database")
	}
	db, err := sql.Open(driver, address)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &oddDatabase{f: f, db: db, connection: connection}
}

// destinationCount numbers the destination databases, so each test has its own.
var destinationCount atomic.Int64

// oddDestination gives an empty destination that holds the canary alone, and removes at the end
// of the test what the test put in it. On PostgreSQL and SQL Server it is a database of its own
// on the server of the source; on MySQL and MariaDB, where the units are databases that bear the
// names of the source's, it is a second server.
func (f *sampleFixture) oddDestination(t *testing.T) *oddDatabase {
	t.Helper()
	if f.engine.family == familyMysql {
		server := fixtureFor(t, sampleEngine{
			name:   f.engine.name + " destination",
			family: familyMysql,
			start: func(ctx context.Context) (*sampleFixture, error) {
				if f.engine.name == "mariadb" {
					return startMysqlFixture(ctx, tcmysql.WithImage("mariadb:11.4"))
				}
				return startMysqlFixture(ctx)
			},
		})
		destination := &oddDatabase{f: server, db: server.db, connection: server.connection}
		server.dataset(t, "canary", func() { destination.createCanary(t) })
		t.Cleanup(func() {
			for _, name := range oddNames {
				_, _ = server.db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+server.quote(name))
			}
		})
		return destination
	}

	name := fmt.Sprintf("odd_destination_%d", destinationCount.Add(1))
	f.exec(t, "CREATE DATABASE "+f.quote(name))
	t.Cleanup(func() {
		drop := "DROP DATABASE " + f.quote(name) + " WITH (FORCE)"
		if f.engine.family == familyMssql {
			drop = fmt.Sprintf("ALTER DATABASE %[1]s SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE %[1]s", f.quote(name))
		}
		_, err := f.db.ExecContext(context.Background(), drop)
		require.NoError(t, err)
	})
	destination := f.openDatabase(t, name)
	destination.createCanary(t)
	return destination
}

// tableKeys gives the tables of the units the way a job names them: schema.table.
func tableKeys(units []oddUnit) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, u := range units {
		keys[u.schema+"."+u.parent] = struct{}{}
		keys[u.schema+"."+u.child] = struct{}{}
	}
	return keys
}

func schemasOf(units []oddUnit) []string {
	schemas := make([]string, 0, len(units))
	for _, u := range units {
		schemas = append(schemas, u.schema)
	}
	return schemas
}
