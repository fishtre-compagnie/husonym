package usagereport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
)

// InstanceKey reads the license key the instance holds, with the ring that verifies it. What
// the usage report says of the license, and what the license provides for the report, are both
// read through it: they never tell of two keys.
type InstanceKey struct {
	keys KeySource
	ring license.Keyring
}

func NewInstanceKey(keys KeySource, ring license.Keyring) *InstanceKey {
	return &InstanceKey{keys: keys, ring: ring}
}

// inForce gives the key the instance holds, as it was installed and as it reads, or
// ErrNoLicenseInForce when there is none or when it is frozen at the given moment.
func (k *InstanceKey) inForce(ctx context.Context, now time.Time) (string, *license.Key, error) {
	value, err := k.keys.Current(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("unable to read the license key: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil, ErrNoLicenseInForce
	}
	key, err := license.ParseWith(value, k.ring)
	if err != nil {
		return "", nil, fmt.Errorf("unable to verify the license key: %w", err)
	}
	if key.StateAt(now) == license.StateFrozen {
		return "", nil, ErrNoLicenseInForce
	}
	return value, key, nil
}

// TelemetryMode gives what the key in force provides for the usage report, or
// ErrNoLicenseInForce.
func (k *InstanceKey) TelemetryMode(ctx context.Context, now time.Time) (license.TelemetryMode, error) {
	_, key, err := k.inForce(ctx, now)
	if err != nil {
		return "", err
	}
	return key.TelemetryMode(), nil
}
