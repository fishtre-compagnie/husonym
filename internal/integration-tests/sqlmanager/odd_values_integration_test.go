package sqlmanager

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	schemamanager "github.com/fishtre-compagnie/husonym/internal/schema-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// oddValues are string values a statement must write as one literal: an apostrophe inside the
// value and at its end, a backslash inside the value, before an apostrophe and at its end, and
// a line break.
var oddValues = map[string]string{
	"apostrophe": "o'clock",
	"ending":     "quarter past'",
	"backslash":  `back\slash`,
	"escaped":    `it\'s`,
	"trailing":   `tail\`,
	"line_break": "new\nline",
}

const (
	oddValuesSchema = "odd_values"
	oddValuesTable  = "defaults"
	oddValuesEnum   = "moment"
)

// valueLiteral writes a text literal of this test. On MySQL and MariaDB a value that holds a
// backslash is written by its bytes, which reads the same under every sql_mode.
func (f *sampleFixture) valueLiteral(value string) string {
	if f.engine.family == familyMysql && strings.Contains(value, `\`) {
		return "_utf8mb4 0x" + hex.EncodeToString([]byte(value))
	}
	return quoteText(value)
}

// computedEscaped is what the default of the column computed_escaped gives on MySQL and MariaDB:
// an expression whose texts hold a backslash before an apostrophe, and a backslash.
const computedEscaped = `it\'s and back\slash`

// oddValuesStatements gives the statements that create the schema of the odd values: a table
// whose columns have each value as their default, the default of an ordinary value, of a date
// and of a number given as strings, and a default that is an expression. On MySQL and MariaDB
// columns are an ENUM and a SET whose default is one of the values, and one more default is an
// expression whose texts hold backslashes; on PostgreSQL the labels of an enum type are the
// values, and two columns of that type have one as their default.
func (d *oddDatabase) oddValuesStatements() []string {
	q, lit := d.f.quote, d.f.valueLiteral
	table := d.table(oddValuesSchema, oddValuesTable)
	columns := []string{"id INT NOT NULL PRIMARY KEY"}
	for _, name := range sortedKeys(oddValues) {
		columns = append(columns, fmt.Sprintf("%s VARCHAR(40) NOT NULL DEFAULT %s", q(name), lit(oddValues[name])))
	}
	columns = append(columns,
		"plain VARCHAR(40) DEFAULT 'plain value_1'",
		"fixed CHAR(20) DEFAULT "+lit(oddValues["apostrophe"]),
		"fixed_escaped CHAR(20) DEFAULT "+lit(oddValues["escaped"]),
		"started DATE DEFAULT '2020-01-02'",
		"amount INT NOT NULL DEFAULT 5",
	)
	if d.f.engine.family == familyMysql {
		// The member list of an ENUM or a SET takes the bytes of a value without a character
		// set before them.
		labels := []string{"'plain'"}
		for _, name := range sortedKeys(oddValues) {
			labels = append(labels, strings.TrimPrefix(lit(oddValues[name]), "_utf8mb4 "))
		}
		members := "(" + strings.Join(labels, ", ") + ")"
		columns = append(columns,
			"seen DATETIME DEFAULT '2020-01-02 03:04:05'",
			fmt.Sprintf("choice ENUM%s DEFAULT %s", members, lit(oddValues["apostrophe"])),
			fmt.Sprintf("choices SET%s DEFAULT %s", members, lit(oddValues["ending"])),
			fmt.Sprintf("choice_escaped ENUM%s DEFAULT %s", members, lit(oddValues["escaped"])),
			fmt.Sprintf("choices_trailing SET%s DEFAULT %s", members, lit(oddValues["trailing"])),
			fmt.Sprintf("computed VARCHAR(40) DEFAULT (CONCAT(%s, ' sharp'))", lit(oddValues["apostrophe"])),
			// The texts of this expression are written for a session that reads a backslash in a
			// text as an escape, the default of both servers.
			`computed_escaped VARCHAR(40) DEFAULT (CONCAT('it\\''s', ' and ', 'back\\slash'))`,
		)
		return []string{
			"CREATE DATABASE " + q(oddValuesSchema),
			fmt.Sprintf("CREATE TABLE %s (%s)", table, strings.Join(columns, ", ")),
		}
	}
	enum := d.table(oddValuesSchema, oddValuesEnum)
	labels := []string{"'plain'"}
	for _, name := range sortedKeys(oddValues) {
		labels = append(labels, lit(oddValues[name]))
	}
	columns = append(columns,
		"seen TIMESTAMP DEFAULT '2020-01-02 03:04:05'",
		fmt.Sprintf("choice %s DEFAULT %s", enum, lit(oddValues["apostrophe"])),
		fmt.Sprintf("other_choice %s DEFAULT %s", enum, lit(oddValues["backslash"])),
		fmt.Sprintf("computed VARCHAR(40) DEFAULT (%s || ' sharp')", lit(oddValues["apostrophe"])),
	)
	return []string{
		"CREATE SCHEMA " + q(oddValuesSchema),
		fmt.Sprintf("CREATE TYPE %s AS ENUM (%s)", enum, strings.Join(labels, ", ")),
		fmt.Sprintf("CREATE TABLE %s (%s)", table, strings.Join(columns, ", ")),
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// dropOddValues removes the schema of the odd values at the end of the test.
func (d *oddDatabase) dropOddValues(t *testing.T) {
	t.Helper()
	drop := "DROP DATABASE IF EXISTS " + d.f.quote(oddValuesSchema)
	if d.f.engine.family == familyPostgres {
		drop = "DROP SCHEMA IF EXISTS " + d.f.quote(oddValuesSchema) + " CASCADE"
	}
	t.Cleanup(func() {
		_, err := d.db.ExecContext(context.Background(), drop)
		require.NoError(t, err)
	})
}

// enumLabels lists the labels of the enum types of the schema of the odd values, in their order.
func (d *oddDatabase) enumLabels(t *testing.T) [][]string {
	t.Helper()
	return d.queryTexts(t, `SELECT t.typname, e.enumlabel
		FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = '`+oddValuesSchema+`' ORDER BY t.typname, e.enumsortorder`)
}

// rowOfDefaults inserts a row that leaves every column but the key to its default, and gives it.
func (d *oddDatabase) rowOfDefaults(t *testing.T) map[string]string {
	t.Helper()
	table := d.table(oddValuesSchema, oddValuesTable)
	d.exec(t, fmt.Sprintf("INSERT INTO %s (id) VALUES (1)", table))
	rows, err := d.db.QueryContext(context.Background(), "SELECT * FROM "+table)
	require.NoError(t, err)
	columns, err := rows.Columns()
	require.NoError(t, err)
	require.NoError(t, rows.Close())
	values := d.queryTexts(t, "SELECT * FROM "+table)
	require.Len(t, values, 1)
	row := map[string]string{}
	for i, column := range columns {
		row[column] = values[0][i]
	}
	return row
}

// listedDefaults gives the defaults of the columns of the table of the odd values the way the
// product lists the columns of a database, by column name.
func (d *oddDatabase) listedDefaults(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	conn, err := d.f.sqlmanager.NewSqlConnection(ctx, connectionmanager.NewUniqueSession(), d.connection,
		testutil.GetTestLogger(t))
	require.NoError(t, err)
	defer conn.Db().Close()
	columns, err := conn.Db().GetDatabaseSchema(ctx)
	require.NoError(t, err)
	defaults := map[string]string{}
	for _, column := range columns {
		if column.TableSchema == oddValuesSchema && column.TableName == oddValuesTable {
			defaults[column.ColumnName] = column.ColumnDefault
		}
	}
	return defaults
}

// The schema initialization of a sync run gives the columns of the destination the defaults of
// the source, and its enum types the labels of the source, whatever characters the values hold:
// a row inserted with its defaults holds the same values on both sides.
func Test_OddValues_SchemaInit(t *testing.T) {
	forEachEngine(t, []string{familyPostgres, familyMysql}, func(t *testing.T, f *sampleFixture) {
		source := f.oddSource(t)
		destination := f.oddDestination(t)
		source.dropOddValues(t)
		destination.dropOddValues(t)
		atFirst := destination.objects(t)
		for _, statement := range source.oddValuesStatements() {
			source.exec(t, statement)
		}

		for run := 1; run <= 2; run++ {
			f.withSchemaManager(t, source, destination, f.oddDestinationOptions(true, false, false),
				func(manager schemamanager.SchemaManagerService) {
					failed, err := manager.InitializeSchema(context.Background(),
						map[string]struct{}{oddValuesSchema + "." + oddValuesTable: {}})
					require.NoError(t, err, "run %d", run)
					require.Empty(t, failed, "run %d", run)
				})
		}

		want := source.describe(t, []string{oddValuesSchema})
		require.NotEmpty(t, want)
		require.Equal(t, want, destination.describe(t, []string{oddValuesSchema}),
			"the destination does not hold the columns and the defaults of the source")
		if f.engine.family == familyPostgres {
			labels := source.enumLabels(t)
			require.Len(t, labels, len(oddValues)+1)
			require.Equal(t, labels, destination.enumLabels(t))
		}

		expected := append([]string{}, atFirst...)
		expected = append(expected,
			objectLine("schema", oddValuesSchema, "", ""),
			objectLine("table", oddValuesSchema, "", oddValuesTable))
		sort.Strings(expected)
		require.Equal(t, expected, destination.objects(t), "the schemas and tables of the destination")

		row := source.rowOfDefaults(t)
		for name, value := range oddValues {
			require.Equal(t, value, row[name], "the default of column %q of the source", name)
		}
		require.Equal(t, oddValues["apostrophe"], row["choice"])
		require.Equal(t, oddValues["apostrophe"]+" sharp", row["computed"])
		require.Equal(t, oddValues["escaped"], strings.TrimRight(row["fixed_escaped"], " "))
		if f.engine.family == familyMysql {
			require.Equal(t, oddValues["escaped"], row["choice_escaped"])
			require.Equal(t, oddValues["trailing"], row["choices_trailing"])
			require.Equal(t, computedEscaped, row["computed_escaped"])

			// The columns of a database are listed with the same defaults.
			listed := source.listedDefaults(t)
			if f.engine.name == "mariadb" {
				// MariaDB gives every default as an expression, which is listed as it is.
				asRead := source.queryTexts(t, `SELECT COLUMN_NAME, COLUMN_DEFAULT FROM information_schema.COLUMNS
					WHERE TABLE_SCHEMA = '`+oddValuesSchema+`' AND COLUMN_DEFAULT IS NOT NULL`)
				require.Len(t, asRead, len(listed)-1, "every column but the key has a default")
				for _, column := range asRead {
					require.Equal(t, column[1], listed[column[0]], "the default of column %q, as listed", column[0])
				}
			} else {
				require.Equal(t, oddValues["escaped"], listed["escaped"])
				require.Equal(t, oddValues["trailing"], listed["choices_trailing"])
				require.Equal(t, `concat(_utf8mb4'it\\\'s',_utf8mb4' and ',_utf8mb4'back\\slash')`, listed["computed_escaped"])
			}
		}
		require.Equal(t, row, destination.rowOfDefaults(t), "a row inserted with its defaults")

		destination.requireCanaryIntact(t)
		source.requireCanaryIntact(t)
	})
}
