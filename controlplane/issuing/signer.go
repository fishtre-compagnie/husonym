// Package issuing drafts a license from what the operator typed and signs it with the code the
// product verifies with. It is the only package of the control plane that holds the private key,
// and nothing here gives it back: not a function, not an error, not a printed value.
package issuing

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
)

// maxKeyFileSize is the largest file LoadSigner reads: a PEM Ed25519 private key is some 120
// bytes, and a file far over that is not one.
const maxKeyFileSize = 16 * 1024

// Why LoadSigner or NewSigner refused. Each message is fixed: none carries a byte of the file, nor
// what the parser said about it.
var (
	ErrNoKeyFile         = errors.New("no signing key file was named")
	ErrKeyFileMissing    = errors.New("the signing key file does not exist")
	ErrKeyFileUnreadable = errors.New("the signing key file cannot be read")
	ErrKeyFileTooLarge   = errors.New("the signing key file is larger than 16 KiB")
	ErrKeyFileNotAKey    = errors.New("the signing key file does not hold a PEM-encoded Ed25519 private key")
	ErrKeyNotInRing      = errors.New("the signing key is not one of the keys license keys are verified against")
)

// signerWord is all a Signer ever prints.
const signerWord = "signer"

// Signer signs licenses with a private key of the ring. It never gives the key back.
type Signer struct {
	priv ed25519.PrivateKey
	ring license.Keyring
	kid  string
}

// LoadSigner reads the PEM Ed25519 private key at path and refuses one the ring does not hold: the
// product would reject every license signed with it.
func LoadSigner(path string, ring license.Keyring) (*Signer, error) {
	if path == "" {
		return nil, ErrNoKeyFile
	}
	raw, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	// What the parser says is dropped: only the fixed message leaves this package.
	priv, err := license.ParsePrivateKey(raw)
	if err != nil {
		return nil, ErrKeyFileNotAKey
	}
	return NewSigner(priv, ring)
}

// readKeyFile reads the file at path, up to maxKeyFileSize. The error of the system is dropped: it
// is replaced by a fixed message naming the cause.
func readKeyFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrKeyFileMissing
	}
	if err != nil {
		return nil, ErrKeyFileUnreadable
	}
	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(io.LimitReader(file, maxKeyFileSize+1))
	if err != nil {
		clear(raw)
		return nil, ErrKeyFileUnreadable
	}
	if len(raw) > maxKeyFileSize {
		clear(raw)
		return nil, ErrKeyFileTooLarge
	}
	return raw, nil
}

// NewSigner returns a Signer for a private key already in memory. It refuses a key the ring does
// not hold.
func NewSigner(priv ed25519.PrivateKey, ring license.Keyring) (*Signer, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrKeyFileNotAKey
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, ErrKeyFileNotAKey
	}
	kid, ok := ring.KidOf(pub)
	if !ok {
		return nil, ErrKeyNotInRing
	}
	return &Signer{priv: slices.Clone(priv), ring: maps.Clone(ring), kid: kid}, nil
}

// Kid is the ring's name for the key this Signer signs with.
func (s *Signer) Kid() string {
	return s.kid
}

// String is a fixed word: a Signer printed by mistake shows nothing of its key.
func (Signer) String() string {
	return signerWord
}

// Format prints the same fixed word under every verb. Without it, %#v or %d would walk the fields
// and print the key.
func (Signer) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, signerWord)
}

// Issue signs the license d describes, issued at now, and returns it with its key as the product
// reads it: the value is parsed back against the ring, and what is returned is that verified
// content. A draft the signed key would not say the same as is an error.
func (s *Signer) Issue(d *Draft, now time.Time) (*license.IssuedLicense, *license.Key, error) {
	if d == nil {
		return nil, nil, errors.New("there is no draft to issue")
	}
	if now.IsZero() {
		return nil, nil, errors.New("the instant of issuing is not set")
	}
	// Without an id of its own the draft would be given a new one at each signing, and a
	// confirmation submitted twice would issue two licenses.
	if !validLicenseID(d.LicenseID) {
		return nil, nil, errors.New("the draft has no license id of 16 lowercase hexadecimal characters")
	}
	if d.AllFeatures && len(d.Features) > 0 {
		return nil, nil, errors.New("the draft says both all the features and a list of features")
	}
	// The wildcard is refused with the unknown names: every feature is said by AllFeatures, which
	// writes no list.
	for _, name := range d.Features {
		if _, declared := license.ParseFeature(name); !declared {
			return nil, nil, errors.New("the draft lists a feature that is not declared")
		}
	}

	req := d.request(now)
	issued, err := license.Issue(req, s.priv, s.ring)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to issue the license: %w", err)
	}
	key, err := license.ParseWith(issued.Encoded, s.ring)
	if err != nil {
		return nil, nil, fmt.Errorf("the license just signed does not verify: %w", err)
	}
	if field := disagreement(key, req); field != "" {
		return nil, nil, fmt.Errorf("the license just signed does not say what the draft says: %s differs", field)
	}
	return issued, key, nil
}

// disagreement names the first field of key that is not what req asked for, or nothing when the
// key says exactly what was asked. It names the field and never its value.
func disagreement(key *license.Key, req *license.IssueRequest) string {
	switch {
	case key.Id != req.Id:
		return "id"
	case key.IssuedTo != req.IssuedTo:
		return "issued_to"
	case key.CustomerId != req.CustomerId:
		return "customer_id"
	case !key.IssuedAt.Equal(req.IssuedAt.UTC().Truncate(time.Second)):
		return "issued_at"
	case !key.ExpiresAt.Equal(req.ExpiresAt.UTC().Truncate(time.Second)):
		return "expires_at"
	case !sameInt(key.GraceDays, req.GraceDays):
		return "grace_days"
	case !sameLimits(key.Limits, req.Limits):
		return "limits"
	case key.Plan != req.Plan:
		return "plan"
	// No list and an empty list are opposite: every feature, and none.
	case (key.Features == nil) != (req.Features == nil) || !slices.Equal(key.Features, req.Features):
		return "features"
	case key.Telemetry != req.Telemetry:
		return "telemetry"
	default:
		return ""
	}
}

func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameLimits(a, b *license.Limits) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameInt(a.MaxJobs, b.MaxJobs) &&
		sameInt(a.MaxConnections, b.MaxConnections) &&
		sameInt(a.MaxSources, b.MaxSources) &&
		slices.Equal(a.AllowedConnectionTypes, b.AllowedConnectionTypes)
}
