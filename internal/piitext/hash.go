package piitext

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	stdhash "hash"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// HashKey is the key the hash operator is computed under. The same text gives the same hash
// under the same key, and nothing tells a text from its hash without the key.
type HashKey [32]byte

// HashKeyHeader is the request header a run hands its HashKey to the API in.
const HashKeyHeader = "Husonym-Pii-Hash-Key"

// Encode returns the key as it travels in HashKeyHeader.
func (k HashKey) Encode() string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// ParseHashKey reads what Encode wrote.
func ParseHashKey(header string) (HashKey, error) {
	var key HashKey
	bits, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return key, errors.New("the hash key is not base64")
	}
	if len(bits) != len(key) {
		return key, fmt.Errorf("the hash key has %d bytes, and not %d", len(bits), len(key))
	}
	copy(key[:], bits)
	return key, nil
}

func drawHashKey() (HashKey, error) {
	var key HashKey
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("drawing the hash key of the process: %w", err)
	}
	return key, nil
}

// AccountHashKey returns the key of the values of an account that belong to no run: a direct
// call to the anonymization endpoints, the preview of a column. It is derived from the key of
// the process under a label of its own, so two accounts never hash under the same key, and one
// account hashes a text the same way for as long as the process lives.
//
// A nil engine has no key to give.
func (e *Engine) AccountHashKey(accountId string) *HashKey {
	if e == nil {
		return nil
	}
	mac := hmac.New(sha256.New, e.processKey[:])
	mac.Write([]byte("account:" + accountId))
	var key HashKey
	copy(key[:], mac.Sum(nil))
	return &key
}

// hashVariant is one of the three forms of a hash. The hash type of a configuration chooses the
// length of what is written, which is what the column it lands in depends on; every form is an
// HMAC of the exact text of the finding.
type hashVariant struct {
	// name starts the message, so that the short form of a text is not the beginning of its
	// longer ones.
	name   string
	digest func() stdhash.Hash
	// bytes is how much of the HMAC is written, in hexadecimal: twice as many characters.
	bytes int
}

var (
	hashShort = hashVariant{name: "md5", digest: sha256.New, bytes: 16}
	hash256   = hashVariant{name: "sha256", digest: sha256.New, bytes: sha256.Size}
	hash512   = hashVariant{name: "sha512", digest: sha512.New, bytes: sha512.Size}
)

// variantOf reads a hash type: 64 hexadecimal characters for SHA256, 128 for SHA512, and 32 for
// MD5, for no type and for a type this version does not know.
func variantOf(algo mgmtv1alpha1.PiiAnonymizer_Hash_HashType) hashVariant {
	switch algo {
	case mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256:
		return hash256
	case mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA512:
		return hash512
	default:
		return hashShort
	}
}

// hashOperator puts the keyed hash of a finding in its place, in lowercase hexadecimal. The entity type
// is no part of it: a text has one hash under a key, whatever it was found as.
func hashOperator(key HashKey, algo mgmtv1alpha1.PiiAnonymizer_Hash_HashType) operator {
	variant := variantOf(algo)
	return func(_ context.Context, _, text string) (string, error) {
		mac := hmac.New(variant.digest, key[:])
		mac.Write([]byte(variant.name))
		mac.Write([]byte{0})
		mac.Write([]byte(text))
		return hex.EncodeToString(mac.Sum(nil)[:variant.bytes]), nil
	}
}
