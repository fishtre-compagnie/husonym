package telemetry

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/fishtre-compagnie/husonym/internal/license"
)

// sealInfo is the context string the seal secret is derived with.
const sealInfo = "husonym usage report seal v1"

// KeyFingerprint designates a license key without revealing it: the hex SHA-256 of the key
// value, surrounding space trimmed.
func KeyFingerprint(keyValue string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(keyValue)))
	return hex.EncodeToString(sum[:])
}

// sealShape is the one spelling of a seal: the 32 bytes of the HMAC in lowercase hex.
var sealShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Seal returns the lowercase hex HMAC-SHA-256 of the exact bytes of document, keyed with a
// secret derived from the signature the license key carries. A key whose signature is not the
// 64 bytes of an Ed25519 signature is refused: the secret derived from an empty one is anybody's
// to derive. Errors never cite the key.
func Seal(keyValue string, document []byte) (string, error) {
	sum, err := sealOf(keyValue, document)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}

// sealOf is the seal as the 32 bytes of the HMAC.
func sealOf(keyValue string, document []byte) ([]byte, error) {
	signature, err := license.SignatureOf(strings.TrimSpace(keyValue))
	if err != nil {
		return nil, fmt.Errorf("sealing the usage report: %w", err)
	}
	secret, err := hkdf.Key(sha256.New, signature, nil, sealInfo, sha256.Size)
	if err != nil {
		return nil, errors.New("sealing the usage report: deriving the secret failed")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(document)
	return mac.Sum(nil), nil
}

// Verify checks that seal is the seal of document for the license key. A seal is 64 lowercase
// hex characters: anything else is refused, upper case included, so that a seal has one
// spelling. The 32 bytes it stands for are compared in constant time.
func Verify(keyValue string, document []byte, seal string) error {
	want, err := sealOf(keyValue, document)
	if err != nil {
		return err
	}
	if !sealShape.MatchString(seal) {
		return errors.New("the seal is not 64 lowercase hex characters")
	}
	given, err := hex.DecodeString(seal)
	if err != nil {
		return errors.New("the seal is not 64 lowercase hex characters")
	}
	if !hmac.Equal(want, given) {
		return errors.New("the seal does not match the document")
	}
	return nil
}
