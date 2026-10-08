package usagereport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

const (
	// RenewalPeriod is how long the instance waits between two asks for its license.
	RenewalPeriod = 24 * time.Hour
	// RenewalPeriodNearExpiry is how long it waits once the license expires within thirty days,
	// is in its grace period or is past it.
	RenewalPeriodNearExpiry = 6 * time.Hour
)

// InstanceIdSource gives the identity of the instance. *usagestore.Store is one.
type InstanceIdSource interface {
	InstanceId(ctx context.Context) (string, error)
}

// OfferRenewal puts a key received as a renewal forward to the rule every license key goes
// through: (*licensestore.Store).Offer with licensestore.OriginRenewal is one.
type OfferRenewal func(ctx context.Context, value string) (*licensestore.Result, error)

// Renewer asks, when it is due, for the license that succeeds the one of the instance, and
// hands what it receives to the rule every license key goes through.
type Renewer struct {
	keys      *InstanceKey
	instance  InstanceIdSource
	transport RenewalTransport
	offer     OfferRenewal
	setting   string
	logger    *slog.Logger

	passTimeout time.Duration
	now         func() time.Time

	// asked and lastAsk are when this process last asked, in its memory alone: a process that
	// starts asks again, which is harmless.
	mu      sync.Mutex
	asked   bool
	lastAsk time.Time
}

// NewRenewer takes the key of the instance, which is the license asked for, seals the request
// and says what it provides for the usage report; the setting of the operator as it is written
// (HUSONYM_TELEMETRY), which may only lower it; and the clock.
func NewRenewer(
	keys *InstanceKey,
	instance InstanceIdSource,
	transport RenewalTransport,
	offer OfferRenewal,
	setting string,
	now func() time.Time,
	logger *slog.Logger,
) *Renewer {
	return &Renewer{
		keys: keys, instance: instance, transport: transport, offer: offer, setting: setting,
		logger: logger, passTimeout: passTimeout, now: now,
	}
}

// AskIfDue asks for the license that succeeds the one the instance holds, when the instance
// sends its usage report and has not asked for a day; for six hours once its license expires
// within thirty days, is in its grace period or is past it: a successor is what brings a frozen
// instance back. An instance that holds no key asks for nothing.
//
// A license that is received is offered to the rule every key goes through, which alone decides
// whether it replaces the one in force: one that is refused is told in a line and changes
// nothing. An ask that fails is an error, changes nothing either, and is made again once the
// period has elapsed.
//
// It may be called at once, in one process as in several replicas: one call of a process asks,
// and a license offered twice is stored once.
func (r *Renewer) AskIfDue(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, r.passTimeout)
	defer cancel()

	now := r.now()
	value, key, err := r.keys.held(ctx)
	if errors.Is(err, ErrNoLicenseInForce) {
		return nil
	}
	if err != nil {
		return err
	}
	if mode, _ := telemetry.EffectiveMode(key.TelemetryMode(), r.setting); mode != telemetry.ModeOnline {
		return nil
	}

	instanceID, err := r.instance.InstanceId(ctx)
	if err != nil {
		return fmt.Errorf("unable to read the identity of the instance: %w", err)
	}
	document, err := telemetry.NewRenewalRequest(key.Id, instanceID, now).Marshal()
	if err != nil {
		return err
	}
	seal, err := telemetry.Seal(value, document)
	if err != nil {
		return errors.New("unable to seal the request for the license of the instance")
	}
	// Claimed last: only a request that leaves counts as an ask.
	if !r.claim(now, renewalPeriod(key, now)) {
		return nil
	}

	received, err := r.transport.Ask(ctx, &RenewalAsk{
		Document: document, Seal: seal, KeyFingerprint: telemetry.KeyFingerprint(value),
	})
	if err != nil {
		return err
	}
	if received == "" {
		r.logger.DebugContext(parent, "no license succeeds the one of the instance")
		return nil
	}

	result, err := r.offer(ctx, received)
	if err != nil {
		return fmt.Errorf("unable to offer the license received as a renewal to the database: %w", err)
	}
	// A result nobody filled in is no key taken.
	outcome := licensestore.Outcome(0)
	if result != nil {
		outcome = result.Outcome
	}
	switch outcome {
	case licensestore.Accepted:
		licenseID := ""
		if result.Key != nil {
			licenseID = result.Key.Id
		}
		r.logger.InfoContext(parent, "the license of the instance was renewed", "licenseId", licenseID)
	case licensestore.Unchanged:
		r.logger.DebugContext(parent, "the license received as a renewal is the one already in force")
	default:
		// The reason of the rule is not logged: the word of its outcome says enough here, and
		// the key in force stays as it is.
		r.logger.WarnContext(parent, "the license received as a renewal was refused and the one in force stays",
			"outcome", outcome.String())
	}
	return nil
}

// renewalPeriod is how long the instance waits between two asks, by where its key stands.
func renewalPeriod(key *license.Key, now time.Time) time.Duration {
	if key.StateAt(now) == license.StateValid {
		return RenewalPeriod
	}
	return RenewalPeriodNearExpiry
}

// claim tells whether an ask is due, which it is when this process never asked or last asked a
// period ago or more, and records it as made: of several calls made at once, one asks.
func (r *Renewer) claim(now time.Time, period time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asked && now.Sub(r.lastAsk) < period {
		return false
	}
	r.asked, r.lastAsk = true, now
	return true
}
