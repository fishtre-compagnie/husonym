package license

import (
	"crypto/ed25519"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// LegacyKid names the key that verifies a license key whose envelope carries no kid:
// every key issued before kids existed.
const LegacyKid = "k1"

//go:embed keys/*.pem
var keysFS embed.FS

// Keyring is the set of public keys a license key may be verified against, by kid.
type Keyring map[string]ed25519.PublicKey

// EmbeddedKeyring returns the public keys this binary verifies license keys against.
// The kid of a key is the name of its file under keys/, without the .pem extension.
func EmbeddedKeyring() (Keyring, error) {
	entries, err := keysFS.ReadDir("keys")
	if err != nil {
		return nil, fmt.Errorf("unable to list the embedded public keys: %w", err)
	}
	ring := Keyring{}
	for _, entry := range entries {
		name := entry.Name()
		raw, err := keysFS.ReadFile("keys/" + name)
		if err != nil {
			return nil, fmt.Errorf("unable to read the embedded public key %s: %w", name, err)
		}
		kid := strings.TrimSuffix(name, ".pem")
		pub, err := parsePublicKeyPEM(raw)
		if err != nil {
			return nil, fmt.Errorf("embedded public key %s: %w", kid, err)
		}
		ring[kid] = pub
	}
	return ring, nil
}

// KidOf returns the kid under which pub is in the ring.
func (r Keyring) KidOf(pub ed25519.PublicKey) (string, bool) {
	for kid, candidate := range r {
		if candidate.Equal(pub) {
			return kid, true
		}
	}
	return "", false
}

func parsePublicKeyPEM(raw []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse the public key: %w", err)
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the public key is not ed25519: %T", parsed)
	}
	return pub, nil
}
