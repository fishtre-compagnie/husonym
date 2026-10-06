package sqlmanager

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

const (
	// sparseRows is the size of the table whose text column is filled in 2 percent of its rows.
	sparseRows = 5000
	// filledEvery is the spacing of the rows that hold a value: one row in fifty.
	filledEvery = 50
)

// sampleColumn draws the filled values of a column through the service.
func (f *sampleFixture) sampleColumn(t *testing.T, table, column string, numRows uint) ([]map[string]any, error) {
	t.Helper()
	service := connectiondata.NewSQLConnectionDataService(
		testutil.GetTestLogger(t),
		&sqlconnect.SqlOpenConnector{},
		f.sqlmanager,
		f.connection,
	)
	stream := &collectingStream{}
	if err := service.SampleColumn(context.Background(), stream, f.schema, table, column, numRows); err != nil {
		return nil, err
	}
	return stream.rows(t), nil
}

// sparseTable builds, once, a table of sparseRows rows. Its column nick holds a value 'row-N'
// in one row out of filledEvery, the empty string in the row after each of them, and NULL in
// the others.
func (f *sampleFixture) sparseTable(t *testing.T) string {
	t.Helper()
	return f.dataset(t, "sparse_text", func() {
		f.exec(t, fmt.Sprintf(
			"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL, nick VARCHAR(40) NULL)",
			f.qualified("sparse_text"), f.quote("rank")))
		f.load(t, "sparse_text", 1, sparseRows, 1)
		rank := f.quote("rank")
		f.exec(t, fmt.Sprintf("UPDATE %s SET nick = label WHERE %s %% %d = 0",
			f.qualified("sparse_text"), rank, filledEvery))
		f.exec(t, fmt.Sprintf("UPDATE %s SET nick = '' WHERE %s %% %d = 1",
			f.qualified("sparse_text"), rank, filledEvery))
		f.analyze(t, "sparse_text")
	})
}

// sampledText gives the text of a value a driver read, whichever Go type it used.
func sampledText(t *testing.T, value any) string {
	t.Helper()
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		require.FailNow(t, "unexpected type of a text value", "%T", value)
		return ""
	}
}

// A column filled in 2 percent of the rows still gives the number of values asked for, and
// none of them is NULL or empty. Each row holds the column name as its only key.
//
// The 100 filled values are drawn by the query that reads the filled values of a window, or by
// the one spread across the table; either one gives the values that are there.
func Test_SampleColumn_ASparseColumnGivesItsFilledValues(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.sparseTable(t)

		rows, err := f.sampleColumn(t, table, "nick", 50)

		require.NoError(t, err)
		require.Len(t, rows, 50)
		for _, row := range rows {
			require.Len(t, row, 1)
			value := sampledText(t, row["nick"])
			require.True(t, strings.HasPrefix(value, "row-"), "unexpected value %q", value)
		}
	})
}

// Fewer filled values than asked for are all returned: 100 values exist and 500 are asked.
func Test_SampleColumn_ReturnsAllTheFilledValuesWhenFewerThanAsked(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.sparseTable(t)

		rows, err := f.sampleColumn(t, table, "nick", 500)

		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), sparseRows/filledEvery)
		for _, row := range rows {
			require.NotEmpty(t, sampledText(t, row["nick"]))
		}
	})
}

// A column the table does not have is refused with connect.CodeNotFound, before any row of
// the table is read.
func Test_SampleColumn_ColumnAbsent(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.sparseTable(t)

		rows, err := f.sampleColumn(t, table, "missing", 50)

		require.Error(t, err)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
		require.Empty(t, rows)
	})
}

// oddColumnNames gives each family a column name with a space and the quote characters the
// statements of that engine are written with. SQL Server is given both of its own.
var oddColumnNames = map[string]string{
	familyPostgres: `Nick "Name`,
	familyMysql:    "Nick `Name",
	familyMssql:    `Nick "Na]me`,
}

// A column name that holds the quote character of the engine is written as one identifier, and
// the values of that column come back.
func Test_SampleColumn_ColumnNameHoldingAQuoteCharacter(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		column := oddColumnNames[f.engine.family]
		table := f.dataset(t, "odd_column", func() {
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL, %s VARCHAR(40) NULL)",
				f.qualified("odd_column"), f.quote("rank"), f.quote(column)))
			f.load(t, "odd_column", 1, 300, 1)
			f.exec(t, fmt.Sprintf("UPDATE %s SET %s = label WHERE %s %% 3 = 0",
				f.qualified("odd_column"), f.quote(column), f.quote("rank")))
			f.analyze(t, "odd_column")
		})

		rows, err := f.sampleColumn(t, table, column, 20)

		require.NoError(t, err)
		require.Len(t, rows, 20)
		for _, row := range rows {
			require.Len(t, row, 1)
			require.True(t, strings.HasPrefix(sampledText(t, row[column]), "row-"))
		}
	})
}

// The legacy text and ntext types of SQL Server take no comparison with the empty string: their
// values are read, filtered on NULL only.
func Test_SampleColumn_SqlServerLegacyTextTypes(t *testing.T) {
	forEachEngine(t, sqlServerOnly, func(t *testing.T, f *sampleFixture) {
		for _, dataType := range []string{"TEXT", "NTEXT"} {
			name := "legacy_" + strings.ToLower(dataType)
			table := f.dataset(t, name, func() {
				f.exec(t, fmt.Sprintf(
					"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL, body %s NULL)",
					f.qualified(name), f.quote("rank"), dataType))
				f.load(t, name, 1, 300, 1)
				f.exec(t, fmt.Sprintf("UPDATE %s SET body = label WHERE %s %% 30 = 0", f.qualified(name), f.quote("rank")))
				f.analyze(t, name)
			})

			rows, err := f.sampleColumn(t, table, "body", 5)

			require.NoError(t, err, dataType)
			require.Len(t, rows, 5, dataType)
			for _, row := range rows {
				require.True(t, strings.HasPrefix(sampledText(t, row["body"]), "row-"), dataType)
			}
		}
	})
}

// requireColumnCoversTheTable draws samples of the values of an integer column that holds the
// rank of its row, until values from the lowest and from the highest quarter of the rank range
// have been seen, and fails when maxCoverageDraws draws have not shown both. It is
// requireCoversTheTable for a column sample, with quarters: a sample that keeps the rows of the
// first pages it reads stops about the middle of the table, which halves would not tell.
func requireColumnCoversTheTable(t *testing.T, f *sampleFixture, table, column string, numRows uint, lastRank int64) {
	t.Helper()
	var low, high bool
	for draw := 0; draw < maxCoverageDraws && !(low && high); draw++ {
		rows, err := f.sampleColumn(t, table, column, numRows)
		require.NoError(t, err)
		require.Len(t, rows, int(numRows))
		for _, row := range rows {
			rank := intColumn(t, row, column)
			low = low || rank <= lastRank/4
			high = high || rank > lastRank/4*3
		}
	}
	require.True(t, low, "no sampled value in the lowest quarter of the table")
	require.True(t, high, "no sampled value in the highest quarter of the table")
}

// A column filled in every row of a large table of narrow rows gives values from across the
// table: the rows of the pages drawn are shuffled before any cap is applied to them.
func Test_SampleColumn_ADenseColumnCoversTheTable(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		requireColumnCoversTheTable(t, f, f.bigTable(t), "rank", 100, bigRows)
	})
}

const (
	// lateRows is the size of the table whose column is filled beyond the rows a window reads.
	lateRows = 25000
	// lateFrom is the first rank that holds a value: past the 20000 rows of the window.
	lateFrom = 20001
)

// lateTable builds, once, a table of lateRows rows whose column nick is NULL in the first 20000
// rows and filled in the others. It is not analyzed in the background either.
func (f *sampleFixture) lateTable(t *testing.T, name string, withStatistics bool) string {
	t.Helper()
	return f.dataset(t, name, func() {
		options := ""
		if f.engine.family == familyPostgres && !withStatistics {
			options = " WITH (autovacuum_enabled = false)"
		}
		f.exec(t, fmt.Sprintf(
			"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL, nick VARCHAR(40) NULL)%s",
			f.qualified(name), f.quote("rank"), options))
		f.load(t, name, 1, lateRows, 1)
		f.exec(t, fmt.Sprintf("UPDATE %s SET nick = label WHERE %s >= %d", f.qualified(name), f.quote("rank"), lateFrom))
		if withStatistics {
			f.analyze(t, name)
		}
	})
}

// The window of a column reads the first 20000 rows of the table and filters those: values that
// lie beyond them are not looked for, and the call ends without an error and without a value.
// The table is read by the window query alone: on PostgreSQL it was never analyzed, on MySQL
// and MariaDB it is read through a view, which has no key to slice on.
func Test_SampleColumn_TheWindowReadsABoundedNumberOfRows(t *testing.T) {
	forEachEngine(t, []string{familyPostgres, familyMysql}, func(t *testing.T, f *sampleFixture) {
		table := f.lateTable(t, "late_values", f.engine.family != familyPostgres)
		if f.engine.family == familyMysql {
			table = f.dataset(t, "late_view", func() {
				f.exec(t, fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s", f.qualified("late_view"), f.qualified("late_values")))
			})
		}

		rows, err := f.sampleColumn(t, table, "nick", 50)

		require.NoError(t, err)
		require.Empty(t, rows)
	})
}
