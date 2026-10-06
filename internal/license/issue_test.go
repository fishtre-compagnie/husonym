package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_Issue(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ring := Keyring{LegacyKid: pub}

	// The property that matters: whatever Issue mints, ParseWith must accept and read
	// back identically. Issuing and verifying share their structures precisely so this
	// cannot drift.
	t.Run("round-trips through verification", func(t *testing.T) {
		expires := time.Now().UTC().Add(365 * 24 * time.Hour)
		issued, err := Issue(&IssueRequest{
			IssuedTo:   "Acme Co.",
			CustomerId: "cust-001",
			ExpiresAt:  expires,
			GraceDays:  ptr(30),
			Limits:     &Limits{MaxJobs: ptr(10), AllowedConnectionTypes: []string{"postgres"}},
		}, priv, ring)
		require.NoError(t, err)
		require.NotEmpty(t, issued.Encoded)
		require.NotEmpty(t, issued.Id)

		got, err := ParseWith(issued.Encoded, Keyring{LegacyKid: pub})
		require.NoError(t, err)
		require.Equal(t, "Acme Co.", got.IssuedTo)
		require.Equal(t, "cust-001", got.CustomerId)
		require.Equal(t, expires.Truncate(time.Second), got.ExpiresAt)
		require.Equal(t, 30, *got.GraceDays)
		require.Equal(t, 10, *got.Limits.MaxJobs)
		require.True(t, got.Limits.Allows("postgres"))
		require.False(t, got.Limits.Allows("mssql"))
		require.Equal(t, StateValid, got.StateAt(time.Now().UTC()))
	})

	t.Run("generates a distinct id when none is given", func(t *testing.T) {
		req := IssueRequest{IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour)}
		first, err := Issue(&req, priv, ring)
		require.NoError(t, err)
		second, err := Issue(&req, priv, ring)
		require.NoError(t, err)
		require.NotEqual(t, first.Id, second.Id)
	})

	t.Run("honours an explicit id", func(t *testing.T) {
		issued, err := Issue(&IssueRequest{
			Id: "contract-42", IssuedTo: "A", CustomerId: "c",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}, priv, ring)
		require.NoError(t, err)
		require.Equal(t, "contract-42", issued.Id)
	})

	t.Run("rejects invalid requests", func(t *testing.T) {
		valid := time.Now().UTC().Add(time.Hour)
		cases := map[string]IssueRequest{
			"no issued_to":   {CustomerId: "c", ExpiresAt: valid},
			"no customer_id": {IssuedTo: "A", ExpiresAt: valid},
			"no expiry":      {IssuedTo: "A", CustomerId: "c"},
			"expiry in past": {IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(-time.Minute)},
			"negative grace": {IssuedTo: "A", CustomerId: "c", ExpiresAt: valid, GraceDays: ptr(-1)},
			"negative max":   {IssuedTo: "A", CustomerId: "c", ExpiresAt: valid, Limits: &Limits{MaxJobs: ptr(-1)}},
		}
		for name, req := range cases {
			t.Run(name, func(t *testing.T) {
				issued, err := Issue(&req, priv, ring)
				require.Error(t, err)
				require.Nil(t, issued)
			})
		}
	})

	t.Run("rejects a missing key", func(t *testing.T) {
		issued, err := Issue(&IssueRequest{
			IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour),
		}, nil, ring)
		require.Error(t, err)
		require.Nil(t, issued)
	})

	// A license minted with the wrong key is the failure mode that would only surface at
	// the customer's site, so it must be impossible to miss.
	t.Run("a license from another key does not verify", func(t *testing.T) {
		otherPub, otherPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		issued, err := Issue(&IssueRequest{
			IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour),
		}, otherPriv, Keyring{LegacyKid: otherPub})
		require.NoError(t, err)

		got, err := ParseWith(issued.Encoded, Keyring{LegacyKid: pub})
		require.Error(t, err)
		require.Nil(t, got)
	})
}

func Test_Registry(t *testing.T) {
	newEntry := func(id string, expiresIn time.Duration, grace *int) *RegistryEntry {
		return &RegistryEntry{
			Id:         id,
			IssuedTo:   "Customer " + id,
			CustomerId: "cust-" + id,
			IssuedAt:   time.Now().UTC(),
			ExpiresAt:  time.Now().UTC().Add(expiresIn),
			GraceDays:  grace,
			Encoded:    "encoded-" + id,
		}
	}

	t.Run("a missing file loads as empty", func(t *testing.T) {
		r, err := LoadRegistry(filepath.Join(t.TempDir(), "nope.json"))
		require.NoError(t, err)
		require.Empty(t, r.Entries)
	})

	t.Run("round-trips through disk", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "registry.json")
		r, err := LoadRegistry(path)
		require.NoError(t, err)
		require.NoError(t, r.Add(newEntry("a", 30*24*time.Hour, ptr(7))))
		require.NoError(t, r.Save(path))

		reloaded, err := LoadRegistry(path)
		require.NoError(t, err)
		require.Len(t, reloaded.Entries, 1)
		require.Equal(t, "Customer a", reloaded.Entries[0].IssuedTo)
		require.Equal(t, 7, *reloaded.Entries[0].GraceDays)
	})

	t.Run("refuses duplicate ids", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Add(newEntry("dup", time.Hour, nil)))
		require.Error(t, r.Add(newEntry("dup", time.Hour, nil)))
		require.Len(t, r.Entries, 1)
	})

	t.Run("is written with restrictive permissions", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "registry.json")
		r := &Registry{}
		require.NoError(t, r.Add(newEntry("a", time.Hour, nil)))
		require.NoError(t, r.Save(path))

		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, "-rw-------", info.Mode().String(), "the registry holds customer data")
	})

	t.Run("expiring worklist is sorted and excludes frozen", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Add(newEntry("soon", 5*24*time.Hour, nil)))
		require.NoError(t, r.Add(newEntry("later", 20*24*time.Hour, nil)))
		require.NoError(t, r.Add(newEntry("far", 300*24*time.Hour, nil)))
		// Expired well beyond its grace: past saving by a reminder.
		require.NoError(t, r.Add(newEntry("gone", -60*24*time.Hour, ptr(14))))
		// Expired but still in grace: very much actionable.
		require.NoError(t, r.Add(newEntry("grace", -time.Hour, ptr(14))))

		got := r.ExpiringWithin(45 * 24 * time.Hour)
		ids := []string{}
		for _, e := range got {
			ids = append(ids, e.Id)
		}
		require.Equal(t, []string{"grace", "soon", "later"}, ids)

		frozen := r.Frozen()
		require.Len(t, frozen, 1)
		require.Equal(t, "gone", frozen[0].Id)
	})

	t.Run("lists a customer's renewal chain", func(t *testing.T) {
		r := &Registry{}
		a := newEntry("y2", 400*24*time.Hour, nil)
		a.CustomerId = "acme"
		b := newEntry("y1", 30*24*time.Hour, nil)
		b.CustomerId = "acme"
		other := newEntry("other", time.Hour, nil)
		require.NoError(t, r.Add(a))
		require.NoError(t, r.Add(b))
		require.NoError(t, r.Add(other))

		chain := r.ForCustomer("acme")
		require.Len(t, chain, 2)
		require.Equal(t, "y1", chain[0].Id, "soonest expiry first")
	})

	t.Run("entry reports its lifecycle state", func(t *testing.T) {
		require.Equal(t, StateValid, newEntry("a", 90*24*time.Hour, nil).State())
		require.Equal(t, StateExpiring, newEntry("b", 5*24*time.Hour, nil).State())
		require.Equal(t, StateGrace, newEntry("c", -time.Hour, nil).State())
		require.Equal(t, StateFrozen, newEntry("d", -60*24*time.Hour, nil).State())
	})
}

func newIssueRequest(mutate func(*IssueRequest)) *IssueRequest {
	req := &IssueRequest{IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if mutate != nil {
		mutate(req)
	}
	return req
}

func issueTestKeys(t *testing.T) (Keyring, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return Keyring{LegacyKid: pub}, priv
}

func Test_Issue_RefusesAnUnknownFeatureName(t *testing.T) {
	ring, priv := issueTestKeys(t)
	issued, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Features = []string{"job_hook"} }), priv, ring)
	require.ErrorContains(t, err, "job_hook")
	require.Nil(t, issued)
}

func Test_Issue_RefusesADuplicateFeature(t *testing.T) {
	ring, priv := issueTestKeys(t)
	_, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Features = []string{"job_hooks", "job_hooks"} }), priv, ring)
	require.ErrorContains(t, err, "job_hooks")
}

func Test_Issue_RefusesAnUnknownTelemetryMode(t *testing.T) {
	ring, priv := issueTestKeys(t)
	issued, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Telemetry = "offline" }), priv, ring)
	require.ErrorContains(t, err, "offline")
	require.Nil(t, issued)
}

func Test_Issue_RefusesANegativeSourceCap(t *testing.T) {
	ring, priv := issueTestKeys(t)
	issued, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Limits = &Limits{MaxSources: ptr(-1)} }), priv, ring)
	require.ErrorContains(t, err, "limits.max_sources cannot be negative")
	require.Nil(t, issued)
}

func Test_Issue_WildcardCannotBeMixedWithNames(t *testing.T) {
	ring, priv := issueTestKeys(t)
	_, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Features = []string{"*", "job_hooks"} }), priv, ring)
	require.Error(t, err)

	issued, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Features = []string{"*"} }), priv, ring)
	require.NoError(t, err)
	got, err := ParseWith(issued.Encoded, ring)
	require.NoError(t, err)
	require.True(t, got.HasFeature(FeatureSso))
}

func Test_Issue_CarriesTheKidOfTheSigningKey(t *testing.T) {
	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ring := Keyring{LegacyKid: pubA, "k2": pubB}

	issued, err := Issue(newIssueRequest(nil), privB, ring)
	require.NoError(t, err)
	require.Equal(t, "k2", issued.Kid)

	outer, err := base64.StdEncoding.DecodeString(issued.Encoded)
	require.NoError(t, err)
	var env envelope
	require.NoError(t, json.Unmarshal(outer, &env))
	require.Equal(t, "k2", env.Kid)

	_, err = ParseWith(issued.Encoded, ring)
	require.NoError(t, err)
}

func Test_Issue_RefusesASigningKeyOutsideTheRing(t *testing.T) {
	ring, _ := issueTestKeys(t)
	_, stranger, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	issued, err := Issue(newIssueRequest(nil), stranger, ring)
	require.Nil(t, issued)
	require.ErrorContains(t, err, PublicKeyFingerprint(stranger.Public().(ed25519.PublicKey)))
	require.ErrorContains(t, err, PublicKeyFingerprint(ring[LegacyKid]))
}

func Test_Issue_WithoutFeaturesWritesNoFeaturesField(t *testing.T) {
	ring, priv := issueTestKeys(t)
	issued, err := Issue(newIssueRequest(nil), priv, ring)
	require.NoError(t, err)

	fields := signedContent(t, issued.Encoded)
	require.NotContains(t, fields, "features")
	require.NotContains(t, fields, "plan")
	require.NotContains(t, fields, "telemetry")
	require.Equal(t, "v1", fields["version"])
}

func Test_Issue_WritesTheNewFieldsAndTheyReadBack(t *testing.T) {
	ring, priv := issueTestKeys(t)
	issued, err := Issue(newIssueRequest(func(r *IssueRequest) {
		r.Plan = "Team"
		r.Features = []string{"job_hooks", "sso"}
		r.Telemetry = string(TelemetryOfflineReport)
		r.Limits = &Limits{MaxSources: ptr(5)}
	}), priv, ring)
	require.NoError(t, err)
	require.Equal(t, "Team", issued.Plan)
	require.Equal(t, []string{"job_hooks", "sso"}, issued.Features)
	require.Equal(t, "offline_report", issued.Telemetry)

	got, err := ParseWith(issued.Encoded, ring)
	require.NoError(t, err)
	require.Equal(t, "Team", got.Plan)
	require.True(t, got.HasFeature(FeatureSso))
	require.False(t, got.HasFeature(FeatureMcp))
	require.Equal(t, TelemetryOfflineReport, got.TelemetryMode())
	require.Equal(t, 5, *got.Limits.MaxSources)
}

func Test_Issue_AnExplicitEmptyFeatureListIsWritten(t *testing.T) {
	ring, priv := issueTestKeys(t)
	issued, err := Issue(newIssueRequest(func(r *IssueRequest) { r.Features = []string{} }), priv, ring)
	require.NoError(t, err)
	require.Contains(t, signedContent(t, issued.Encoded), "features")

	got, err := ParseWith(issued.Encoded, ring)
	require.NoError(t, err)
	require.False(t, got.HasFeature(FeatureJobHooks))
}

// signedContent returns the top-level fields of the signed JSON of a key value.
func signedContent(t *testing.T, encoded string) map[string]any {
	t.Helper()
	outer, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var env envelope
	require.NoError(t, json.Unmarshal(outer, &env))
	content, err := base64.StdEncoding.DecodeString(env.License)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(content, &fields))
	return fields
}

func Test_Registry_KeepsTheNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	r, err := LoadRegistry(path)
	require.NoError(t, err)
	require.NoError(t, r.Add(&RegistryEntry{
		Id: "a", IssuedTo: "A", CustomerId: "c", Encoded: "e",
		Kid: "k2", Plan: "Team", Features: []string{"job_hooks"}, Telemetry: "none",
	}))
	require.NoError(t, r.Add(&RegistryEntry{Id: "b", IssuedTo: "B", CustomerId: "c", Encoded: "e", Features: []string{}}))
	require.NoError(t, r.Save(path))

	reloaded, err := LoadRegistry(path)
	require.NoError(t, err)
	require.Equal(t, "k2", reloaded.Entries[0].Kid)
	require.Equal(t, "Team", reloaded.Entries[0].Plan)
	require.Equal(t, []string{"job_hooks"}, reloaded.Entries[0].Features)
	require.Equal(t, "none", reloaded.Entries[0].Telemetry)
	require.NotNil(t, reloaded.Entries[1].Features, "an explicit empty list must survive")
}

func Test_Registry_LoadsAFileWrittenBeforeTheNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	old := `{"version":"v1","entries":[{"id":"a","issued_to":"A","customer_id":"c","issued_at":"2026-01-01T00:00:00Z","expires_at":"2027-01-01T00:00:00Z","encoded":"e","key_fingerprint":"abcd"}]}`
	require.NoError(t, os.WriteFile(path, []byte(old), 0o600))

	r, err := LoadRegistry(path)
	require.NoError(t, err)
	require.Len(t, r.Entries, 1)
	require.Nil(t, r.Entries[0].Features)
	require.Empty(t, r.Entries[0].Kid)
}
