package usagereport

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// sendNow is when the passes of these tests run; the last closed day is the 9th.
var sendNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func dayOf(text string) time.Time {
	day, err := time.Parse(time.DateOnly, text)
	if err != nil {
		panic(err)
	}
	return day
}

type sendingRow struct {
	report        usagestore.StoredReport
	attempts      int32
	lastAttemptAt *time.Time
	sentAt        *time.Time
}

// fakeSendingStore keeps the sending state the way the usage store does: a claim takes the
// oldest report that is due and counts the attempt.
type fakeSendingStore struct {
	mu    sync.Mutex
	since *time.Time
	// sentBefore is when a report that is no longer kept was last sent.
	sentBefore *time.Time
	rows       []*sendingRow
	calls      []string
	markErr    error
	listErr    error
	// panicOnStart makes StartSending panic.
	panicOnStart bool
	// hang makes SendingSince wait for its context to end, then return what ended it.
	hang bool
	// onMark is called when a report is about to be marked.
	onMark func()
}

// withReports adds the reports of the given days, each prepared two minutes after its day closed.
func (f *fakeSendingStore) withReports(days ...string) *fakeSendingStore {
	for _, text := range days {
		f.withReport(text, dayOf(text).Add(24*time.Hour+2*time.Minute))
	}
	return f
}

func (f *fakeSendingStore) withReport(day string, preparedAt time.Time) *fakeSendingStore {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, &sendingRow{report: usagestore.StoredReport{
		Day: dayOf(day), Document: []byte(`{"day": "` + day + `"}` + "\n"), Seal: "seal-" + day, KeyFingerprint: "fp",
		PreparedAt: preparedAt,
	}})
	return f
}

// sendingSince is an instance that sends since the given moment and has sent a report since:
// none of its reports waits.
func (f *fakeSendingStore) sendingSince(at time.Time) *fakeSendingStore {
	return f.startedSending(at).lastSent(at)
}

// startedSending is an instance that sends since the given moment and has sent nothing since.
func (f *fakeSendingStore) startedSending(at time.Time) *fakeSendingStore {
	f.since = &at
	return f
}

func (f *fakeSendingStore) lastSent(at time.Time) *fakeSendingStore {
	f.sentBefore = &at
	return f
}

func (f *fakeSendingStore) row(day string) *sendingRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.report.Day.Equal(dayOf(day)) {
			return row
		}
	}
	return nil
}

func (f *fakeSendingStore) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeSendingStore) SendingSince(ctx context.Context) (*time.Time, error) {
	if f.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.since, nil
}

func (f *fakeSendingStore) StartSending(_ context.Context, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panicOnStart {
		panic("a value that must not be logged")
	}
	f.calls = append(f.calls, "start")
	if f.since == nil {
		f.since = &at
	}
	return nil
}

func (f *fakeSendingStore) StopSending(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "stop")
	f.since = nil
	return nil
}

func (f *fakeSendingStore) LastSentAt(context.Context) (*time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.sentBefore
	for _, row := range f.rows {
		if row.sentAt != nil && (last == nil || row.sentAt.After(*last)) {
			last = row.sentAt
		}
	}
	return last, nil
}

func (f *fakeSendingStore) ClaimReport(_ context.Context, claim usagestore.ReportClaim) (*usagestore.StoredReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "claim")
	slices.SortFunc(f.rows, func(a, b *sendingRow) int { return a.report.Day.Compare(b.report.Day) })
	for _, row := range f.rows {
		if row.sentAt != nil || row.report.Day.Before(claim.From) || row.report.Day.After(claim.To) {
			continue
		}
		if row.lastAttemptAt != nil && !row.lastAttemptAt.Before(claim.NotAttemptedSince) {
			continue
		}
		if claim.PreparedBy != nil && row.report.PreparedAt.After(*claim.PreparedBy) {
			continue
		}
		row.attempts++
		row.lastAttemptAt = &claim.At
		report := row.report
		return &report, nil
	}
	return nil, nil //nolint:nilnil // none due
}

func (f *fakeSendingStore) MarkReportSent(ctx context.Context, day, at time.Time) error {
	if f.onMark != nil {
		f.onMark()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Like the database: nothing is written under a context that ended.
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return errors.New("the marking is not bounded")
	}
	if f.markErr != nil {
		return f.markErr
	}
	for _, row := range f.rows {
		if row.report.Day.Equal(day) && row.sentAt == nil {
			row.sentAt = &at
		}
	}
	return nil
}

func (f *fakeSendingStore) ListReportSendings(ctx context.Context, from, to time.Time) ([]usagestore.ReportSending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	var sendings []usagestore.ReportSending
	for _, row := range f.rows {
		if row.report.Day.Before(from) || row.report.Day.After(to) {
			continue
		}
		sendings = append(sendings, usagestore.ReportSending{
			Day: row.report.Day, SentAt: row.sentAt, LastAttemptAt: row.lastAttemptAt, Attempts: row.attempts,
		})
	}
	return sendings, nil
}

// fakeTransport keeps the days it was given, and fails those it is told to.
type fakeTransport struct {
	mu      sync.Mutex
	posted  []string
	failing map[string]error
	// hang makes Post wait for its context to end, then return what ended it.
	hang bool
	// afterPost is called once a report was taken, before Post returns.
	afterPost func()
}

func (f *fakeTransport) Post(ctx context.Context, report *usagestore.StoredReport) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	day := report.Day.Format(time.DateOnly)
	f.posted = append(f.posted, day)
	if f.hang {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.afterPost != nil {
		f.afterPost()
	}
	return f.failing[day]
}

func (f *fakeTransport) days() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.posted)
}

type fakeKeyMode struct {
	mode  license.TelemetryMode
	err   error
	calls int
}

func (f *fakeKeyMode) TelemetryMode(context.Context, time.Time) (license.TelemetryMode, error) {
	f.calls++
	return f.mode, f.err
}

type sending struct {
	store     *fakeSendingStore
	license   *fakeLicense
	key       *fakeKeyMode
	setting   string
	transport *fakeTransport
	logs      *syncBuffer
}

// newSending is an instance whose key provides for sending, and that has no setting.
func newSending(store *fakeSendingStore) *sending {
	return &sending{
		store:     store,
		license:   &fakeLicense{inForce: true},
		key:       &fakeKeyMode{mode: license.TelemetryOnline},
		transport: &fakeTransport{failing: map[string]error{}},
		logs:      &syncBuffer{},
	}
}

func (s *sending) sender() *Sender {
	return NewSender(s.store, s.license, s.key, s.setting, s.transport, slog.New(slog.NewTextHandler(s.logs, nil)))
}

// sendDue makes one pass at the given moment, on a clock that stays at it.
func (s *sending) sendDue(t *testing.T, at time.Time) error {
	t.Helper()
	return sendDueAt(t.Context(), s.sender(), at)
}

func sendDueAt(ctx context.Context, sender *Sender, at time.Time) error {
	sender.now = func() time.Time { return at }
	return sender.SendDue(ctx, at)
}

func Test_SendDue_WithoutALicenseInForceDoesNothing(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.license.inForce = false

	require.NoError(t, s.sendDue(t, sendNow))
	require.Zero(t, s.key.calls)
	require.Empty(t, s.store.called())
	require.Empty(t, s.transport.days())
	require.NotNil(t, s.store.since)
}

func Test_SendDue_WithoutAKeyInForceDoesNothing(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.key.err = ErrNoLicenseInForce

	require.NoError(t, s.sendDue(t, sendNow))
	require.Empty(t, s.store.called())
	require.Empty(t, s.transport.days())
}

func Test_SendDue_AKeyThatCannotBeReadIsAnErrorAndNothingIsSent(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.key.err = errors.New("boom")

	require.ErrorContains(t, s.sendDue(t, sendNow), "boom")
	require.Empty(t, s.store.called())
	require.Empty(t, s.transport.days())
}

func Test_SendDue_AModeThatIsNotOnlineStopsSendingAndSendsNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		key     license.TelemetryMode
		setting string
	}{
		"the key provides for a report file":   {key: license.TelemetryOfflineReport},
		"the key provides for nothing":         {key: license.TelemetryNone},
		"the setting keeps it for a file":      {key: license.TelemetryOnline, setting: "offline"},
		"the setting keeps it":                 {key: license.TelemetryOnline, setting: "off"},
		"the setting, whatever its case":       {key: license.TelemetryOnline, setting: " OFF "},
		"a report file, and the setting keeps": {key: license.TelemetryOfflineReport, setting: "off"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
			s.key.mode, s.setting = tc.key, tc.setting

			require.NoError(t, s.sendDue(t, sendNow))
			require.Equal(t, []string{"stop"}, s.store.called())
			require.Nil(t, s.store.since)
			require.Empty(t, s.transport.days())
		})
	}
}

func Test_SendDue_ASettingThatIsNeitherOfflineNorOffCountsAsNone(t *testing.T) {
	for _, setting := range []string{"", "on", "online", "true", "none"} {
		t.Run(setting, func(t *testing.T) {
			s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
			s.setting = setting

			require.NoError(t, s.sendDue(t, sendNow))
			require.Equal(t, []string{"2026-10-09"}, s.transport.days())
		})
	}
}

func Test_SendDue_AnInstanceThatStartsSendingRecordsSinceWhenAndKeepsWhatWasPreparedBefore(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-08", "2026-10-09"))

	require.NoError(t, s.sendDue(t, sendNow))
	require.NotNil(t, s.store.since)
	require.True(t, sendNow.Equal(*s.store.since))
	require.Equal(t, "start", s.store.called()[0])
	require.Empty(t, s.transport.days())

	// However long after: the first date is kept, and the reports of before are never taken.
	require.NoError(t, s.sendDue(t, sendNow.Add(72*time.Hour)))
	require.True(t, sendNow.Equal(*s.store.since))
	require.Empty(t, s.transport.days())
}

// The wait is on the report: an instance that became a sender half an hour into a day does not
// send the report of that day in the pass that prepares it, but a day after it exists.
func Test_SendDue_TheFirstReportWaitsADayOnceItExists(t *testing.T) {
	since := time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC)
	prepared := time.Date(2026, 10, 10, 0, 32, 0, 0, time.UTC)
	s := newSending((&fakeSendingStore{}).startedSending(since).withReport("2026-10-09", prepared))

	// The pass that prepared it, then 23 hours later, then one second short of a day.
	for _, at := range []time.Time{prepared, prepared.Add(23 * time.Hour), prepared.Add(24*time.Hour - time.Second)} {
		require.NoError(t, s.sendDue(t, at))
		require.Empty(t, s.transport.days(), "at %s", at)
		require.Zero(t, s.store.row("2026-10-09").attempts, "at %s", at)
	}

	require.NoError(t, s.sendDue(t, prepared.Add(24*time.Hour)))
	require.Equal(t, []string{"2026-10-09"}, s.transport.days())

	// A first report was sent: the one of the next day goes in the pass that follows its preparation.
	next := time.Date(2026, 10, 11, 0, 34, 0, 0, time.UTC)
	s.store.withReport("2026-10-10", next)
	require.NoError(t, s.sendDue(t, next))
	require.Equal(t, []string{"2026-10-09", "2026-10-10"}, s.transport.days())
}

// Each state the wait can be in, at a pass on the 10th at noon: the last closed day is the 9th.
func Test_SendDue_WhichReportsWaitForTheFirstSending(t *testing.T) {
	type report struct {
		day        string
		preparedAt time.Time
	}
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC) }
	aDayAgo := sendNow.Add(-24 * time.Hour)

	for name, tc := range map[string]struct {
		since    time.Time
		lastSent *time.Time
		reports  []report
		failing  string
		want     []string
	}{
		"nothing sent and no report yet": {
			since: at(10, 0, 30),
		},
		"nothing sent and only reports of before sending began": {
			since:   at(10, 0, 30),
			reports: []report{{"2026-10-08", at(9, 0, 2)}, {"2026-10-09", at(10, 0, 2)}},
		},
		"a first report prepared less than a day ago": {
			since:   at(8, 0, 30),
			reports: []report{{"2026-10-08", aDayAgo.Add(time.Second)}},
		},
		"a first report prepared a day ago": {
			since:   at(8, 0, 30),
			reports: []report{{"2026-10-08", aDayAgo}},
			want:    []string{"2026-10-08"},
		},
		"several unsent, none a day old": {
			since:   at(8, 0, 30),
			reports: []report{{"2026-10-08", aDayAgo.Add(time.Minute)}, {"2026-10-09", at(10, 0, 2)}},
		},
		// Once the oldest left, a report was sent since sending began: the rest go as they are due.
		"several unsent, only the oldest a day old": {
			since:   at(7, 0, 30),
			reports: []report{{"2026-10-07", at(8, 0, 2)}, {"2026-10-08", aDayAgo.Add(time.Minute)}, {"2026-10-09", at(10, 0, 2)}},
			want:    []string{"2026-10-07", "2026-10-08", "2026-10-09"},
		},
		// The oldest did not leave: nothing was sent, and the others still wait.
		"several unsent, the oldest a day old and it fails": {
			since:   at(7, 0, 30),
			reports: []report{{"2026-10-07", at(8, 0, 2)}, {"2026-10-08", aDayAgo.Add(time.Minute)}, {"2026-10-09", at(10, 0, 2)}},
			failing: "2026-10-07",
			want:    []string{"2026-10-07"},
		},
		"a report sent since sending began: the next one does not wait": {
			since:    at(5, 0, 30),
			lastSent: new(at(9, 0, 40)),
			reports:  []report{{"2026-10-09", at(10, 0, 2)}},
			want:     []string{"2026-10-09"},
		},
		"a report sent the moment sending began counts as sent since": {
			since:    at(8, 0, 30),
			lastSent: new(at(8, 0, 30)),
			reports:  []report{{"2026-10-09", at(10, 0, 2)}},
			want:     []string{"2026-10-09"},
		},
		// Back to sending after a time without: what was sent before does not count.
		"the last sending is of before sending began again, the first report is fresh": {
			since:    at(8, 0, 30),
			lastSent: new(at(7, 0, 40)),
			reports:  []report{{"2026-10-08", aDayAgo.Add(time.Second)}},
		},
		"the last sending is of before sending began again, the first report is a day old": {
			since:    at(8, 0, 30),
			lastSent: new(at(7, 0, 40)),
			reports:  []report{{"2026-10-08", aDayAgo}, {"2026-10-09", at(10, 0, 2)}},
			want:     []string{"2026-10-08", "2026-10-09"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := (&fakeSendingStore{}).startedSending(tc.since)
			store.sentBefore = tc.lastSent
			for _, r := range tc.reports {
				store.withReport(r.day, r.preparedAt)
			}
			s := newSending(store)
			if tc.failing != "" {
				s.transport.failing[tc.failing] = errors.New("boom")
			}

			require.NoError(t, s.sendDue(t, sendNow))
			require.Equal(t, tc.want, s.transport.days())
			for _, r := range tc.reports {
				if !slices.Contains(tc.want, r.day) {
					require.Zero(t, s.store.row(r.day).attempts, "the report of %s was claimed", r.day)
				}
			}
		})
	}
}

// A first report that could not be sent does not let the others through: an hour later it is not
// due again, nothing was sent, and the next one is still less than a day old.
func Test_SendDue_AFirstReportThatFailsLeavesTheOthersWaiting(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		startedSending(time.Date(2026, 10, 8, 0, 30, 0, 0, time.UTC)).
		withReports("2026-10-08", "2026-10-09"))
	s.transport.failing["2026-10-08"] = errors.New("boom")

	require.NoError(t, s.sendDue(t, sendNow))
	require.NoError(t, s.sendDue(t, sendNow.Add(time.Hour)))
	require.Equal(t, []string{"2026-10-08"}, s.transport.days())
	require.Zero(t, s.store.row("2026-10-09").attempts)

	// A day after the second was prepared, the first is due again and fails again; at the pass
	// after that the second goes, though nothing was sent yet: it is a day old.
	aDayAfter := time.Date(2026, 10, 11, 0, 2, 0, 0, time.UTC)
	require.NoError(t, s.sendDue(t, aDayAfter))
	require.Equal(t, []string{"2026-10-08", "2026-10-08"}, s.transport.days())
	require.NoError(t, s.sendDue(t, aDayAfter.Add(time.Hour)))
	require.Equal(t, []string{"2026-10-08", "2026-10-08", "2026-10-09"}, s.transport.days())
}

// From a mode that does not send to the one that does: what was prepared before stays, and the
// first report to leave is the one of the day sending started on, a day after it was prepared,
// whatever was sent before the instance stopped sending.
func Test_SendDue_AfterSendingStartsAgainTheReportsOfBeforeStayAndTheFirstWaitsADay(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		withReports("2026-10-07", "2026-10-08", "2026-10-09").
		sendingSince(sendNow.AddDate(0, 0, -5)))

	s.setting = "offline"
	require.NoError(t, s.sendDue(t, sendNow))
	require.Nil(t, s.store.since)

	s.setting = ""
	require.NoError(t, s.sendDue(t, sendNow))
	require.True(t, sendNow.Equal(*s.store.since))
	require.Empty(t, s.transport.days())

	// The day sending started on closes, and its report is prepared two minutes later.
	s.store.withReports("2026-10-10")
	prepared := time.Date(2026, 10, 11, 0, 2, 0, 0, time.UTC)
	for _, at := range []time.Time{prepared, sendNow.Add(24 * time.Hour), prepared.Add(24*time.Hour - time.Minute)} {
		require.NoError(t, s.sendDue(t, at))
		require.Empty(t, s.transport.days(), "at %s", at)
	}

	require.NoError(t, s.sendDue(t, prepared.Add(24*time.Hour)))
	require.Equal(t, []string{"2026-10-10"}, s.transport.days())
	for _, day := range []string{"2026-10-07", "2026-10-08", "2026-10-09"} {
		require.Nil(t, s.store.row(day).sentAt, day)
		require.Zero(t, s.store.row(day).attempts, day)
	}
}

func Test_SendDue_StopsAtTheFirstFailureOfThePass(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		withReports("2026-10-07", "2026-10-08", "2026-10-09").
		sendingSince(sendNow.AddDate(0, 0, -5)))
	s.transport.failing["2026-10-08"] = errors.New("the address did not answer")

	require.NoError(t, s.sendDue(t, sendNow))
	require.Equal(t, []string{"2026-10-07", "2026-10-08"}, s.transport.days())
	require.NotNil(t, s.store.row("2026-10-07").sentAt)
	require.Nil(t, s.store.row("2026-10-08").sentAt)
	require.EqualValues(t, 1, s.store.row("2026-10-08").attempts)
	// The third was not claimed: a claim counts as an attempt.
	require.Zero(t, s.store.row("2026-10-09").attempts)
	require.Nil(t, s.store.row("2026-10-09").lastAttemptAt)

	logs := s.logs.String()
	require.Contains(t, logs, "level=WARN")
	require.Contains(t, logs, "day=2026-10-08")
	require.Contains(t, logs, "attempts=1")
	require.Contains(t, logs, "the address did not answer")
	require.NotContains(t, logs, `{\"day\": \"2026-10-08\"}`)
}

func Test_SendDue_AFailedReportIsTriedAgainSixHoursLater(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		withReports("2026-10-08", "2026-10-09").
		sendingSince(sendNow.AddDate(0, 0, -5)))
	s.transport.failing["2026-10-08"] = errors.New("boom")
	require.NoError(t, s.sendDue(t, sendNow))
	require.Equal(t, []string{"2026-10-08"}, s.transport.days())

	// An hour later the failed one is not due, and the next one goes: a report that fails does
	// not hold the others for more than its pass.
	require.NoError(t, s.sendDue(t, sendNow.Add(time.Hour)))
	require.Equal(t, []string{"2026-10-08", "2026-10-09"}, s.transport.days())

	require.NoError(t, s.sendDue(t, sendNow.Add(6*time.Hour)))
	require.Len(t, s.transport.days(), 2)

	require.NoError(t, s.sendDue(t, sendNow.Add(6*time.Hour+time.Second)))
	require.Equal(t, []string{"2026-10-08", "2026-10-09", "2026-10-08"}, s.transport.days())
	require.Contains(t, s.logs.String(), "attempts=2")
}

func Test_SendDue_SendsNothingOlderThanThirtyDays(t *testing.T) {
	// The 9th is the last closed day: the 10th of September is the thirtieth day back.
	s := newSending((&fakeSendingStore{}).
		withReports("2026-09-09", "2026-09-10", "2026-10-09").
		sendingSince(sendNow.AddDate(0, 0, -90)))

	require.NoError(t, s.sendDue(t, sendNow))
	require.Equal(t, []string{"2026-09-10", "2026-10-09"}, s.transport.days())
	require.Zero(t, s.store.row("2026-09-09").attempts)
}

// Sent, then not marked: the line that tells of the sending is there all the same, a second one
// says the report will leave again, and the pass ends there.
func Test_SendDue_AReportThatLeftAndCannotBeMarkedIsLoggedAsSentAndEndsThePass(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		withReports("2026-10-08", "2026-10-09").
		sendingSince(sendNow.AddDate(0, 0, -5)))
	s.store.markErr = errors.New("boom")

	require.ErrorContains(t, s.sendDue(t, sendNow), "boom")
	require.Equal(t, []string{"2026-10-08"}, s.transport.days())

	logs := s.logs.String()
	require.Contains(t, logs, `level=INFO msg="the usage report of the day is sent" day=2026-10-08`)
	require.Contains(t, logs, `document="{\"day\": \"2026-10-08\"}\n"`)
	require.Contains(t, logs, "level=WARN")
	require.Contains(t, logs, "it will be sent again")
	require.Contains(t, logs, "boom")
}

func Test_SendDue_AReportIsLoggedAsSentBeforeItIsMarked(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	var logsAtMark string
	s.store.onMark = func() { logsAtMark = s.logs.String() }

	require.NoError(t, s.sendDue(t, sendNow))
	require.Contains(t, logsAtMark, "the usage report of the day is sent")
	require.NotNil(t, s.store.row("2026-10-09").sentAt)
}

// The process stops right after the answer: the report that left is marked all the same, and
// is not sent again.
func Test_SendDue_AReportThatLeftIsMarkedEvenAsTheProcessStops(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		withReports("2026-10-08", "2026-10-09").
		sendingSince(sendNow.AddDate(0, 0, -5)))
	ctx, stop := context.WithCancel(t.Context())
	s.transport.afterPost = stop

	_ = sendDueAt(ctx, s.sender(), sendNow)
	require.Equal(t, []string{"2026-10-08"}, s.transport.days())
	require.NotNil(t, s.store.row("2026-10-08").sentAt)
	require.Contains(t, s.logs.String(), "day=2026-10-08")
	require.NotContains(t, s.logs.String(), "it will be sent again")
}

func Test_SendDue_AReportIsClaimedAndMarkedAtTheMomentItIs(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	sender := s.sender()
	readings := 0
	sender.now = func() time.Time {
		readings++
		return sendNow.Add(time.Duration(readings) * time.Minute)
	}

	require.NoError(t, sender.SendDue(t.Context(), sendNow))
	row := s.store.row("2026-10-09")
	require.True(t, sendNow.Add(time.Minute).Equal(*row.lastAttemptAt), "claimed at %s", row.lastAttemptAt)
	require.True(t, sendNow.Add(2*time.Minute).Equal(*row.sentAt), "marked at %s", row.sentAt)
}

func Test_SendDue_AStoreThatDoesNotAnswerEndsThePassWithinItsBound(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.store.hang = true
	sender := s.sender()
	require.Equal(t, 2*time.Minute, sender.passTimeout)
	sender.passTimeout = 20 * time.Millisecond

	started := time.Now()
	require.ErrorIs(t, sendDueAt(t.Context(), sender, sendNow), context.DeadlineExceeded)
	require.Less(t, time.Since(started), 5*time.Second)
	require.Empty(t, s.transport.days())
}

// The bound of the pass ends a request: that is a failure like another, told with its attempts,
// which are read past the bound.
func Test_SendDue_ARequestEndedByTheBoundOfThePassIsLoggedAsAFailure(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.transport.hang = true
	sender := s.sender()
	sender.passTimeout = 20 * time.Millisecond

	require.NoError(t, sendDueAt(t.Context(), sender, sendNow))
	require.Contains(t, s.logs.String(), "level=WARN")
	require.Contains(t, s.logs.String(), "day=2026-10-09")
	require.Contains(t, s.logs.String(), "attempts=1")
}

func Test_SendDue_ARequestEndedByTheProcessStoppingIsNotLoggedAsAFailure(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.transport.failing["2026-10-09"] = context.Canceled
	ctx, stop := context.WithCancel(t.Context())
	s.transport.afterPost = stop

	require.NoError(t, sendDueAt(ctx, s.sender(), sendNow))
	require.Empty(t, s.logs.String())
}

func Test_SendDue_AFailureIsLoggedWithoutItsAttemptsWhenTheyCannotBeRead(t *testing.T) {
	s := newSending((&fakeSendingStore{}).withReports("2026-10-09").sendingSince(sendNow.AddDate(0, 0, -5)))
	s.transport.failing["2026-10-09"] = errors.New("the address did not answer")
	s.store.listErr = errors.New("boom")

	require.NoError(t, s.sendDue(t, sendNow))
	logs := s.logs.String()
	require.Contains(t, logs, "level=WARN")
	require.Contains(t, logs, "day=2026-10-09")
	require.Contains(t, logs, "the address did not answer")
	require.NotContains(t, logs, "attempts")
}

func Test_SendDue_TheWaitAndTheSpacingAreTheOnesOfTheSender(t *testing.T) {
	s := newSending((&fakeSendingStore{}).
		startedSending(time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC)).
		withReports("2026-10-09"))
	s.transport.failing["2026-10-09"] = errors.New("boom")
	sender := s.sender()
	require.Equal(t, WaitBeforeFirst, sender.waitBeforeFirst)
	require.Equal(t, 24*time.Hour, WaitBeforeFirst)
	require.Equal(t, 30, SentDays)
	require.Equal(t, 6*time.Hour, sender.retryAfter)

	sender.waitBeforeFirst, sender.retryAfter = time.Minute, time.Minute
	prepared := s.store.row("2026-10-09").report.PreparedAt
	require.NoError(t, sendDueAt(t.Context(), sender, prepared.Add(time.Minute-time.Second)))
	require.Empty(t, s.transport.days())
	require.NoError(t, sendDueAt(t.Context(), sender, prepared.Add(time.Minute)))
	require.NoError(t, sendDueAt(t.Context(), sender, prepared.Add(2*time.Minute)))
	require.Equal(t, []string{"2026-10-09"}, s.transport.days())
	require.NoError(t, sendDueAt(t.Context(), sender, prepared.Add(2*time.Minute+time.Second)))
	require.Equal(t, []string{"2026-10-09", "2026-10-09"}, s.transport.days())
}

func Test_InstanceKey_TelemetryMode(t *testing.T) {
	online := keyExpiring(testExpiry)
	file := keyExpiring(testExpiry)
	file.Telemetry = string(license.TelemetryOfflineReport)

	for name, tc := range map[string]struct {
		key  license.Key
		now  time.Time
		want license.TelemetryMode
	}{
		"a key that does not say sends":       {key: online, now: reportNow, want: license.TelemetryOnline},
		"a key that provides a report file":   {key: file, now: reportNow, want: license.TelemetryOfflineReport},
		"an expired key still in its grace":   {key: online, now: testExpiry.Add(time.Hour), want: license.TelemetryOnline},
		"a frozen key is not a key in force":  {key: online, now: testExpiry.AddDate(1, 0, 0)},
		"a frozen key that provides anything": {key: file, now: testExpiry.AddDate(1, 0, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			value, ring := mintKey(t, tc.key)
			mode, err := NewInstanceKey(&fakeKeys{value: value}, ring).TelemetryMode(t.Context(), tc.now)
			if tc.want == "" {
				require.ErrorIs(t, err, ErrNoLicenseInForce)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, mode)
		})
	}
}

func Test_InstanceKey_TelemetryMode_WithoutAKeyThereIsNoLicenseInForce(t *testing.T) {
	_, ring := mintKey(t, keyExpiring(testExpiry))
	_, err := NewInstanceKey(&fakeKeys{value: "  "}, ring).TelemetryMode(t.Context(), reportNow)
	require.ErrorIs(t, err, ErrNoLicenseInForce)
}
