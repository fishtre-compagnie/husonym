// Package renewal answers an instance that asks for the license that succeeds its own: the last
// license of the chain of successors, as it was issued and stored. Nothing is signed here.
package renewal

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// ChainCutShort is the line logged when the successors of a license go on past the bound of the
// walk. It is made of these words alone.
const ChainCutShort = "the chain of successors of a license goes on past the bound of the walk"

// The lines logged when something of a request could not be written down. The request was
// answered all the same. Each is made of these words alone: no license, no instance, and not the
// text of the error, which may quote what was to be written.
const (
	AskNotRecorded      = "an ask for a license renewal was answered and could not be recorded"
	RejectionNotCounted = "a seal refused for a license renewal could not be counted"
)

// bookkeepingTimeout bounds each of the two writes that do not decide the answer: one that hangs
// is given up and told like one that fails, so that it cannot hold the answer of an instance.
const bookkeepingTimeout = 2 * time.Second

// Outcome is what became of a request for a renewal.
type Outcome int

const (
	// Nothing: there is no license to give. No issued license has the fingerprint, the seal is
	// not the one of the request under the license that has it, the request names another
	// license than the one it is sealed under, or nothing succeeds the license. A caller is not
	// told which.
	Nothing Outcome = iota
	// Served: a license succeeds the one of the instance, and the answer carries the last one.
	Served
	// Refused: the caller got something wrong that is told without knowing any license. The seal
	// or the fingerprint is malformed, the body is not a request, or its instant is too far from
	// the clock.
	Refused
)

// Observer counts what the renewal could not write down of a request it answered all the same.
type Observer interface {
	RenewalBookkeepingFailed()
}

// Renewal answers the requests for a renewal.
type Renewal struct {
	store    *cpstore.Store
	now      func() time.Time
	logger   *slog.Logger
	observer Observer
}

// New returns a Renewal that reads from store and takes the time from now. logger is only told
// that a chain of successors was cut short and that something of a request could not be written
// down, each in fixed words; the latter is also counted in observer, unless it is nil.
func New(store *cpstore.Store, now func() time.Time, logger *slog.Logger, observer Observer) *Renewal {
	return &Renewal{store: store, now: now, logger: logger, observer: observer}
}

// Answer checks a request and gives the license that succeeds the one it is sealed with: the last
// of its chain, so that an instance several renewals behind receives the latest.
//
// What is refused is refused before the database is read. Past that, an ask whose seal verifies
// is recorded under its license and its instance, served or not; a seal that does not verify
// under a license that is known is counted as the one of a report is.
//
// Neither of those two writes decides the answer. One that fails is told in a line of fixed words
// and counted in the observer, and the request is answered as the chain says: an instance is not
// kept from its license by a trace of ours, and a seal refused under a license that is known is
// answered as an unknown fingerprint is, whether it could be counted or not.
//
// The answer is non-nil for Served alone. A non-nil error comes with Nothing and says nothing of
// the request: it is a read of ours that failed, the license by its fingerprint or its chain, or
// the context of a caller that went away before the request was recorded.
func (r *Renewal) Answer(
	ctx context.Context, body []byte, seal, fingerprint string,
) (Outcome, *telemetry.RenewalAnswer, error) {
	if !intake.HexShaped(seal) || !intake.HexShaped(fingerprint) {
		return Refused, nil, nil
	}
	request, err := telemetry.ParseRenewalRequest(body)
	if err != nil {
		return Refused, nil, nil
	}
	now := r.now()
	if apart := now.Sub(request.At()); apart > telemetry.RenewalFreshness || apart < -telemetry.RenewalFreshness {
		return Refused, nil, nil
	}

	issued, err := r.store.LicenseByFingerprint(ctx, fingerprint)
	if errors.Is(err, cpstore.ErrNoLicense) {
		return Nothing, nil, nil
	}
	if err != nil {
		return Nothing, nil, err
	}
	if telemetry.Verify(issued.Encoded, body, seal) != nil {
		writing, done := context.WithTimeout(ctx, bookkeepingTimeout)
		err = r.store.CountSealRejection(writing, issued.Id, now)
		done()
		if err != nil {
			if ctx.Err() != nil {
				return Nothing, nil, ctx.Err()
			}
			r.unrecorded(ctx, RejectionNotCounted)
		}
		return Nothing, nil, nil
	}
	// The id of the license, as the product writes it.
	if request.LicenseID != telemetry.LicenseId(issued.Id) {
		return Nothing, nil, nil
	}

	successor, cutShort, err := r.store.LatestSuccessor(ctx, issued.Id)
	if err != nil {
		return Nothing, nil, err
	}
	if cutShort {
		r.logger.WarnContext(ctx, ChainCutShort)
	}
	served := ""
	if successor != nil {
		served = successor.Id
	}
	writing, done := context.WithTimeout(ctx, bookkeepingTimeout)
	err = r.store.RecordRenewalAsk(writing, issued.Id, request.InstanceID, now, served)
	done()
	if err != nil {
		if ctx.Err() != nil {
			return Nothing, nil, ctx.Err()
		}
		r.unrecorded(ctx, AskNotRecorded)
	}
	if successor == nil {
		return Nothing, nil, nil
	}
	return Served, &telemetry.RenewalAnswer{
		SchemaVersion: telemetry.RenewalSchemaVersion,
		License:       successor.Encoded,
	}, nil
}

// unrecorded tells, in the fixed words of line, that something of a request could not be written
// down, and counts it. The error of the write is left out: the request is answered, and nothing of
// it belongs in a log.
func (r *Renewal) unrecorded(ctx context.Context, line string) {
	r.logger.ErrorContext(ctx, line)
	if r.observer != nil {
		r.observer.RenewalBookkeepingFailed()
	}
}
