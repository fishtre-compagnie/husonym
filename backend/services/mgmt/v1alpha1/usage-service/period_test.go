package v1alpha1_usageservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagereport"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// fakePeriods makes the report for a period, or fails, and keeps what it was asked.
type fakePeriods struct {
	sealed *usagereport.Sealed
	err    error

	calls         int
	from, to, now time.Time
}

func (f *fakePeriods) BuildPeriod(_ context.Context, from, to, now time.Time) (*usagereport.Sealed, error) {
	f.calls++
	f.from, f.to, f.now = from, to, now
	return f.sealed, f.err
}

func periodRequest(from, to string) *connect.Request[mgmtv1alpha1.GetUsagePeriodReportRequest] {
	return connect.NewRequest(&mgmtv1alpha1.GetUsagePeriodReportRequest{
		AccountId: anAccountId, FromMonth: from, ToMonth: to,
	})
}

func Test_GetUsagePeriodReport_IsRefusedWithoutAccessToTheAccount(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", false)

	_, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest("2026-08", "2026-10"))

	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.Zero(t, f.periods.calls)
}

// The document is the one that was sealed, byte for byte, whatever the mode the report of the
// day is sent under.
func Test_GetUsagePeriodReport_GivesTheSealedBytes(t *testing.T) {
	for _, setting := range []string{"", "offline", "off"} {
		for _, mode := range []license.TelemetryMode{license.TelemetryOnline, license.TelemetryOfflineReport, license.TelemetryNone} {
			f := reporting(t, fakeKey{mode: mode}, setting, true)
			// The document is ASCII JSON; its escapes must come back as they were sealed.
			document := `{"from":"2026-08", "to":"2026-10","note":"café \"bar\""}`
			f.periods.sealed = &usagereport.Sealed{Document: []byte(document), Seal: "a-seal", KeyFingerprint: "a-fingerprint"}

			res, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest("2026-08", "2026-10"))

			require.NoError(t, err)
			require.Equal(t, document, res.Msg.GetDocument())
			require.Equal(t, "a-seal", res.Msg.GetSeal())
			require.Equal(t, "a-fingerprint", res.Msg.GetKeyFingerprint())
			require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), f.periods.from)
			require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), f.periods.to)
			require.Equal(t, now, f.periods.now)
		}
	}
}

func Test_GetUsagePeriodReport_NeedsALicenseKeyInForce(t *testing.T) {
	f := reporting(t, fakeKey{err: usagereport.ErrNoLicenseInForce}, "", true)
	f.periods.err = usagereport.ErrNoLicenseInForce

	_, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest("2026-08", "2026-10"))

	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func Test_GetUsagePeriodReport_RefusesAPeriodThatIsNotOne(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
	f.periods.err = fmt.Errorf("%w: its first month is after its last", usagereport.ErrPeriod)

	_, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest("2026-10", "2026-08"))

	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.ErrorContains(t, err, "its first month is after its last")
}

func Test_GetUsagePeriodReport_RefusesAMonthThatIsNotOne(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"", "2026-10"}, {"2026-08", ""}, {"2026-13", "2026-10"}, {"2026-00", "2026-10"}, {"2026-8", "2026-10"},
		{"2026-08", "2026-10-07"}, {"2026-08", "october"}, {"26-08", "2026-10"}, {"2026-08 ", "2026-10"},
		{"2026/08", "2026-10"},
	} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)

			_, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest(tc.from, tc.to))

			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			require.Zero(t, f.periods.calls)
		})
	}
}

func Test_GetUsagePeriodReport_FailsWhenTheReportCannotBeMade(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
	f.periods.err = errors.New("boom")

	_, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest("2026-08", "2026-10"))

	require.ErrorContains(t, err, "boom")
	require.NotEqual(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.NotEqual(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}
