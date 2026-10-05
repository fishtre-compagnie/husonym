package license

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// fileRecheckInterval bounds how often the license file is read again. It is measured
// on the Provider's clock, and the check runs on demand: there is no background goroutine.
const fileRecheckInterval = time.Minute

// EEInterface is what the rest of the code asks of a license.
type EEInterface interface {
	IsValid() bool
	ExpiresAt() time.Time
	Limits() *Limits
}

// Source says where the license value comes from. When both fields are set, the file
// wins.
type Source struct {
	// Value is the license key itself, read once.
	Value string
	// File is the path of a file holding the license key, read again on demand.
	File string
}

// SourceFromEnv reads the source from EE_LICENSE and EE_LICENSE_FILE.
func SourceFromEnv() Source {
	return Source{
		Value: viper.GetString("EE_LICENSE"),
		File:  viper.GetString("EE_LICENSE_FILE"),
	}
}

// fileObservation is what the last read of the license file saw, so that the same
// content is neither verified nor logged twice.
type fileObservation struct {
	content string
	// readFailed means the file could not be read at all, whatever content says.
	readFailed bool
}

// Provider holds the last verified license key and answers from the clock on every
// call. It is safe for concurrent use.
type Provider struct {
	pub    ed25519.PublicKey
	now    func() time.Time
	logger *slog.Logger
	file   string

	mu        sync.Mutex
	key       *Key
	problem   error
	lastCheck time.Time
	lastSeen  *fileObservation
}

var _ EEInterface = (*Provider)(nil)

// NewProvider builds a Provider that verifies keys against the embedded public key.
func NewProvider(src Source, logger *slog.Logger) *Provider {
	pub, err := EmbeddedPublicKey()
	if err != nil {
		// The embedded key is part of the binary; a failure here is a build defect.
		// Verification then fails for every key, which leaves the state at none.
		pub = nil
		if logger != nil {
			logger.Error("unable to load the embedded license public key", "error", err)
		}
	}
	return newProvider(src, pub, time.Now, logger)
}

func newProvider(src Source, pub ed25519.PublicKey, now func() time.Time, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	p := &Provider{pub: pub, now: now, logger: logger, file: src.File}

	switch {
	case src.File != "":
		if src.Value != "" {
			logger.Warn("both EE_LICENSE and EE_LICENSE_FILE are set: the file wins")
		}
		p.mu.Lock()
		p.lastCheck = now()
		p.readFile()
		p.mu.Unlock()
	case src.Value != "":
		key, err := parseWith(strings.TrimSpace(src.Value), pub)
		if err != nil {
			p.problem = err
			logger.Error("the license key is not usable", "error", err)
		} else {
			p.key = key
		}
	}
	return p
}

// readFile reads the license file and applies what it holds. The caller holds p.mu.
//
// A key that cannot be trusted never replaces the one in place: the error is kept in
// problem and logged once per faulty content. A well-signed key replaces it whatever
// its dates say, so that the file is the single source of truth.
func (p *Provider) readFile() {
	var seen fileObservation
	raw, err := os.ReadFile(p.file)
	if err != nil {
		seen.readFailed = true
	} else {
		seen.content = strings.TrimSpace(string(raw))
	}
	if p.lastSeen != nil && *p.lastSeen == seen {
		return
	}
	p.lastSeen = &seen

	var key *Key
	switch {
	case seen.readFailed:
		err = fmt.Errorf("the license file cannot be read: %w", unwrapPathError(err))
	case seen.content == "":
		err = errors.New("the license file is empty")
	default:
		key, err = parseWith(seen.content, p.pub)
	}
	if err != nil {
		p.problem = err
		p.logger.Error("the license file is not usable, keeping the previous key", "error", err)
		return
	}
	p.key = key
	p.problem = nil
}

// unwrapPathError drops the path from a file error, keeping the operation's cause.
func unwrapPathError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// snapshot reads the file again when due, then returns the key and the clock under one
// lock, so that an answer always comes from a single key.
func (p *Provider) snapshot() snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.file != "" && now.Sub(p.lastCheck) >= fileRecheckInterval {
		p.lastCheck = now
		p.readFile()
	}
	return snapshot{key: p.key, problem: p.problem, now: now}
}

// snapshot is one consistent answer: the key, the problem and the instant all come from
// the same critical section.
type snapshot struct {
	key     *Key
	problem error
	now     time.Time
}

// State is where the current key stands in its lifecycle, or StateNone without a key.
func (p *Provider) State() State {
	snap := p.snapshot()
	if snap.key == nil {
		return StateNone
	}
	return snap.key.StateAt(snap.now)
}

// IsValid is true up to the end of the grace period.
func (p *Provider) IsValid() bool {
	state := p.State()
	return state != StateNone && state != StateFrozen
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

// Problem is the reason the last read of the license was refused, or nil once a good
// read happens.
func (p *Provider) Problem() error {
	return p.snapshot().problem
}
