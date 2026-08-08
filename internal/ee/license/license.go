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

	"github.com/spf13/viper"
)

//go:embed husonym_ee_pub.pem
var publicKeyPEM string

const (
	eeLicenseEvKey = "EE_LICENSE"
)

// The expected base64 decoded structure of the EE_LICENSE file
type licenseFile struct {
	License   string `json:"license"`
	Signature string `json:"signature"`
}

// Default grace period applied when a license does not name one. Chosen over a hard
// stop at expiry: a lapsed license is usually a slow invoice, and cutting a customer's
// environment off the same day turns that into an incident they blame us for.
const DefaultGraceDays = 14

// Lifecycle of a license, derived entirely from the expiry date plus the grace period.
// There is no state stored anywhere — the same license yields the same state on any
// instance at any moment.
type State string

const (
	// No license configured at all.
	StateNone State = "none"
	// Valid, and not close enough to expiry to warn about.
	StateValid State = "valid"
	// Valid, but expiring within ExpiringWindow — worth warning about.
	StateExpiring State = "expiring"
	// Past expiry, inside the grace period. Everything still works.
	StateGrace State = "grace"
	// Past expiry and past grace. Paid features stop.
	StateFrozen State = "frozen"
)

// How long before expiry we start flagging it.
const ExpiringWindow = 30 * 24 * time.Hour

// IsValid answers one question: may this deployment use the paid features right now.
// It is deliberately true throughout the grace period, so every caller that gates on it
// inherits the grace behaviour without knowing the lifecycle exists.
//
// Use State() when the distinction matters — warning banners, logs, diagnostics.
type EEInterface interface {
	IsValid() bool
	ExpiresAt() time.Time
}

var _ EEInterface = (*EELicense)(nil)

type EELicense struct {
	contents *licenseContents
}

func (e *EELicense) IsValid() bool {
	return e.contents != nil && e.contents.IsValid()
}

// State reports where in its lifecycle this license sits.
func (e *EELicense) State() State {
	if e.contents == nil {
		return StateNone
	}
	return e.contents.State()
}

// Limits carried by the license, or nil when it names none — in which case nothing is
// capped. Callers must treat nil as unlimited rather than as zero.
func (e *EELicense) Limits() *Limits {
	if e.contents == nil {
		return nil
	}
	return e.contents.Limits
}

// GracePeriodEndsAt is the moment paid features actually stop, which is later than
// ExpiresAt. Surfacing the distinction avoids telling a customer inside their grace
// window that everything has already stopped.
func (e *EELicense) GracePeriodEndsAt() time.Time {
	if e.contents == nil {
		return time.Now().UTC()
	}
	return e.contents.graceEndsAt()
}

func (e *EELicense) ExpiresAt() time.Time {
	if e.contents == nil {
		return time.Now().UTC()
	}
	return e.contents.ExpiresAt
}

type ValidLicense struct {
}

func (v *ValidLicense) IsValid() bool {
	return true
}

func (v *ValidLicense) ExpiresAt() time.Time {
	return time.Now().UTC().Add(time.Hour * 24 * 365 * 10)
}

func NewValidLicense() *ValidLicense {
	return &ValidLicense{}
}

// Retrieves the EE license from the environment
// If not enabled, will still return valid struct.
// Errors if not able to properly parse a provided EE license from the environment
func NewFromEnv() (*EELicense, error) {
	lc, _, err := getLicenseFromEnv()
	if err != nil {
		return nil, err
	}
	return newFromLicenseContents(lc), nil
}

func newFromLicenseContents(contents *licenseContents) *EELicense {
	return &EELicense{contents: contents}
}

// Usage caps carried inside the signed payload. They have to live in the license rather
// than in configuration: a limit the customer can edit is not a limit.
//
// Every field is a pointer so that "not specified" is distinguishable from zero. A nil
// field means uncapped; a field set to 0 means none allowed, which is a legitimate way
// to sell an edition without a given capability.
type Limits struct {
	MaxJobs        *int `json:"max_jobs,omitempty"`
	MaxConnections *int `json:"max_connections,omitempty"`
	// Named connection types the license permits, e.g. ["postgres","mysql"]. Empty or
	// absent means no restriction by type.
	AllowedConnectionTypes []string `json:"allowed_connection_types,omitempty"`
}

// Allows reports whether a named connection type is permitted. An empty allowlist means
// everything is permitted, so adding a type to Husonym never retroactively invalidates
// licenses already in the field.
func (l *Limits) Allows(connectionType string) bool {
	if l == nil || len(l.AllowedConnectionTypes) == 0 {
		return true
	}
	for _, t := range l.AllowedConnectionTypes {
		if t == connectionType {
			return true
		}
	}
	return false
}

// The expected base64 decoded structure of the EE_LICENSE.contents file
type licenseContents struct {
	Version    string    `json:"version"`
	Id         string    `json:"id"`
	IssuedTo   string    `json:"issued_to"`
	CustomerId string    `json:"customer_id"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`

	// Days past ExpiresAt during which everything keeps working. Pointer so an issuer can
	// deliberately sell a license with no grace at all (0), distinct from not saying.
	GraceDays *int `json:"grace_days,omitempty"`

	Limits *Limits `json:"limits,omitempty"`
}

func (l *licenseContents) graceDays() int {
	if l.GraceDays == nil {
		return DefaultGraceDays
	}
	if *l.GraceDays < 0 {
		return 0
	}
	return *l.GraceDays
}

func (l *licenseContents) graceEndsAt() time.Time {
	return l.ExpiresAt.Add(time.Duration(l.graceDays()) * 24 * time.Hour)
}

// Valid for as long as the paid features should keep working — through the grace period,
// not merely up to the expiry date.
func (l *licenseContents) IsValid() bool {
	return time.Now().UTC().Before(l.graceEndsAt())
}

func (l *licenseContents) State() State {
	now := time.Now().UTC()
	switch {
	case !now.Before(l.graceEndsAt()):
		return StateFrozen
	case !now.Before(l.ExpiresAt):
		return StateGrace
	case now.Add(ExpiringWindow).After(l.ExpiresAt):
		return StateExpiring
	default:
		return StateValid
	}
}

// Retrieves the EE license from the environment
func getLicenseFromEnv() (*licenseContents, bool, error) {
	input := viper.GetString(eeLicenseEvKey)
	if input == "" {
		return nil, false, nil
	}
	pk, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		return nil, false, fmt.Errorf("unable to parse ee public key: %w", err)
	}
	contents, err := getLicense(input, pk)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse provided ee license: %w", err)
	}
	return contents, true, nil
}

// Expected the license data to be a base64 encoded json string that matches the licenseFile structure.
func getLicense(licenseData string, publicKey ed25519.PublicKey) (*licenseContents, error) {
	licenseDataContents, err := base64.StdEncoding.DecodeString(licenseData)
	if err != nil {
		return nil, fmt.Errorf("unable to decode license data: %w", err)
	}

	var license licenseFile
	err = json.Unmarshal(licenseDataContents, &license)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal license data from input: %w", err)
	}
	contents, err := base64.StdEncoding.DecodeString(license.License)
	if err != nil {
		return nil, fmt.Errorf("unable to decode contents: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(license.Signature)
	if err != nil {
		return nil, fmt.Errorf("unable to decode signature: %w", err)
	}

	ok := ed25519.Verify(publicKey, contents, signature)
	if !ok {
		return nil, errors.New("unable to verify contents against public key")
	}

	var lc licenseContents
	err = json.Unmarshal(contents, &lc)
	if err != nil {
		return nil, fmt.Errorf(
			"contents verified, but unable to unmarshal license contents from input: %w",
			err,
		)
	}

	return &lc, nil
}

func parsePublicKey(data string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(data))
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the ee public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DER encoded public key: %v", err)
	}

	switch pub := pub.(type) {
	case ed25519.PublicKey:
		return pub, nil
	default:
		return nil, fmt.Errorf("unsupported public key: %T", pub)
	}
}
