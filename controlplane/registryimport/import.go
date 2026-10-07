// Package registryimport loads the registry of issued licenses into the control plane.
package registryimport

import (
	"context"
	"fmt"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// originRegistry marks a license that came from the registry file.
const originRegistry = "registry"

// Result counts what an import did.
type Result struct {
	Added        int
	AlreadyThere int
	Refused      int
}

// Run records every entry of registry in store. An entry whose encoded key does not verify
// against ring, or whose id differs from the one inside the key, is refused and counted; the
// other entries are still imported. Nothing about a refused entry is reported but the count.
func Run(ctx context.Context, store *cpstore.Store, registry *license.Registry, ring license.Keyring) (Result, error) {
	var result Result
	for i := range registry.Entries {
		entry := &registry.Entries[i]
		key, err := license.ParseWith(entry.Encoded, ring)
		if err != nil || key.Id != entry.Id {
			result.Refused++
			continue
		}
		added, err := store.AddLicense(ctx, entry, originRegistry)
		if err != nil {
			return result, fmt.Errorf("unable to import the registry: %w", err)
		}
		if added {
			result.Added++
		} else {
			result.AlreadyThere++
		}
	}
	return result, nil
}
