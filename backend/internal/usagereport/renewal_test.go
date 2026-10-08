package usagereport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

// heldKey is the key the instance holds, which a test may change, read under a lock.
type heldKey struct {
	mu    sync.Mutex
	value string
	err   error
}

func (h *heldKey) Current(context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.value, h.err
}

type instanceId struct {
	id  string
	err error
}

func (i instanceId) InstanceId(context.Context) (string, error) { return i.id, i.err }

// askedTransport keeps the requests it was asked, and answers each with one license or one error.
type askedTransport struct {
	mu      sync.Mutex
	asks    []*RenewalAsk
	license string
	err     error
	panics  bool
}

func (a *askedTransport) Ask(_ context.Context, ask *RenewalAsk) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asks = append(a.asks, ask)
	if a.panics {
		panic("a value that must not be logged")
	}
	return a.license, a.err
}

func (a *askedTransport) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.asks)
}

// offered keeps the keys that were offered, and gives each one outcome or one error.
type offered struct {
	mu     sync.Mutex
	values []string
	result *licensestore.Result
	err    error
}

func (o *offered) offer(_ context.Context, value string) (*licensestore.Result, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.values = append(o.values, value)
	return o.result, o.err
}

// renewal is a renewer on a clock a test moves, with what it is made of.
type renewal struct {
	held      *heldKey
	transport *askedTransport
	offered   *offered
	logs      *syncBuffer
	clock     time.Time
	renewer   *Renewer
}

// newRenewal gives a renewer of an instance that holds the given key, under the given setting
// of the operator.
func newRenewal(t *testing.T, key license.Key, setting string) *renewal { //nolint:gocritic // hugeParam: a value a test writes in place
	t.Helper()
	value, ring := mintKey(t, key)
	r := &renewal{
		held:      &heldKey{value: value},
		transport: &askedTransport{},
		offered:   &offered{},
		logs:      &syncBuffer{},
		clock:     reportNow,
	}
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.renewer = NewRenewer(
		NewInstanceKey(r.held, ring), instanceId{id: testInstance}, r.transport, r.offered.offer, setting,
		func() time.Time { return r.clock }, logger,
	)
	return r
}

// above tells how many lines the renewer logged above Debug.
func (r *renewal) above() int {
	logs := r.logs.String()
	return strings.Count(logs, "level=INFO") + strings.Count(logs, "level=WARN") + strings.Count(logs, "level=ERROR")
}

func Test_AskIfDue_AsksForTheLicenseInForceInASealedRequest(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.clock = reportNow.Add(300 * time.Millisecond)

	require.NoError(t, r.renewer.AskIfDue(t.Context()))

	require.Equal(t, 1, r.transport.count())
	ask := r.transport.asks[0]
	request, err := telemetry.ParseRenewalRequest(ask.Document)
	require.NoError(t, err)
	require.Equal(t, testLicense, request.LicenseID)
	require.Equal(t, testInstance, request.InstanceID)
	require.Equal(t, "2026-10-07T00:05:12Z", request.RequestedAt)
	require.NoError(t, telemetry.Verify(r.held.value, ask.Document, ask.Seal))
	require.Equal(t, telemetry.KeyFingerprint(r.held.value), ask.KeyFingerprint)
}

func Test_AskIfDue_ALicenseIdSomeoneChoseIsNotWrittenInTheRequest(t *testing.T) {
	key := keyExpiring(testExpiry)
	key.Id = leak + "-license"
	r := newRenewal(t, key, "")

	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 1, r.transport.count())
	require.NotContains(t, string(r.transport.asks[0].Document), leak)
}

func Test_AskIfDue_AsksAgainOnlyOnceItsPeriodElapsed(t *testing.T) {
	day := 24 * time.Hour
	for name, tc := range map[string]struct {
		expiresAt time.Time
		grace     *int
		period    time.Duration
	}{
		"far from its expiry":       {expiresAt: testExpiry, period: 24 * time.Hour},
		"a month and more from it":  {expiresAt: reportNow.Add(33 * day), period: 24 * time.Hour},
		"within thirty days":        {expiresAt: reportNow.Add(30*day - time.Second), period: 6 * time.Hour},
		"the day before its expiry": {expiresAt: reportNow.Add(day), period: 6 * time.Hour},
		"in grace":                  {expiresAt: reportNow.Add(-day), period: 6 * time.Hour},
		"frozen":                    {expiresAt: reportNow.Add(-60 * day), period: 6 * time.Hour},
		"frozen without any grace":  {expiresAt: reportNow.Add(-time.Second), grace: intp(0), period: 6 * time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			key := keyExpiring(tc.expiresAt)
			key.GraceDays = tc.grace
			r := newRenewal(t, key, "")

			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Equal(t, 1, r.transport.count(), "a process that never asked asks")

			r.clock = reportNow.Add(tc.period - time.Second)
			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Equal(t, 1, r.transport.count(), "asked again before the period elapsed")

			r.clock = reportNow.Add(tc.period)
			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Equal(t, 2, r.transport.count(), "not asked again once the period elapsed")

			// Counted from the last ask, not from the first.
			r.clock = reportNow.Add(2*tc.period - time.Second)
			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Equal(t, 2, r.transport.count())
		})
	}
}

// The period is the one of the key held when the pass runs: a license that nears its expiry
// between two asks is asked for sooner.
func Test_AskIfDue_ThePeriodFollowsTheStateOfTheLicense(t *testing.T) {
	r := newRenewal(t, keyExpiring(reportNow.Add(30*24*time.Hour+time.Hour)), "")

	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	r.clock = reportNow.Add(6 * time.Hour)
	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 2, r.transport.count())
}

func Test_AskIfDue_AsksOnlyInTheModeThatSends(t *testing.T) {
	for name, tc := range map[string]struct {
		telemetry, setting string
		asks               int
	}{
		"online by default":                    {asks: 1},
		"online":                               {telemetry: "online", asks: 1},
		"a setting that lowers nothing":        {setting: "online", asks: 1},
		"a key that provides a report file":    {telemetry: "offline_report"},
		"a key that provides nothing":          {telemetry: "none"},
		"an operator who set offline":          {setting: "offline"},
		"an operator who set off":              {setting: " OFF "},
		"a setting that cannot raise the mode": {telemetry: "none", setting: "online"},
	} {
		t.Run(name, func(t *testing.T) {
			key := keyExpiring(reportNow.Add(time.Hour))
			key.Telemetry = tc.telemetry
			r := newRenewal(t, key, tc.setting)
			r.transport.license = "a-license"

			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Equal(t, tc.asks, r.transport.count())
			require.Len(t, r.offered.values, tc.asks)
			if tc.asks == 0 {
				require.Empty(t, r.logs.String())
			}
		})
	}
}

// A mode that does not send is not an ask: the instance asks as soon as it sends again.
func Test_AskIfDue_AModeThatDoesNotSendDoesNotCountAsAnAsk(t *testing.T) {
	offline := keyExpiring(testExpiry)
	offline.Telemetry = "offline_report"
	r := newRenewal(t, offline, "")
	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Zero(t, r.transport.count())

	r.held.value, _ = mintKey(t, keyExpiring(testExpiry))
	r.clock = reportNow.Add(time.Minute)
	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 1, r.transport.count())
}

func Test_AskIfDue_WithoutAKeyNothingIsAsked(t *testing.T) {
	for name, value := range map[string]string{"none": "", "only spaces": " \n"} {
		t.Run(name, func(t *testing.T) {
			r := newRenewal(t, keyExpiring(testExpiry), "")
			r.held.value = value

			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Zero(t, r.transport.count())
			require.Empty(t, r.logs.String())
		})
	}
}

func Test_AskIfDue_AKeyThatCannotBeReadIsAnErrorAndNothingIsAsked(t *testing.T) {
	t.Run("a store that does not answer", func(t *testing.T) {
		r := newRenewal(t, keyExpiring(testExpiry), "")
		r.held.err = errors.New("boom")

		require.ErrorContains(t, r.renewer.AskIfDue(t.Context()), "boom")
		require.Zero(t, r.transport.count())
	})
	t.Run("a key the ring does not verify", func(t *testing.T) {
		r := newRenewal(t, keyExpiring(testExpiry), "")
		held := r.held.value
		r.held.value = "not-a-key"

		err := r.renewer.AskIfDue(t.Context())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "not-a-key")
		require.Zero(t, r.transport.count())

		// Nothing was asked: the instance asks as soon as its key reads again.
		r.held.value = held
		require.NoError(t, r.renewer.AskIfDue(t.Context()))
		require.Equal(t, 1, r.transport.count())
	})
}

func Test_AskIfDue_AnInstanceIdThatCannotBeReadIsAnErrorAndIsNotAnAsk(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.renewer.instance = instanceId{err: errors.New("boom")}

	require.ErrorContains(t, r.renewer.AskIfDue(t.Context()), "boom")
	require.Zero(t, r.transport.count())

	r.renewer.instance = instanceId{id: testInstance}
	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 1, r.transport.count())
}

func Test_AskIfDue_NothingToGiveOffersNothingAndSaysNothingAboveDebug(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")

	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 1, r.transport.count())
	require.Empty(t, r.offered.values)
	require.Zero(t, r.above())
	require.Contains(t, r.logs.String(), "level=DEBUG")
}

func Test_AskIfDue_AKeyReceivedIsOfferedAsItCameAndItsAcceptanceIsSaidOnce(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.license = "the-license-received"
	r.offered.result = &licensestore.Result{Outcome: licensestore.Accepted, Key: &license.Key{Id: "0123456789abcdef"}}

	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, []string{"the-license-received"}, r.offered.values)
	require.Equal(t, 1, r.above())
	require.Contains(t, r.logs.String(), "level=INFO")
	require.Contains(t, r.logs.String(), "licenseId=0123456789abcdef")
	require.NotContains(t, r.logs.String(), "the-license-received")
	require.NotContains(t, r.logs.String(), r.held.value)
}

// Another replica asked first and stored it: there is nothing to say.
func Test_AskIfDue_AKeyReceivedThatIsAlreadyInForceSaysNothingAboveDebug(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.license = "the-license-received"
	r.offered.result = &licensestore.Result{Outcome: licensestore.Unchanged, Key: &license.Key{Id: "0123456789abcdef"}}

	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Zero(t, r.above())
	require.NotContains(t, r.logs.String(), "the-license-received")
}

func Test_AskIfDue_AKeyTheRuleRefusesIsAWarningInFixedWordsAndNothingElse(t *testing.T) {
	for word, outcome := range map[string]licensestore.Outcome{
		"invalid":        licensestore.RefusedInvalid,
		"older":          licensestore.RefusedOlder,
		"other_customer":   licensestore.RefusedOtherCustomer,
		"nothing_to_renew": licensestore.RefusedNothingToRenew,
		"none":             0,
	} {
		t.Run(word, func(t *testing.T) {
			r := newRenewal(t, keyExpiring(testExpiry), "")
			r.transport.license = "the-license-received"
			r.offered.result = &licensestore.Result{
				Outcome: outcome, Reason: "a reason that is not logged", Key: &license.Key{Id: leak + "-license"},
			}

			require.NoError(t, r.renewer.AskIfDue(t.Context()))
			require.Equal(t, 1, r.above())
			require.Contains(t, r.logs.String(), "level=WARN")
			require.Contains(t, r.logs.String(), "outcome="+word)
			require.NotContains(t, r.logs.String(), "the-license-received")
			require.NotContains(t, r.logs.String(), "a reason that is not logged")
			require.NotContains(t, r.logs.String(), leak)
		})
	}
}

func Test_AskIfDue_ARuleThatGivesNoVerdictIsAWarning(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.license = "the-license-received"

	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Contains(t, r.logs.String(), "level=WARN")
	require.Contains(t, r.logs.String(), "outcome=none")
}

func Test_AskIfDue_ADatabaseThatDoesNotTakeTheKeyIsAnError(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.license = "the-license-received"
	r.offered.err = errors.New("boom")

	err := r.renewer.AskIfDue(t.Context())
	require.ErrorContains(t, err, "boom")
	require.NotContains(t, err.Error(), "the-license-received")
	require.Zero(t, r.above())
}

// A failed ask is an ask: a destination that fails is not asked at every pass.
func Test_AskIfDue_AnAskThatFailsIsAnErrorOffersNothingAndIsTriedAgainAfterThePeriod(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.err = errors.New("license.example.com answered with the status 500")

	require.EqualError(t, r.renewer.AskIfDue(t.Context()), "license.example.com answered with the status 500")
	require.Empty(t, r.offered.values)
	// The line is the one of the loop, which logs what a step returns.
	require.Empty(t, r.logs.String())

	r.clock = reportNow.Add(time.Hour)
	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 1, r.transport.count())

	r.transport.err = nil
	r.clock = reportNow.Add(24 * time.Hour)
	require.NoError(t, r.renewer.AskIfDue(t.Context()))
	require.Equal(t, 2, r.transport.count())
}

// refusing is a destination that refuses every ask with a 400, and says in its Date header that
// it answered at the given instant; a zero instant is an answer without that header.
func refusing(t *testing.T, answeredAt time.Time) *httptest.Server {
	t.Helper()
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if answeredAt.IsZero() {
			// A nil value keeps net/http from writing the header by itself.
			w.Header()["Date"] = nil
		} else {
			w.Header().Set("Date", answeredAt.UTC().Format(http.TimeFormat))
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(destination.Close)
	return destination
}

// An ask is refused with a 400 when its instant is too far from the clock of the server: an
// instance whose clock is off would otherwise read, every day, a line that says nothing of it.
func Test_AskIfDue_AnAskRefusedByAServerWhoseClockDiffers_SaysTheClockIsOff(t *testing.T) {
	for name, apart := range map[string]time.Duration{
		"the instance is behind": telemetry.RenewalFreshness + time.Second,
		"the instance is ahead":  -telemetry.RenewalFreshness - time.Second,
		"a day apart":            24 * time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRenewal(t, keyExpiring(testExpiry), "")
			held := r.held.value
			r.renewer.transport = renewalTransportTo(t, refusing(t, r.clock.Add(apart)).URL)

			require.NoError(t, r.renewer.AskIfDue(t.Context()))

			logged := r.logs.String()
			require.Equal(t, 1, strings.Count(logged, "\n"), "one line: %s", logged)
			require.Contains(t, logged, "level=WARN")
			require.Contains(t, logged, `msg="`+renewalClockOff+`"`+"\n", "the words and nothing after them")
			require.Empty(t, r.offered.values, "nothing was offered")
			require.Equal(t, held, r.held.value, "the key in force is untouched")
		})
	}
}

func Test_AskIfDue_AnAskRefusedWithoutATellingDate_IsTheErrorItWas(t *testing.T) {
	for name, answeredAt := range map[string]func(now time.Time) time.Time{
		"no Date header":             func(time.Time) time.Time { return time.Time{} },
		"the same clock":             func(now time.Time) time.Time { return now },
		"five minutes behind":        func(now time.Time) time.Time { return now.Add(telemetry.RenewalFreshness) },
		"five minutes ahead":         func(now time.Time) time.Time { return now.Add(-telemetry.RenewalFreshness) },
		"a few seconds on the way":   func(now time.Time) time.Time { return now.Add(3 * time.Second) },
		"a little under the minutes": func(now time.Time) time.Time { return now.Add(telemetry.RenewalFreshness - time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRenewal(t, keyExpiring(testExpiry), "")
			held := r.held.value
			destination := refusing(t, answeredAt(r.clock))
			r.renewer.transport = renewalTransportTo(t, destination.URL)

			err := r.renewer.AskIfDue(t.Context())

			require.EqualError(t, err, strings.TrimPrefix(destination.URL, "http://")+" answered with the status 400")
			require.Empty(t, r.logs.String(), "the line is the one of the loop")
			require.Empty(t, r.offered.values)
			require.Equal(t, held, r.held.value)
		})
	}
}

// Only a refusal says something of the clock: a server that fails says nothing of the instance.
func Test_AskIfDue_AnAskThatFailsOtherwise_SaysNothingOfTheClockWhateverItsDate(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", r.clock.Add(24*time.Hour).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(destination.Close)
	r.renewer.transport = renewalTransportTo(t, destination.URL)

	err := r.renewer.AskIfDue(t.Context())

	require.EqualError(t, err, strings.TrimPrefix(destination.URL, "http://")+" answered with the status 503")
	require.Empty(t, r.logs.String())
}

// A Date that is not one says nothing, and nothing of it is logged or quoted.
func Test_AskIfDue_AnAskRefusedWithADateThatIsNotOne_IsTheErrorItWas(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", "what the receiver answered")
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(destination.Close)
	r.renewer.transport = renewalTransportTo(t, destination.URL)

	err := r.renewer.AskIfDue(t.Context())

	require.EqualError(t, err, strings.TrimPrefix(destination.URL, "http://")+" answered with the status 400")
	require.Empty(t, r.logs.String())
}

// In the loop, the line about the clock stands in place of the generic one, and the pass goes on.
func Test_Pass_AnInstanceWhoseClockIsOff_IsToldSoInPlaceOfTheGenericLine(t *testing.T) {
	d := newDaily(&syncBuilder{})
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.renewer.transport = renewalTransportTo(t, refusing(t, r.clock.Add(time.Hour)).URL)
	d.loop.renewer = r.renewer

	d.loop.Pass(t.Context())
	d.loop.Pass(t.Context())

	require.Equal(t, 2, d.builder.count(), "the passes ran whole")
	require.NotContains(t, d.logs.String(), "could not ask for the license")
	require.Equal(t, 1, strings.Count(r.logs.String(), renewalClockOff), "an ask is made once a period, and so is its line")
	require.Empty(t, r.offered.values)
}

func Test_RenewalClockOff_SaysTheBoundItIsToldBy(t *testing.T) {
	require.Equal(t, 5*time.Minute, telemetry.RenewalFreshness)
	require.Contains(t, renewalClockOff, "five minutes")
}

func Test_AskIfDue_CalledAtOnceAsksOnce(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 16)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = r.renewer.AskIfDue(t.Context())
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, r.transport.count())
}

func Test_NewRenewer_AsksOnceADayAndFourTimesNearTheExpiry(t *testing.T) {
	require.Equal(t, 24*time.Hour, RenewalPeriod)
	require.Equal(t, 6*time.Hour, RenewalPeriodNearExpiry)
}

func Test_Pass_AsksForTheLicenseAfterTheSending(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.store.withReports("2026-10-06").sendingSince(now.AddDate(0, 0, -5))
	r := newRenewal(t, keyExpiring(testExpiry), "")
	// Told by the order of what each step leaves: the report had left when the license was asked.
	var sentBefore []string
	r.renewer.instance = instanceIdFunc(func() (string, error) {
		sentBefore = d.transport.days()
		return testInstance, nil
	})
	d.loop.renewer = r.renewer

	d.loop.Pass(t.Context())
	require.Equal(t, 1, d.builder.count())
	require.Equal(t, []string{"2026-10-06"}, d.transport.days())
	require.Equal(t, 1, r.transport.count())
	require.Equal(t, []string{"2026-10-06"}, sentBefore)
}

type instanceIdFunc func() (string, error)

func (f instanceIdFunc) InstanceId(context.Context) (string, error) { return f() }

func Test_Pass_AnAskThatFailsIsLoggedOnceAndTheNextPassRunsAll(t *testing.T) {
	d := newDaily(&syncBuilder{})
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.err = errors.New("license.example.com answered with the status 500")
	d.loop.renewer = r.renewer

	d.loop.Pass(t.Context())
	d.loop.Pass(t.Context())
	require.Equal(t, 2, d.builder.count())
	require.Equal(t, 2, d.key.count())
	require.Equal(t, 1, strings.Count(d.logs.String(), "level=WARN"))
	require.Contains(t, d.logs.String(), "could not ask for the license that succeeds the one of the instance")
	require.Contains(t, d.logs.String(), "license.example.com answered with the status 500")
}

func Test_Pass_AnAskThatPanicsEndsNeitherTheProcessNorTheNextPass(t *testing.T) {
	d := newDaily(&syncBuilder{})
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.panics = true
	d.loop.renewer = r.renewer

	d.loop.Pass(t.Context())
	d.loop.Pass(t.Context())
	require.Equal(t, 2, d.builder.count())
	require.Equal(t, 2, d.key.count())
	require.Contains(t, d.logs.String(), "could not ask for the license that succeeds the one of the instance")
	require.Contains(t, d.logs.String(), "panicked=true")
	require.NotContains(t, d.logs.String(), "must not be logged")
}

func Test_Pass_APreparationAndASendingThatFailDoNotPreventTheAsk(t *testing.T) {
	d := newDaily(&syncBuilder{err: errors.New("boom")})
	d.key.err = errors.New("boom")
	r := newRenewal(t, keyExpiring(testExpiry), "")
	d.loop.renewer = r.renewer

	d.loop.Pass(t.Context())
	require.Equal(t, 1, r.transport.count())
}

func Test_Pass_ASendingThatPanicsDoesNotPreventTheAsk(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.store.panicOnStart = true
	r := newRenewal(t, keyExpiring(testExpiry), "")
	d.loop.renewer = r.renewer

	d.loop.Pass(t.Context())
	require.Equal(t, 1, r.transport.count())
}

// An instance that only prepares its report still asks for its license: the two stand alone.
func Test_Pass_WithoutASenderStillAsks(t *testing.T) {
	r := newRenewal(t, keyExpiring(testExpiry), "")
	logger := slog.New(slog.DiscardHandler)
	loop := NewDaily(NewPreparer(&syncBuilder{}, newFakeStore(), logger), nil, r.renewer, logger)
	loop.now = func() time.Time { return now.Add(12 * time.Hour) }

	loop.Pass(t.Context())
	require.Equal(t, 1, r.transport.count())
}

func Test_Pass_AnAskInterruptedByTheEndOfTheProcessIsNotLoggedAsAFailure(t *testing.T) {
	d := newDaily(&syncBuilder{})
	r := newRenewal(t, keyExpiring(testExpiry), "")
	r.transport.err = errors.New("unable to send to license.example.com: the request was interrupted")
	d.loop.renewer = r.renewer
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	d.loop.Pass(ctx)
	require.NotContains(t, d.logs.String(), "could not ask for the license")
}
