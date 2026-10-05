package sqlmanager

import (
	"context"
	"fmt"
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

const (
	// spreadDraws is the number of samples Test_SampleData_DrawsFromManyPlaces draws.
	spreadDraws = 3
	// spreadBuckets is the number of ranges of 1000 consecutive ranks one of them must touch.
	spreadBuckets = 20
)

// A sample of 100 rows of the table of 200 000 narrow rows comes from many places of the table:
// one of spreadDraws samples holds rows of at least spreadBuckets ranges of 1000 consecutive
// ranks, out of 200.
//
// Why it does not fail by chance. A page holds 157 rows on PostgreSQL (1274 pages) and 235 on
// SQL Server (852 pages), so SampleMinPages pages are 3.92 and 5.87 percent of them. Every page
// is taken on its own with that probability, every row of a taken page is kept with the
// probability that leaves about 1000 of them, and 100 are drawn. Simulated ten million times
// with these figures, a draw touches 18 to 64 ranges on PostgreSQL and 19 to 64 on SQL Server,
// and fewer than 20 in 4 draws out of ten million on PostgreSQL and 1 on SQL Server. On the
// servers, 400 draws on PostgreSQL touched 29 to 55 ranges and 200 on SQL Server 30 to 53. The
// three draws are independent: all three under 20 is under 1e-18, and stays under one in a
// billion as long as the simulation is not wrong by more than a factor of a thousand for one
// draw.
//
// A sample cut from the pages that hold 1000 rows alone (6 pages on PostgreSQL, 4 on SQL Server)
// reaches 20 ranges in less than one draw in 200, in the same simulation.
func Test_SampleData_DrawsFromManyPlaces(t *testing.T) {
	forEachEngine(t, []string{familyPostgres, familyMssql}, func(t *testing.T, f *sampleFixture) {
		table := f.bigTable(t)
		best := 0
		for draw := 0; draw < spreadDraws && best < spreadBuckets; draw++ {
			rows := f.mustSample(t, table, 100)
			require.Len(t, rows, 100)
			requireDistinct(t, rows, "rank")
			best = max(best, len(rankBuckets(t, rows)))
		}
		require.GreaterOrEqual(t, best, spreadBuckets,
			"the widest of %d samples touches %d ranges of 1000 ranks", spreadDraws, best)
	})
}

// rankBuckets gives the ranges of 1000 consecutive ranks the rows of a sample fall in.
func rankBuckets(t *testing.T, rows []map[string]any) map[int64]bool {
	t.Helper()
	buckets := map[int64]bool{}
	for _, row := range rows {
		buckets[intColumn(t, row, "rank")/1000] = true
	}
	return buckets
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

// builtQueryTries bounds the times a query of the builder is run until it returns a row.
const builtQueryTries = 5

// requireBuiltQueryDraws runs a query of the builder until it returns rows, and checks that they
// are distinct rows and no more than limit.
func requireBuiltQueryDraws(t *testing.T, f *sampleFixture, query string, limit int) {
	t.Helper()
	var ranks []int64
	for try := 0; try < builtQueryTries && len(ranks) == 0; try++ {
		ranks = f.queryRanks(t, query)
	}
	require.NotEmpty(t, ranks, "the query returned no row in %d runs", builtQueryTries)
	require.LessOrEqual(t, len(ranks), limit)
	seen := map[int64]bool{}
	for _, rank := range ranks {
		require.False(t, seen[rank], "a row came twice")
		seen[rank] = true
	}
}

// A share of pages that is a whole number is written as an integer. The table of 5000 rows is on
// 32 pages: 20 percent of them, each taken on its own, are none at all with probability 0.8^32,
// 8 in 10 000, and five runs in a row with probability 3 in 10^16. The query the builder
// produces runs and returns distinct rows, no more than asked.
func Test_SampleData_IntegerShare(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.filledTable(t, "five_thousand", 5000)

		query, ok, err := querybuilder.BuildTableSampleQuery(
			sqlmanager_shared.GoquPostgresDriver, sqlmanager_shared.BuildTable(f.schema, table),
			querybuilder.TableSize{Rows: 5000, Pages: 250}, 100)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, query, "SYSTEM (20) LIMIT")
		requireBuiltQueryDraws(t, f, query, 100)

		rows := f.mustSample(t, table, 100)
		require.Len(t, rows, 100)
		requireDistinct(t, rows, "rank")
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
		require.Greater(t, after, before, "the statistics view did not report the rows read")
		t.Logf("rows read by one sample: %d", after-before)
		// The sample reads SampleMinPages pages of 157 rows on average, 7850 rows, each of the
		// 1274 pages being taken on its own: 110 pages or more, 17 270 rows, comes less than once
		// in 10^13 samples. A scan of the table reads 200 000 rows.
		require.LessOrEqual(t, after-before, int64(17_500))

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

// SQL Server draws a share of its pages with TABLESAMPLE and thins their rows one by one. The
// query the builder produces for the size of the table runs and returns distinct rows, no more
// than asked; it returns none only when no page is taken, e^-50 for a share of fifty pages.
func Test_SampleData_SqlServerSampledQuery(t *testing.T) {
	forEachEngine(t, sqlServerOnly, func(t *testing.T, f *sampleFixture) {
		table := f.bigTable(t)
		var size querybuilder.TableSize
		require.NoError(t, f.db.QueryRowContext(context.Background(),
			"SELECT SUM(row_count), SUM(in_row_data_page_count) FROM sys.dm_db_partition_stats "+
				"WHERE object_id = OBJECT_ID(@p1) AND index_id IN (0, 1)", f.qualified(table)).
			Scan(&size.Rows, &size.Pages))
		require.Equal(t, int64(bigRows), size.Rows)

		query, ok, err := querybuilder.BuildTableSampleQuery(
			sqlmanager_shared.MssqlDriver, sqlmanager_shared.BuildTable(f.schema, table), size, 100)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, query, "PERCENT) WHERE")
		requireBuiltQueryDraws(t, f, query, 100)

		requireCoversTheTable(t, f, table, 100, bigRows)
	})
}

// sampleCallsForSessions is the number of samples drawn while the sessions of the server are
// counted.
const sampleCallsForSessions = 30

// A sample leaves no session open on the server: after many samples the server holds as many
// sessions as before. The sessions are counted on a connection of the fixture that stays open, and
// the count is read again for a few seconds, since a server may report a closed session late.
func Test_SampleData_ClosesItsConnections(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		tables := []string{f.filledTable(t, "small", 50), f.filledTable(t, "Order Lines", 3000)}
		ctx := context.Background()
		counter, err := f.db.Conn(ctx)
		require.NoError(t, err)
		defer counter.Close()

		sessions := func() int {
			var n int
			require.NoError(t, counter.QueryRowContext(ctx, f.sessionCountQuery()).Scan(&n))
			return n
		}
		before := sessions()
		for i := range sampleCallsForSessions {
			require.Len(t, f.mustSample(t, tables[i%len(tables)], 20), 20)
		}
		require.Eventually(t, func() bool { return sessions() <= before }, 5*time.Second, 100*time.Millisecond,
			"%d sessions before the samples, %d after", before, sessions())
	})
}
