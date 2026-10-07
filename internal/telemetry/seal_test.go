package telemetry

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const sealVectorPath = "testdata/seal-vector.json"

// sealVector is the reference for any other implementation of the seal.
type sealVector struct {
	Key            string `json:"key"`
	Document       string `json:"document"`
	KeyFingerprint string `json:"key_fingerprint"`
	Seal           string `json:"seal"`
}

// mintKey builds a key value signed by a throwaway pair, the way the issuer lays it out.
func mintKey(t *testing.T, content string, kid string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	env := map[string]string{
		"license":   base64.StdEncoding.EncodeToString([]byte(content)),
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(content))),
	}
	if kid != "" {
		env["kid"] = kid
	}
	raw, err := json.Marshal(env)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func readSealVector(t *testing.T) sealVector {
	t.Helper()
	raw, err := os.ReadFile(sealVectorPath)
	require.NoError(t, err)
	var v sealVector
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func Test_Seal_MatchesThePublishedVector(t *testing.T) {
	if *update {
		v := sealVector{
			Key:      mintKey(t, `{"version":"1","id":"lic_vector"}`, ""),
			Document: "{\"schema_version\":1,\"day\":\"2026-10-06\"}\n",
		}
		v.KeyFingerprint = KeyFingerprint(v.Key)
		sealed, err := Seal(v.Key, []byte(v.Document))
		require.NoError(t, err)
		v.Seal = sealed
		raw, err := json.MarshalIndent(v, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(sealVectorPath, append(raw, '\n'), 0o644))
	}

	v := readSealVector(t)
	require.Equal(t, v.KeyFingerprint, KeyFingerprint(v.Key))
	sealed, err := Seal(v.Key, []byte(v.Document))
	require.NoError(t, err)
	require.Equal(t, v.Seal, sealed)
	require.NoError(t, Verify(v.Key, []byte(v.Document), v.Seal))
}

func Test_Verify_RefusesAChangedDocumentOrAnotherKey(t *testing.T) {
	key := mintKey(t, `{"id":"a"}`, "k2")
	doc := []byte(`{"day":"2026-10-06"}`)
	sealed, err := Seal(key, doc)
	require.NoError(t, err)
	require.NoError(t, Verify(key, doc, sealed))

	changed := append([]byte(nil), doc...)
	changed[3] ^= 1
	require.Error(t, Verify(key, changed, sealed))
	require.Error(t, Verify(mintKey(t, `{"id":"a"}`, "k2"), doc, sealed))
}

func Test_Verify_RefusesASealThatIsNotHex(t *testing.T) {
	key := mintKey(t, `{"id":"a"}`, "")
	require.Error(t, Verify(key, []byte("x"), "not hex"))
	require.Error(t, Verify(key, []byte("x"), ""))
	sealed, err := Seal(key, []byte("x"))
	require.NoError(t, err)
	_, err = hex.DecodeString(sealed)
	require.NoError(t, err)
	require.Len(t, sealed, 64)
}

func Test_Seal_ErrorsNeverCiteTheKey(t *testing.T) {
	_, err := Seal("not-a-key-secret-value", []byte("x"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value")
	require.Error(t, Verify("not-a-key-secret-value", []byte("x"), "00"))
}

func Test_KeyFingerprint_IgnoresSurroundingSpace(t *testing.T) {
	key := mintKey(t, `{"id":"a"}`, "")
	require.Equal(t, KeyFingerprint(key), KeyFingerprint("  "+key+"\n"))
	require.Len(t, KeyFingerprint(key), 64)
	require.NotEqual(t, KeyFingerprint(key), KeyFingerprint(mintKey(t, `{"id":"a"}`, "")))
}
