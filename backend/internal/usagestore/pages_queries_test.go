package usagestore

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const usageQueries = "../../sql/postgresql/queries/usage.sql"

// dayOfRun is the day of a run in the zone of a read, as the queries of the pages write it.
const dayOfRun = "(recorded_at AT TIME ZONE sqlc.arg(zone)::text)::date"

// periodOfRuns is how a query of the pages keeps the runs of a period: the two lines that let
// the index find the rows, a day wider on each side, then the two that decide, on the day of the
// run alone.
var periodOfRuns = strings.Join([]string{
	"  AND recorded_at >= (sqlc.arg(from_day)::date - 1)::timestamp AT TIME ZONE sqlc.arg(zone)::text",
	"  AND recorded_at < (sqlc.arg(before_day)::date + 1)::timestamp AT TIME ZONE sqlc.arg(zone)::text",
	"  AND " + dayOfRun + " >= sqlc.arg(from_day)::date",
	"  AND " + dayOfRun + " < sqlc.arg(before_day)::date",
}, "\n")

// queriesOf gives the statements of a file of queries by their names, without their comments.
func queriesOf(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.ReadFile(path)
	require.NoError(t, err)
	queries := map[string]string{}
	name := ""
	for line := range strings.SplitSeq(string(file), "\n") {
		if declared, ok := strings.CutPrefix(line, "-- name: "); ok {
			name, _, _ = strings.Cut(declared, " ")
			continue
		}
		if name != "" && !strings.HasPrefix(line, "--") {
			queries[name] += line + "\n"
		}
	}
	return queries
}

// sqlc has no way to write an expression once for several queries, and a function of the database
// is not wanted: the day of a run is written in each read of the runs, and this test is what keeps
// the copies from drifting. Every query that takes a zone holds the same four lines, places a run
// in a day nowhere else than in them and in the day it groups by, and no other query takes a zone.
func Test_PageQueries_PlaceARunInItsDayTheSameWay(t *testing.T) {
	byDay := map[string]bool{
		"SumAccountRunUsageBetween":         false,
		"SumJobRunUsageBetween":             false,
		"SumAccountRunUsageByJobBetween":    false,
		"SumAccountRunUsageByDayBetween":    true,
		"SumJobRunUsageByDayBetween":        true,
		"CountAccountRunUsageErrorsBetween": false,
		"ListJobRunUsageBetween":            false,
	}

	zoned := []string{}
	for name, query := range queriesOf(t, usageQueries) {
		if !strings.Contains(query, "sqlc.arg(zone)") {
			continue
		}
		zoned = append(zoned, name)
		groups, known := byDay[name]
		require.True(t, known, "%s takes a zone and is not one of the reads this test knows", name)

		require.Equal(t, 1, strings.Count(query, periodOfRuns), "%s keeps the runs of the period by the four lines", name)
		days, zones := 2, 4
		if groups {
			require.Contains(t, query, "\n  "+dayOfRun+" AS day,\n", "%s groups by the day of the run", name)
			require.Contains(t, query, "\nGROUP BY day\n", name)
			days, zones = 3, 5
		}
		require.Equal(t, days, strings.Count(query, dayOfRun), "%s writes the day of a run where it is expected only", name)
		require.Equal(t, zones, strings.Count(query, "AT TIME ZONE"), "%s uses the zone where it is expected only", name)
		require.Equal(t, zones, strings.Count(query, "sqlc.arg(zone)"), name)
	}
	require.ElementsMatch(t, slices.Collect(maps.Keys(byDay)), zoned)
}
