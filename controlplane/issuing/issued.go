package issuing

import (
	"slices"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/google/uuid"
)

// IsTheContentOf reports whether stored, a license of the store, is the license this draft would be
// issued as to the customer of id customerID: the same id, customer, plan, features, cap on sources,
// expiry, grace period and telemetry, and the same license succeeded. It is what tells a
// confirmation sent again from another draft that bears the same id.
//
// Every field is compared as the key carries it, not as the product reads it: no list of features
// is not an empty one, a grace period or a telemetry the key does not say is not its default, and a
// stored license that carries a limit a draft has no field for is never the content of a draft.
func (d *Draft) IsTheContentOf(stored *cpstore.LicenseDetail, customerID uuid.UUID) bool {
	if stored == nil {
		return false
	}
	var features []string
	if !d.AllFeatures {
		features = d.Features
	}
	var storedSources *int
	if limits := stored.Limits; limits != nil {
		if limits.MaxJobs != nil || limits.MaxConnections != nil || limits.AllowedConnectionTypes != nil {
			return false
		}
		storedSources = limits.MaxSources
	}
	return stored.ID == d.LicenseID &&
		stored.CustomerID == customerID &&
		stored.Plan == d.Plan &&
		(stored.Features == nil) == d.AllFeatures &&
		slices.Equal(stored.Features, features) &&
		sameInt(storedSources, d.MaxSources) &&
		stored.ExpiresAt.Equal(d.ExpiresAt) &&
		sameInt(stored.GraceDays, d.GraceDays) &&
		stored.StoredTelemetry == d.Telemetry &&
		stored.PredecessorID == d.Succeeds
}
