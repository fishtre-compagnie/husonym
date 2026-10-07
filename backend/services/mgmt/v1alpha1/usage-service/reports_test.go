package v1alpha1_usageservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagereport"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata/userdatatest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeReports keeps the reports and what became of them.
type fakeReports struct {
	since, lastSent *time.Time
	sendings        []usagestore.ReportSending
	stored          map[time.Time]*usagestore.StoredReport
	// listed is the window the service asked for.
	listedFrom, listedTo time.Time
}

func (f *fakeReports) SendingSince(context.Context) (*time.Time, error) { return f.since, nil }
func (f *fakeReports) LastSentAt(context.Context) (*time.Time, error)   { return f.lastSent, nil }

func (f *fakeReports) ListReportSendings(
	_ context.Context, from, to time.Time,
) ([]usagestore.ReportSending, error) {
	f.listedFrom, f.listedTo = from, to
	return f.sendings, nil
}

func (f *fakeReports) Report(_ context.Context, day time.Time) (*usagestore.StoredReport, error) {
	return f.stored[day], nil
}

// fakeKey is the key in force: its mode, or none.
type fakeKey struct {
	mode license.TelemetryMode
	err  error
}

func (f fakeKey) TelemetryMode(context.Context, time.Time) (license.TelemetryMode, error) {
	return f.mode, f.err
}

var now = time.Date(2026, 10, 7, 15, 30, 0, 0, time.UTC)

type reportingFixture struct {
	svc     *Service
	reports *fakeReports
	periods *fakePeriods
}

// reporting builds a service whose caller may view the account, or not.
func reporting(t *testing.T, key fakeKey, setting string, mayView bool) *reportingFixture {
	t.Helper()
	enforcer := userdata.NewMockEntityEnforcer(t)
	if mayView {
		enforcer.On("EnforceAccount", mock.Anything, mock.Anything, rbac.AccountAction_View).Return(nil)
	} else {
		enforcer.On("EnforceAccount", mock.Anything, mock.Anything, rbac.AccountAction_View).
			Return(connect.NewError(connect.CodePermissionDenied, errors.New("no access")))
	}
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).
		Return(userdatatest.NewUser(t, testutil.NewFakeEELicense(testutil.WithIsValid()), enforcer), nil)
	reports := &fakeReports{stored: map[time.Time]*usagestore.StoredReport{}}
	periods := &fakePeriods{}
	svc := newService(
		&Config{ModeSetting: setting, Diagnostics: true}, nil, users, nil, reports, key, periods,
		func() time.Time { return now },
	)
	return &reportingFixture{svc: svc, reports: reports, periods: periods}
}

func aView() *connect.Request[mgmtv1alpha1.GetUsageReportingRequest] {
	return connect.NewRequest(&mgmtv1alpha1.GetUsageReportingRequest{AccountId: anAccountId})
}

func at(hoursAgo int) *time.Time {
	t := now.Add(-time.Duration(hoursAgo) * time.Hour)
	return &t
}

func day(daysAgo int) time.Time {
	return time.Date(2026, 10, 7-daysAgo, 0, 0, 0, 0, time.UTC)
}

func Test_GetUsageReporting_IsRefusedWithoutAccessToTheAccount(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", false)

	_, err := f.svc.GetUsageReporting(t.Context(), aView())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	_, err = f.svc.GetUsageReport(t.Context(), reportRequest(2026, 10, 6))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func Test_GetUsageReporting_TellsNothingOfModeWithoutAKeyInForce(t *testing.T) {
	f := reporting(t, fakeKey{err: usagereport.ErrNoLicenseInForce}, "", true)
	f.reports.sendings = []usagestore.ReportSending{{Day: day(1), PreparedAt: now}}
	f.reports.since = at(100)

	res, err := f.svc.GetUsageReporting(t.Context(), aView())

	require.NoError(t, err)
	require.Equal(t, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_UNSPECIFIED, res.Msg.GetLicenseMode())
	require.Equal(t, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_UNSPECIFIED, res.Msg.GetMode())
	require.Empty(t, res.Msg.GetReports())
	require.Nil(t, res.Msg.GetSendingSince())
	require.True(t, res.Msg.GetDiagnostics())
}

func Test_GetUsageReporting_FailsWhenTheKeyCannotBeRead(t *testing.T) {
	f := reporting(t, fakeKey{err: errors.New("boom")}, "", true)

	_, err := f.svc.GetUsageReporting(t.Context(), aView())

	require.ErrorContains(t, err, "boom")
}

func Test_GetUsageReporting_TellsTheModeInForce(t *testing.T) {
	tests := []struct {
		name    string
		key     license.TelemetryMode
		setting string
		license mgmtv1alpha1.UsageReportingMode
		mode    mgmtv1alpha1.UsageReportingMode
		below   bool
	}{
		{"online, no setting", license.TelemetryOnline, "", mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE, false},
		{"online, offline", license.TelemetryOnline, "offline", mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_OFFLINE_REPORT, true},
		{"online, off", license.TelemetryOnline, "off", mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_ONLINE, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_NONE, true},
		{"offline report, offline", license.TelemetryOfflineReport, "offline", mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_OFFLINE_REPORT, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_OFFLINE_REPORT, false},
		{"none, whatever", license.TelemetryNone, "bogus", mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_NONE, mgmtv1alpha1.UsageReportingMode_USAGE_REPORTING_MODE_NONE, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := reporting(t, fakeKey{mode: tt.key}, tt.setting, true)

			res, err := f.svc.GetUsageReporting(t.Context(), aView())

			require.NoError(t, err)
			require.Equal(t, tt.license, res.Msg.GetLicenseMode())
			require.Equal(t, tt.mode, res.Msg.GetMode())
			require.Equal(t, tt.below, res.Msg.GetBelowLicense())
		})
	}
}

func Test_GetUsageReporting_AsksTheLastThirtyClosedDays(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)

	_, err := f.svc.GetUsageReporting(t.Context(), aView())

	require.NoError(t, err)
	require.Equal(t, day(30), f.reports.listedFrom)
	require.Equal(t, day(1), f.reports.listedTo)
}

func Test_GetUsageReporting_TellsTheStatusOfEachReport(t *testing.T) {
	since := at(24 * 5)
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
	f.reports.since = since
	sentAt := at(2)
	attempted := at(9)
	f.reports.sendings = []usagestore.ReportSending{
		{Day: day(1), PreparedAt: now},                                        // waiting
		{Day: day(2), PreparedAt: now, LastAttemptAt: attempted, Attempts: 3}, // tried
		{Day: day(3), PreparedAt: now, SentAt: sentAt, Attempts: 1},           // sent
		{Day: day(9), PreparedAt: now},                                        // before sending began
		{Day: day(9), PreparedAt: now, SentAt: sentAt, Attempts: 1},           // sent, whenever
	}

	res, err := f.svc.GetUsageReporting(t.Context(), aView())

	require.NoError(t, err)
	var got []mgmtv1alpha1.UsageReportStatus
	for _, report := range res.Msg.GetReports() {
		got = append(got, report.GetStatus())
	}
	require.Equal(t, []mgmtv1alpha1.UsageReportStatus{
		mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_TO_BE_SENT,
		mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_NOT_SENT,
		mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_SENT,
		mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_KEPT,
		mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_SENT,
	}, got)
	first := res.Msg.GetReports()[0]
	require.EqualValues(t, 2026, first.GetDay().GetYear())
	require.EqualValues(t, 10, first.GetDay().GetMonth())
	require.EqualValues(t, 6, first.GetDay().GetDay())
	require.Nil(t, first.GetSentAt())
	require.EqualValues(t, 3, res.Msg.GetReports()[1].GetAttempts())
	require.True(t, res.Msg.GetReports()[2].GetSentAt().AsTime().Equal(*sentAt))
}

func Test_GetUsageReporting_KeepsEveryReportWhenTheModeIsNotOnline(t *testing.T) {
	for _, setting := range []string{"offline", "off"} {
		f := reporting(t, fakeKey{mode: license.TelemetryOnline}, setting, true)
		// Left over from before the operator lowered the mode: it is not the sending in force.
		f.reports.since = at(100)
		f.reports.sendings = []usagestore.ReportSending{
			{Day: day(1), PreparedAt: now},
			{Day: day(2), PreparedAt: now, Attempts: 2, LastAttemptAt: at(30)},
		}

		res, err := f.svc.GetUsageReporting(t.Context(), aView())

		require.NoError(t, err)
		require.Nil(t, res.Msg.GetSendingSince(), setting)
		require.False(t, res.Msg.GetSilent(), setting)
		for _, report := range res.Msg.GetReports() {
			require.Equal(t, mgmtv1alpha1.UsageReportStatus_USAGE_REPORT_STATUS_KEPT, report.GetStatus(), setting)
		}
	}
}

func Test_GetUsageReporting_TellsTheFirstSendOnlyDuringTheFirstDay(t *testing.T) {
	tests := []struct {
		name   string
		since  *time.Time
		hasOne bool
	}{
		{"just started", at(1), true},
		{"almost a day", at(23), true},
		{"a day", at(24), false},
		{"long ago", at(24 * 3), false},
		{"not sending", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
			f.reports.since = tt.since

			res, err := f.svc.GetUsageReporting(t.Context(), aView())

			require.NoError(t, err)
			require.Equal(t, tt.hasOne, res.Msg.GetFirstSendAt() != nil)
			if tt.hasOne {
				require.True(t, res.Msg.GetFirstSendAt().AsTime().Equal(tt.since.Add(24*time.Hour)))
			}
			require.Equal(t, tt.since != nil, res.Msg.GetSendingSince() != nil)
		})
	}
}

func Test_GetUsageReporting_IsSilentWhenNothingWasSentForThirtyDays(t *testing.T) {
	tests := []struct {
		name     string
		mode     license.TelemetryMode
		since    *time.Time
		lastSent *time.Time
		silent   bool
	}{
		{"sending for long, never sent", license.TelemetryOnline, at(24 * 31), nil, true},
		{"sending for long, sent long ago", license.TelemetryOnline, at(24 * 60), at(24 * 31), true},
		{"sending for long, sent lately", license.TelemetryOnline, at(24 * 60), at(24 * 3), false},
		{"sending for less than thirty days", license.TelemetryOnline, at(24 * 29), nil, false},
		{"not online", license.TelemetryOfflineReport, at(24 * 60), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := reporting(t, fakeKey{mode: tt.mode}, "", true)
			f.reports.since = tt.since
			f.reports.lastSent = tt.lastSent

			res, err := f.svc.GetUsageReporting(t.Context(), aView())

			require.NoError(t, err)
			require.Equal(t, tt.silent, res.Msg.GetSilent())
			require.Equal(t, tt.lastSent != nil, res.Msg.GetLastSentAt() != nil)
		})
	}
}

func reportRequest(year, month, dayOfMonth uint32) *connect.Request[mgmtv1alpha1.GetUsageReportRequest] {
	return connect.NewRequest(&mgmtv1alpha1.GetUsageReportRequest{
		AccountId: anAccountId,
		Day:       &mgmtv1alpha1.Date{Year: year, Month: month, Day: dayOfMonth},
	})
}

func Test_GetUsageReport_GivesTheStoredBytes(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
	// The document is ASCII JSON; its escapes must come back as they were kept.
	document := `{"name":"café \"bar\"","runs":3}`
	f.reports.stored[day(1)] = &usagestore.StoredReport{
		Day: day(1), Document: []byte(document), Seal: "a-seal", KeyFingerprint: "a-fingerprint",
	}

	res, err := f.svc.GetUsageReport(t.Context(), reportRequest(2026, 10, 6))

	require.NoError(t, err)
	require.Equal(t, document, res.Msg.GetDocument())
	require.Equal(t, "a-seal", res.Msg.GetSeal())
	require.Equal(t, "a-fingerprint", res.Msg.GetKeyFingerprint())
}

func Test_GetUsageReport_IsNotFoundForADayWithoutReport(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)

	_, err := f.svc.GetUsageReport(t.Context(), reportRequest(2026, 10, 6))

	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func Test_GetUsageReport_RefusesADayThatIsNotOne(t *testing.T) {
	tests := []struct {
		name                string
		year, month, dayNum uint32
	}{
		{"no day", 0, 0, 0},
		{"no year", 0, 10, 6},
		{"month 13", 2026, 13, 1},
		{"day 0", 2026, 10, 0},
		{"31 September", 2026, 9, 31},
		{"29 February of a common year", 2026, 2, 29},
		{"tomorrow", 2026, 10, 8},
		{"next year", 2027, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)

			_, err := f.svc.GetUsageReport(t.Context(), reportRequest(tt.year, tt.month, tt.dayNum))

			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

func Test_GetUsageReport_AcceptsTodayAndALeapDay(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)

	_, err := f.svc.GetUsageReport(t.Context(), reportRequest(2026, 10, 7))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	_, err = f.svc.GetUsageReport(t.Context(), reportRequest(2024, 2, 29))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}
