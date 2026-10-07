package v1alpha1_usageservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
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
			// Uneven spacing, escaped quotes and a letter outside ASCII: all must come back as they were sealed.
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
			require.Equal(t, 30*time.Second, f.svc.periodTimeout, "the time a service gives a period by default")
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

// A report that cannot be made is an internal error told in fixed words: why goes to the log of
// the instance, not to the caller.
func Test_GetUsagePeriodReport_FailsWhenTheReportCannotBeMade(t *testing.T) {
	for name, tc := range map[string]struct {
		failure error
		logged  bool
	}{
		"a reading that fails": {errors.New("boom: relation husonym_api.run_usage"), true},
		// The builder already logged what the schema refuses.
		"a document the schema refuses": {usagereport.ErrPeriodNotBuilt, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
			f.periods.err = tc.failure
			output := &bytes.Buffer{}
			ctx := logger_interceptor.SetLoggerContext(t.Context(), slog.New(slog.NewJSONHandler(output, nil)))

			_, err := f.svc.GetUsagePeriodReport(ctx, periodRequest("2026-08", "2026-10"))

			require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
			var answered *connect.Error
			require.ErrorAs(t, err, &answered)
			require.Equal(t, "the report for the period could not be built", answered.Message())
			require.NotContains(t, err.Error(), "boom")
			if tc.logged {
				require.Equal(t, 1, strings.Count(output.String(), `"level":"WARN"`))
				require.Contains(t, output.String(), "boom: relation husonym_api.run_usage")
			} else {
				require.Empty(t, output.String())
			}
		})
	}
}

// blockedPeriods makes nothing: it waits for the request to be given up.
type blockedPeriods struct{}

func (blockedPeriods) BuildPeriod(ctx context.Context, _, _, _ time.Time) (*usagereport.Sealed, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("unable to read the runs of a month: %w", ctx.Err())
}

// The making of a period has its own time: past it the caller is told so, and is not left
// waiting on a database that does not answer.
func Test_GetUsagePeriodReport_GivesUpPastItsDeadline(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
	f.svc.periods = blockedPeriods{}
	f.svc.periodTimeout = 10 * time.Millisecond

	_, err := f.svc.GetUsagePeriodReport(t.Context(), periodRequest("2026-08", "2026-10"))

	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
}

// A caller who gives up is not told the deadline of the instance passed.
func Test_GetUsagePeriodReport_ACallerWhoGivesUpIsNotADeadline(t *testing.T) {
	f := reporting(t, fakeKey{mode: license.TelemetryOnline}, "", true)
	f.svc.periods = blockedPeriods{}
	ctx, cancel := context.WithCancel(
		logger_interceptor.SetLoggerContext(t.Context(), slog.New(slog.DiscardHandler)),
	)
	cancel()

	_, err := f.svc.GetUsagePeriodReport(ctx, periodRequest("2026-08", "2026-10"))

	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}
