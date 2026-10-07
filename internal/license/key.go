package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

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

// envelope is what the key value decodes to: the content and its signature. The kid names
// the public key to verify with; it sits outside the signed content, so altering it can
// only make verification fail. Keys issued before kids existed carry none.
type envelope struct {
	License   string `json:"license"`
	Signature string `json:"signature"`
	Kid       string `json:"kid,omitempty"`
}

// Parse reads a key value and verifies its signature against the embedded public keys.
func Parse(value string) (*Key, error) {
	ring, err := EmbeddedKeyring()
	if err != nil {
		return nil, err
	}
	return ParseWith(value, ring)
}

// ParseWith reads a key value and verifies its signature against the key of ring that its
// kid names, or LegacyKid without one. Errors name the stage that failed and never echo
// the key material.
func ParseWith(value string, ring Keyring) (*Key, error) {
	env, content, signature, err := decode(value)
	if err != nil {
		return nil, err
	}

	kid := env.Kid
	if kid == "" {
		kid = LegacyKid
	}
	pub, known := ring[kid]
	if !known {
		return nil, errors.New("license key was signed with a key this version does not know (kid)")
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

// SignatureOf returns the signature bytes a key value carries. It does not verify them: the
// caller holds a key that a provider already verified. It refuses a signature that is not of
// the length Ed25519 gives: what is derived from an empty one, anybody can derive. Errors never
// echo the key material.
func SignatureOf(value string) ([]byte, error) {
	_, _, signature, err := decode(value)
	if err != nil {
		return nil, err
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, errors.New("license key envelope has a signature that is not an Ed25519 signature (envelope)")
	}
	return signature, nil
}

// decode unwraps a key value into its envelope, the signed content and the signature.
func decode(value string) (env envelope, content, signature []byte, err error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return env, nil, nil, errors.New("license key is not valid base64 (decoding)")
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, nil, nil, errors.New("license key envelope is not valid JSON (envelope)")
	}
	content, err = base64.StdEncoding.DecodeString(env.License)
	if err != nil {
		return env, nil, nil, errors.New("license key envelope has a license field that is not valid base64 (envelope)")
	}
	signature, err = base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return env, nil, nil, errors.New("license key envelope has a signature field that is not valid base64 (envelope)")
	}
	return env, content, signature, nil
}
