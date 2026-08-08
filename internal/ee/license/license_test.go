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

	t.Run("expiry decides validity", func(t *testing.T) {
		future := &licenseContents{Version: "v1", Id: "1", ExpiresAt: time.Now().UTC().Add(time.Hour)}
		got, err := getLicense(issueLicense(t, priv, future), pub)
		require.NoError(t, err)
		require.True(t, got.IsValid())

		past := &licenseContents{Version: "v1", Id: "2", ExpiresAt: time.Now().UTC().Add(-time.Hour)}
		got, err = getLicense(issueLicense(t, priv, past), pub)
		require.NoError(t, err)
		require.False(t, got.IsValid())
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
