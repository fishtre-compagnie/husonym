package renewal_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/renewal"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	instanceA = "123e4567-e89b-12d3-a456-426614174000"
	instanceB = "223e4567-e89b-12d3-a456-426614174000"

	// The ids of the licenses of the tests are spelled as the product spells them, so that a
	// request names its license and not the word that stands for any other spelling.
	firstID  = "1111111111111111"
	secondID = "2222222222222222"
	thirdID  = "3333333333333333"
	fourthID = "4444444444444444"
)

var asked = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// bench is a renewal over a fresh database, and what it logged.
type bench struct {
	t       *testing.T
	pool    *pgxpool.Pool
	store   *cpstore.Store
	renewal *renewal.Renewal
	issuer  *cptest.Issuer
	logs    *bytes.Buffer
	// now is what the renewal reads the time from.
	now time.Time
}

func newBench(t *testing.T) *bench {
	t.Helper()
	b := &bench{t: t, pool: cptest.NewDatabase(t), issuer: cptest.NewIssuer(t), logs: &bytes.Buffer{}, now: asked}
	b.store = cpstore.New(b.pool)
	b.renewal = renewal.New(b.store, func() time.Time { return b.now }, slog.New(slog.NewTextHandler(b.logs, nil)))
	return b
}

// license mints a license of the customer of the bench and records it, as the successor of
// succeeds unless that is empty.
func (b *bench) license(id, succeeds string) license.RegistryEntry {
	b.t.Helper()
	entry := b.minted(id)
	cptest.AddLicense(b.t, b.store, b.issuer, &entry)
	if succeeds != "" {
		cptest.Succeed(b.t, b.pool, id, succeeds)
	}
	return entry
}

// minted mints a license the store does not know.
func (b *bench) minted(id string) license.RegistryEntry {
	b.t.Helper()
	return b.issuer.Entry(id, "cust-1", "Acme")
}

// answer asks as the instance does under the license of entry, at the time of the bench.
func (b *bench) answer(entry *license.RegistryEntry, instance string) (renewal.Outcome, *telemetry.RenewalAnswer) {
	b.t.Helper()
	return b.answerTo(cptest.RenewalFor(b.t, entry, instance, b.now))
}

func (b *bench) answerTo(ask cptest.SealedRenewal) (renewal.Outcome, *telemetry.RenewalAnswer) {
	b.t.Helper()
	outcome, answer, err := b.renewal.Answer(b.t.Context(), ask.Document, ask.Seal, ask.Fingerprint)
	require.NoError(b.t, err)
	if outcome != renewal.Served {
		require.Nil(b.t, answer, "only a request that is served has an answer")
	}
	return outcome, answer
}

func (b *bench) count(table string) int {
	b.t.Helper()
	var n int
	require.NoError(b.t, b.pool.QueryRow(b.t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

// recorded is the ask recorded for a license and an instance: when, and the license served, empty
// when none was.
func (b *bench) recorded(licenseID, instance string) (at time.Time, served string) {
	b.t.Helper()
	var servedID *string
	require.NoError(b.t, b.pool.QueryRow(b.t.Context(), `
		SELECT last_asked_at, last_served_license_id FROM controlplane.renewal_asks
		WHERE license_id = $1 AND instance_id = $2`, licenseID, instance).Scan(&at, &servedID))
	if servedID != nil {
		served = *servedID
	}
	return at, served
}

func Test_Answer_ASuccessor_IsServedAsItIsStored(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	second := b.license(secondID, firstID)

	outcome, answer := b.answer(&first, instanceA)

	require.Equal(t, renewal.Served, outcome)
	require.Equal(t, telemetry.RenewalSchemaVersion, answer.SchemaVersion)
	require.Equal(t, second.Encoded, answer.License)
	at, served := b.recorded(firstID, instanceA)
	require.True(t, asked.Equal(at))
	require.Equal(t, secondID, served)
	require.Zero(t, b.count("seal_rejections"))
	require.Empty(t, b.logs.String())
}

// An instance two renewals behind is given the latest, and so is the one a single renewal behind.
func Test_Answer_AChainOfSuccessors_ServesTheLastOne(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	second := b.license(secondID, firstID)
	third := b.license(thirdID, secondID)
	fourth := b.license(fourthID, thirdID)

	for name, held := range map[string]*license.RegistryEntry{"first": &first, "second": &second, "third": &third} {
		outcome, answer := b.answer(held, instanceA)
		require.Equal(t, renewal.Served, outcome, name)
		require.Equal(t, fourth.Encoded, answer.License, name)
		_, served := b.recorded(held.Id, instanceA)
		require.Equal(t, fourthID, served, name)
	}

	// The instance that holds the last one has nothing to receive.
	outcome, _ := b.answer(&fourth, instanceA)
	require.Equal(t, renewal.Nothing, outcome)
	require.Empty(t, b.logs.String())
}

func Test_Answer_NoSuccessor_IsNothing_AndTheAskIsRecorded(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")

	outcome, _ := b.answer(&first, instanceA)

	require.Equal(t, renewal.Nothing, outcome)
	at, served := b.recorded(firstID, instanceA)
	require.True(t, asked.Equal(at))
	require.Empty(t, served)

	// The next ask of the instance moves its row; another instance has its own.
	b.now = asked.Add(24 * time.Hour)
	b.answer(&first, instanceA)
	b.answer(&first, instanceB)
	at, _ = b.recorded(firstID, instanceA)
	require.True(t, b.now.Equal(at))
	require.Equal(t, 2, b.count("renewal_asks"))
}

func Test_Answer_AnUnknownFingerprint_IsNothing_AndLeavesNoTrace(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	b.license(firstID, "")
	b.license(secondID, firstID)
	unknown := b.minted(thirdID)

	outcome, _ := b.answer(&unknown, instanceA)

	require.Equal(t, renewal.Nothing, outcome)
	require.Zero(t, b.count("renewal_asks"))
	require.Zero(t, b.count("seal_rejections"))
	require.Empty(t, b.logs.String())
}

func Test_Answer_AWrongSealForAKnownLicense_IsNothing_AndIsCounted(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	b.license(secondID, firstID)
	other := b.minted(thirdID)

	// The request of the license, sealed with another key, and the seal of another request.
	stolen := cptest.RenewalFor(t, &first, instanceA, b.now)
	stolen.Seal = cptest.RenewalFor(t, &other, instanceA, b.now).Seal
	outcome, _ := b.answerTo(stolen)
	require.Equal(t, renewal.Nothing, outcome)

	replayed := cptest.RenewalFor(t, &first, instanceA, b.now)
	replayed.Seal = cptest.RenewalFor(t, &first, instanceB, b.now).Seal
	outcome, _ = b.answerTo(replayed)
	require.Equal(t, renewal.Nothing, outcome)

	require.Zero(t, b.count("renewal_asks"), "an ask whose seal does not verify is not the one of an instance")
	var license string
	var day string
	var rejections int
	require.NoError(t, b.pool.QueryRow(t.Context(),
		`SELECT license_id, day::text, count FROM controlplane.seal_rejections`).Scan(&license, &day, &rejections))
	require.Equal(t, firstID, license)
	require.Equal(t, "2026-10-08", day)
	require.Equal(t, 2, rejections)
	require.Empty(t, b.logs.String())
}

// A request sealed with the key of a license and naming another is not the one of that license.
func Test_Answer_ARequestThatNamesAnotherLicense_IsNothing(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	b.license(secondID, firstID)

	document, err := telemetry.NewRenewalRequest(thirdID, instanceA, b.now).Marshal()
	require.NoError(t, err)
	seal, err := telemetry.Seal(first.Encoded, document)
	require.NoError(t, err)
	outcome, _ := b.answerTo(cptest.SealedRenewal{
		Document: document, Seal: seal, Fingerprint: telemetry.KeyFingerprint(first.Encoded),
	})

	require.Equal(t, renewal.Nothing, outcome)
	require.Zero(t, b.count("renewal_asks"))
	require.Zero(t, b.count("seal_rejections"), "the seal is the right one")
}

// A license whose id is not spelled as the product spells one is named by the word that stands
// for it, on both sides.
func Test_Answer_ALicenseWithAnotherSpellingOfId_IsServed(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license("lic-1", "")
	second := b.license("lic-2", "lic-1")

	outcome, answer := b.answer(&first, instanceA)

	require.Equal(t, renewal.Served, outcome)
	require.Equal(t, second.Encoded, answer.License)
}

// What can be refused without the database is refused before it is read: the pool of this store
// is closed, and any read of it would be an error.
func Test_Answer_WhatIsMalformedOrStale_IsRefusedBeforeTheLicenseIsLookedUp(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	b.license(secondID, firstID)
	good := cptest.RenewalFor(t, &first, instanceA, b.now)
	resealed := func(document string) cptest.SealedRenewal {
		seal, err := telemetry.Seal(first.Encoded, []byte(document))
		require.NoError(t, err)
		return cptest.SealedRenewal{Document: []byte(document), Seal: seal, Fingerprint: good.Fingerprint}
	}
	at := func(instant time.Time) cptest.SealedRenewal { return cptest.RenewalFor(t, &first, instanceA, instant) }
	body := string(good.Document)
	refused := map[string]cptest.SealedRenewal{
		"a seal that is not one":        {Document: good.Document, Seal: "abc", Fingerprint: good.Fingerprint},
		"a fingerprint that is not one": {Document: good.Document, Seal: good.Seal, Fingerprint: strings.ToUpper(good.Fingerprint)},
		"an empty body":                 resealed(""),
		"a body that is not JSON":       resealed(`{"schema_version":1,`),
		"an unknown field":              resealed(strings.Replace(body, `{`, `{"hostname":"db-1",`, 1)),
		"another version":               resealed(strings.Replace(body, `"schema_version":1`, `"schema_version":2`, 1)),
		"no instance":                   resealed(strings.Replace(body, instanceA, "", 1)),
		"an instant that is not one":    resealed(strings.Replace(body, "2026-10-08T12:00:00Z", "yesterday", 1)),
		"data after the request":        resealed(body + body),
		"older than five minutes":       at(asked.Add(-telemetry.RenewalFreshness - time.Second)),
		"younger than five minutes":     at(asked.Add(telemetry.RenewalFreshness + time.Second)),
		"a day old":                     at(asked.Add(-24 * time.Hour)),
	}
	b.pool.Close()

	for name, ask := range refused {
		outcome, answer, err := b.renewal.Answer(t.Context(), ask.Document, ask.Seal, ask.Fingerprint)
		require.NoError(t, err, "%s: the database was read", name)
		require.Equal(t, renewal.Refused, outcome, name)
		require.Nil(t, answer, name)
	}
	require.Empty(t, b.logs.String())

	// The same store does fail a request that reaches it.
	outcome, answer, err := b.renewal.Answer(t.Context(), good.Document, good.Seal, good.Fingerprint)
	require.Error(t, err)
	require.Equal(t, renewal.Nothing, outcome)
	require.Nil(t, answer)
	require.NotContains(t, err.Error(), good.Seal)
	require.NotContains(t, err.Error(), good.Fingerprint)
	require.NotContains(t, err.Error(), instanceA)
}

func Test_Answer_ARequestFiveMinutesOffEitherSide_IsStillFresh(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	b.license(secondID, firstID)

	for _, instant := range []time.Time{asked.Add(-telemetry.RenewalFreshness), asked.Add(telemetry.RenewalFreshness)} {
		outcome, _ := b.answerTo(cptest.RenewalFor(t, &first, instanceA, instant))
		require.Equal(t, renewal.Served, outcome)
	}
}

// A chain longer than the walk stops at its bound: the license there is served, and one line says
// so in fixed words.
func Test_Answer_AChainLongerThanTheWalk_ServesTheLicenseAtTheBound_AndSaysSo(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	id := func(n int) string { return fmt.Sprintf("%016x", n) }
	first := b.license(id(0), "")
	var atTheBound license.RegistryEntry
	for n := 1; n <= cpstore.SuccessorChainBound+3; n++ {
		entry := b.license(id(n), id(n-1))
		if n == cpstore.SuccessorChainBound {
			atTheBound = entry
		}
	}

	outcome, answer := b.answer(&first, instanceA)

	require.Equal(t, renewal.Served, outcome)
	require.Equal(t, atTheBound.Encoded, answer.License)
	_, served := b.recorded(id(0), instanceA)
	require.Equal(t, atTheBound.Id, served)
	logged := b.logs.String()
	require.Equal(t, 1, strings.Count(logged, "\n"), "one line")
	require.Contains(t, logged, "level=WARN")
	require.Contains(t, logged, renewal.ChainCutShort)
	for _, secret := range []string{
		id(0), atTheBound.Id, atTheBound.Encoded, first.Encoded, instanceA, telemetry.KeyFingerprint(first.Encoded),
	} {
		require.NotContains(t, logged, secret)
	}
}

func Test_Answer_BeyondFiftyInstances_ALicenseIsStillServed_AndTheAskIsNotRecorded(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.license(firstID, "")
	second := b.license(secondID, firstID)
	for n := range cpstore.RenewalAsksPerLicense {
		outcome, _ := b.answer(&first, fmt.Sprintf("instance-%02d", n))
		require.Equal(t, renewal.Served, outcome)
	}
	require.Equal(t, cpstore.RenewalAsksPerLicense, b.count("renewal_asks"))

	outcome, answer := b.answer(&first, "one-instance-too-many")

	require.Equal(t, renewal.Served, outcome)
	require.Equal(t, second.Encoded, answer.License)
	require.Equal(t, cpstore.RenewalAsksPerLicense, b.count("renewal_asks"))
}
