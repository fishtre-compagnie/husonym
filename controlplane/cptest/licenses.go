package cptest

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// Issuer mints registry entries with a throwaway key pair.
type Issuer struct {
	t    *testing.T
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// NewIssuer returns an Issuer with a fresh key pair.
func NewIssuer(t *testing.T) *Issuer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return &Issuer{t: t, priv: priv, pub: pub}
}

// Keyring is the keyring that verifies what this Issuer mints.
func (i *Issuer) Keyring() license.Keyring {
	return license.Keyring{license.LegacyKid: i.pub}
}

// Entry mints a license and returns its registry entry.
func (i *Issuer) Entry(id, customerID, issuedTo string) license.RegistryEntry {
	i.t.Helper()
	return i.EntryFor(&license.IssueRequest{
		Id:         id,
		IssuedTo:   issuedTo,
		CustomerId: customerID,
		ExpiresAt:  time.Now().UTC().Add(365 * 24 * time.Hour),
		Telemetry:  string(license.TelemetryOnline),
	})
}

// Key reads the content of the key of entry, verified against this Issuer's keyring.
func (i *Issuer) Key(entry *license.RegistryEntry) *license.Key {
	i.t.Helper()
	key, err := license.ParseWith(entry.Encoded, i.Keyring())
	require.NoError(i.t, err)
	return key
}

// EntryFor mints the license req describes and returns its registry entry.
func (i *Issuer) EntryFor(req *license.IssueRequest) license.RegistryEntry {
	i.t.Helper()
	issued, err := license.Issue(req, i.priv, i.Keyring())
	require.NoError(i.t, err)
	return license.RegistryEntry{
		Id:             issued.Id,
		IssuedTo:       issued.IssuedTo,
		CustomerId:     issued.CustomerId,
		IssuedAt:       issued.IssuedAt,
		ExpiresAt:      issued.ExpiresAt,
		Encoded:        issued.Encoded,
		Kid:            issued.Kid,
		Telemetry:      issued.Telemetry,
		KeyFingerprint: license.PublicKeyFingerprint(i.pub),
	}
}
