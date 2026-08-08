package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// Issuing lives in the same package as verification on purpose: both sides share
// licenseContents and Limits, so it is structurally impossible to issue a license this
// codebase cannot read back. The previous shell script signed an arbitrary JSON file,
// which made a typo in a field name a silent, undetectable problem.

// IssueRequest describes a license to mint.
type IssueRequest struct {
	// Human-readable customer name, recorded in the license and the registry.
	IssuedTo string
	// Stable customer identifier, used to tie a license to an account.
	CustomerId string
	// When the paid features stop, before the grace period is applied.
	ExpiresAt time.Time
	// Defaults to now when zero.
	IssuedAt time.Time
	// Nil means DefaultGraceDays. Zero means no grace at all.
	GraceDays *int
	// Nil means uncapped.
	Limits *Limits
	// Defaults to a random identifier when empty.
	Id string
}

func (r *IssueRequest) validate() error {
	var problems []string
	if r.IssuedTo == "" {
		problems = append(problems, "issued_to is required")
	}
	if r.CustomerId == "" {
		problems = append(problems, "customer_id is required")
	}
	if r.ExpiresAt.IsZero() {
		problems = append(problems, "expires_at is required")
	}
	// Refuse to mint something already dead: it is almost always a units mistake in the
	// duration, and it would be indistinguishable from a lapsed license in the field.
	if !r.ExpiresAt.IsZero() && !r.ExpiresAt.After(time.Now().UTC()) {
		problems = append(problems, "expires_at is in the past")
	}
	if r.GraceDays != nil && *r.GraceDays < 0 {
		problems = append(problems, "grace_days cannot be negative")
	}
	if l := r.Limits; l != nil {
		if l.MaxJobs != nil && *l.MaxJobs < 0 {
			problems = append(problems, "limits.max_jobs cannot be negative")
		}
		if l.MaxConnections != nil && *l.MaxConnections < 0 {
			problems = append(problems, "limits.max_connections cannot be negative")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid license request: %v", problems)
	}
	return nil
}

// IssuedLicense is the result of minting: the encoded value to hand to the customer,
// plus the metadata worth recording.
type IssuedLicense struct {
	// The value to set as EE_LICENSE.
	Encoded string

	Id         string    `json:"id"`
	IssuedTo   string    `json:"issued_to"`
	CustomerId string    `json:"customer_id"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	GraceDays  *int      `json:"grace_days,omitempty"`
	Limits     *Limits   `json:"limits,omitempty"`
}

// Issue mints and signs a license. The returned Encoded value is what goes into
// EE_LICENSE, and getLicense() verifies it against the embedded public key.
func Issue(req IssueRequest, priv ed25519.PrivateKey) (*IssuedLicense, error) {
	if len(priv) == 0 {
		return nil, errors.New("no private key provided")
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	id := req.Id
	if id == "" {
		var err error
		if id, err = randomId(); err != nil {
			return nil, err
		}
	}
	issuedAt := req.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}

	contents := &licenseContents{
		Version:    "v1",
		Id:         id,
		IssuedTo:   req.IssuedTo,
		CustomerId: req.CustomerId,
		IssuedAt:   issuedAt.UTC().Truncate(time.Second),
		ExpiresAt:  req.ExpiresAt.UTC().Truncate(time.Second),
		GraceDays:  req.GraceDays,
		Limits:     req.Limits,
	}

	raw, err := json.Marshal(contents)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal license contents: %w", err)
	}
	envelope, err := json.Marshal(licenseFile{
		License:   base64.StdEncoding.EncodeToString(raw),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw)),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to marshal license envelope: %w", err)
	}

	return &IssuedLicense{
		Encoded:    base64.StdEncoding.EncodeToString(envelope),
		Id:         contents.Id,
		IssuedTo:   contents.IssuedTo,
		CustomerId: contents.CustomerId,
		IssuedAt:   contents.IssuedAt,
		ExpiresAt:  contents.ExpiresAt,
		GraceDays:  contents.GraceDays,
		Limits:     contents.Limits,
	}, nil
}

func randomId() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("unable to generate license id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ParsePrivateKey reads a PEM-encoded Ed25519 private key, as produced by
// `openssl genpkey -algorithm ed25519`.
func ParsePrivateKey(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found in private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key: %w", err)
	}
	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an ed25519 private key: %T", parsed)
	}
	return priv, nil
}

// PublicKeyFingerprint identifies which signing key a license was minted with, so a key
// rotation is traceable in the registry rather than guessed at.
func PublicKeyFingerprint(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)[:16]
}

// EmbeddedPublicKey returns the key this binary verifies against. Comparing it to the
// signing key before issuing catches the mistake of minting licenses with a key the
// shipped product will reject.
func EmbeddedPublicKey() (ed25519.PublicKey, error) {
	return parsePublicKey(publicKeyPEM)
}
