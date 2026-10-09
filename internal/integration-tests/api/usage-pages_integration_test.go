package integrationtests_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// pagesGround is a team account whose runs the usage pages read, with the client of its
// administrator. The ids of its runs carry its name: the runs of two grounds never collide.
type pagesGround struct {
	*usageGround
	name  string
	usage mgmtv1alpha1connect.UsageServiceClient
}

func (s *IntegrationTestSuite) newPagesGround(name string) *pagesGround {
	g := s.newUsageGround(name)
	return &pagesGround{
		usageGround: g, name: name,
		usage: s.OSSAuthenticatedExpiringClients.Usage(integrationtests_test.WithUserId(name + "-admin")),
	}
}

// ranOnce tells the API, as the worker does, that a run of the job ended, then has the end be
// recorded at the moment given: it is the moment a run is counted on. It gives the id of the run
// and the moment it started.
func (s *IntegrationTestSuite) ranOnce(
	g *pagesGround, jobId, run string, recordedAt time.Time, lasted time.Duration, tell func(*runusage.RunEndedRequest),
) (runId string, startedAt time.Time) {
	t := s.T()
	t.Helper()
	runId = g.name + "-" + run
	endedAt := recordedAt.Add(-time.Second)
	end := &runusage.RunEndedRequest{
		JobId: jobId, RunId: runId, StartedAt: endedAt.Add(-lasted), EndedAt: endedAt, Outcome: runusage.OutcomeCompleted,
	}
	if tell != nil {
		tell(end)
	}
	worker := s.OSSAuthenticatedExpiringClients.Usage(integrationtests_test.WithUserId(apikey.NewV1WorkerKey()))
	require.NoError(t, runusage.New(worker).RecordRunEnded(s.ctx, end))
	tag, err := s.Pgcontainer.DB.Exec(s.ctx,
		`UPDATE husonym_api.run_usage SET recorded_at = $2 WHERE run_id = $1`, runId, recordedAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())
	return runId, end.StartedAt
}

func usageDay(year, month, day uint32) *mgmtv1alpha1.Date {
	return &mgmtv1alpha1.Date{Year: year, Month: month, Day: day}
}

func (g *pagesGround) accountUsage(
	s *IntegrationTestSuite, from, to *mgmtv1alpha1.Date, zone string,
) *mgmtv1alpha1.GetAccountUsageResponse {
	s.T().Helper()
	resp, err := g.usage.GetAccountUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
		AccountId: g.accountId, FromDay: from, ToDay: to, TimeZone: zone,
	}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg
}

func (g *pagesGround) jobUsage(
	s *IntegrationTestSuite, jobId string, from, to *mgmtv1alpha1.Date, zone string,
) *mgmtv1alpha1.GetJobUsageResponse {
	s.T().Helper()
	resp, err := g.usage.GetJobUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobUsageRequest{
		AccountId: g.accountId, JobId: jobId, FromDay: from, ToDay: to, TimeZone: zone,
	}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg
}

func requireTotals(s *IntegrationTestSuite, totals *mgmtv1alpha1.UsageTotals, runs, completed, canceled, rowsRead, rowsDiscarded, uncounted int64) {
	t := s.T()
	t.Helper()
	require.Equal(t, runs, totals.GetRuns(), "runs")
	require.Equal(t, completed, totals.GetRunsCompleted(), "runs completed")
	require.Equal(t, canceled, totals.GetRunsCanceled(), "runs canceled")
	require.Equal(t, rowsRead, totals.GetRowsRead(), "rows read")
	require.Equal(t, rowsDiscarded, totals.GetRowsDiscarded(), "rows discarded")
	require.Equal(t, uncounted, totals.GetRunsWithUncountedRows(), "runs with uncounted rows")
}

func requireDay(s *IntegrationTestSuite, day *mgmtv1alpha1.UsageDay, dayOfOctober uint32, rowsRead, runs int64) {
	t := s.T()
	t.Helper()
	require.Equal(t, uint32(2026), day.GetDay().GetYear())
	require.Equal(t, uint32(10), day.GetDay().GetMonth())
	require.Equal(t, dayOfOctober, day.GetDay().GetDay())
	require.Equal(t, rowsRead, day.GetRowsRead(), "the rows of October %d", dayOfOctober)
	require.Equal(t, runs, day.GetRuns(), "the runs of October %d", dayOfOctober)
}

// The usage of an account and of its jobs, from the runs the worker told and the refusals the
// gates counted, to what the two procedures answer: only the account asked, by the days of the
// zone asked, and nothing of a job of another account.
func (s *IntegrationTestSuite) Test_UsagePages_GiveTheUsageOfTheAccountAndOfItsJobs() {
	t := s.T()
	ctx := s.ctx
	ours := s.newPagesGround("usage-pages-ours")
	theirs := s.newPagesGround("usage-pages-theirs")

	orders := s.mustCreateJob(ours.capGround, ours.jobRequest("orders")).GetId()
	scanRequest := ours.jobRequest("scan")
	scanRequest.JobType = piiDetectJobType()
	scan := s.mustCreateJob(ours.capGround, scanRequest).GetId()
	gone := s.mustCreateJob(ours.capGround, ours.jobRequest("gone")).GetId()
	s.mustCreateJob(ours.capGround, ours.jobRequest("idle"))
	theirJob := s.mustCreateJob(theirs.capGround, theirs.jobRequest("theirs")).GetId()

	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC) }
	okId, okStarted := s.ranOnce(ours, orders, "ok", at(6, 9, 0), time.Minute, func(r *runusage.RunEndedRequest) {
		r.RowsRead, r.RowsDiscarded = 100, 5
	})
	failedId, _ := s.ranOnce(ours, orders, "failed", at(6, 10, 0), 2*time.Minute, func(r *runusage.RunEndedRequest) {
		r.Outcome, r.RowsRead, r.TablesUncounted = runusage.OutcomeFailed, 40, 1
		r.ErrorCategory = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED
		r.ErrorStep = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_TABLE_SYNC
	})
	canceledId, _ := s.ranOnce(ours, orders, "canceled", at(6, 11, 0), 20*time.Second, func(r *runusage.RunEndedRequest) {
		r.Outcome, r.RowsRead = runusage.OutcomeCanceled, 3
	})
	// Half past midnight on October 7 in Paris, and still October 6 in UTC.
	lateId, _ := s.ranOnce(ours, orders, "late", at(6, 22, 30), 30*time.Second, func(r *runusage.RunEndedRequest) {
		r.RowsRead = 7
	})
	s.ranOnce(ours, scan, "scan", at(5, 12, 0), 10*time.Minute, nil)
	s.ranOnce(ours, gone, "gone", at(6, 12, 0), 10*time.Second, func(r *runusage.RunEndedRequest) { r.RowsRead = 1000 })
	theirRun, _ := s.ranOnce(theirs, theirJob, "ok", at(6, 9, 0), time.Minute, func(r *runusage.RunEndedRequest) {
		r.RowsRead = 9999
	})
	// The job is deleted after it ran: its runs stay.
	_, err := s.Pgcontainer.DB.Exec(ctx, `DELETE FROM husonym_api.jobs WHERE id = $1`, gone)
	require.NoError(t, err)

	require.NoError(t, s.CountUsageRefusal(ctx, ours.accountId, license.FeatureGate(license.FeatureRbac), at(6, 8, 0)))
	require.NoError(t, s.CountUsageRefusal(ctx, ours.accountId, license.FeatureGate(license.FeatureRbac), at(6, 9, 0)))
	require.NoError(t, s.CountUsageRefusal(ctx, ours.accountId, license.GateJobCap, at(7, 9, 0)))
	require.NoError(t, s.CountUsageRefusal(ctx, ours.accountId, license.GateJobCap, at(8, 9, 0)))
	require.NoError(t, s.CountUsageRefusal(ctx, theirs.accountId, license.FeatureGate(license.FeatureSso), at(6, 9, 0)))

	oct5, oct7 := usageDay(2026, 10, 5), usageDay(2026, 10, 7)

	t.Run("the account, by the days of Paris", func(t *testing.T) {
		usage := ours.accountUsage(s, oct5, oct7, "Europe/Paris")
		require.Equal(t, "Europe/Paris", usage.GetTimeZone())

		// Every run of the account, the one of the deleted job among them, and none of the other
		// account. Four of six completed, one was canceled.
		requireTotals(s, usage.GetTotals(), 6, 4, 1, 1150, 5, 1)
		require.NotNil(t, usage.DurationTotalSeconds)
		require.Equal(t, int64(60+120+20+30+600+10), usage.GetDurationTotalSeconds())

		require.Len(t, usage.GetDays(), 3)
		requireDay(s, usage.GetDays()[0], 5, 0, 1)
		requireDay(s, usage.GetDays()[1], 6, 1143, 4)
		requireDay(s, usage.GetDays()[2], 7, 7, 1)

		// The jobs that exist and ran, the one that read the most rows first: neither the deleted
		// job, which read the most, nor the job that did not run.
		require.Len(t, usage.GetJobs(), 2)
		first, second := usage.GetJobs()[0], usage.GetJobs()[1]
		require.Equal(t, orders, first.GetJobId())
		require.Equal(t, "orders", first.GetJobName())
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_SYNC, first.GetKind())
		requireTotals(s, first.GetTotals(), 4, 2, 1, 150, 5, 1)
		require.Equal(t, int64(45), first.GetDurationMedianSeconds(), "the median of 20, 30, 60 and 120 seconds")
		require.Equal(t, scan, second.GetJobId())
		require.Equal(t, "scan", second.GetJobName())
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_PII_DETECT, second.GetKind())
		requireTotals(s, second.GetTotals(), 1, 1, 0, 0, 0, 0)
		require.Equal(t, int64(600), second.GetDurationMedianSeconds())

		require.Len(t, usage.GetErrors(), 2)
		require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED, usage.GetErrors()[0].GetCategory())
		require.Equal(t, int64(1), usage.GetErrors()[0].GetRuns())
		require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED, usage.GetErrors()[1].GetCategory())
		require.Equal(t, int64(1), usage.GetErrors()[1].GetRuns())

		// The refusals of the days asked, read as UTC days: the one of October 8 is not among them.
		require.Len(t, usage.GetRefusals(), 2)
		require.Equal(t, "job_cap", usage.GetRefusals()[0].GetGate())
		require.Equal(t, int64(1), usage.GetRefusals()[0].GetRefusals())
		require.Equal(t, "rbac", usage.GetRefusals()[1].GetGate())
		require.Equal(t, int64(2), usage.GetRefusals()[1].GetRefusals())
	})

	t.Run("the same days in UTC hold the late run a day earlier", func(t *testing.T) {
		for _, zone := range []string{"", "UTC", "Nowhere/Land"} {
			usage := ours.accountUsage(s, oct5, oct7, zone)
			require.Equal(t, "UTC", usage.GetTimeZone(), zone)
			require.Len(t, usage.GetDays(), 3)
			requireDay(s, usage.GetDays()[1], 6, 1150, 5)
			requireDay(s, usage.GetDays()[2], 7, 0, 0)
		}
	})

	// The API runs in the process of the test: "Local" is a name it knows, the zone of this
	// machine, and no zone of the database. The days are read as UTC days, and the answer says so.
	t.Run("a zone the API knows and the database does not is read as UTC", func(t *testing.T) {
		usage := ours.accountUsage(s, oct5, oct7, "Local")
		require.Equal(t, "UTC", usage.GetTimeZone())
		requireTotals(s, usage.GetTotals(), 6, 4, 1, 1150, 5, 1)
		requireDay(s, usage.GetDays()[1], 6, 1150, 5)

		ofJob := ours.jobUsage(s, orders, oct5, oct7, "Local")
		require.Equal(t, "UTC", ofJob.GetTimeZone())
		requireDay(s, ofJob.GetDays()[1], 6, 150, 4)
		require.Len(t, ofJob.GetRuns(), 4)
	})

	t.Run("a period with no run is told at zero", func(t *testing.T) {
		usage := ours.accountUsage(s, usageDay(2026, 9, 1), usageDay(2026, 9, 30), "Europe/Paris")
		requireTotals(s, usage.GetTotals(), 0, 0, 0, 0, 0, 0)
		require.Nil(t, usage.DurationTotalSeconds)
		require.Len(t, usage.GetDays(), 30)
		require.Empty(t, usage.GetJobs())
		require.Empty(t, usage.GetErrors())
		require.Empty(t, usage.GetRefusals())
	})

	t.Run("a job of the account", func(t *testing.T) {
		usage := ours.jobUsage(s, orders, oct5, oct7, "Europe/Paris")
		require.Equal(t, "Europe/Paris", usage.GetTimeZone())
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_SYNC, usage.GetKind())
		requireTotals(s, usage.GetTotals(), 4, 2, 1, 150, 5, 1)
		require.Equal(t, int64(45), usage.GetDurationMedianSeconds())
		require.Len(t, usage.GetDays(), 3)
		requireDay(s, usage.GetDays()[0], 5, 0, 0)
		requireDay(s, usage.GetDays()[1], 6, 143, 3)
		requireDay(s, usage.GetDays()[2], 7, 7, 1)

		// The runs counted in the period, the most recently recorded first.
		runs := usage.GetRuns()
		require.Len(t, runs, 4)
		require.Equal(t, lateId, runs[0].GetRunId())
		require.Equal(t, canceledId, runs[1].GetRunId())
		require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_CANCELED, runs[1].GetStatus())
		require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED, runs[1].GetErrorCategory())
		require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER, runs[1].GetErrorStep())
		require.Equal(t, failedId, runs[2].GetRunId())
		require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_FAILED, runs[2].GetStatus())
		require.Equal(t, int64(40), runs[2].GetRowsRead())
		require.Equal(t, int64(1), runs[2].GetTablesUncounted())
		require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED, runs[2].GetErrorCategory())
		require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_TABLE_SYNC, runs[2].GetErrorStep())
		require.Equal(t, okId, runs[3].GetRunId())
		require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE, runs[3].GetStatus())
		require.Equal(t, timestamppb.New(okStarted), runs[3].GetStartedAt())
		require.Equal(t, timestamppb.New(okStarted.Add(time.Minute)), runs[3].GetEndedAt())
		require.Equal(t, int64(100), runs[3].GetRowsRead())
		require.Zero(t, runs[3].GetTablesUncounted())
		require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED, runs[3].GetErrorCategory())
		require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED, runs[3].GetErrorStep())

		// The latest runs follow the period: October 7 in Paris holds the late run alone.
		seventh := ours.jobUsage(s, orders, oct7, oct7, "Europe/Paris")
		requireTotals(s, seventh.GetTotals(), 1, 1, 0, 7, 0, 0)
		require.Len(t, seventh.GetRuns(), 1)
		require.Equal(t, lateId, seventh.GetRuns()[0].GetRunId())
	})

	t.Run("a job that counts no rows tells its kind, with or without a run in the period", func(t *testing.T) {
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_PII_DETECT, ours.jobUsage(s, scan, oct5, oct7, "").GetKind())
		quiet := ours.jobUsage(s, scan, oct7, oct7, "")
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_PII_DETECT, quiet.GetKind())
		requireTotals(s, quiet.GetTotals(), 0, 0, 0, 0, 0, 0)
		require.Nil(t, quiet.DurationMedianSeconds)
	})

	// Asked through our account, the job of the other account is a job that never ran: no
	// refusal, no not-found, nothing that tells it exists, and nothing of its run.
	t.Run("a job of another account has no usage in ours", func(t *testing.T) {
		usage := ours.jobUsage(s, theirJob, oct5, oct7, "Europe/Paris")
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, usage.GetKind())
		requireTotals(s, usage.GetTotals(), 0, 0, 0, 0, 0, 0)
		require.Nil(t, usage.DurationMedianSeconds)
		require.Empty(t, usage.GetRuns())
		require.Len(t, usage.GetDays(), 3)
		for i, day := range usage.GetDays() {
			requireDay(s, day, uint32(5+i), 0, 0) //nolint:gosec // a day of October
		}

		// It answers as a job that does not exist at all does.
		nowhere := ours.jobUsage(s, uuid.NewString(), oct5, oct7, "Europe/Paris")
		require.True(t, proto.Equal(usage, nowhere), "a job of another account reads as no job")
	})

	t.Run("the other account sees its own usage only", func(t *testing.T) {
		usage := theirs.accountUsage(s, oct5, oct7, "Europe/Paris")
		requireTotals(s, usage.GetTotals(), 1, 1, 0, 9999, 0, 0)
		require.Len(t, usage.GetJobs(), 1)
		require.Equal(t, theirJob, usage.GetJobs()[0].GetJobId())
		require.Equal(t, "theirs", usage.GetJobs()[0].GetJobName())
		require.Empty(t, usage.GetErrors())
		require.Len(t, usage.GetRefusals(), 1)
		require.Equal(t, "sso", usage.GetRefusals()[0].GetGate())

		ofJob := theirs.jobUsage(s, theirJob, oct5, oct7, "Europe/Paris")
		require.Len(t, ofJob.GetRuns(), 1)
		require.Equal(t, theirRun, ofJob.GetRuns()[0].GetRunId())
		// And our job has no usage in theirs.
		requireTotals(s, theirs.jobUsage(s, orders, oct5, oct7, "Europe/Paris").GetTotals(), 0, 0, 0, 0, 0, 0)
	})

	t.Run("the runs of a deleted job are still the account's", func(t *testing.T) {
		usage := ours.jobUsage(s, gone, oct5, oct7, "")
		requireTotals(s, usage.GetTotals(), 1, 1, 0, 1000, 0, 0)
		require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, usage.GetKind(), "the job is no longer there to tell its kind")
	})
}

// The usage of an account is told to who may view the account, and to nobody else: not to a
// member of another account, and not to an API key that does not hold the view of the account,
// whatever else it holds.
func (s *IntegrationTestSuite) Test_UsagePages_NeedToViewTheAccount() {
	t := s.T()
	g := s.newPagesGround("usage-pages-view")
	other := s.newPagesGround("usage-pages-view-other")
	jobId := s.mustCreateJob(g.capGround, g.jobRequest("orders")).GetId()
	s.ranOnce(g, jobId, "ok", time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC), time.Minute, func(r *runusage.RunEndedRequest) {
		r.RowsRead = 100
	})
	from, to := usageDay(2026, 10, 1), usageDay(2026, 10, 31)

	// ask calls both procedures as the caller given, and gives the code of each answer.
	ask := func(as string) (account, job connect.Code, rows int64) {
		client := s.OSSAuthenticatedExpiringClients.Usage(integrationtests_test.WithUserId(as))
		ofAccount, err := client.GetAccountUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
			AccountId: g.accountId, FromDay: from, ToDay: to,
		}))
		account = connect.CodeOf(err)
		if err == nil {
			account, rows = 0, ofAccount.Msg.GetTotals().GetRowsRead()
		}
		_, err = client.GetJobUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobUsageRequest{
			AccountId: g.accountId, JobId: jobId, FromDay: from, ToDay: to,
		}))
		job = connect.CodeOf(err)
		if err == nil {
			job = 0
		}
		return account, job, rows
	}
	requireRefused := func(as, who string) {
		t.Helper()
		account, job, _ := ask(as)
		require.Equal(t, connect.CodePermissionDenied, account, "%s asks for the usage of the account", who)
		require.Equal(t, connect.CodePermissionDenied, job, "%s asks for the usage of the job", who)
	}
	requireAnswered := func(as, who string) {
		t.Helper()
		account, job, rows := ask(as)
		require.Zero(t, account, "%s asks for the usage of the account", who)
		require.Zero(t, job, "%s asks for the usage of the job", who)
		require.Equal(t, int64(100), rows, who)
	}

	requireAnswered(g.name+"-admin", "the administrator")

	s.setUser(s.ctx, s.OSSAuthenticatedExpiringClients.Users(integrationtests_test.WithUserId("usage-pages-view-stranger")))
	requireRefused("usage-pages-view-stranger", "a person who is no member")
	requireRefused(other.name+"-admin", "the administrator of another account")

	g.roles.member(s, "usage-pages-view-viewer", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
	requireAnswered("usage-pages-view-viewer", "a member who may only view")

	// A key of the account holding one permission: the view of the account lets it through, and
	// no other permission does.
	everything := integrationtests_test.PermissionNames()
	require.Contains(t, everything, "account:view")
	require.Greater(t, len(everything), 10)
	for _, held := range everything {
		key := s.accountKey(g.accountId, g.adminId, held)
		if held == "account:view" {
			requireAnswered(key, "a key holding "+held)
		} else {
			requireRefused(key, "a key holding "+held)
		}
	}
	requireRefused(s.accountKey(g.accountId, g.adminId), "a key holding nothing")
	// A key is of its account: the view of another account opens nothing here.
	requireRefused(s.accountKey(other.accountId, other.adminId, everything...), "a key of another account holding everything")
}

// A period that is not one is refused in plain words, and a time zone never is.
func (s *IntegrationTestSuite) Test_UsagePages_RefuseAPeriodThatIsNotOne() {
	t := s.T()
	g := s.newPagesGround("usage-pages-period")
	jobId := s.mustCreateJob(g.capGround, g.jobRequest("orders")).GetId()

	for name, c := range map[string]struct {
		from, to *mgmtv1alpha1.Date
		sentence string
	}{
		"reversed":     {usageDay(2026, 10, 9), usageDay(2026, 10, 8), "the period cannot be read: its last day is before its first"},
		"too long":     {usageDay(2026, 1, 1), usageDay(2027, 1, 2), "the period cannot be read: it has more than 366 days"},
		"of no day":    {usageDay(2026, 2, 30), usageDay(2026, 3, 2), "the period cannot be read: one of its days does not exist"},
		"of long ago":  {usageDay(2020, 1, 1), usageDay(2026, 1, 1), "the period cannot be read: it has more than 366 days"},
		"with no days": {nil, nil, ""},
	} {
		for _, zone := range []string{"", "Europe/Paris", "Local", "Nowhere/Land"} {
			account, err := g.usage.GetAccountUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
				AccountId: g.accountId, FromDay: c.from, ToDay: c.to, TimeZone: zone,
			}))
			requireErrResp(t, account, err)
			requireConnectError(t, err, connect.CodeInvalidArgument)
			job, jobErr := g.usage.GetJobUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobUsageRequest{
				AccountId: g.accountId, JobId: jobId, FromDay: c.from, ToDay: c.to, TimeZone: zone,
			}))
			requireErrResp(t, job, jobErr)
			requireConnectError(t, jobErr, connect.CodeInvalidArgument)
			if c.sentence != "" {
				var refusal, jobRefusal *connect.Error
				require.ErrorAs(t, err, &refusal)
				require.Equal(t, c.sentence, refusal.Message(), "%s, in the zone %q", name, zone)
				require.ErrorAs(t, jobErr, &jobRefusal)
				require.Equal(t, c.sentence, jobRefusal.Message(), "%s, in the zone %q", name, zone)
			}
		}
	}

	// The longest period, and one that is all after today, are read.
	year := g.accountUsage(s, usageDay(2028, 1, 1), usageDay(2028, 12, 31), "Europe/Paris")
	require.Len(t, year.GetDays(), 366)
	require.Len(t, g.jobUsage(s, jobId, usageDay(2028, 1, 1), usageDay(2028, 12, 31), "Europe/Paris").GetDays(), 366)
}
