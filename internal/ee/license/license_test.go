package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func Test_parsePublicKey(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		actual, err := parsePublicKey("")
		require.Error(t, err)
		require.Nil(t, actual)
	})
	t.Run("invalid format", func(t *testing.T) {
		actual, err := parsePublicKey("blah")
		require.Error(t, err)
		require.Nil(t, actual)
	})
	// Guards the embedded key itself: if husonym_ee_pub.pem is ever replaced by
	// something malformed, no license can be verified at all.
	t.Run("embedded key is valid", func(t *testing.T) {
		actual, err := parsePublicKey(publicKeyPEM)
		require.NoError(t, err)
		require.NotNil(t, actual)
	})
}

// Issues a license the way scripts/gen-cust-license.sh does: sign the contents bytes,
// wrap contents and signature in a JSON envelope, base64 the envelope.
//
// Tests mint their own throwaway keypair rather than carrying a fixture signed by the
// production key. That keeps the suite honest about what it covers — the verification
// logic — and means rotating the signing key does not invalidate the tests.
func issueLicense(t *testing.T, priv ed25519.PrivateKey, contents *licenseContents) string {
	t.Helper()
	raw, err := json.Marshal(contents)
	require.NoError(t, err)
	envelope, err := json.Marshal(licenseFile{
		License:   base64.StdEncoding.EncodeToString(raw),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw)),
	})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(envelope)
}

func Test_getLicense(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	t.Run("valid signature, fields round-trip", func(t *testing.T) {
		want := &licenseContents{
			Version:    "v1",
			Id:         "123",
			IssuedTo:   "Acme Co.",
			CustomerId: "456",
			IssuedAt:   time.Date(2022, 12, 31, 12, 0, 0, 0, time.UTC),
			ExpiresAt:  time.Date(2023, 12, 31, 12, 0, 0, 0, time.UTC),
		}
		got, err := getLicense(issueLicense(t, priv, want), pub)
		require.NoError(t, err)
		require.Equal(t, want.Id, got.Id)
		require.Equal(t, want.Version, got.Version)
		require.Equal(t, want.IssuedTo, got.IssuedTo)
		require.Equal(t, want.CustomerId, got.CustomerId)
		require.Equal(t, want.IssuedAt, got.IssuedAt)
		require.Equal(t, want.ExpiresAt, got.ExpiresAt)
		// Expired in the past, so it must not grant anything.
		require.False(t, got.IsValid())
	})

	// IsValid tracks "may use paid features", not "is before the expiry date". The two
	// diverge inside the grace period, which is the whole point of having one — see
	// Test_lifecycle for the states in detail.
	t.Run("validity spans the grace period, not just the expiry", func(t *testing.T) {
		future := &licenseContents{Version: "v1", Id: "1", ExpiresAt: time.Now().UTC().Add(time.Hour)}
		got, err := getLicense(issueLicense(t, priv, future), pub)
		require.NoError(t, err)
		require.True(t, got.IsValid())

		justExpired := &licenseContents{Version: "v1", Id: "2", ExpiresAt: time.Now().UTC().Add(-time.Hour)}
		got, err = getLicense(issueLicense(t, priv, justExpired), pub)
		require.NoError(t, err)
		require.True(t, got.IsValid(), "an hour past expiry is still inside the grace period")

		longGone := &licenseContents{
			Version: "v1", Id: "3",
			ExpiresAt: time.Now().UTC().Add(-(DefaultGraceDays + 1) * 24 * time.Hour),
		}
		got, err = getLicense(issueLicense(t, priv, longGone), pub)
		require.NoError(t, err)
		require.False(t, got.IsValid(), "past the grace period, paid features stop")
	})

	// The point of the whole mechanism: a license signed by anyone else is rejected.
	t.Run("rejects a license signed by another key", func(t *testing.T) {
		_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		forged := issueLicense(t, otherPriv, &licenseContents{
			Version: "v1", Id: "forged", ExpiresAt: time.Now().UTC().Add(time.Hour * 24 * 365),
		})
		got, err := getLicense(forged, pub)
		require.Error(t, err)
		require.Nil(t, got)
	})

	t.Run("rejects tampered contents", func(t *testing.T) {
		original := &licenseContents{Version: "v1", Id: "1", ExpiresAt: time.Now().UTC().Add(-time.Hour)}
		encoded := issueLicense(t, priv, original)

		// Extend the expiry while keeping the original signature.
		envelope, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		var lf licenseFile
		require.NoError(t, json.Unmarshal(envelope, &lf))
		tampered, err := json.Marshal(&licenseContents{
			Version: "v1", Id: "1", ExpiresAt: time.Now().UTC().Add(time.Hour * 24 * 365),
		})
		require.NoError(t, err)
		lf.License = base64.StdEncoding.EncodeToString(tampered)
		reEnvelope, err := json.Marshal(lf)
		require.NoError(t, err)

		got, err := getLicense(base64.StdEncoding.EncodeToString(reEnvelope), pub)
		require.Error(t, err)
		require.Nil(t, got)
	})

	t.Run("rejects malformed input", func(t *testing.T) {
		for name, input := range map[string]string{
			"not base64":     "!!!not base64!!!",
			"not json":       base64.StdEncoding.EncodeToString([]byte("hello")),
			"empty envelope": base64.StdEncoding.EncodeToString([]byte(`{}`)),
		} {
			t.Run(name, func(t *testing.T) {
				got, err := getLicense(input, pub)
				require.Error(t, err)
				require.Nil(t, got)
			})
		}
	})
}

func ptr[T any](v T) *T { return &v }

func Test_lifecycle(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now().UTC()

	load := func(t *testing.T, c *licenseContents) *licenseContents {
		t.Helper()
		got, err := getLicense(issueLicense(t, priv, c), pub)
		require.NoError(t, err)
		return got
	}

	t.Run("states", func(t *testing.T) {
		cases := []struct {
			name      string
			expiresAt time.Time
			graceDays *int
			want      State
			usable    bool
		}{
			{"comfortably valid", now.Add(90 * 24 * time.Hour), nil, StateValid, true},
			{"expiring within the window", now.Add(10 * 24 * time.Hour), nil, StateExpiring, true},
			{"just expired, in grace", now.Add(-time.Hour), nil, StateGrace, true},
			{"past grace", now.Add(-30 * 24 * time.Hour), nil, StateFrozen, false},
			// An issuer may sell no grace at all; that must differ from saying nothing.
			{"no grace, expired", now.Add(-time.Minute), ptr(0), StateFrozen, false},
			{"no grace, still valid", now.Add(time.Hour), ptr(0), StateExpiring, true},
			{"long grace keeps it usable", now.Add(-20 * 24 * time.Hour), ptr(60), StateGrace, true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := load(t, &licenseContents{
					Version: "v1", Id: "1", ExpiresAt: tc.expiresAt, GraceDays: tc.graceDays,
				})
				require.Equal(t, tc.want, got.State())
				// IsValid means "may use paid features", which stays true through grace.
				require.Equal(t, tc.usable, got.IsValid())
			})
		}
	})

	t.Run("grace defaults to DefaultGraceDays", func(t *testing.T) {
		got := load(t, &licenseContents{Version: "v1", Id: "1", ExpiresAt: now})
		require.Equal(t, now.Add(DefaultGraceDays*24*time.Hour), got.graceEndsAt())
	})

	// A negative grace must not extend validity backwards into a nonsensical window.
	t.Run("negative grace is treated as none", func(t *testing.T) {
		got := load(t, &licenseContents{
			Version: "v1", Id: "1", ExpiresAt: now.Add(-time.Minute), GraceDays: ptr(-30),
		})
		require.Equal(t, StateFrozen, got.State())
		require.False(t, got.IsValid())
	})

	t.Run("EELicense surfaces the lifecycle", func(t *testing.T) {
		frozen := newFromLicenseContents(load(t, &licenseContents{
			Version: "v1", Id: "1", ExpiresAt: now.Add(-90 * 24 * time.Hour),
		}))
		require.Equal(t, StateFrozen, frozen.State())
		require.False(t, frozen.IsValid())

		// Grace end is later than expiry — the difference is what we tell the customer.
		grace := newFromLicenseContents(load(t, &licenseContents{
			Version: "v1", Id: "1", ExpiresAt: now.Add(-time.Hour),
		}))
		require.Equal(t, StateGrace, grace.State())
		require.True(t, grace.IsValid())
		require.True(t, grace.GracePeriodEndsAt().After(grace.ExpiresAt()))
	})

	t.Run("absent license is StateNone and grants nothing", func(t *testing.T) {
		var empty EELicense
		require.Equal(t, StateNone, empty.State())
		require.False(t, empty.IsValid())
		require.Nil(t, empty.Limits())
	})
}

func Test_Limits(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	t.Run("round-trip through the signed payload", func(t *testing.T) {
		got, err := getLicense(issueLicense(t, priv, &licenseContents{
			Version: "v1", Id: "1", ExpiresAt: time.Now().UTC().Add(time.Hour),
			Limits: &Limits{
				MaxJobs:                ptr(5),
				MaxConnections:         ptr(3),
				AllowedConnectionTypes: []string{"postgres", "mysql"},
			},
		}), pub)
		require.NoError(t, err)
		require.NotNil(t, got.Limits)
		require.Equal(t, 5, *got.Limits.MaxJobs)
		require.Equal(t, 3, *got.Limits.MaxConnections)
	})

	// nil must mean uncapped, never zero — getting this backwards would lock out every
	// customer holding a license that predates limits.
	t.Run("nil limits are uncapped", func(t *testing.T) {
		var l *Limits
		require.True(t, l.Allows("postgres"))
		require.True(t, l.Allows("anything"))
	})

	t.Run("an empty allowlist permits everything", func(t *testing.T) {
		l := &Limits{}
		require.True(t, l.Allows("mssql"))
	})

	t.Run("a populated allowlist is exclusive", func(t *testing.T) {
		l := &Limits{AllowedConnectionTypes: []string{"postgres"}}
		require.True(t, l.Allows("postgres"))
		require.False(t, l.Allows("mssql"))
	})
}

func Test_NewFromEnv(t *testing.T) {
	// Unset means "no EE license", which is a valid state and must not error — it simply
	// grants nothing.
	t.Run("unset", func(t *testing.T) {
		viper.Set(eeLicenseEvKey, "")
		eelicense, err := NewFromEnv()
		require.NoError(t, err)
		require.NotNil(t, eelicense)
		require.False(t, eelicense.IsValid())
		require.False(t, eelicense.ExpiresAt().IsZero())
	})

	// A license the embedded key cannot verify is a configuration error, and must be
	// surfaced rather than silently downgraded to "no license".
	t.Run("signed by a foreign key errors", func(t *testing.T) {
		_, foreignPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		viper.Set(eeLicenseEvKey, issueLicense(t, foreignPriv, &licenseContents{
			Version: "v1", Id: "foreign", ExpiresAt: time.Now().UTC().Add(time.Hour),
		}))
		t.Cleanup(func() { viper.Set(eeLicenseEvKey, "") })

		eelicense, err := NewFromEnv()
		require.Error(t, err)
		require.Nil(t, eelicense)
	})

	t.Run("garbage errors", func(t *testing.T) {
		viper.Set(eeLicenseEvKey, "not-a-license")
		t.Cleanup(func() { viper.Set(eeLicenseEvKey, "") })

		eelicense, err := NewFromEnv()
		require.Error(t, err)
		require.Nil(t, eelicense)
	})
}
