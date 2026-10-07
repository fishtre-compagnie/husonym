// Package registryimport loads the registry of issued licenses into the control plane.
package registryimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// originRegistry marks a license that came from the registry file.
const originRegistry = "registry"

// Result counts what an import did.
type Result struct {
	Added        int
	AlreadyThere int
	Refused      int
}

// Promoter promotes the reports pending under a fingerprint whose license is now known.
type Promoter interface {
	PromotePending(ctx context.Context, fingerprint string) (stored, discarded int, err error)
}

// Run records every entry of registry in store. An entry whose encoded key does not verify
// against ring, whose id differs from the one inside the key, or whose key names no customer,
// is refused and counted; the other entries are still imported. What is stored comes from the verified key, not from the
// entry. Nothing about a refused entry is reported but the count.
//
// Once the entries are in, the reports pending under the license of each entry added are
// promoted. Should that fail, the error comes with the result, and the import stays: the hourly
// maintenance promotes what is left.
func Run(
	ctx context.Context, store *cpstore.Store, promoter Promoter, registry *license.Registry, ring license.Keyring,
) (Result, error) {
	var result Result
	var added []string
	for i := range registry.Entries {
		entry := &registry.Entries[i]
		key, err := license.ParseWith(entry.Encoded, ring)
		if err != nil || key.Id != entry.Id || key.CustomerId == "" {
			result.Refused++
			continue
		}
		inserted, err := store.AddLicense(ctx, key, entry, originRegistry)
		if err != nil {
			return result, fmt.Errorf("unable to import the registry: %w", err)
		}
		if inserted {
			result.Added++
			added = append(added, telemetry.KeyFingerprint(entry.Encoded))
		} else {
			result.AlreadyThere++
		}
	}

	var failed error
	for _, fingerprint := range added {
		if _, _, err := promoter.PromotePending(ctx, fingerprint); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	if failed != nil {
		return result, fmt.Errorf("the registry is imported but its pending reports were not all promoted: %w", failed)
	}
	return result, nil
}
