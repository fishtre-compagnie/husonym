package cptest

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// AddLicense records the license of entry in store, as read from its key.
func AddLicense(t *testing.T, store *cpstore.Store, issuer *Issuer, entry *license.RegistryEntry) {
	t.Helper()
	added, err := store.AddLicense(t.Context(), issuer.Key(entry), entry, "registry")
	require.NoError(t, err)
	require.True(t, added)
}

// StoreReport stores the report ReportFor builds for an instance and a day under the license of
// entry, received at receivedAt.
func StoreReport(
	t *testing.T, store *cpstore.Store, entry *license.RegistryEntry, instanceID string, day, receivedAt time.Time,
) {
	t.Helper()
	sealed := ReportFor(t, entry, instanceID, day)
	outcome, err := store.StoreReport(t.Context(), &cpstore.Report{
		InstanceID:     instanceID,
		Day:            day,
		LicenseID:      entry.Id,
		Document:       sealed.Document,
		Seal:           sealed.Seal,
		HusonymVersion: "v0.3.0",
		InstallKind:    "helm",
		ReceivedAt:     receivedAt,
	}, cpstore.InstanceCap{Max: 1000})
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportStored, outcome)
}

// Succeed makes the license successorID the successor of predecessorID. Nothing writes that link
// yet, so it is set in the table.
func Succeed(t *testing.T, pool *pgxpool.Pool, successorID, predecessorID string) {
	t.Helper()
	tag, err := pool.Exec(t.Context(),
		`UPDATE controlplane.licenses SET succeeds_license_id = $2 WHERE id = $1`, successorID, predecessorID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}
