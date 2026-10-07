package usagereport

import (
	"context"
	"strings"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// noOrchestrator is the instance of a test, which runs no orchestrator: what is asked of one is
// answered here, and the rest is read from the database.
type noOrchestrator struct{ *InstanceReader }

func (noOrchestrator) TemporalVersion(context.Context) (string, error) { return "1.25.2", nil }
func (noOrchestrator) Workers(context.Context) (int, error)            { return 2, nil }

// The report for a period of an instance whose every name holds the marker, made from its own
// tables: the months add up the rows they hold and stop at their edges, a month tells the state
// of its last report and the most sources it counted, and the marker is nowhere in the document
// nor in what making it logged.
func Test_BuildPeriod_AddsUpTheMonthsOfTheInstanceAndCarriesNoName(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx, output := logged(t)
	pool := migratedPool(ctx, t)

	queries := db_queries.New()
	db := husonymdb.New(pool, queries)
	roles, err := rbac.New(ctx, pool, testutil.GetTestLogger(t))
	require.NoError(t, err)
	store := usagestore.New(db)
	keyValue, ring := mintKey(t, keyExpiring(testExpiry))
	builder := NewBuilder(
		store,
		NewInventoryReader(db, licensegate.NewUsageReader(db, roles), roles, store, true),
		noOrchestrator{NewInstanceReader(db, nil)},
		&fakeLicense{inForce: true},
		&fakeKeys{value: keyValue},
		ring,
		Facts{
			Version: "v0.3.0", InstallKind: "helm", OS: "linux", Arch: "amd64",
			AuthEnabled: true, AuthProvider: "keycloak", Presidio: true, RunLogs: "loki", Diagnostics: true,
		},
	)

	// What a customer names, each with the marker.
	account, err := queries.CreateTeamAccount(ctx, pool, leak+"-account")
	require.NoError(t, err)
	other, err := queries.CreateTeamAccount(ctx, pool, leak+"-other-account")
	require.NoError(t, err)
	user, err := queries.CreateNonMachineUser(ctx, pool)
	require.NoError(t, err)
	require.NoError(t, queries.CreateAccountUserAssociation(ctx, pool, db_queries.CreateAccountUserAssociationParams{
		AccountID: account.ID, UserID: user.ID,
	}))
	connection := func(name string, config *pg_models.ConnectionConfig) db_queries.HusonymApiConnection {
		t.Helper()
		created, err := queries.CreateConnection(ctx, pool, db_queries.CreateConnectionParams{
			Name: name, AccountID: account.ID, ConnectionConfig: config, CreatedByID: user.ID, UpdatedByID: user.ID,
		})
		require.NoError(t, err)
		return created
	}
	job := func(name string, source db_queries.HusonymApiConnection) db_queries.HusonymApiJob {
		t.Helper()
		created, err := queries.CreateJob(ctx, pool, db_queries.CreateJobParams{
			Name:              name,
			AccountID:         account.ID,
			ConnectionOptions: postgresFrom(husonymdb.UUIDString(source.ID), leak+"_tenant = 'acme'"),
			Mappings: []*pg_models.JobMapping{
				passthrough(t, leak+"_schema", leak+"_users", leak+"_email"),
			},
			CronSchedule:       pgtype.Text{String: "0 3 * * *", Valid: true},
			CreatedByID:        user.ID,
			UpdatedByID:        user.ID,
			WorkflowOptions:    &pg_models.WorkflowOptions{},
			SyncOptions:        &pg_models.ActivityOptions{},
			VirtualForeignKeys: []*pg_models.VirtualForeignConstraint{},
			JobtypeConfig:      []byte("{}"),
		})
		require.NoError(t, err)
		return created
	}
	production := connection(leak+"-production", postgresConnection())
	archive := connection(leak+"-archive", postgresConnection())
	nightly := job(leak+"-nightly", production)
	accountId, otherId, jobId := husonymdb.UUIDString(account.ID), husonymdb.UUIDString(other.ID), husonymdb.UUIDString(nightly.ID)

	at := func(m time.Month, day int, clock ...time.Duration) time.Time {
		moment := time.Date(2026, m, day, 0, 0, 0, 0, time.UTC)
		for _, d := range clock {
			moment = moment.Add(d)
		}
		return moment
	}
	// keep makes the report of a day the way the daily pass does, from what the instance holds
	// now, and keeps it.
	keep := func(day time.Time) {
		t.Helper()
		prepared := day.Add(24*time.Hour + 5*time.Minute)
		sealed, err := builder.Build(ctx, day, prepared)
		require.NoError(t, err)
		saved, err := store.SaveReport(ctx, usagestore.StoredReport{
			Day: day, Document: sealed.Document, Seal: sealed.Seal, KeyFingerprint: sealed.KeyFingerprint, PreparedAt: prepared,
		})
		require.NoError(t, err)
		require.True(t, saved)
	}
	// run ends a run of the given seconds, whose end the API recorded at the given moment.
	run := func(runId string, recorded time.Time, seconds int, rows int64, status usagestore.Status) {
		t.Helper()
		require.NoError(t, store.RunEnded(ctx, usagestore.RunEnd{
			RunId: runId, AccountId: accountId, JobId: jobId, Kind: usagestore.JobKindSync,
			StartedAt: recorded.Add(-time.Duration(seconds) * time.Second), EndedAt: recorded, Status: status,
			RowsRead: rows, RowsDiscarded: 1, Retries: 1,
		}))
		_, err := pool.Exec(ctx, `UPDATE husonym_api.run_usage SET recorded_at = $2 WHERE run_id = $1`, runId, recorded)
		require.NoError(t, err)
	}

	// August: one source, then two, then one again; three reports. Three runs, the last of them
	// recorded at the last second of the month, that read a thousand rows between them.
	keep(at(time.August, 10))
	extra := job(leak+"-extra", archive)
	keep(at(time.August, 20))
	_, err = pool.Exec(ctx, `DELETE FROM husonym_api.jobs WHERE id = $1`, extra.ID)
	require.NoError(t, err)
	keep(at(time.August, 31))
	run(leak+"-run-a", at(time.August, 1), 60, 600, usagestore.StatusCompleted)
	run(leak+"-run-b", at(time.August, 15, 12*time.Hour), 120, 300, usagestore.StatusCompleted)
	run(leak+"-run-c", at(time.September, 1, -time.Second), 600, 100, usagestore.StatusFailed)
	require.NoError(t, store.CountRefusal(ctx, accountId, []license.Gate{license.FeatureGate(license.FeatureSubsetting)}, at(time.September, 1, -time.Second)))

	// September: no report. Two runs at its two edges, one of which ended in August and was
	// recorded in September; refusals of two accounts on its first day.
	run(leak+"-run-d", at(time.September, 1), 30, 5_000, usagestore.StatusCompleted)
	run(leak+"-run-e", at(time.October, 1, -time.Second), 30, 6_000, usagestore.StatusCompleted)
	for _, refused := range []string{accountId, otherId} {
		require.NoError(t, store.CountRefusal(ctx, refused, []license.Gate{license.GateJobCap}, at(time.September, 1)))
	}

	// October, the month under way: a run at its first instant, a report that reads, and one
	// that no longer does and holds a name.
	run(leak+"-run-f", at(time.October, 1), 30, 7, usagestore.StatusCompleted)
	keep(at(time.October, 6))
	saved, err := store.SaveReport(ctx, usagestore.StoredReport{
		Day: at(time.October, 3), Document: []byte(`{"day":"2026-10-03","job":"` + leak + `-nightly"}`), Seal: "seal", KeyFingerprint: "fp",
		PreparedAt: at(time.October, 4),
	})
	require.NoError(t, err)
	require.True(t, saved)
	// What comes after the period is not of it.
	run(leak+"-run-later", at(time.November, 1), 30, 9_000_000, usagestore.StatusCompleted)
	require.Empty(t, output.String(), "the reports of the days read everything")

	// July to October, asked on the seventh of October.
	sealed, err := builder.BuildPeriod(ctx, at(time.July, 1), at(time.October, 1), reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.ValidatePeriod(sealed.Document))
	require.NoError(t, telemetry.Verify(keyValue, sealed.Document, sealed.Seal))
	require.Equal(t, telemetry.KeyFingerprint(keyValue), sealed.KeyFingerprint)

	read := months(t, sealed.Document)
	require.Len(t, read, 4)
	emptyRuns := map[string]any{
		"by_status": []any{}, "rows_read": "lt_1k", "rows_discarded": "lt_1k",
		"retries": float64(0), "with_uncounted_rows": float64(0),
	}

	// July: no run, no report.
	require.Equal(t, map[string]any{
		"month": "2026-07", "days_reported": float64(0), "sources": map[string]any{"count": float64(0)},
		"runs": emptyRuns, "refusals": []any{},
	}, read[0])

	// August: its three runs, a thousand rows that each run alone would tell as fewer, the most
	// sources a report counted, and the state of the last report.
	august := read[1]
	require.Equal(t, "2026-08", august["month"])
	require.Equal(t, float64(3), august["days_reported"])
	require.Equal(t, map[string]any{"husonym": "v0.3.0"}, august["version"])
	require.Equal(t, map[string]any{"count": float64(2)}, august["sources"])
	require.Equal(t, map[string]any{
		"by_status": []any{
			map[string]any{"kind": "sync", "status": "completed", "count": float64(2)},
			map[string]any{"kind": "sync", "status": "failed", "count": float64(1)},
		},
		"duration_seconds": map[string]any{"median": float64(120), "p95": float64(552)},
		"rows_read":        "lt_10k", "rows_discarded": "lt_1k", "retries": float64(3), "with_uncounted_rows": float64(0),
	}, august["runs"])
	require.Equal(t, []any{map[string]any{"gate": "subsetting", "count": float64(1)}}, august["refusals"])
	require.Equal(t,
		[]any{map[string]any{"kind": "sync", "scheduled": true, "count": float64(1)}},
		block(t, august, "state", "jobs")["by_kind"], "the job that came and went is not in the last report")
	require.Len(t, august["state"], 9)

	// September: the runs recorded at its two edges, the refusals of both accounts as one count,
	// and nothing of a state, as no report was kept.
	require.Equal(t, map[string]any{
		"month": "2026-09", "days_reported": float64(0), "sources": map[string]any{"count": float64(0)},
		"runs": map[string]any{
			"by_status":        []any{map[string]any{"kind": "sync", "status": "completed", "count": float64(2)}},
			"duration_seconds": map[string]any{"median": float64(30), "p95": float64(30)},
			"rows_read":        "lt_100k", "rows_discarded": "lt_1k", "retries": float64(2), "with_uncounted_rows": float64(0),
		},
		"refusals": []any{map[string]any{"gate": "job_cap", "count": float64(2)}},
	}, read[2])

	// October: as far as it went, and without the report that can no longer be read.
	october := read[3]
	require.Equal(t, float64(1), october["days_reported"])
	require.Equal(t, map[string]any{"count": float64(1)}, october["sources"])
	require.Equal(t,
		[]any{map[string]any{"kind": "sync", "status": "completed", "count": float64(1)}},
		block(t, october, "runs")["by_status"])
	require.Contains(t, october, "state")

	// The report that was left out is said by its day, and by nothing it holds.
	require.Equal(t, 1, strings.Count(output.String(), `"level":"WARN"`))
	require.Contains(t, output.String(), `"day":"2026-10-03"`)

	for where, text := range map[string]string{"the report": string(sealed.Document), "the logs": output.String()} {
		requireNoLeak(t, text, where)
		for _, id := range []string{accountId, otherId, jobId} {
			require.NotContains(t, text, id, where)
		}
	}
	require.NotContains(t, string(sealed.Document), keyValue)
	require.NotContains(t, output.String(), keyValue)
}
