package license

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// EEInterface is what the rest of the code asks of a license.
type EEInterface interface {
	IsValid() bool
	ExpiresAt() time.Time
	Limits() *Limits
	// HasFeature is true when the license is in force and allows the feature.
	HasFeature(Feature) bool
}

// Loader gives the value of the license key in force, or an empty string when it has
// none to give.
type Loader func(ctx context.Context) (string, error)

// ErrKeyNotLoaded is the problem of a refresh whose loader failed. The loader's own error
// is wrapped next to it: it may name a host, a user or a database, so a caller that shows
// the problem to someone tells this one apart and keeps the detail for the log.
var ErrKeyNotLoaded = errors.New("the license key could not be loaded")

// Provider holds the last key its loader gave and answers from the clock on every call.
// It is safe for concurrent use.
//
// Only Refresh calls the loader. Every other method answers from memory and never does
// I/O: workflow code calls them from the workflow thread, where a call that blocks trips
// the deadlock detector of Temporal.
type Provider struct {
	load   Loader
	ring   Keyring
	now    func() time.Time
	logger *slog.Logger

	// refreshing lets one Refresh run at a time, so that two of them cannot apply what
	// they loaded in the wrong order. It is held across the loader call and guards the
	// two fields below; the reads never take it.
	refreshing sync.Mutex
	// applied is the last value that became the key in place.
	applied string
	// reported is the text of the last problem logged, so that a problem that lasts is
	// logged once.
	reported string

	// mu guards what the reads answer from. It is never held across the loader call.
	mu      sync.Mutex
	key     *Key
	problem error
}

var _ EEInterface = (*Provider)(nil)

// NewProvider builds a Provider that verifies keys against the embedded public keys. It
// holds no key until Refresh is called.
func NewProvider(load Loader, logger *slog.Logger) *Provider {
	ring, err := EmbeddedKeyring()
	if err != nil {
		// The embedded keys are part of the binary; a failure here is a build defect.
		// Verification then fails for every key, which leaves the state at none.
		ring = nil
		if logger != nil {
			logger.Error("unable to load the embedded license public keys", "error", err)
		}
	}
	return newProvider(load, ring, time.Now, logger)
}

// NewProviderWithKeyring builds a Provider that verifies keys against ring. A process that
// also verifies keys elsewhere gives every verifier the one ring it loaded; a test gives the
// ring of the keys it signs. It holds no key until Refresh is called.
func NewProviderWithKeyring(load Loader, ring Keyring, logger *slog.Logger) *Provider {
	return newProvider(load, ring, time.Now, logger)
}

func newProvider(load Loader, ring Keyring, now func() time.Time, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{load: load, ring: ring, now: now, logger: logger}
}

// Refresh asks the loader for the key in force and applies what it gives.
//
// A key that cannot be loaded or trusted never replaces the one in place: the reason is
// returned, kept for Problem and logged once for as long as it lasts. A load that fails
// once ctx is done is only returned. A well-signed key
// replaces the one in place whatever its dates say, so that the loader is the single
// source of truth. A loader with nothing to give leaves the key in place: a store that
// answers nothing does not take a license away.
func (p *Provider) Refresh(ctx context.Context) error {
	p.refreshing.Lock()
	defer p.refreshing.Unlock()

	value, err := p.load(ctx)
	if err != nil {
		err = fmt.Errorf("%w: %w", ErrKeyNotLoaded, err)
		// A refresh in flight when its context ends, as at shutdown, was told to stop: that
		// says nothing about the key, so it is neither kept as the problem nor logged.
		if ctx.Err() != nil {
			return err
		}
		return p.refuse(err)
	}
	value = strings.TrimSpace(value)
	if value == "" || value == p.applied {
		p.settle()
		return nil
	}

	key, err := ParseWith(value, p.ring)
	if err != nil {
		return p.refuse(err)
	}
	p.mu.Lock()
	p.key = key
	p.problem = nil
	p.mu.Unlock()
	p.applied = value
	p.reported = ""
	p.logger.Info("a license key is now in force", "licenseId", key.Id, "expiresAt", key.ExpiresAt)
	return nil
}

// refuse keeps the key in place and records why the load was refused. The caller holds
// p.refreshing.
func (p *Provider) refuse(problem error) error {
	p.mu.Lock()
	p.problem = problem
	p.mu.Unlock()
	if text := problem.Error(); text != p.reported {
		p.reported = text
		p.logger.Error("the license key is not usable, keeping the key in place", "error", problem)
	}
	return problem
}

// settle records that the loader answered and that the key in place stands. The caller
// holds p.refreshing.
func (p *Provider) settle() {
	p.mu.Lock()
	p.problem = nil
	p.mu.Unlock()
	p.reported = ""
}

// RefreshEvery refreshes at the given interval until ctx is done. It does not refresh on
// entry: the caller does the first Refresh itself. A refresh that fails is not reported
// here, Refresh already recorded and logged it.
func (p *Provider) RefreshEvery(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.Refresh(ctx)
		}
	}
}

// snapshot returns the key, the problem and the clock under one lock, so that an answer
// always comes from a single key.
func (p *Provider) snapshot() snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return snapshot{key: p.key, problem: p.problem, now: p.now()}
}

// snapshot is one consistent answer: the key, the problem and the instant all come from
// the same critical section.
type snapshot struct {
	key     *Key
	problem error
	now     time.Time
}

// state is where the snapshot's key stands in its lifecycle, or StateNone without a key.
func (s snapshot) state() State {
	if s.key == nil {
		return StateNone
	}
	return s.key.StateAt(s.now)
}

// inForce is true up to the end of the grace period.
func (s snapshot) inForce() bool {
	return inForce(s.state())
}

// inForce is true for the states of a key that still allows what it says.
func inForce(state State) bool {
	return state != StateNone && state != StateFrozen
}

// Description is what the Provider holds at one instant.
type Description struct {
	State State
	// Key is the key in place, or nil without one. It is the Provider's own: the caller
	// must not modify it.
	Key *Key
	// Problem is why the last refresh was refused, or nil.
	Problem error
}

// InForce is true up to the end of the grace period, as Provider.IsValid is.
func (d Description) InForce() bool {
	return inForce(d.State)
}

// Describe returns the state, the key and the problem from one snapshot, so that the
// three never come from two keys or two instants.
func (p *Provider) Describe() Description {
	snap := p.snapshot()
	return Description{State: snap.state(), Key: snap.key, Problem: snap.problem}
}

// State is where the current key stands in its lifecycle, or StateNone without a key.
func (p *Provider) State() State {
	return p.snapshot().state()
}

// IsValid is true up to the end of the grace period.
func (p *Provider) IsValid() bool {
	return p.snapshot().inForce()
}

// HasFeature is true when the license is in force and its key allows the feature. Both come
// from one snapshot, so the answer never mixes two keys or two instants.
func (p *Provider) HasFeature(f Feature) bool {
	snap := p.snapshot()
	return snap.inForce() && snap.key.HasFeature(f)
}

// ExpiresAt is when the key stops being in force, or the current time without a key.
func (p *Provider) ExpiresAt() time.Time {
	snap := p.snapshot()
	if snap.key == nil {
		return snap.now
	}
	return snap.key.ExpiresAt
}

// GracePeriodEndsAt is when the grace period runs out, or the current time without a key.
func (p *Provider) GracePeriodEndsAt() time.Time {
	snap := p.snapshot()
	if snap.key == nil {
		return snap.now
	}
	return snap.key.GraceEndsAt()
}

// Limits is what the current key caps, or nil without a key.
func (p *Provider) Limits() *Limits {
	snap := p.snapshot()
	if snap.key == nil {
		return nil
	}
	return snap.key.Limits
}

// Problem is the reason the last refresh was refused, or nil once a refresh goes through.
func (p *Provider) Problem() error {
	return p.snapshot().problem
}
