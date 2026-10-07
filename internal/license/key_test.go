package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return strings.TrimSpace(string(raw))
}

func readPublicKey(t *testing.T, name string) ed25519.PublicKey {
	t.Helper()
	block, _ := pem.Decode([]byte(readTestdata(t, name)))
	require.NotNil(t, block)
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	require.NoError(t, err)
	pub, ok := parsed.(ed25519.PublicKey)
	require.True(t, ok)
	return pub
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// encode builds a key value from raw content bytes, signed with priv.
func encode(t *testing.T, content []byte, priv ed25519.PrivateKey) string {
	t.Helper()
	return encodeEnvelope(t, b64(content), b64(ed25519.Sign(priv, content)))
}

func encodeEnvelope(t *testing.T, license, signature string) string {
	t.Helper()
	envelope, err := json.Marshal(map[string]string{"license": license, "signature": signature})
	require.NoError(t, err)
	return b64(envelope)
}

func Test_ParseWith_AcceptsAKeyIssuedBeforeThisPackage(t *testing.T) {
	key, err := ParseWith(readTestdata(t, "issued-before.key"), Keyring{LegacyKid: readPublicKey(t, "issued-before.pub.pem")})
	require.NoError(t, err)
	require.Equal(t, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), key.ExpiresAt)
	require.Equal(t, 7, *key.GraceDays)
	require.Equal(t, 20, *key.Limits.MaxJobs)
	require.Equal(t, 10, *key.Limits.MaxConnections)
	require.Equal(t, []string{"postgres", "mysql"}, key.Limits.AllowedConnectionTypes)
	require.Equal(t, "v1", key.Version)
	require.Equal(t, "Fixture Corp", key.IssuedTo)
	require.Equal(t, "cust-fixture", key.CustomerId)
	require.NotEmpty(t, key.Id)
}

func Test_ParseWith_AKeyIssuedBeforeNamesNoFeatureAndUnlocksAll(t *testing.T) {
	key, err := ParseWith(readTestdata(t, "issued-before.key"), Keyring{LegacyKid: readPublicKey(t, "issued-before.pub.pem")})
	require.NoError(t, err)
	require.Nil(t, key.Features)
	require.Nil(t, key.Limits.MaxSources)
	for _, f := range AllFeatures() {
		require.True(t, key.HasFeature(f), f)
	}
}

func Test_ParseWith_ReadsBackTheFeaturesAndSourceCapItCarries(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	content, err := json.Marshal(Key{
		Version:   "v1",
		Id:        "a",
		Plan:      "team",
		Features:  []string{"job_hooks"},
		Telemetry: "none",
		Limits:    &Limits{MaxSources: ptr(3)},
	})
	require.NoError(t, err)

	key, err := ParseWith(encode(t, content, priv), Keyring{LegacyKid: pub})
	require.NoError(t, err)
	require.Equal(t, []string{"job_hooks"}, key.Features)
	require.Equal(t, "team", key.Plan)
	require.Equal(t, TelemetryNone, key.TelemetryMode())
	require.Equal(t, 3, *key.Limits.MaxSources)
	require.True(t, key.HasFeature(FeatureJobHooks))
	require.False(t, key.HasFeature(FeatureSso))
}

func Test_ParseWith_DistinguishesAbsentFromZero(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	bare, err := ParseWith(encode(t, []byte(`{"version":"v1","id":"a","issued_to":"x","customer_id":"c","issued_at":"2026-01-01T00:00:00Z","expires_at":"2027-01-01T00:00:00Z"}`), priv), Keyring{LegacyKid: pub})
	require.NoError(t, err)
	require.Nil(t, bare.GraceDays)
	require.Nil(t, bare.Limits)

	zero, err := ParseWith(encode(t, []byte(`{"version":"v1","id":"a","issued_to":"x","customer_id":"c","issued_at":"2026-01-01T00:00:00Z","expires_at":"2027-01-01T00:00:00Z","grace_days":0,"limits":{"max_jobs":0}}`), priv), Keyring{LegacyKid: pub})
	require.NoError(t, err)
	require.NotNil(t, zero.GraceDays)
	require.Equal(t, 0, *zero.GraceDays)
	require.NotNil(t, zero.Limits.MaxJobs)
	require.Equal(t, 0, *zero.Limits.MaxJobs)
	require.Nil(t, zero.Limits.MaxConnections)
}

func Test_ParseWith_Refuses(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	content := []byte(`{"version":"v1","id":"a","issued_to":"x","customer_id":"c","issued_at":"2026-01-01T00:00:00Z","expires_at":"2027-01-01T00:00:00Z"}`)
	tampered := append([]byte(nil), content...)
	tampered[len(tampered)-3] ^= 0x01

	tests := []struct {
		name    string
		value   string
		message string
	}{
		{"value is not base64", "%%% not base64 %%%", "decoding"},
		{"envelope is not JSON", b64([]byte("not json")), "envelope"},
		{"license field is not base64", encodeEnvelope(t, "%%%", b64(ed25519.Sign(priv, content))), "envelope"},
		{"signature field is not base64", encodeEnvelope(t, b64(content), "%%%"), "envelope"},
		{"signed by another pair", encode(t, content, otherPriv), "signature"},
		{"signed content is not JSON", encode(t, []byte("not json"), priv), "content"},
		{"content altered after signing", encodeEnvelope(t, b64(tampered), b64(ed25519.Sign(priv, content))), "signature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParseWith(tt.value, Keyring{LegacyKid: pub})
			require.Error(t, err)
			require.Nil(t, key)
			require.Contains(t, err.Error(), tt.message)
			if len(tt.value) > 8 {
				require.NotContains(t, err.Error(), tt.value)
			}
		})
	}
}

func Test_Parse_RefusesAKeyNotSignedByTheEmbeddedKey(t *testing.T) {
	_, err := Parse(readTestdata(t, "issued-before.key"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature")
}

func Test_SignatureOf_ReturnsTheSignatureTheKeyCarries(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	content := []byte(`{"version":"1"}`)
	signature := ed25519.Sign(priv, content)

	got, err := SignatureOf(encode(t, content, priv))
	require.NoError(t, err)
	require.Equal(t, signature, got)

	withKid, err := json.Marshal(map[string]string{"license": b64(content), "signature": b64(signature), "kid": "k2"})
	require.NoError(t, err)
	got, err = SignatureOf(b64(withKid))
	require.NoError(t, err)
	require.Equal(t, signature, got)
}

func Test_SignatureOf_RefusesWithoutEchoingTheValue(t *testing.T) {
	for _, value := range []string{"%%%secret", b64([]byte("secret")), encodeEnvelope(t, b64([]byte("x")), "%%%secret")} {
		_, err := SignatureOf(value)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}
