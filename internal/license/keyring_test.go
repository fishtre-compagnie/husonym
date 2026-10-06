package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

var keyringTestContent = []byte(`{"version":"v1","id":"a","issued_to":"x","customer_id":"c","issued_at":"2026-01-01T00:00:00Z","expires_at":"2027-01-01T00:00:00Z"}`)

// encodeWithKid builds a key value whose envelope names kid next to the signature.
func encodeWithKid(t *testing.T, content []byte, priv ed25519.PrivateKey, kid string) string {
	t.Helper()
	envelope, err := json.Marshal(map[string]string{
		"license":   b64(content),
		"signature": b64(ed25519.Sign(priv, content)),
		"kid":       kid,
	})
	require.NoError(t, err)
	return b64(envelope)
}

func generateKeyring(t *testing.T) (Keyring, ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return Keyring{LegacyKid: pubA, "k2": pubB}, privA, privB
}

func Test_Parse_PicksTheKeyByKid(t *testing.T) {
	ring, _, privB := generateKeyring(t)

	key, err := parseWith(encodeWithKid(t, keyringTestContent, privB, "k2"), ring)
	require.NoError(t, err)
	require.Equal(t, "a", key.Id)
}

func Test_Parse_WithoutKidUsesTheLegacyKey(t *testing.T) {
	ring, privA, privB := generateKeyring(t)

	key, err := parseWith(encode(t, keyringTestContent, privA), ring)
	require.NoError(t, err)
	require.Equal(t, "a", key.Id)

	_, err = parseWith(encode(t, keyringTestContent, privB), ring)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature")
}

func Test_Parse_WithoutKidRefusesWhenTheRingHasNoLegacyKey(t *testing.T) {
	_, privA, _ := generateKeyring(t)
	pubB, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	_, err = parseWith(encode(t, keyringTestContent, privA), Keyring{"k2": pubB})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not know (kid)")
}

func Test_Parse_RefusesAnUnknownKid(t *testing.T) {
	ring, _, privB := generateKeyring(t)

	key, err := parseWith(encodeWithKid(t, keyringTestContent, privB, "k9"), ring)
	require.Error(t, err)
	require.Nil(t, key)
	require.Contains(t, err.Error(), "license key was signed with a key this version does not know (kid)")
}

func Test_Parse_ATamperedKidOnlyFailsVerification(t *testing.T) {
	ring, privA, _ := generateKeyring(t)

	_, err := parseWith(encodeWithKid(t, keyringTestContent, privA, "k2"), ring)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature")
}

func Test_Keyring_KidOf(t *testing.T) {
	ring, _, _ := generateKeyring(t)
	other, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	kid, ok := ring.KidOf(ring["k2"])
	require.True(t, ok)
	require.Equal(t, "k2", kid)

	_, ok = ring.KidOf(other)
	require.False(t, ok)
}

func Test_EmbeddedKeyring_HoldsTheLegacyKey(t *testing.T) {
	ring, err := EmbeddedKeyring()
	require.NoError(t, err)
	require.Contains(t, ring, LegacyKid)
	require.Len(t, ring[LegacyKid], ed25519.PublicKeySize)
}
