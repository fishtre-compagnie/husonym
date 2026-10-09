package v1alpha1_usageservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata/userdatatest"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	anotherJobId = "0b6f1d1e-7a43-4c36-9d6b-3f1b6c0f9a22"
	aDeletedJob  = "0b6f1d1e-7a43-4c36-9d6b-3f1b6c0f9a33"
)

// pageCall is one read the service asked of the store.
type pageCall struct {
	read   string
	scope  usagestore.Scope
	period usagestore.Period
	limit  int32
}

// fakePages answers the reads of the pages with what it was given, and keeps what it was asked.
type fakePages struct {
	calls []pageCall

	totals   usagestore.UsageTotals
	days     []usagestore.UsageDay
	jobs     []usagestore.JobUsage
	errors   []usagestore.CategoryCount
	refusals []usagestore.GateCount
	runs     []usagestore.RunRow

	// unknownZone makes every read in a zone other than UTC fail as the database does for a zone
	// it does not know. The refusals are of no zone, as in the store.
	unknownZone bool
	// failure is what every read fails with.
	failure error
}

func (f *fakePages) asked(read string, scope usagestore.Scope, period usagestore.Period, limit int32) error {
	f.calls = append(f.calls, pageCall{read: read, scope: scope, period: period, limit: limit})
	if f.failure != nil {
		return f.failure
	}
	if f.unknownZone && read != "AccountRefusals" && period.ZoneName() != "UTC" {
		return fmt.Errorf("unable to read: %w", &pgconn.PgError{Code: "22023"})
	}
	return nil
}

func (f *fakePages) UsageTotals(_ context.Context, scope usagestore.Scope, period usagestore.Period) (*usagestore.UsageTotals, error) {
	if err := f.asked("UsageTotals", scope, period, 0); err != nil {
		return nil, err
	}
	totals := f.totals
	return &totals, nil
}

func (f *fakePages) UsageDays(_ context.Context, scope usagestore.Scope, period usagestore.Period) ([]usagestore.UsageDay, error) {
	return f.days, f.asked("UsageDays", scope, period, 0)
}

func (f *fakePages) UsageJobs(_ context.Context, accountId string, period usagestore.Period) ([]usagestore.JobUsage, error) {
	return f.jobs, f.asked("UsageJobs", usagestore.Scope{AccountId: accountId}, period, 0)
}

func (f *fakePages) UsageErrors(_ context.Context, accountId string, period usagestore.Period) ([]usagestore.CategoryCount, error) {
	return f.errors, f.asked("UsageErrors", usagestore.Scope{AccountId: accountId}, period, 0)
}

func (f *fakePages) AccountRefusals(_ context.Context, accountId string, period usagestore.Period) ([]usagestore.GateCount, error) {
	return f.refusals, f.asked("AccountRefusals", usagestore.Scope{AccountId: accountId}, period, 0)
}

func (f *fakePages) LatestRuns(_ context.Context, scope usagestore.Scope, period usagestore.Period, limit int32) ([]usagestore.RunRow, error) {
	return f.runs, f.asked("LatestRuns", scope, period, limit)
}

func (f *fakePages) reads() []string {
	reads := make([]string, 0, len(f.calls))
	for _, call := range f.calls {
		reads = append(reads, call.read)
	}
	return reads
}

type pagesFixture struct {
	svc     *Service
	pages   *fakePages
	querier *db_queries.MockQuerier
}

// usagePages builds a service whose caller may view the account, or not, over the store given.
func usagePagesOver(t *testing.T, mayView bool, pages pageStore) (*Service, *db_queries.MockQuerier) {
	t.Helper()
	enforcer := userdata.NewMockEntityEnforcer(t)
	if mayView {
		enforcer.On("EnforceAccount", mock.Anything, userdata.NewIdentifier(anAccountId), rbac.AccountAction_View).Return(nil)
	} else {
		enforcer.On("EnforceAccount", mock.Anything, userdata.NewIdentifier(anAccountId), rbac.AccountAction_View).
			Return(connect.NewError(connect.CodePermissionDenied, errors.New("no access")))
	}
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).
		Return(userdatatest.NewUser(t, testutil.NewFakeEELicense(testutil.WithIsValid()), enforcer), nil)
	querier := db_queries.NewMockQuerier(t)
	svc := newService(
		&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), querier), users, nil, nil, pages, nil, nil,
		func() time.Time { return now },
	)
	return svc, querier
}

func usagePages(t *testing.T, mayView bool) *pagesFixture {
	t.Helper()
	pages := &fakePages{}
	svc, querier := usagePagesOver(t, mayView, pages)
	return &pagesFixture{svc: svc, pages: pages, querier: querier}
}

// aJob is a job as the account names it: nothing else of a job is read for the table of jobs.
func aJob(t *testing.T, id, name string) db_queries.ListJobNamesByAccountRow {
	t.Helper()
	jobUuid, err := husonymdb.ToUuid(id)
	require.NoError(t, err)
	return db_queries.ListJobNamesByAccountRow{ID: jobUuid, Name: name}
}

// holdsJobs makes the account of the test hold the jobs, and no other account any.
func (f *pagesFixture) holdsJobs(t *testing.T, jobs ...db_queries.ListJobNamesByAccountRow) {
	t.Helper()
	accountUuid, err := husonymdb.ToUuid(anAccountId)
	require.NoError(t, err)
	f.querier.On("ListJobNamesByAccount", mock.Anything, mock.Anything, accountUuid).Return(jobs, nil).Once()
}

// theJobOfTheAccount is the one read the usage of a job makes of its job: by its id and by the
// account at once.
func theJobOfTheAccount(t *testing.T) db_queries.GetJobKindSourceByAccountParams {
	t.Helper()
	accountUuid, err := husonymdb.ToUuid(anAccountId)
	require.NoError(t, err)
	jobUuid, err := husonymdb.ToUuid(aJobId)
	require.NoError(t, err)
	return db_queries.GetJobKindSourceByAccountParams{ID: jobUuid, AccountID: accountUuid}
}

// holdsTheJob makes the account of the test hold the job asked: a synchronization, or a
// detection of PII. Its jobs are not listed: the querier of the fixture expects no other call.
func (f *pagesFixture) holdsTheJob(t *testing.T, detectsPii bool) {
	t.Helper()
	jobType := &mgmtv1alpha1.JobTypeConfig{}
	if detectsPii {
		jobType.JobType = &mgmtv1alpha1.JobTypeConfig_PiiDetect{PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{}}
	}
	config, err := json.Marshal(jobType)
	require.NoError(t, err)
	f.querier.On("GetJobKindSourceByAccount", mock.Anything, mock.Anything, theJobOfTheAccount(t)).
		Return(db_queries.GetJobKindSourceByAccountRow{
			JobtypeConfig:     config,
			ConnectionOptions: &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{}},
		}, nil).Once()
}

// holdsNoSuchJob makes the job asked be none of the account: it is of another account, or of none.
func (f *pagesFixture) holdsNoSuchJob(t *testing.T) {
	t.Helper()
	f.querier.On("GetJobKindSourceByAccount", mock.Anything, mock.Anything, theJobOfTheAccount(t)).
		Return(db_queries.GetJobKindSourceByAccountRow{}, pgx.ErrNoRows).Once()
}

func on(year int, month time.Month, dayOfMonth int) usagestore.CalendarDay {
	return usagestore.CalendarDay{Year: year, Month: month, Day: dayOfMonth}
}

func dateOn(year, month, dayOfMonth uint32) *mgmtv1alpha1.Date {
	return &mgmtv1alpha1.Date{Year: year, Month: month, Day: dayOfMonth}
}

func seconds(n int64) *int64 { return &n }

// thirtyDays asks for the usage of the account from September 10 to October 9, 2026.
func thirtyDays(zone string) *connect.Request[mgmtv1alpha1.GetAccountUsageRequest] {
	return connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
		AccountId: anAccountId, FromDay: dateOn(2026, 9, 10), ToDay: dateOn(2026, 10, 9), TimeZone: zone,
	})
}

func thirtyDaysOfJob(zone string) *connect.Request[mgmtv1alpha1.GetJobUsageRequest] {
	return connect.NewRequest(&mgmtv1alpha1.GetJobUsageRequest{
		AccountId: anAccountId, JobId: aJobId, FromDay: dateOn(2026, 9, 10), ToDay: dateOn(2026, 10, 9), TimeZone: zone,
	})
}

func Test_GetAccountUsage_IsRefusedWithoutAccessToTheAccount(t *testing.T) {
	f := usagePages(t, false)

	_, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// Nothing was read: neither the runs nor the jobs (the querier of the fixture expects no call).
	require.Empty(t, f.pages.calls)
}

func Test_GetAccountUsage_ReadsTheDaysAskedOfTheAccountAskedInTheZoneAsked(t *testing.T) {
	f := usagePages(t, true)
	f.holdsJobs(t)
	paris, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)

	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays("Europe/Paris"))
	require.NoError(t, err)

	// The last day is the one asked: the store includes it.
	asked := usagestore.Period{From: on(2026, 9, 10), To: on(2026, 10, 9), Zone: paris}
	require.Equal(t, []string{"UsageTotals", "UsageDays", "UsageJobs", "UsageErrors", "AccountRefusals"}, f.pages.reads())
	for _, call := range f.pages.calls {
		require.Equal(t, usagestore.Scope{AccountId: anAccountId}, call.scope, call.read)
		require.Equal(t, asked, call.period, call.read)
	}
	require.Equal(t, "Europe/Paris", res.Msg.GetTimeZone())
}

func Test_GetAccountUsage_AZoneThatIsNotOneReadsAsUtc(t *testing.T) {
	for name, zone := range map[string]string{
		"no zone":               "",
		"UTC itself":            "UTC",
		"a name that is none":   "Nowhere/Land",
		"a path out of zones":   "../../etc/passwd",
		"an offset, not a name": "+02:00",
	} {
		f := usagePages(t, true)
		f.holdsJobs(t)

		res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(zone))
		require.NoError(t, err, name)
		require.Len(t, f.pages.calls, 5, name)
		for _, call := range f.pages.calls {
			require.Equal(t, time.UTC, call.period.Zone, "%s: %s", name, call.read)
		}
		require.Equal(t, "UTC", res.Msg.GetTimeZone(), name)

		job := usagePages(t, true)
		job.holdsNoSuchJob(t)
		ofJob, err := job.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(zone))
		require.NoError(t, err, name)
		require.Equal(t, "UTC", ofJob.Msg.GetTimeZone(), name)
	}
}

// A zone Go knows and the database does not: every read is made again as UTC days, and the
// answer says the days are UTC days.
func Test_GetAccountUsage_AZoneTheDatabaseDoesNotKnowReadsAsUtc(t *testing.T) {
	f := usagePages(t, true)
	f.holdsJobs(t)
	f.pages.unknownZone = true
	f.pages.totals = usagestore.UsageTotals{Runs: 4}

	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays("Europe/Paris"))
	require.NoError(t, err)
	require.Equal(t, "UTC", res.Msg.GetTimeZone())
	require.Equal(t, int64(4), res.Msg.GetTotals().GetRuns())
	// The read that was refused, then all of them in UTC.
	require.Equal(t, []string{"UsageTotals", "UsageTotals", "UsageDays", "UsageJobs", "UsageErrors", "AccountRefusals"}, f.pages.reads())
	for _, call := range f.pages.calls[1:] {
		require.Equal(t, usagestore.Period{From: on(2026, 9, 10), To: on(2026, 10, 9), Zone: time.UTC}, call.period, call.read)
	}

	job := usagePages(t, true)
	job.holdsNoSuchJob(t)
	job.pages.unknownZone = true
	ofJob, err := job.svc.GetJobUsage(t.Context(), thirtyDaysOfJob("Europe/Paris"))
	require.NoError(t, err)
	require.Equal(t, "UTC", ofJob.Msg.GetTimeZone())
	require.Equal(t, []string{"UsageTotals", "UsageTotals", "UsageDays", "LatestRuns"}, job.pages.reads())
}

// The period is refused by the store, before it asks anything of the database: the store of
// this test has none.
func Test_GetAccountUsage_RefusesAPeriodThatIsNotOne(t *testing.T) {
	for name, c := range map[string]struct {
		from, to *mgmtv1alpha1.Date
		sentence string
	}{
		"that ends before it starts":   {dateOn(2026, 10, 9), dateOn(2026, 10, 8), "the period cannot be read: its last day is before its first"},
		"of a day that does not exist": {dateOn(2026, 2, 30), dateOn(2026, 3, 2), "the period cannot be read: one of its days does not exist"},
		"of no day":                    {dateOn(0, 0, 0), dateOn(2026, 3, 2), "the period cannot be read: one of its days does not exist"},
		"of 367 days":                  {dateOn(2026, 1, 1), dateOn(2027, 1, 2), "the period cannot be read: it has more than 366 days"},
	} {
		svc, _ := usagePagesOver(t, true, usagestore.New(nil))
		_, err := svc.GetAccountUsage(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
			AccountId: anAccountId, FromDay: c.from, ToDay: c.to, TimeZone: "Europe/Paris",
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
		require.Equal(t, c.sentence, connectMessage(t, err), name)

		svc, _ = usagePagesOver(t, true, usagestore.New(nil))
		_, err = svc.GetJobUsage(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobUsageRequest{
			AccountId: anAccountId, JobId: aJobId, FromDay: c.from, ToDay: c.to,
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
		require.Equal(t, c.sentence, connectMessage(t, err), name)
	}
}

// A year the database cannot hold is the fault of the request, told in plain words, and no
// failure of the service: the store refuses it before it asks anything of the database.
func Test_GetAccountUsage_RefusesAPeriodOfAYearOutOfReach(t *testing.T) {
	for name, year := range map[string]uint32{
		"long after the calendar of the database": 300000,
		"the largest year a request can write":    4294967295,
		"the year after the last":                 10000,
	} {
		svc, _ := usagePagesOver(t, true, usagestore.New(nil))
		_, err := svc.GetAccountUsage(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
			AccountId: anAccountId, FromDay: dateOn(year, 1, 1), ToDay: dateOn(year, 1, 2), TimeZone: "Europe/Paris",
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
		require.Equal(t, "the period cannot be read: its days are not of the years 1 to 9999", connectMessage(t, err), name)

		svc, _ = usagePagesOver(t, true, usagestore.New(nil))
		_, err = svc.GetJobUsage(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobUsageRequest{
			AccountId: anAccountId, JobId: aJobId, FromDay: dateOn(year, 1, 1), ToDay: dateOn(year, 1, 2),
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
		require.Equal(t, "the period cannot be read: its days are not of the years 1 to 9999", connectMessage(t, err), name)
	}
}

// parisBy is a name of the length given under which this process loads the zone of Paris: where
// the zones are files, a name is a path among them, and a path can be written at any length.
func parisBy(t *testing.T, length int) string {
	t.Helper()
	name := "Europe/" + strings.Repeat("./", (length-len("Europe/Paris"))/2) + "Paris"
	require.Len(t, name, length)
	if _, err := time.LoadLocation(name); err != nil {
		t.Skipf("this machine does not load a zone by a path: %v", err)
	}
	return name
}

// No zone has a name of more than 64 characters: a longer one is not looked for among the zones,
// even when it would be found there, and reads as UTC like any name that is none.
func Test_GetAccountUsage_AZoneNameTooLongReadsAsUtc(t *testing.T) {
	tooLong := parisBy(t, 66)

	f := usagePages(t, true)
	f.holdsJobs(t)
	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(tooLong))
	require.NoError(t, err)
	require.Equal(t, "UTC", res.Msg.GetTimeZone())
	for _, call := range f.pages.calls {
		require.Equal(t, time.UTC, call.period.Zone, call.read)
	}

	job := usagePages(t, true)
	job.holdsNoSuchJob(t)
	ofJob, err := job.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(tooLong))
	require.NoError(t, err)
	require.Equal(t, "UTC", ofJob.Msg.GetTimeZone())
}

// A name of 64 characters is still looked for.
func Test_GetAccountUsage_AZoneNameOfTheLongestLengthIsRead(t *testing.T) {
	longest := parisBy(t, 64)

	f := usagePages(t, true)
	f.holdsJobs(t)
	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(longest))
	require.NoError(t, err)
	require.Equal(t, longest, res.Msg.GetTimeZone())
}

func connectMessage(t *testing.T, err error) string {
	t.Helper()
	var refusal *connect.Error
	require.ErrorAs(t, err, &refusal)
	return refusal.Message()
}

func Test_GetAccountUsage_TakesTheLongestPeriodAndOneDay(t *testing.T) {
	for name, days := range map[string][2]*mgmtv1alpha1.Date{
		"366 days":             {dateOn(2026, 1, 1), dateOn(2027, 1, 1)},
		"one day":              {dateOn(2026, 10, 9), dateOn(2026, 10, 9)},
		"a year after today":   {dateOn(2027, 10, 1), dateOn(2027, 10, 31)},
		"across a leap day":    {dateOn(2028, 2, 28), dateOn(2028, 3, 1)},
		"the days of a decade": {dateOn(2016, 1, 1), dateOn(2016, 1, 31)},
	} {
		f := usagePages(t, true)
		f.holdsJobs(t)
		_, err := f.svc.GetAccountUsage(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetAccountUsageRequest{
			AccountId: anAccountId, FromDay: days[0], ToDay: days[1],
		}))
		require.NoError(t, err, name)
		require.Equal(t, on(int(days[0].GetYear()), time.Month(days[0].GetMonth()), int(days[0].GetDay())), f.pages.calls[0].period.From, name)
		require.Equal(t, on(int(days[1].GetYear()), time.Month(days[1].GetMonth()), int(days[1].GetDay())), f.pages.calls[0].period.To, name)
	}
}

// A read that fails is an error of the service, not a refusal of the request, and nothing of a
// half-read page is answered.
func Test_GetAccountUsage_AReadThatFailsIsNotARefusalOfThePeriod(t *testing.T) {
	away := errors.New("the database is away")

	f := usagePages(t, true)
	f.pages.failure = away
	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.ErrorIs(t, err, away)
	require.Nil(t, res)
	require.NotEqual(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	job := usagePages(t, true)
	job.pages.failure = away
	ofJob, err := job.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(""))
	require.ErrorIs(t, err, away)
	require.Nil(t, ofJob)

	names := usagePages(t, true)
	accountUuid, err := husonymdb.ToUuid(anAccountId)
	require.NoError(t, err)
	names.querier.On("ListJobNamesByAccount", mock.Anything, mock.Anything, accountUuid).Return(nil, away)
	res, err = names.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.ErrorIs(t, err, away)
	require.Nil(t, res)

	// The job that cannot be read is not a job the account does not hold.
	kind := usagePages(t, true)
	kind.querier.On("GetJobKindSourceByAccount", mock.Anything, mock.Anything, theJobOfTheAccount(t)).
		Return(db_queries.GetJobKindSourceByAccountRow{}, away)
	ofJob, err = kind.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(""))
	require.ErrorIs(t, err, away)
	require.Nil(t, ofJob)
}

func Test_GetAccountUsage_GivesTheTotalsTheDaysTheErrorsAndTheRefusals(t *testing.T) {
	f := usagePages(t, true)
	f.holdsJobs(t)
	f.pages.totals = usagestore.UsageTotals{
		Runs: 5, Completed: 2, Canceled: 1, RowsRead: 1143, RowsDiscarded: 5, WithUncountedRows: 1,
		DurationMedian: seconds(90), DurationTotal: seconds(800),
	}
	f.pages.days = []usagestore.UsageDay{
		{Day: on(2026, 10, 5), RowsRead: 7, Runs: 1},
		{Day: on(2026, 10, 6)},
		{Day: on(2026, 10, 7), RowsRead: 1136, Runs: 4},
	}
	f.pages.errors = []usagestore.CategoryCount{
		{Category: "constraint_violated", Count: 2}, {Category: usagestore.ErrorCategoryCanceled, Count: 1},
		{Category: usagestore.ErrorCategoryOther, Count: 1},
	}
	f.pages.refusals = []usagestore.GateCount{{Gate: license.GateJobCap, Count: 1}, {Gate: "rbac", Count: 2}}

	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.NoError(t, err)
	msg := res.Msg

	// The terms of the success rate: 2 completed of 5 runs less 1 canceled.
	require.Equal(t, int64(5), msg.GetTotals().GetRuns())
	require.Equal(t, int64(2), msg.GetTotals().GetRunsCompleted())
	require.Equal(t, int64(1), msg.GetTotals().GetRunsCanceled())
	require.Equal(t, int64(1143), msg.GetTotals().GetRowsRead())
	require.Equal(t, int64(5), msg.GetTotals().GetRowsDiscarded())
	require.Equal(t, int64(1), msg.GetTotals().GetRunsWithUncountedRows())
	// The account tells the time its runs took in all.
	require.NotNil(t, msg.DurationTotalSeconds)
	require.Equal(t, int64(800), msg.GetDurationTotalSeconds())

	require.Len(t, msg.GetDays(), 3)
	require.Equal(t, dateOn(2026, 10, 5), msg.GetDays()[0].GetDay())
	require.Equal(t, int64(7), msg.GetDays()[0].GetRowsRead())
	require.Equal(t, int64(1), msg.GetDays()[0].GetRuns())
	require.Equal(t, dateOn(2026, 10, 6), msg.GetDays()[1].GetDay())
	require.Zero(t, msg.GetDays()[1].GetRowsRead())
	require.Zero(t, msg.GetDays()[1].GetRuns())
	require.Equal(t, dateOn(2026, 10, 7), msg.GetDays()[2].GetDay())
	require.Equal(t, int64(1136), msg.GetDays()[2].GetRowsRead())
	require.Equal(t, int64(4), msg.GetDays()[2].GetRuns())

	require.Len(t, msg.GetErrors(), 3)
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED, msg.GetErrors()[0].GetCategory())
	require.Equal(t, int64(2), msg.GetErrors()[0].GetRuns())
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED, msg.GetErrors()[1].GetCategory())
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER, msg.GetErrors()[2].GetCategory())
	require.Equal(t, int64(1), msg.GetErrors()[2].GetRuns())

	require.Len(t, msg.GetRefusals(), 2)
	require.Equal(t, "job_cap", msg.GetRefusals()[0].GetGate())
	require.Equal(t, int64(1), msg.GetRefusals()[0].GetRefusals())
	require.Equal(t, "rbac", msg.GetRefusals()[1].GetGate())
	require.Equal(t, int64(2), msg.GetRefusals()[1].GetRefusals())
}

func Test_GetAccountUsage_GivesNoDurationWhenNoRunHasAnEnd(t *testing.T) {
	f := usagePages(t, true)
	f.holdsJobs(t, aJob(t, aJobId, "orders"))
	f.pages.totals = usagestore.UsageTotals{Runs: 1}
	f.pages.jobs = []usagestore.JobUsage{{JobId: aJobId, Totals: usagestore.UsageTotals{Runs: 1}}}

	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.NoError(t, err)
	require.Nil(t, res.Msg.DurationTotalSeconds)
	require.Len(t, res.Msg.GetJobs(), 1)
	require.Nil(t, res.Msg.GetJobs()[0].DurationMedianSeconds)
}

// The table of jobs holds the jobs that still exist, in the order of the store, each with its
// name, its kind and what its runs add up to. A job with runs and no row is left out: its runs
// are in the totals of the account all the same.
func Test_GetAccountUsage_NamesTheJobsThatStillExist(t *testing.T) {
	f := usagePages(t, true)
	// The account also holds a job that did not run: it is not listed.
	f.holdsJobs(t, aJob(t, anotherJobId, "scan"), aJob(t, aJobId, "orders"),
		aJob(t, "0b6f1d1e-7a43-4c36-9d6b-3f1b6c0f9a44", "idle"))
	f.pages.totals = usagestore.UsageTotals{Runs: 6, Completed: 3, RowsRead: 1340}
	f.pages.jobs = []usagestore.JobUsage{
		{JobId: aDeletedJob, Kind: usagestore.JobKindSync, Totals: usagestore.UsageTotals{Runs: 1, Completed: 1, RowsRead: 1200, DurationMedian: seconds(600), DurationTotal: seconds(600)}},
		{JobId: aJobId, Kind: usagestore.JobKindSync, Totals: usagestore.UsageTotals{
			Runs: 3, Completed: 1, Canceled: 1, RowsRead: 140, RowsDiscarded: 5, WithUncountedRows: 1,
			DurationMedian: seconds(90), DurationTotal: seconds(200),
		}},
		{JobId: anotherJobId, Kind: usagestore.JobKindPiiDetect, Totals: usagestore.UsageTotals{Runs: 2, Completed: 1}},
	}

	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.NoError(t, err)

	jobs := res.Msg.GetJobs()
	require.Len(t, jobs, 2)
	require.Equal(t, aJobId, jobs[0].GetJobId())
	require.Equal(t, "orders", jobs[0].GetJobName())
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_SYNC, jobs[0].GetKind())
	require.Equal(t, int64(3), jobs[0].GetTotals().GetRuns())
	require.Equal(t, int64(1), jobs[0].GetTotals().GetRunsCompleted())
	require.Equal(t, int64(1), jobs[0].GetTotals().GetRunsCanceled())
	require.Equal(t, int64(140), jobs[0].GetTotals().GetRowsRead())
	require.Equal(t, int64(5), jobs[0].GetTotals().GetRowsDiscarded())
	require.Equal(t, int64(1), jobs[0].GetTotals().GetRunsWithUncountedRows())
	require.Equal(t, int64(90), jobs[0].GetDurationMedianSeconds())

	require.Equal(t, anotherJobId, jobs[1].GetJobId())
	require.Equal(t, "scan", jobs[1].GetJobName())
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_PII_DETECT, jobs[1].GetKind())
	require.Nil(t, jobs[1].DurationMedianSeconds)

	// The runs of the deleted job still count for the account.
	require.Equal(t, int64(6), res.Msg.GetTotals().GetRuns())
	require.Equal(t, int64(1340), res.Msg.GetTotals().GetRowsRead())
}

// The table of jobs tells the kind the runs of each job tell, as the store reads it: nothing of
// a job but its name is read for it.
func Test_GetAccountUsage_TellsTheKindTheRunsOfAJobTell(t *testing.T) {
	f := usagePages(t, true)
	f.holdsJobs(t, aJob(t, aJobId, "orders"), aJob(t, anotherJobId, "made up"))
	f.pages.jobs = []usagestore.JobUsage{
		{JobId: aJobId, Kind: usagestore.JobKindAiGenerate, Totals: usagestore.UsageTotals{Runs: 1}},
		{JobId: anotherJobId, Kind: "of a later version", Totals: usagestore.UsageTotals{Runs: 1}},
	}

	res, err := f.svc.GetAccountUsage(t.Context(), thirtyDays(""))
	require.NoError(t, err)
	require.Len(t, res.Msg.GetJobs(), 2)
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_AI_GENERATE, res.Msg.GetJobs()[0].GetKind())
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, res.Msg.GetJobs()[1].GetKind())
}

func Test_GetJobUsage_ReadsOnlyTheJobOfTheAccountAsked(t *testing.T) {
	f := usagePages(t, true)
	f.holdsTheJob(t, false)
	paris, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)

	res, err := f.svc.GetJobUsage(t.Context(), thirtyDaysOfJob("Europe/Paris"))
	require.NoError(t, err)

	// Neither the jobs, the errors nor the refusals of the account are read for a job.
	require.Equal(t, []string{"UsageTotals", "UsageDays", "LatestRuns"}, f.pages.reads())
	for _, call := range f.pages.calls {
		require.Equal(t, usagestore.Scope{AccountId: anAccountId, JobId: aJobId}, call.scope, call.read)
		require.Equal(t, usagestore.Period{From: on(2026, 9, 10), To: on(2026, 10, 9), Zone: paris}, call.period, call.read)
	}
	require.Equal(t, int32(20), f.pages.calls[2].limit)
	require.Equal(t, "Europe/Paris", res.Msg.GetTimeZone())
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_SYNC, res.Msg.GetKind())
}

// A job that is not of the account, be it of another account or of none, has no run in the
// account and is not among its jobs: the answer is empty, and is no refusal that would tell the
// job exists elsewhere.
func Test_GetJobUsage_AJobThatIsNotOfTheAccountGivesAnEmptyAnswer(t *testing.T) {
	f := usagePages(t, true)
	// The job is asked by its id and by the account at once: another account's job is not read.
	f.holdsNoSuchJob(t)
	f.pages.days = []usagestore.UsageDay{{Day: on(2026, 10, 9)}}

	res, err := f.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(""))
	require.NoError(t, err)
	msg := res.Msg
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, msg.GetKind())
	require.NotNil(t, msg.GetTotals())
	require.Zero(t, msg.GetTotals().GetRuns())
	require.Zero(t, msg.GetTotals().GetRowsRead())
	require.Nil(t, msg.DurationMedianSeconds)
	require.Empty(t, msg.GetRuns())
	require.Len(t, msg.GetDays(), 1)
	require.Zero(t, msg.GetDays()[0].GetRuns())
}

// The kind is the one of the job, so that a job that has not run in the period tells it too.
func Test_GetJobUsage_TellsTheKindOfAJobThatDidNotRun(t *testing.T) {
	f := usagePages(t, true)
	f.holdsTheJob(t, true)

	res, err := f.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(""))
	require.NoError(t, err)
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_PII_DETECT, res.Msg.GetKind())
	require.Zero(t, res.Msg.GetTotals().GetRuns())
}

func Test_GetJobUsage_TellsTheTotalsAndEachRun(t *testing.T) {
	f := usagePages(t, true)
	f.holdsTheJob(t, false)
	f.pages.totals = usagestore.UsageTotals{
		Runs: 3, Completed: 1, Canceled: 0, RowsRead: 140, RowsDiscarded: 5, WithUncountedRows: 1,
		DurationMedian: seconds(90), DurationTotal: seconds(180),
	}
	f.pages.days = []usagestore.UsageDay{{Day: on(2026, 10, 9), RowsRead: 140, Runs: 3}}
	failedAt := began.Add(2 * time.Minute)
	f.pages.runs = []usagestore.RunRow{
		{RunId: "settled", Status: usagestore.StatusTerminated, StartedAt: began,
			Error: usagestore.RunError{Category: usagestore.ErrorCategoryOther, Step: usagestore.ErrorStepOther}},
		{RunId: "failed", Status: usagestore.StatusFailed, StartedAt: began, EndedAt: &failedAt,
			RowsRead: 40, TablesUncounted: 1,
			Error: usagestore.RunError{Category: "constraint_violated", Step: "table_sync"}},
		{RunId: "ok", Status: usagestore.StatusCompleted, StartedAt: began, EndedAt: &ended, RowsRead: 100},
	}

	res, err := f.svc.GetJobUsage(t.Context(), thirtyDaysOfJob(""))
	require.NoError(t, err)
	msg := res.Msg

	require.Equal(t, int64(3), msg.GetTotals().GetRuns())
	require.Equal(t, int64(1), msg.GetTotals().GetRunsCompleted())
	require.Equal(t, int64(140), msg.GetTotals().GetRowsRead())
	require.Equal(t, int64(5), msg.GetTotals().GetRowsDiscarded())
	require.Equal(t, int64(1), msg.GetTotals().GetRunsWithUncountedRows())
	require.Equal(t, int64(90), msg.GetDurationMedianSeconds())
	require.Len(t, msg.GetDays(), 1)
	require.Equal(t, dateOn(2026, 10, 9), msg.GetDays()[0].GetDay())
	require.Equal(t, int64(140), msg.GetDays()[0].GetRowsRead())

	runs := msg.GetRuns()
	require.Len(t, runs, 3)
	require.Equal(t, "settled", runs[0].GetRunId())
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TERMINATED, runs[0].GetStatus())
	require.Equal(t, timestamppb.New(began), runs[0].GetStartedAt())
	require.Nil(t, runs[0].GetEndedAt(), "settled without a known end")
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER, runs[0].GetErrorCategory())
	require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER, runs[0].GetErrorStep())

	require.Equal(t, "failed", runs[1].GetRunId())
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_FAILED, runs[1].GetStatus())
	require.Equal(t, timestamppb.New(failedAt), runs[1].GetEndedAt())
	require.Equal(t, int64(40), runs[1].GetRowsRead())
	require.Equal(t, int64(1), runs[1].GetTablesUncounted())
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED, runs[1].GetErrorCategory())
	require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_TABLE_SYNC, runs[1].GetErrorStep())

	require.Equal(t, "ok", runs[2].GetRunId())
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE, runs[2].GetStatus())
	require.Equal(t, int64(100), runs[2].GetRowsRead())
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED, runs[2].GetErrorCategory())
	require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED, runs[2].GetErrorStep())
}

func Test_runStatusOf_KnowsEveryStatusOfTheStore(t *testing.T) {
	seen := map[mgmtv1alpha1.JobRunStatus]usagestore.Status{}
	for _, status := range usagestore.Statuses() {
		told := runStatusOf(status)
		require.NotEqual(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_UNSPECIFIED, told, status)
		require.NotContains(t, seen, told, "%s and %s read as the same status", status, seen[told])
		seen[told] = status
	}
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE, runStatusOf(usagestore.StatusCompleted))
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_FAILED, runStatusOf(usagestore.StatusFailed))
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_CANCELED, runStatusOf(usagestore.StatusCanceled))
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TERMINATED, runStatusOf(usagestore.StatusTerminated))
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TIMED_OUT, runStatusOf(usagestore.StatusTimedOut))
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING, runStatusOf(usagestore.StatusRunning))
	require.Equal(t, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_UNSPECIFIED, runStatusOf("nonsense"))
}

// The error a run is told with is the member of the enum the worker told it by: what toldError
// reads of a member, toldCategory and toldStep give back.
func Test_toldCategory_IsTheMemberTheWorkerTold(t *testing.T) {
	for _, name := range telemetry.ErrorCategories {
		category := toldCategory(usagestore.ErrorCategory(name))
		require.NotEqual(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED, category, name)
		back := toldError(&mgmtv1alpha1.RecordRunEndedRequest{ErrorCategory: category})
		require.Equal(t, usagestore.ErrorCategory(name), back.Category)
	}
	for _, name := range telemetry.ErrorSteps {
		step := toldStep(usagestore.ErrorStep(name))
		require.NotEqual(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED, step, name)
		back := toldError(&mgmtv1alpha1.RecordRunEndedRequest{ErrorStep: step})
		require.Equal(t, usagestore.ErrorStep(name), back.Step)
	}
	// Every member of the two enums is reached.
	require.Len(t, telemetry.ErrorCategories, len(mgmtv1alpha1.RunErrorCategory_name)-1)
	require.Len(t, telemetry.ErrorSteps, len(mgmtv1alpha1.RunErrorStep_name)-1)

	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED, toldCategory(""))
	require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED, toldStep(""))
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER, toldCategory("nonsense"))
	require.Equal(t, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER, toldStep("nonsense"))
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER, toldCategory("unspecified"))
}

func Test_kindOf_KnowsEveryKindOfTheStore(t *testing.T) {
	seen := map[mgmtv1alpha1.JobKind]usagestore.JobKind{}
	for _, kind := range usagestore.JobKinds() {
		told := kindOf(kind)
		require.NotEqual(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, told, kind)
		require.NotContains(t, seen, told, "%s and %s read as the same kind", kind, seen[told])
		seen[told] = kind
	}
	// Every member of the enum is the kind of a run.
	require.Len(t, seen, len(mgmtv1alpha1.JobKind_name)-1)
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_PII_DETECT, kindOf(usagestore.JobKindPiiDetect))
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_AI_GENERATE, kindOf(usagestore.JobKindAiGenerate))
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, kindOf(""))
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, kindOf("nonsense"))
	require.Equal(t, mgmtv1alpha1.JobKind_JOB_KIND_UNSPECIFIED, kindOf("unspecified"))
}
