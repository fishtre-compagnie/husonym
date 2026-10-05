package piitext

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

const (
	md5Type    = mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_MD5
	sha256Type = mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256
	sha512Type = mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA512
)

func key(fill byte) *HashKey {
	var k HashKey
	for i := range k {
		k[i] = fill
	}
	return &k
}

// hashed is the hash a transformer writes for part of text, alone in the output.
func hashed(t *testing.T, operator *mgmtv1alpha1.PiiAnonymizer, hashKey *HashKey, text, part, entity string) string {
	t.Helper()
	config := withDefault(redact())
	config.EntityAnonymizers = map[string]*mgmtv1alpha1.PiiAnonymizer{entity: operator}
	out := mustRewrite(t, config, Options{HashKey: hashKey}, text, found(t, text, part, entity, 0.85))
	before, after, _ := strings.Cut(text, part)
	return strings.TrimSuffix(strings.TrimPrefix(out, before), after)
}

func Test_Hash(t *testing.T) {
	text := "Call Zoé now"

	t.Run("the same text under the same key gives the same hash", func(t *testing.T) {
		first := hashed(t, hashOf(sha256Type), key(1), text, "Zoé", "PERSON")
		second := hashed(t, hashOf(sha256Type), key(1), "Zoé a écrit", "Zoé", "PERSON")
		require.Equal(t, first, second)
	})

	t.Run("another key gives another hash", func(t *testing.T) {
		require.NotEqual(t,
			hashed(t, hashOf(sha256Type), key(1), text, "Zoé", "PERSON"),
			hashed(t, hashOf(sha256Type), key(2), text, "Zoé", "PERSON"))
	})

	t.Run("another text gives another hash", func(t *testing.T) {
		require.NotEqual(t,
			hashed(t, hashOf(sha256Type), key(1), "Call Zoé now", "Zoé", "PERSON"),
			hashed(t, hashOf(sha256Type), key(1), "Call zoé now", "zoé", "PERSON"))
	})

	t.Run("the entity type does not change the hash", func(t *testing.T) {
		require.Equal(t,
			hashed(t, hashOf(sha256Type), key(1), text, "Zoé", "PERSON"),
			hashed(t, hashOf(sha256Type), key(1), text, "Zoé", "ORGANIZATION"))
	})

	t.Run("each hash type has its length, in lowercase hexadecimal", func(t *testing.T) {
		var unknown mgmtv1alpha1.PiiAnonymizer_Hash_HashType = 99
		for name, tc := range map[string]struct {
			operator *mgmtv1alpha1.PiiAnonymizer
			length   int
		}{
			"sha256":      {hashOf(sha256Type), 64},
			"sha512":      {hashOf(sha512Type), 128},
			"md5":         {hashOf(md5Type), 32},
			"unspecified": {hashOf(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_UNSPECIFIED), 32},
			"unknown":     {hashOf(unknown), 32},
			"unset": {&mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{
				Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{},
			}}, 32},
		} {
			require.Regexp(t, "^[0-9a-f]+$", hashed(t, tc.operator, key(1), text, "Zoé", "PERSON"), name)
			require.Len(t, hashed(t, tc.operator, key(1), text, "Zoé", "PERSON"), tc.length, name)
		}
	})

	t.Run("a hash is the keyed digest of its length and of the exact text", func(t *testing.T) {
		mac256 := hmac.New(sha256.New, key(1)[:])
		mac256.Write([]byte("sha256\x00Zoé"))
		require.Equal(t, hex.EncodeToString(mac256.Sum(nil)), hashed(t, hashOf(sha256Type), key(1), text, "Zoé", "PERSON"))

		mac512 := hmac.New(sha512.New, key(1)[:])
		mac512.Write([]byte("sha512\x00Zoé"))
		require.Equal(t, hex.EncodeToString(mac512.Sum(nil)), hashed(t, hashOf(sha512Type), key(1), text, "Zoé", "PERSON"))

		short := hmac.New(sha256.New, key(1)[:])
		short.Write([]byte("md5\x00Zoé"))
		require.Equal(t, hex.EncodeToString(short.Sum(nil)[:16]), hashed(t, hashOf(md5Type), key(1), text, "Zoé", "PERSON"))
	})

	t.Run("the short hash of a text is not the beginning of its longer ones", func(t *testing.T) {
		short := hashed(t, hashOf(md5Type), key(1), text, "Zoé", "PERSON")
		require.False(t, strings.HasPrefix(hashed(t, hashOf(sha256Type), key(1), text, "Zoé", "PERSON"), short))
		require.False(t, strings.HasPrefix(hashed(t, hashOf(sha512Type), key(1), text, "Zoé", "PERSON"), short))
	})

	t.Run("without a key of its own a transformer uses the key of its engine", func(t *testing.T) {
		analyzer := finding(t, found(t, text, "Zoé", "PERSON", 0.85))
		first, second := newEngine(t, analyzer), newEngine(t, analyzer)
		hashWith := func(engine *Engine) string {
			transformer, err := engine.Transformer(withDefault(hashOf(sha256Type)), Options{})
			require.NoError(t, err)
			out, err := transformer.Transform(context.Background(), text)
			require.NoError(t, err)
			return out
		}
		require.Equal(t, hashWith(first), hashWith(first), "one engine hashes a text the same way every time")
		require.NotEqual(t, hashWith(first), hashWith(second), "two engines draw two keys")
	})

	t.Run("outside a run two accounts never hash under the same key, and one account keeps its own", func(t *testing.T) {
		engine := newEngine(t, finding(t, found(t, text, "Zoé", "PERSON", 0.85)))
		hashFor := func(key *HashKey) string {
			transformer, err := engine.Transformer(withDefault(hashOf(sha256Type)), Options{HashKey: key})
			require.NoError(t, err)
			out, err := transformer.Transform(context.Background(), text)
			require.NoError(t, err)
			return out
		}
		require.Equal(t, hashFor(engine.AccountHashKey("account-a")), hashFor(engine.AccountHashKey("account-a")))
		require.NotEqual(t, hashFor(engine.AccountHashKey("account-a")), hashFor(engine.AccountHashKey("account-b")))
		require.NotEqual(t, hashFor(engine.AccountHashKey("account-a")), hashFor(nil),
			"the key of an account is not the key of the process")

		other := newEngine(t, finding(t, found(t, text, "Zoé", "PERSON", 0.85)))
		require.NotEqual(t, *engine.AccountHashKey("account-a"), *other.AccountHashKey("account-a"),
			"another process gives the account another key")
		var none *Engine
		require.Nil(t, none.AccountHashKey("account-a"))
	})

	t.Run("two hashed findings of one value each hash their own text", func(t *testing.T) {
		both := "Zoé met Bob and Zoé"
		config := withDefault(hashOf(md5Type))
		out := mustRewrite(t, config, Options{HashKey: key(1)}, both,
			found(t, both, "Zoé", "PERSON", 0.85), found(t, both, "Bob", "PERSON", 0.85))
		zoe := hashed(t, hashOf(md5Type), key(1), text, "Zoé", "PERSON")
		bob := hashed(t, hashOf(md5Type), key(1), "Call Bob", "Bob", "PERSON")
		require.Equal(t, zoe+" met "+bob+" and Zoé", out)
	})
}

func Test_HashKey_Header(t *testing.T) {
	t.Run("a key survives its header form", func(t *testing.T) {
		parsed, err := ParseHashKey(key(7).Encode())
		require.NoError(t, err)
		require.Equal(t, *key(7), parsed)
	})

	t.Run("what is not a key of 32 bytes is refused", func(t *testing.T) {
		for _, header := range []string{"", "not base64 !", "c2hvcnQ=", key(7).Encode() + "AAAA"} {
			_, err := ParseHashKey(header)
			require.Error(t, err, header)
		}
	})
}
