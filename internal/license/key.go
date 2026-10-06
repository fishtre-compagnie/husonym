package license

import (
	"crypto/ed25519"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

//go:embed husonym_ee_pub.pem
var publicKeyPEM []byte

// Key is the content of a license key, as signed by the issuer.
type Key struct {
	Version    string    `json:"version"`
	Id         string    `json:"id"`
	IssuedTo   string    `json:"issued_to"`
	CustomerId string    `json:"customer_id"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// Nil means the key does not say; zero means no grace at all.
	GraceDays *int    `json:"grace_days,omitempty"`
	Limits    *Limits `json:"limits,omitempty"`
	// Plan is a label for humans; nothing is decided from it.
	Plan string `json:"plan,omitempty"`
	// Nil means the key does not list features and allows all of them; an empty list allows none.
	// omitzero, unlike omitempty, keeps an empty list when the key is written.
	Features []string `json:"features,omitzero"`
	// Telemetry is read through TelemetryMode, which gives unknown values a meaning.
	Telemetry string `json:"telemetry,omitempty"`
}

// Limits caps what a license allows. A nil field is uncapped, which is distinct
// from a cap of zero.
type Limits struct {
	MaxJobs                *int     `json:"max_jobs,omitempty"`
	MaxConnections         *int     `json:"max_connections,omitempty"`
	MaxSources             *int     `json:"max_sources,omitempty"`
	AllowedConnectionTypes []string `json:"allowed_connection_types,omitempty"`
}

// envelope is what the key value decodes to: the content and its signature.
type envelope struct {
	License   string `json:"license"`
	Signature string `json:"signature"`
}

// Parse reads a key value and verifies its signature against the embedded public key.
func Parse(value string) (*Key, error) {
	pub, err := EmbeddedPublicKey()
	if err != nil {
		return nil, err
	}
	return parseWith(value, pub)
}

// parseWith reads a key value and verifies its signature against pub. Errors name the
// stage that failed and never echo the key material.
func parseWith(value string, pub ed25519.PublicKey) (*Key, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("license key is not valid base64 (decoding)")
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("license key envelope is not valid JSON (envelope)")
	}
	content, err := base64.StdEncoding.DecodeString(env.License)
	if err != nil {
		return nil, errors.New("license key envelope has a license field that is not valid base64 (envelope)")
	}
	signature, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return nil, errors.New("license key envelope has a signature field that is not valid base64 (envelope)")
	}

	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, content, signature) {
		return nil, errors.New("license key signature does not match its content (signature)")
	}

	var key Key
	if err := json.Unmarshal(content, &key); err != nil {
		return nil, errors.New("license key content is not valid JSON (content)")
	}
	return &key, nil
}

// EmbeddedPublicKey returns the public key this binary verifies license keys against.
func EmbeddedPublicKey() (ed25519.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, errors.New("no PEM block found in the embedded public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse the embedded public key: %w", err)
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the embedded public key is not ed25519: %T", parsed)
	}
	return pub, nil
}
