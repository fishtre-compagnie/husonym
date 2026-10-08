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
	"strings"
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
//
// No field of it holds a byte of the key. A printer that walks a value by reflection — fmt on a
// struct that has a Signer in an unexported field, a structured logger, the diff of a failed test
// — reaches every field whatever methods the type has, so the key is kept where none of them goes:
// in what sign closes over. A function prints as an address.
type Signer struct {
	// sign signs a request with the key, which only it holds.
	sign func(*license.IssueRequest) (*license.IssuedLicense, error)
	// ring holds public keys only.
	ring        license.Keyring
	kid         string
	fingerprint string
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

	// A file that says it is too large is refused before a byte of it is read.
	if info, err := file.Stat(); err == nil && info.Mode().IsRegular() && info.Size() > maxKeyFileSize {
		return nil, ErrKeyFileTooLarge
	}
	return readKey(file)
}

// readKey reads a key from source and stops one byte past maxKeyFileSize: what does not say its
// size, or says it wrong, is not read whole either.
func readKey(source io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(source, maxKeyFileSize+1))
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
	// Copies: what the caller does with its own afterwards changes nothing here.
	key := slices.Clone(priv)
	held := maps.Clone(ring)
	return &Signer{
		sign: func(req *license.IssueRequest) (*license.IssuedLicense, error) {
			return license.Issue(req, key, held)
		},
		ring:        held,
		kid:         kid,
		fingerprint: license.PublicKeyFingerprint(pub),
	}, nil
}

// Kid is the ring's name for the key this Signer signs with.
func (s *Signer) Kid() string {
	return s.kid
}

// PublicKeyFingerprint identifies the public key that verifies what this Signer signs, as the
// registry of issued licenses writes it. It is not secret.
func (s *Signer) PublicKeyFingerprint() string {
	return s.fingerprint
}

// String is a fixed word: a Signer printed by mistake shows nothing of its key.
func (Signer) String() string {
	return signerWord
}

// Format prints the same fixed word under every verb, so that a Signer printed for itself shows
// nothing of what it holds. It is not what keeps the key out of a print: no field holds it.
func (Signer) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, signerWord)
}

// Issue signs the license d describes, issued at now, and returns it with its key as the product
// reads it: the value is parsed back against the ring, and what is returned is that verified
// content. A draft the signed key would not say the same as is an error.
//
// The draft is held to every rule ParseDraft holds a form to, whoever built it: an expiry that is
// not after now is refused here, against the instant given. The issuing code of the product then
// checks the expiry once more against the wall clock, which cannot be given to it: with a now
// that is behind the wall clock, an expiry between the two is refused by that second check.
func (s *Signer) Issue(d *Draft, now time.Time) (*license.IssuedLicense, *license.Key, error) {
	if s == nil || s.sign == nil {
		return nil, nil, errors.New("there is no signing key")
	}
	if d == nil {
		return nil, nil, errors.New("there is no draft to issue")
	}
	if now.IsZero() {
		return nil, nil, errors.New("the instant of issuing is not set")
	}
	if problems := d.problems(now, false); len(problems) > 0 {
		return nil, nil, fmt.Errorf("the draft cannot be signed: %s", strings.Join(problems, " "))
	}

	req := d.request(now)
	issued, err := s.sign(req)
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
