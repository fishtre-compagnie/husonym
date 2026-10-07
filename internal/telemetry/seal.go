package telemetry

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

// Seal returns the hex HMAC-SHA-256 of the exact bytes of document, keyed with a secret
// derived from the signature the license key carries. Errors never cite the key.
func Seal(keyValue string, document []byte) (string, error) {
	signature, err := license.SignatureOf(strings.TrimSpace(keyValue))
	if err != nil {
		return "", fmt.Errorf("sealing the usage report: %w", err)
	}
	secret, err := hkdf.Key(sha256.New, signature, nil, sealInfo, sha256.Size)
	if err != nil {
		return "", errors.New("sealing the usage report: deriving the secret failed")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(document)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Verify checks that seal is the seal of document for the license key, comparing in constant
// time. A seal that is not valid hex fails the check like any other mismatch.
func Verify(keyValue string, document []byte, seal string) error {
	want, err := Seal(keyValue, document)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(strings.ToLower(seal))) {
		return errors.New("the seal does not match the document")
	}
	return nil
}
