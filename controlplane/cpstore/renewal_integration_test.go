package cpstore_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// chain records n licenses of one customer, each the successor of the one before, and gives them
// in that order. Their ids are prefix-1, prefix-2 and so on.
func (s *seeded) chain(prefix, customerID string, n int) []license.RegistryEntry {
	s.t.Helper()
	entries := make([]license.RegistryEntry, 0, n)
	for i := 1; i <= n; i++ {
		entry := s.license(fmt.Sprintf("%s-%d", prefix, i), customerID, "Acme", today.AddDate(1, 0, i))
		if i > 1 {
			cptest.Succeed(s.t, s.pool, entry.Id, entries[i-2].Id)
		}
		entries = append(entries, entry)
	}
	return entries
}

func (s *seeded) latestSuccessor(licenseID string) (successor *cpstore.License, cutShort bool) {
	s.t.Helper()
	successor, cutShort, err := s.store.LatestSuccessor(s.t.Context(), licenseID)
	require.NoError(s.t, err)
	return successor, cutShort
}

func Test_LatestSuccessor_ALicenseNothingSucceeds_HasNone(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.chain("other", "cust-2", 2)

	for _, id := range []string{"lic-1", "a license nobody recorded"} {
		successor, cutShort := s.latestSuccessor(id)
		require.Nil(t, successor, id)
		require.False(t, cutShort, id)
	}
}

func Test_LatestSuccessor_IsTheLastOfTheChain_NotTheNext(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	chain := s.chain("lic", "cust-1", 4)
	s.chain("other", "cust-2", 3)
	last := &chain[3]

	// Wherever an instance stands in the chain, it is given its end: the one a single renewal
	// behind and the one three renewals behind alike.
	for _, from := range []string{"lic-1", "lic-2", "lic-3"} {
		successor, cutShort := s.latestSuccessor(from)
		require.NotNil(t, successor, from)
		require.Equal(t, "lic-4", successor.Id, from)
		require.Equal(t, last.Encoded, successor.Encoded, from)
		require.False(t, cutShort, from)
	}

	successor, _ := s.latestSuccessor("lic-4")
	require.Nil(t, successor, "the end of the chain has no successor")
}

func Test_LatestSuccessor_StopsAtItsBound(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	// lic-1 and the successors that follow it: lic-2 is the first of them, lic-66 the sixty-fifth.
	s.chain("lic", "cust-1", cpstore.SuccessorChainBound+2)

	successor, cutShort := s.latestSuccessor("lic-1")
	require.NotNil(t, successor)
	require.Equal(t, fmt.Sprintf("lic-%d", cpstore.SuccessorChainBound+1), successor.Id, "the sixty-fourth successor")
	require.True(t, cutShort, "the chain goes on past the bound")

	// From the next license the end is exactly at the bound: reached, and not cut short.
	successor, cutShort = s.latestSuccessor("lic-2")
	require.NotNil(t, successor)
	require.Equal(t, fmt.Sprintf("lic-%d", cpstore.SuccessorChainBound+2), successor.Id)
	require.False(t, cutShort)
}

// The index gives a license one successor at most, and nothing keeps two licenses from naming each
// other: the walk of such a loop ends at the bound all the same.
func Test_LatestSuccessor_AChainThatLoops_EndsAtTheBound(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.chain("lic", "cust-1", 2)
	cptest.Succeed(t, s.pool, "lic-1", "lic-2")

	successor, cutShort := s.latestSuccessor("lic-1")

	require.NotNil(t, successor)
	require.True(t, cutShort)
}

func Test_RenewalBounds_AreTheOnesAgreed(t *testing.T) {
	require.Equal(t, 64, cpstore.SuccessorChainBound)
	require.Equal(t, 50, cpstore.RenewalAsksPerLicense)
}

type askRow struct {
	instance string
	askedAt  time.Time
	served   *string
	servedAt *time.Time
}

func (s *seeded) asks(licenseID string) []askRow {
	s.t.Helper()
	rows, err := s.pool.Query(s.t.Context(), `
		SELECT instance_id, last_asked_at, last_served_license_id, last_served_at
		FROM controlplane.renewal_asks WHERE license_id = $1 ORDER BY instance_id`, licenseID)
	require.NoError(s.t, err)
	var asks []askRow
	for rows.Next() {
		var ask askRow
		require.NoError(s.t, rows.Scan(&ask.instance, &ask.askedAt, &ask.served, &ask.servedAt))
		asks = append(asks, ask)
	}
	require.NoError(s.t, rows.Err())
	return asks
}

func (s *seeded) ask(licenseID, instanceID string, at time.Time, served string) {
	s.t.Helper()
	require.NoError(s.t, s.store.RecordRenewalAsk(s.t.Context(), licenseID, instanceID, at, served))
}

func Test_RecordRenewalAsk_KeepsTheLastAskAndWhatWasLastServed(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.chain("lic", "cust-1", 3)
	hour := func(n int) time.Time { return today.Add(time.Duration(n) * time.Hour) }

	// Nothing to give yet.
	s.ask("lic-1", "inst-1", hour(0), "")
	asks := s.asks("lic-1")
	require.Len(t, asks, 1)
	require.Equal(t, "inst-1", asks[0].instance)
	require.True(t, hour(0).Equal(asks[0].askedAt))
	require.Nil(t, asks[0].served)
	require.Nil(t, asks[0].servedAt)

	// A license is served.
	s.ask("lic-1", "inst-1", hour(1), "lic-2")
	asks = s.asks("lic-1")
	require.Len(t, asks, 1, "one row per license and instance")
	require.True(t, hour(1).Equal(asks[0].askedAt))
	require.Equal(t, "lic-2", *asks[0].served)
	require.True(t, hour(1).Equal(*asks[0].servedAt))

	// An ask that is served nothing leaves what was served before.
	s.ask("lic-1", "inst-1", hour(2), "")
	asks = s.asks("lic-1")
	require.True(t, hour(2).Equal(asks[0].askedAt))
	require.Equal(t, "lic-2", *asks[0].served)
	require.True(t, hour(1).Equal(*asks[0].servedAt))

	// A later license served takes its place.
	s.ask("lic-1", "inst-1", hour(3), "lic-3")
	asks = s.asks("lic-1")
	require.Equal(t, "lic-3", *asks[0].served)
	require.True(t, hour(3).Equal(*asks[0].servedAt))

	// Another instance, and the same instance under another license, are other rows.
	s.ask("lic-1", "inst-2", hour(4), "")
	s.ask("lic-2", "inst-1", hour(5), "lic-3")
	require.Len(t, s.asks("lic-1"), 2)
	require.Len(t, s.asks("lic-2"), 1)
}

func Test_RecordRenewalAsk_AtTheCap_ANewInstanceIsNotRecorded(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.chain("lic", "cust-1", 2)
	s.license("lic-other", "cust-2", "Other", today.AddDate(1, 0, 0))
	for i := range cpstore.RenewalAsksPerLicense {
		s.ask("lic-1", fmt.Sprintf("inst-%02d", i), today, "")
	}
	require.Len(t, s.asks("lic-1"), cpstore.RenewalAsksPerLicense)
	later := today.Add(time.Hour)

	// One more instance: no error, and no row.
	s.ask("lic-1", "inst-one-too-many", later, "lic-2")
	asks := s.asks("lic-1")
	require.Len(t, asks, cpstore.RenewalAsksPerLicense)
	for _, ask := range asks {
		require.NotEqual(t, "inst-one-too-many", ask.instance)
	}

	// An instance that is already there is still kept up to date.
	s.ask("lic-1", "inst-00", later, "lic-2")
	asks = s.asks("lic-1")
	require.Len(t, asks, cpstore.RenewalAsksPerLicense)
	require.Equal(t, "inst-00", asks[0].instance)
	require.True(t, later.Equal(asks[0].askedAt))
	require.Equal(t, "lic-2", *asks[0].served)

	// The cap is the one of each license.
	s.ask("lic-other", "inst-one-too-many", later, "")
	require.Len(t, s.asks("lic-other"), 1)
}

func Test_LicenseDetail_GivesTheRenewalAsks_TheLastOneFirst(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.chain("lic", "cust-1", 2)
	s.ask("lic-1", "inst-early", today, "lic-2")
	s.ask("lic-1", "inst-late", today.Add(2*time.Hour), "")
	s.ask("lic-1", "inst-between", today.Add(time.Hour), "lic-2")
	s.ask("lic-2", "inst-early", today.Add(3*time.Hour), "")

	detail, err := s.store.LicenseDetail(t.Context(), "lic-1", today)
	require.NoError(t, err)

	require.Equal(t, []cpstore.RenewalAsk{
		{InstanceID: "inst-late", LastAskedAt: today.Add(2 * time.Hour)},
		{
			InstanceID: "inst-between", LastAskedAt: today.Add(time.Hour),
			ServedLicenseID: "lic-2", ServedAt: today.Add(time.Hour),
		},
		{InstanceID: "inst-early", LastAskedAt: today, ServedLicenseID: "lic-2", ServedAt: today},
	}, detail.RenewalAsks)

	successor, err := s.store.LicenseDetail(t.Context(), "lic-2", today)
	require.NoError(t, err)
	require.Len(t, successor.RenewalAsks, 1)

	// A license nobody asked about has none, and not a nil list.
	s.license("lic-quiet", "cust-2", "Other", today.AddDate(1, 0, 0))
	quiet, err := s.store.LicenseDetail(t.Context(), "lic-quiet", today)
	require.NoError(t, err)
	require.Empty(t, quiet.RenewalAsks)
}
