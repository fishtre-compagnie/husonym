package sqlmanager

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	schemamanager "github.com/fishtre-compagnie/husonym/internal/schema-manager"
	"github.com/stretchr/testify/require"
)

// oddValues are string values a statement must write as one literal: an apostrophe inside the
// value and at its end, a backslash, and a line break.
var oddValues = map[string]string{
	"apostrophe": "o'clock",
	"ending":     "quarter past'",
	"backslash":  `back\slash`,
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

// computedPart is the text the default that is an expression concatenates. MariaDB 11.4 reports
// an expression with the apostrophe of a text escaped by a backslash, which the product does not
// read back: the text is an ordinary one there.
func (f *sampleFixture) computedPart() string {
	if f.engine.name == "mariadb" {
		return "eight"
	}
	return oddValues["apostrophe"]
}

// oddValuesStatements gives the statements that create the schema of the odd values: a table
// whose columns have each value as their default, the default of an ordinary value, of a date
// and of a number given as strings, and a default that is an expression. On MySQL and MariaDB
// two columns are an ENUM and a SET whose default is one of the values; on PostgreSQL the labels
// of an enum type are the values, and two columns of that type have one as their default.
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
			fmt.Sprintf("computed VARCHAR(40) DEFAULT (CONCAT(%s, ' sharp'))", lit(d.f.computedPart())),
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
		fmt.Sprintf("computed VARCHAR(40) DEFAULT (%s || ' sharp')", lit(d.f.computedPart())),
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
		require.Equal(t, f.computedPart()+" sharp", row["computed"])
		require.Equal(t, row, destination.rowOfDefaults(t), "a row inserted with its defaults")

		destination.requireCanaryIntact(t)
		source.requireCanaryIntact(t)
	})
}
