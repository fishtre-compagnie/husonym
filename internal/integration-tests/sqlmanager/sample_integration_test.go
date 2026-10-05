package sqlmanager

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	querybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/query-builder"
	"github.com/stretchr/testify/require"
)

var (
	allFamilies   = []string{familyPostgres, familyMysql, familyMssql}
	mysqlFamily   = []string{familyMysql}
	postgresOnly  = []string{familyPostgres}
	sqlServerOnly = []string{familyMssql}
)

// maxCoverageDraws bounds the draws requireCoversTheTable makes.
const maxCoverageDraws = 20

// requireCoversTheTable draws samples of numRows rows until rows from both halves of the rank
// range have been seen, and fails when maxCoverageDraws draws have not shown both.
//
// A sample is cut from a few pages (PostgreSQL, SQL Server) or from ten key ranges (MySQL), so a
// single draw can fall in one half by chance: with about seven pages it does so about once in
// sixty. Twenty independent draws all missing a half is below one in 10^30. A sample that reads
// the first rows of the table only never shows the upper half, and fails on every run.
func requireCoversTheTable(t *testing.T, f *sampleFixture, table string, numRows uint, lastRank int64) {
	t.Helper()
	var low, high bool
	for draw := 0; draw < maxCoverageDraws && !(low && high); draw++ {
		rows := f.mustSample(t, table, numRows)
		require.Len(t, rows, int(numRows))
		requireDistinct(t, rows, "rank")
		for _, row := range rows {
			if intColumn(t, row, "rank") > lastRank/2 {
				high = true
			} else {
				low = true
			}
		}
	}
	require.True(t, low, "no sampled row in the lower half of the table")
	require.True(t, high, "no sampled row in the upper half of the table")
}

func Test_SampleData_CoversTheTable(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.bigTable(t)
		requireCoversTheTable(t, f, table, 100, bigRows)
	})
}

func Test_SampleData_SmallTable(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.filledTable(t, "small", 50)
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
	})
}

func Test_SampleData_EmptyTable(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.filledTable(t, "empty", 0)
		require.Empty(t, f.mustSample(t, table, 20))
	})
}

// A table never analyzed has no row count the planner can be asked for.
func Test_SampleData_NeverAnalyzed(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "never_analyzed", func() {
			f.createTable(t, "never_analyzed")
			f.load(t, "never_analyzed", 1, 5000, 1)
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
	})
}

// A view has no primary key to slice on. The table check of PostgreSQL and SQL Server does not
// list views, so only the MySQL family reaches the sample.
func Test_SampleData_View(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		big := f.bigTable(t)
		view := f.dataset(t, "big_view", func() {
			f.exec(t, fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s", f.qualified("big_view"), f.qualified(big)))
		})
		rows := f.mustSample(t, view, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
	})
}

// The parent of a partitioned table has no row count of its own.
func Test_SampleData_PartitionedTable(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "parted", func() {
			parent := f.qualified("parted")
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL) "+
					"PARTITION BY RANGE (id)", parent, f.quote("rank")))
			f.exec(t, fmt.Sprintf("CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (1) TO (2001)",
				f.qualified("parted_1"), parent))
			f.exec(t, fmt.Sprintf("CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (2001) TO (4001)",
				f.qualified("parted_2"), parent))
			f.load(t, "parted", 1, 4000, 1)
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
	})
}

// Only the keys 1..100 and 10^9..10^9+100 exist, so most key ranges are empty: the spread query
// may come back short and the window then answers. A second table has keys spread evenly over a
// wide span, which the key ranges do cover.
func Test_SampleData_KeyWithGaps(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		t.Run("two clusters", func(t *testing.T) {
			table := f.dataset(t, "gaps", func() {
				f.createTable(t, "gaps")
				f.load(t, "gaps", 1, 100, 1)
				f.exec(t, fmt.Sprintf(
					"INSERT INTO %s (id, %s, label) SELECT id + 1000000000, %s + 1000, label FROM %s",
					f.qualified("gaps"), f.quote("rank"), f.quote("rank"), f.qualified("gaps")))
			})
			rows := f.mustSample(t, table, 20)
			require.Len(t, rows, 20)
			requireDistinct(t, rows, "rank")
		})

		t.Run("evenly spaced", func(t *testing.T) {
			table := f.dataset(t, "spaced", func() {
				f.createTable(t, "spaced")
				f.load(t, "spaced", 1, bigRows, 1000)
				f.analyze(t, "spaced")
			})
			requireCoversTheTable(t, f, table, 100, bigRows)
		})
	})
}

// A key of two columns cannot be cut into ranges: the window is read.
func Test_SampleData_CompositeKey(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "composite", func() {
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (a BIGINT NOT NULL, b BIGINT NOT NULL, label VARCHAR(40) NOT NULL, PRIMARY KEY (a, b))",
				f.qualified("composite")))
			f.exec(t, fmt.Sprintf(
				"INSERT INTO %s (a, b, label) SELECT id, id * 2, label FROM %s WHERE id <= 3000",
				f.qualified("composite"), f.qualified(f.bigTable(t))))
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
	})
}

// A key above the largest signed 64-bit integer does not fit the key ranges: the window is read.
func Test_SampleData_UnsignedKeyAboveInt64(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "unsigned_key", func() {
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, label VARCHAR(40) NOT NULL)",
				f.qualified("unsigned_key")))
			f.exec(t, fmt.Sprintf(
				"INSERT INTO %s (id, label) SELECT 18446744073709551615 - id, label FROM %s WHERE id <= 3000",
				f.qualified("unsigned_key"), f.qualified(f.bigTable(t))))
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
	})
}

// A name with a space and capitals is quoted the same way by the estimate, the sample and the
// check that the table exists. The table is large enough for the spread query to apply.
func Test_SampleData_OddTableName(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.filledTable(t, "Order Lines", 3000)
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
	})
}

// The ten slices of a MySQL sample do not overlap, so 100 rows drawn from them are 100 different
// rows, and they come from the whole key span: the first 1000 rows alone could not hold one with
// a rank above 1000.
func Test_SampleData_SlicesAreDisjoint(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		rows := f.mustSample(t, f.bigTable(t), 100)
		require.Len(t, rows, 100)
		requireDistinct(t, rows, "rank")
		var beyondWindow bool
		for _, row := range rows {
			beyondWindow = beyondWindow || intColumn(t, row, "rank") > querybuilder.SampleWindowSize
		}
		require.True(t, beyondWindow, "every sampled row is within the first 1000 rows")
	})
}

// A table of 5000 rows asks for 20 percent of its pages, a whole number the query writes as an
// integer. The query the builder produces runs, and the sample is complete.
func Test_SampleData_IntegerShare(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.filledTable(t, "five_thousand", 5000)

		query, ok, err := querybuilder.BuildTableSampleQuery(
			sqlmanager_shared.GoquPostgresDriver, sqlmanager_shared.BuildTable(f.schema, table), 5000, 100)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, query, "SYSTEM (20)")
		rows, err := f.db.QueryContext(context.Background(), query)
		require.NoError(t, err)
		require.NoError(t, rows.Close())

		require.Len(t, f.mustSample(t, table, 100), 100)
	})
}

// The table was analyzed at 2000 rows and holds 200 000 now. The share of pages follows the
// current size of the table, so the sample still comes from the whole table, and the pages read
// stay bounded.
func Test_SampleData_StaleStatistics(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "stale", func() {
			f.createTable(t, "stale")
			f.load(t, "stale", 1, 2000, 1)
			f.analyze(t, "stale")
			f.load(t, "stale", 2001, bigRows, 1)
		})
		var reltuples float64
		require.NoError(t, f.db.QueryRowContext(context.Background(),
			"SELECT reltuples FROM pg_class WHERE oid = to_regclass($1)", f.qualified(table)).Scan(&reltuples))
		if reltuples > 10_000 {
			t.Skip("the table was analyzed again in the background")
		}

		before := f.seqTupleReads(t, table)
		require.Len(t, f.mustSample(t, table, 100), 100)
		after := f.settledSeqTupleReads(t, before, table)
		if after == before {
			t.Log("the statistics view did not report the reads: rows read not observable")
		} else {
			t.Logf("rows read by one sample: %d", after-before)
			// The sample is cut at 4000 rows; a little more than that is one page.
			require.LessOrEqual(t, after-before, int64(5000))
		}

		requireCoversTheTable(t, f, table, 100, bigRows)
	})
}

func (f *sampleFixture) seqTupleReads(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.QueryRowContext(context.Background(),
		"SELECT COALESCE(seq_tup_read, 0) FROM pg_stat_user_tables WHERE relname = $1", table).Scan(&n))
	return n
}

// settledSeqTupleReads waits for the statistics of the session that ran the sample to be
// published, then reads them once they have stopped moving.
func (f *sampleFixture) settledSeqTupleReads(t *testing.T, before int64, table string) int64 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && f.seqTupleReads(t, table) == before {
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	return f.seqTupleReads(t, table)
}

// SQL Server takes its sample from pages with TABLESAMPLE; the draw covers the table.
func Test_SampleData_SqlServerSampledQuery(t *testing.T) {
	forEachEngine(t, sqlServerOnly, func(t *testing.T, f *sampleFixture) {
		table := f.bigTable(t)
		query, ok, err := querybuilder.BuildTableSampleQuery(
			sqlmanager_shared.MssqlDriver, sqlmanager_shared.BuildTable(f.schema, table), 0, 100)
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, strings.Contains(query, "TABLESAMPLE"))
		rows, err := f.db.QueryContext(context.Background(), query)
		require.NoError(t, err)
		var n int
		for rows.Next() {
			n++
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		// A sample of pages can come back empty on its own; the service then reads the window.
		require.LessOrEqual(t, n, 100)

		requireCoversTheTable(t, f, table, 100, bigRows)
	})
}
