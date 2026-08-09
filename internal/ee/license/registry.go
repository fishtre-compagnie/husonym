package license

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// A record of every license ever minted.
//
// This exists for one commercial reason: renewals are the revenue, and you cannot chase a
// renewal you have no record of. It also answers "what exactly did we sell this customer"
// when a support question arrives, which a signed blob in someone's inbox does not.
//
// It holds customer names and issued licenses, so it belongs next to the signing key —
// outside the repository — and is written 0600. Never commit one.

const registryVersion = "v1"

// RegistryEntry is one issued license. It stores the encoded license alongside the
// metadata so a customer who lost theirs can be served without re-issuing, which would
// otherwise leave two live licenses for one contract.
type RegistryEntry struct {
	Id         string    `json:"id"`
	IssuedTo   string    `json:"issued_to"`
	CustomerId string    `json:"customer_id"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	GraceDays  *int      `json:"grace_days,omitempty"`
	Limits     *Limits   `json:"limits,omitempty"`
	Encoded    string    `json:"encoded"`
	// Which signing key minted this, so a key rotation is visible rather than inferred.
	KeyFingerprint string `json:"key_fingerprint"`
	// Free-form note: contract reference, ticket, whatever makes it findable later.
	Note string `json:"note,omitempty"`
}

// State reports where this license sits in its lifecycle right now. A value receiver so
// it can be called directly on entries returned from the query helpers below.
func (e RegistryEntry) State() State {
	c := &licenseContents{ExpiresAt: e.ExpiresAt, GraceDays: e.GraceDays}
	return c.State()
}

type Registry struct {
	Version string          `json:"version"`
	Entries []RegistryEntry `json:"entries"`
}

// LoadRegistry reads a registry. A missing file is not an error: the first issuance
// creates it.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Registry{Version: registryVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read registry: %w", err)
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("unable to parse registry %s: %w", path, err)
	}
	if r.Version == "" {
		r.Version = registryVersion
	}
	return &r, nil
}

// Add records an issued license. Duplicate ids are refused: two entries sharing an id
// would make the registry ambiguous about what is in the field.
func (r *Registry) Add(entry RegistryEntry) error {
	for i := range r.Entries {
		if r.Entries[i].Id == entry.Id {
			return fmt.Errorf("license id %q is already in the registry", entry.Id)
		}
	}
	r.Entries = append(r.Entries, entry)
	return nil
}

// Save writes the registry atomically: a crash mid-write must not leave a truncated file,
// because losing the registry means losing the renewal pipeline.
func (r *Registry) Save(path string) error {
	if r.Version == "" {
		r.Version = registryVersion
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("unable to create registry directory: %w", err)
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("unable to marshal registry: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	// 0600: customer names and live licenses.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("unable to write registry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("unable to replace registry: %w", err)
	}
	return nil
}

// Find returns the entry with the given id.
func (r *Registry) Find(id string) (*RegistryEntry, bool) {
	for i := range r.Entries {
		if r.Entries[i].Id == id {
			return &r.Entries[i], true
		}
	}
	return nil, false
}

// ForCustomer returns every license issued to a customer, oldest expiry first — a
// customer usually has a chain of renewals rather than a single license.
func (r *Registry) ForCustomer(customerId string) []RegistryEntry {
	var out []RegistryEntry
	for _, e := range r.Entries {
		if e.CustomerId == customerId {
			out = append(out, e)
		}
	}
	sortByExpiry(out)
	return out
}

// ExpiringWithin lists licenses expiring inside the window, soonest first. This is the
// renewal worklist.
//
// Already-frozen licenses are excluded: they are past saving by a reminder and would
// bury the ones still actionable. Use Frozen() for those.
func (r *Registry) ExpiringWithin(window time.Duration) []RegistryEntry {
	cutoff := time.Now().UTC().Add(window)
	var out []RegistryEntry
	for _, e := range r.Entries {
		if e.State() == StateFrozen {
			continue
		}
		if e.ExpiresAt.Before(cutoff) {
			out = append(out, e)
		}
	}
	sortByExpiry(out)
	return out
}

// Frozen lists licenses whose grace period has run out.
func (r *Registry) Frozen() []RegistryEntry {
	var out []RegistryEntry
	for _, e := range r.Entries {
		if e.State() == StateFrozen {
			out = append(out, e)
		}
	}
	sortByExpiry(out)
	return out
}

func sortByExpiry(entries []RegistryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ExpiresAt.Equal(entries[j].ExpiresAt) {
			return entries[i].Id < entries[j].Id
		}
		return entries[i].ExpiresAt.Before(entries[j].ExpiresAt)
	})
}
