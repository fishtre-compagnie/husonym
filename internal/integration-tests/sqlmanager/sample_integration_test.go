package sqlmanager

import (
	"context"
	"database/sql"
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
// A sample is cut from the rows of about fifty pages, each page taken on its own (PostgreSQL, SQL
// Server), or from ten key ranges that follow one another over the whole key span (MySQL). On
// PostgreSQL and SQL Server one draw misses a half when none of its pages is there: with a share
// s of P pages, 2 * (1 - s)^(P/2), about 2 * e^-25 or 3 in 10^11 on the tables of these tests. On
// MySQL the ranges always lie in both halves, and 100 rows out of them all fall in one half less
// than once in 10^25. The draw is repeated all the same, so the test does not depend on these
// figures. A sample that reads the first rows of the table only never shows the upper half, and
// fails on every run.
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
// probability that leaves about 1000 of them, and 100 are drawn. SQL Server keeps every row and
// draws 100 among them all, which is the same draw: 100 rows taken evenly among rows kept evenly
// are 100 rows taken evenly among all. Simulated ten million times
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
		requireDrawsFromManyPlaces(t, f, f.bigTable(t))
	})
}

// requireDrawsFromManyPlaces draws up to spreadDraws samples of 100 rows and fails when none
// touches spreadBuckets ranges of 1000 consecutive ranks.
func requireDrawsFromManyPlaces(t *testing.T, f *sampleFixture, table string) {
	t.Helper()
	best := 0
	for draw := 0; draw < spreadDraws && best < spreadBuckets; draw++ {
		rows := f.mustSample(t, table, 100)
		require.Len(t, rows, 100)
		requireDistinct(t, rows, "rank")
		best = max(best, len(rankBuckets(t, rows)))
	}
	require.GreaterOrEqual(t, best, spreadBuckets,
		"the widest of %d samples touches %d ranges of 1000 ranks", spreadDraws, best)
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

// A table of 50 rows is within the window whichever query reads it: the two cannot be told
// apart here, and both draw among all its rows.
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

// A table never analyzed has no row count the planner can be asked for: its 5000 rows are read
// from the window, the first 1000 of them.
func Test_SampleData_NeverAnalyzed(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "never_analyzed", func() {
			// The table is not analyzed in the background either.
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL) "+
					"WITH (autovacuum_enabled = false)", f.qualified("never_analyzed"), f.quote("rank")))
			f.load(t, "never_analyzed", 1, 5000, 1)
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
		requireWithinWindow(t, rows, "rank")
	})
}

// A view has no primary key to slice on: the window is read, the first 1000 rows of the 200 000
// the view shows. The table check of PostgreSQL and SQL Server does not list views, so only the
// MySQL family reaches the sample.
func Test_SampleData_View(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		big := f.bigTable(t)
		view := f.dataset(t, "big_view", func() {
			f.exec(t, fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s", f.qualified("big_view"), f.qualified(big)))
		})
		rows := f.mustSample(t, view, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
		requireWithinWindow(t, rows, "rank")
	})
}

// partitionedTable builds, once, a table of count rows in equal range partitions on rank, so
// the rows of the first partition are the lowest ranks. No partition is analyzed, in the
// background or otherwise: the caller analyzes the ones it wants.
func (f *sampleFixture) partitionedTable(t *testing.T, table string, count, partitions int) string {
	t.Helper()
	return f.dataset(t, table, func() {
		parent := f.qualified(table)
		f.exec(t, fmt.Sprintf(
			"CREATE TABLE %s (id BIGINT NOT NULL, %s INT NOT NULL, label VARCHAR(40) NOT NULL, PRIMARY KEY (id, %s)) "+
				"PARTITION BY RANGE (%s)", parent, f.quote("rank"), f.quote("rank"), f.quote("rank")))
		width := count / partitions
		for p := range partitions {
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (%d) TO (%d) WITH (autovacuum_enabled = false)",
				f.qualified(partitionName(table, p)), parent, p*width+1, (p+1)*width+1))
		}
		f.load(t, table, 1, count, 1)
	})
}

// analyzedPartitionedTable is the table of 200 000 rows in four partitions, all analyzed.
func (f *sampleFixture) analyzedPartitionedTable(t *testing.T) string {
	t.Helper()
	const table = "parted"
	f.dataset(t, table+" analyzed", func() {
		f.partitionedTable(t, table, bigRows, 4)
		f.analyze(t, table)
	})
	return table
}

func partitionName(table string, partition int) string {
	return fmt.Sprintf("%s_%d", table, partition+1)
}

// analyzedRows gives the row count the planner holds for a table: none for a table never
// analyzed.
func (f *sampleFixture) analyzedRows(t *testing.T, table string) float64 {
	t.Helper()
	var reltuples float64
	require.NoError(t, f.db.QueryRowContext(context.Background(),
		"SELECT reltuples FROM pg_class WHERE oid = to_regclass($1)", f.qualified(table)).Scan(&reltuples))
	return max(reltuples, 0)
}

// The parent of a partitioned table has no page of its own: its size is the sum of its leaf
// partitions, and its sample comes from all of them.
//
// The table of 200 000 rows is in four partitions of 50 000 consecutive ranks, on as many pages
// together as the table of the other tests: the figures of requireCoversTheTable hold, a draw
// misses the two upper partitions about 3 times in 10^11. The window reads the first 1000 rows of
// the first partition and never shows a row of the upper half.
func Test_SampleData_PartitionedTable(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		t.Run("every partition analyzed", func(t *testing.T) {
			table := f.analyzedPartitionedTable(t)
			requireCoversTheTable(t, f, table, 100, bigRows)
			requireDrawsFromManyPlaces(t, f, table)
		})

		// The density of the three analyzed partitions stands for the fourth, whose pages are
		// counted: the share of pages is the same, and the sample reaches the fourth as well.
		t.Run("one partition never analyzed", func(t *testing.T) {
			const table = "parted_partly"
			f.dataset(t, table+" analyzed", func() {
				f.partitionedTable(t, table, bigRows, 4)
				for p := range 3 {
					f.analyze(t, partitionName(table, p))
				}
			})
			require.Positive(t, f.analyzedRows(t, partitionName(table, 0)))
			require.Zero(t, f.analyzedRows(t, partitionName(table, 3)))

			requireCoversTheTable(t, f, table, 100, bigRows)
			lastPartition := false
			for draw := 0; draw < maxCoverageDraws && !lastPartition; draw++ {
				for _, row := range f.mustSample(t, table, 100) {
					lastPartition = lastPartition || intColumn(t, row, "rank") > bigRows/4*3
				}
			}
			require.True(t, lastPartition, "no sampled row in the partition that was never analyzed")
		})

		// The pages are counted over every partition, analyzed or not: one analyzed partition of
		// 750 rows out of four gives the table 3000 rows, more than the window, and they are all
		// read. 20 rows drawn among them are all within the first 1000 with probability (1/3)^20,
		// 3 in 10^10.
		t.Run("one partition analyzed", func(t *testing.T) {
			const table = "parted_mostly_unanalyzed"
			f.dataset(t, table+" analyzed", func() {
				f.partitionedTable(t, table, 3000, 4)
				f.analyze(t, partitionName(table, 0))
			})
			require.Zero(t, f.analyzedRows(t, partitionName(table, 1)))

			rows := f.mustSample(t, table, 20)
			require.Len(t, rows, 20)
			requireDistinct(t, rows, "rank")
			require.True(t, hasRowBeyondWindow(t, rows), "every sampled row is within the first 1000 rows")
		})

		// Without an analyzed partition there is no density: the window is read, the first 1000
		// rows of the 5000.
		t.Run("no partition analyzed", func(t *testing.T) {
			table := f.partitionedTable(t, "parted_unanalyzed", 5000, 2)
			require.Zero(t, f.analyzedRows(t, partitionName(table, 0)))
			require.Zero(t, f.analyzedRows(t, partitionName(table, 1)))

			rows := f.mustSample(t, table, 20)
			require.Len(t, rows, 20)
			requireDistinct(t, rows, "rank")
			requireWithinWindow(t, rows, "rank")
		})

		// The catalog gives the size of the partitions to a login that may only read the table.
		// 20 rows drawn among 200 000 are all within the first 1000 with probability (1/200)^20.
		t.Run("login with select only", func(t *testing.T) {
			table := f.analyzedPartitionedTable(t)
			const login, password = "sample_reader", "sample-READER-1"
			f.dataset(t, "login "+login, func() {
				f.exec(t, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", login, password))
				f.exec(t, fmt.Sprintf("GRANT SELECT ON %s TO %s", f.qualified(table), login))
			})

			rows, err := f.sampleRowsAs(t, f.pgConnectionAs(t, login, password), table, 20)
			require.NoError(t, err)
			require.Len(t, rows, 20)
			requireDistinct(t, rows, "rank")
			require.True(t, hasRowBeyondWindow(t, rows), "every sampled row is within the first 1000 rows")
		})
	})
}

// A foreign table among the leaves of a partitioned table is scanned whole by a sampled scan of
// the parent, so such a table is read from the window: the first 1000 rows of the first
// partition. The foreign leaf points at a table of the same server and holds the last quarter of
// the ranks.
func Test_SampleData_PartitionedTableWithAForeignLeaf(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		const table, remote = "parted_foreign", "parted_foreign_remote"
		f.dataset(t, table, func() {
			f.exec(t, "CREATE EXTENSION IF NOT EXISTS postgres_fdw")
			f.exec(t, `DO $$ BEGIN EXECUTE format(
				'CREATE SERVER sample_loopback FOREIGN DATA WRAPPER postgres_fdw OPTIONS (host %L, port %L, dbname %L)',
				'localhost', current_setting('port'), current_database()); END $$`)
			f.exec(t, `DO $$ BEGIN EXECUTE format(
				'CREATE USER MAPPING FOR CURRENT_USER SERVER sample_loopback OPTIONS (user %L, password_required %L)',
				current_user, 'false'); END $$`)
			f.createTable(t, remote)
			f.load(t, remote, 150001, bigRows, 1)

			parent := f.qualified(table)
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL, %s INT NOT NULL, label VARCHAR(40) NOT NULL) PARTITION BY RANGE (%s)",
				parent, f.quote("rank"), f.quote("rank")))
			for p := range 3 {
				f.exec(t, fmt.Sprintf(
					"CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (%d) TO (%d) WITH (autovacuum_enabled = false)",
					f.qualified(partitionName(table, p)), parent, p*50000+1, (p+1)*50000+1))
			}
			f.exec(t, fmt.Sprintf(
				"CREATE FOREIGN TABLE %s PARTITION OF %s FOR VALUES FROM (150001) TO (200001) "+
					"SERVER sample_loopback OPTIONS (schema_name %s, table_name %s)",
				f.qualified(partitionName(table, 3)), parent, "'"+f.schema+"'", "'"+remote+"'"))
			f.load(t, table, 1, 150000, 1)
			for p := range 4 {
				f.analyze(t, partitionName(table, p))
			}
		})

		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
		requireWithinWindow(t, rows, "rank")
	})
}

// Only the keys 1..100 and 10^9..10^9+100 exist, so most key ranges are empty and the slices
// hold the rows of one range at most, the upper keys: too few, and the window is read. It draws
// among the 200 rows of the table: 20 of them all come from the same hundred 7 times in 10^7,
// and three draws in a row doing so 3 times in 10^19. A second table has keys spread evenly over
// a wide span, which the key ranges do cover.
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
			var lower, upper bool
			for draw := 0; draw < 3 && !(lower && upper); draw++ {
				rows := f.mustSample(t, table, 20)
				require.Len(t, rows, 20)
				requireDistinct(t, rows, "rank")
				for _, row := range rows {
					if intColumn(t, row, "rank") > 1000 {
						upper = true
					} else {
						lower = true
					}
				}
			}
			require.True(t, lower, "no sampled row among the lower keys")
			require.True(t, upper, "no sampled row among the upper keys")
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

// sparseDraws is the number of samples Test_SampleData_KeyRangesMostlyEmpty draws.
const sparseDraws = 3

// The keys 1..200 000 and one key at 2 000 000: nine of the ten key ranges hold no row or the
// last one alone, so the slices hold the 100 consecutive rows of the first range and little
// else. That is fewer than SampleSlicesMinRows: the window is read, and every sampled row is
// within the first 1000 rows of the table.
//
// The window draws among 1000 rows where the slices would among 100 consecutive ones: the ranks
// of a sample span more than SampleSliceRows. 20 rows drawn among 1000 span 100 ranks or fewer
// with probability 20 * 0.1^19 - 19 * 0.1^20, 2 in 10^18.
func Test_SampleData_KeyRangesMostlyEmpty(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "outlier", func() {
			f.createTable(t, "outlier")
			f.load(t, "outlier", 1, bigRows, 1)
			f.exec(t, fmt.Sprintf("INSERT INTO %s (id, %s, label) VALUES (2000000, %d, 'last')",
				f.qualified("outlier"), f.quote("rank"), bigRows+1))
			f.analyze(t, "outlier")
		})
		for range sparseDraws {
			rows := f.mustSample(t, table, 20)
			require.Len(t, rows, 20)
			requireDistinct(t, rows, "rank")
			requireWithinWindow(t, rows, "rank")
			lowest, highest := int64(bigRows), int64(0)
			for _, row := range rows {
				lowest = min(lowest, intColumn(t, row, "rank"))
				highest = max(highest, intColumn(t, row, "rank"))
			}
			require.Greater(t, highest-lowest, int64(querybuilder.SampleSliceRows),
				"the sampled rows are within %d consecutive ranks", querybuilder.SampleSliceRows)
		}
	})
}

// The rows of the slices are counted on the primary key: every slice is a range read of that
// key, and the count reads one entry of it for each row it counts, 1000 at most, whatever the
// size of the table. MySQL reports that the key alone answers; MariaDB does not report it for a
// primary key, which holds the rows.
func Test_SampleData_KeySlicesAreCountedOnTheKey(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		table := f.bigTable(t)
		ranges := make([]querybuilder.KeyRange, querybuilder.SampleSlices)
		for i := range ranges {
			from := int64(i) * bigRows / querybuilder.SampleSlices
			ranges[i] = querybuilder.KeyRange{From: from + 7, To: from + bigRows/querybuilder.SampleSlices - 1}
		}
		query, err := querybuilder.BuildKeySlicesCountQuery(
			sqlmanager_shared.MysqlDriver, sqlmanager_shared.BuildTable(f.schema, table), "id", ranges)
		require.NoError(t, err)

		// The entries read are counted by the session that reads them.
		ctx := context.Background()
		session, err := f.db.Conn(ctx)
		require.NoError(t, err)
		defer session.Close()
		before := keyEntriesRead(t, session)
		var count int64
		require.NoError(t, session.QueryRowContext(ctx, query).Scan(&count))
		read := keyEntriesRead(t, session) - before
		const most = querybuilder.SampleSlices * querybuilder.SampleSliceRows
		require.Equal(t, int64(most), count)
		t.Logf("key entries read by the count: %d", read)
		require.Positive(t, read, "the session did not report the entries read")
		require.LessOrEqual(t, read, int64(most))

		plan := f.explain(t, query)
		slices := 0
		for _, step := range plan {
			if step["table"] != table {
				continue
			}
			slices++
			require.Equal(t, "PRIMARY", step["key"], "%v", step)
			require.Equal(t, "range", step["type"], "%v", step)
			if f.engine.name == "mysql" {
				require.Contains(t, step["Extra"], "Using index", "%v", step)
			}
		}
		require.Equal(t, querybuilder.SampleSlices, slices)
	})
}

// keyEntriesRead gives the number of index entries a session has read so far: the reads that
// position on a key and the reads of the entry that follows.
func keyEntriesRead(t *testing.T, session *sql.Conn) int64 {
	t.Helper()
	rows, err := session.QueryContext(context.Background(),
		"SHOW SESSION STATUS WHERE Variable_name IN ('Handler_read_key', 'Handler_read_next')")
	require.NoError(t, err)
	defer rows.Close()
	var total int64
	for rows.Next() {
		var name string
		var value int64
		require.NoError(t, rows.Scan(&name, &value))
		total += value
	}
	require.NoError(t, rows.Err())
	return total
}

// explain gives the plan of a query, one map per step, and logs it.
func (f *sampleFixture) explain(t *testing.T, query string) []map[string]string {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(), "EXPLAIN "+query)
	require.NoError(t, err)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	var plan []map[string]string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		require.NoError(t, rows.Scan(targets...))
		step := map[string]string{}
		line := make([]string, len(columns))
		for i, column := range columns {
			step[column] = values[i].String
			line[i] = column + "=" + values[i].String
		}
		t.Log(strings.Join(line, " | "))
		plan = append(plan, step)
	}
	require.NoError(t, rows.Err())
	return plan
}

// A key of two columns cannot be cut into ranges: the window is read, the first 1000 rows of the
// 3000 in key order.
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
		requireDistinct(t, rows, "a")
		requireWithinWindow(t, rows, "a")
	})
}

// A key above the largest signed 64-bit integer does not fit the key ranges: the window is read,
// the first 1000 rows of the 3000 in key order. The rank of a row is its place in that order.
func Test_SampleData_UnsignedKeyAboveInt64(t *testing.T) {
	forEachEngine(t, mysqlFamily, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "unsigned_key", func() {
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL)",
				f.qualified("unsigned_key"), f.quote("rank")))
			f.exec(t, fmt.Sprintf(
				"INSERT INTO %s (id, %s, label) SELECT 18446744073709551615 - id, 3001 - id, label FROM %s WHERE id <= 3000",
				f.qualified("unsigned_key"), f.quote("rank"), f.qualified(f.bigTable(t))))
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
		requireWithinWindow(t, rows, "rank")
	})
}

// oddNameDraws bounds the samples Test_SampleData_OddTableName draws.
const oddNameDraws = 10

// A name with a space and capitals is quoted the same way by the check that the table exists, by
// the query that reads its size or its key, and by the query that draws across the table. Only
// that last query returns a row beyond the first 1000 of the table: the window never does.
//
// The table holds 3000 rows. On PostgreSQL and SQL Server it is on fewer than SampleMinPages
// pages, all of them are read, and 20 rows drawn among all are all within the first 1000 with
// probability (1/3)^20, 3 in 10^10. On MySQL the six key ranges above rank 1200 each give 100
// rows two times in three; three of them doing so (nine times in ten) put at least 300 rows above
// rank 1000 against 400 below at most, and 20 rows all below is then under (4/7)^20: one draw
// shows no row beyond the window less than once in ten. Ten draws all doing so is under 1e-10.
func Test_SampleData_OddTableName(t *testing.T) {
	forEachEngine(t, allFamilies, func(t *testing.T, f *sampleFixture) {
		table := f.filledTable(t, "Order Lines", 3000)
		beyondWindow := false
		for draw := 0; draw < oddNameDraws && !beyondWindow; draw++ {
			rows := f.mustSample(t, table, 20)
			require.Len(t, rows, 20)
			requireDistinct(t, rows, "rank")
			beyondWindow = hasRowBeyondWindow(t, rows)
		}
		require.True(t, beyondWindow, "every sampled row is within the first 1000 rows")
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
		require.True(t, hasRowBeyondWindow(t, rows), "every sampled row is within the first 1000 rows")
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

// The table was analyzed with rows of 1 kB, seven to a page, and then received 200 000 rows a
// hundred times narrower: the size read from the catalog, 10 720 rows on 1533 pages, is about
// twenty times under the truth, and the 9.3 percent of the pages meant for 1000 rows hold about
// 18 000. The rows that reach the random order are bounded all the same.
func Test_SampleData_BoundsTheRows(t *testing.T) {
	forEachEngine(t, postgresOnly, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "denser", func() {
			name := f.qualified("denser")
			// The table is never analyzed in the background, so its statistics stay as set here.
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL, "+
					"pad TEXT NOT NULL DEFAULT '') WITH (autovacuum_enabled = false)", name, f.quote("rank")))
			f.exec(t, fmt.Sprintf(
				"INSERT INTO %s (id, %s, label, pad) SELECT -g, -g, 'wide', repeat('x', 1000) FROM generate_series(1, 2000) g",
				name, f.quote("rank")))
			f.analyze(t, "denser")
			f.load(t, "denser", 1, bigRows, 1)
		})

		before := f.seqTupleReads(t, table)
		rows := f.mustSample(t, table, 100)
		require.Len(t, rows, 100)
		requireDistinct(t, rows, "rank")
		after := f.settledSeqTupleReads(t, before, table)
		require.Greater(t, after, before, "the statistics view did not report the rows read")
		t.Logf("rows read by one sample: %d", after-before)
		// The scan stops at SampleRowsBound rows. Unbounded, it reads the rows of about 116 of
		// the 1247 pages of narrow rows: 4200 rows or fewer would be 26 of them at most, less
		// than once in 10^24 samples.
		require.LessOrEqual(t, after-before, int64(querybuilder.SampleRowsBound)+200)
	})
}

// SQL Server draws a share of its pages with TABLESAMPLE and orders all their rows. The query
// the builder produces for the size of the table runs and returns distinct rows, no more than
// asked; it returns none only when no page is taken, e^-50 for a share of fifty pages.
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
		require.Contains(t, query, "PERCENT))")
		requireBuiltQueryDraws(t, f, query, 100)

		requireCoversTheTable(t, f, table, 100, bigRows)
	})
}

// A table without a clustered index is sampled across its pages like any other. Its rows are
// copied from the table with a key by one thread and in key order, so its pages follow the ranks
// as those of that table do, and there are as many of them: the same figures hold.
func Test_SampleData_SqlServerHeap(t *testing.T) {
	forEachEngine(t, sqlServerOnly, func(t *testing.T, f *sampleFixture) {
		big := f.bigTable(t)
		table := f.dataset(t, "heap", func() {
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL, %s INT NOT NULL, label VARCHAR(40) NOT NULL)",
				f.qualified("heap"), f.quote("rank")))
			f.exec(t, fmt.Sprintf(
				"INSERT INTO %s (id, %s, label) SELECT id, %s, label FROM %s ORDER BY id OPTION (MAXDOP 1)",
				f.qualified("heap"), f.quote("rank"), f.quote("rank"), f.qualified(big)))
		})
		requireCoversTheTable(t, f, table, 100, bigRows)
		requireDrawsFromManyPlaces(t, f, table)
	})
}

// The rows of a partitioned table and their pages are counted over its partitions: 4000 rows on
// 18 pages, all read. 20 rows drawn among them are all within the first 1000 with probability
// (1/4)^20, 9 in 10^13; the window alone never returns a row beyond them.
func Test_SampleData_SqlServerPartitionedTable(t *testing.T) {
	forEachEngine(t, sqlServerOnly, func(t *testing.T, f *sampleFixture) {
		table := f.dataset(t, "parted", func() {
			f.exec(t, "CREATE PARTITION FUNCTION sample_halves (BIGINT) AS RANGE RIGHT FOR VALUES (2001)")
			f.exec(t, "CREATE PARTITION SCHEME sample_halves_scheme AS PARTITION sample_halves ALL TO ([PRIMARY])")
			f.exec(t, fmt.Sprintf(
				"CREATE TABLE %s (id BIGINT NOT NULL PRIMARY KEY, %s INT NOT NULL, label VARCHAR(40) NOT NULL) "+
					"ON sample_halves_scheme (id)", f.qualified("parted"), f.quote("rank")))
			f.load(t, "parted", 1, 4000, 1)
		})
		rows := f.mustSample(t, table, 20)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
		require.True(t, hasRowBeyondWindow(t, rows), "every sampled row is within the first 1000 rows")
	})
}

// hasRowBeyondWindow tells whether a sample holds a row the window query cannot return: one
// whose rank is above SampleWindowSize.
func hasRowBeyondWindow(t *testing.T, rows []map[string]any) bool {
	t.Helper()
	for _, row := range rows {
		if intColumn(t, row, "rank") > querybuilder.SampleWindowSize {
			return true
		}
	}
	return false
}

// requireWithinWindow fails when a sample holds a row the window query cannot return: one whose
// place in the table, read in the given column, is above SampleWindowSize.
func requireWithinWindow(t *testing.T, rows []map[string]any, column string) {
	t.Helper()
	for _, row := range rows {
		require.LessOrEqual(t, intColumn(t, row, column), int64(querybuilder.SampleWindowSize),
			"a sampled row is beyond the first %d rows of the table", querybuilder.SampleWindowSize)
	}
}

// A login that may only read the table is given its size by the catalog, and its sample comes
// from across the table: 20 rows drawn among 200 000 are all within the first 1000 with
// probability (1/200)^20.
func Test_SampleData_SqlServerLoginWithSelectOnly(t *testing.T) {
	forEachEngine(t, sqlServerOnly, func(t *testing.T, f *sampleFixture) {
		table := f.bigTable(t)
		const login, password = "sample_reader", "sample-READER-1"
		f.dataset(t, "login "+login, func() {
			f.exec(t, fmt.Sprintf("CREATE LOGIN %s WITH PASSWORD = '%s', CHECK_POLICY = OFF", login, password))
			f.exec(t, fmt.Sprintf("CREATE USER %s FOR LOGIN %s", login, login))
			f.exec(t, fmt.Sprintf("GRANT SELECT ON %s TO %s", f.qualified(table), login))
		})

		rows, err := f.sampleRowsAs(t, f.mssqlConnectionAs(t, login, password), table, 20)
		require.NoError(t, err)
		require.Len(t, rows, 20)
		requireDistinct(t, rows, "rank")
		require.True(t, hasRowBeyondWindow(t, rows), "every sampled row is within the first 1000 rows")
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
