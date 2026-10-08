package cptest

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

// SealedRenewal is a request for a renewal as an instance posts it.
type SealedRenewal struct {
	Document    []byte
	Seal        string
	Fingerprint string
}

// RenewalFor builds the request an instance makes at the instant at for the license that succeeds
// the one of entry, sealed as the instance would.
func RenewalFor(t *testing.T, entry *license.RegistryEntry, instanceID string, at time.Time) SealedRenewal {
	t.Helper()
	document, err := telemetry.NewRenewalRequest(entry.Id, instanceID, at).Marshal()
	require.NoError(t, err)
	seal, err := telemetry.Seal(entry.Encoded, document)
	require.NoError(t, err)
	return SealedRenewal{Document: document, Seal: seal, Fingerprint: telemetry.KeyFingerprint(entry.Encoded)}
}
