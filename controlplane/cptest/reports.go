package cptest

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

// SealedReport is a report as an instance posts it.
type SealedReport struct {
	Document    []byte
	Seal        string
	Fingerprint string
}

// ReportFor builds the report of an instance for a day under the license of entry, sealed as the
// instance would.
func ReportFor(t *testing.T, entry *license.RegistryEntry, instanceID string, day time.Time) SealedReport {
	t.Helper()
	fingerprint := telemetry.KeyFingerprint(entry.Encoded)
	report := &telemetry.Report{
		SchemaVersion: telemetry.SchemaVersion,
		Day:           day.UTC().Format(time.DateOnly),
		GeneratedAt:   day.UTC().Format(time.RFC3339),
		Identification: telemetry.Identification{
			KeyFingerprint: fingerprint,
			LicenseID:      "0123456789abcdef",
			InstanceID:     instanceID,
			LicenseState:   "valid",
			DaysToExpiry:   212,
		},
		Version: telemetry.Version{Husonym: "v0.3.0"},
		Sources: telemetry.Sources{Count: 3},
	}
	document, err := report.Marshal()
	require.NoError(t, err)
	seal, err := telemetry.Seal(entry.Encoded, document)
	require.NoError(t, err)
	return SealedReport{Document: document, Seal: seal, Fingerprint: fingerprint}
}
