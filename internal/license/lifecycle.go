package license

import "time"

// State is where a license key stands in its lifecycle at a given instant.
type State string

const (
	// StateNone means there is no key at all. A Key never reports it.
	StateNone State = "none"
	// StateValid means the key is in force and far from its expiry.
	StateValid State = "valid"
	// StateExpiring means the key is in force but expires within ExpiringWindow.
	StateExpiring State = "expiring"
	// StateGrace means the key has expired and is inside its grace period.
	StateGrace State = "grace"
	// StateFrozen means the grace period has run out.
	StateFrozen State = "frozen"
)

const (
	// DefaultGraceDays is the grace period of a key that does not say.
	DefaultGraceDays = 14
	// ExpiringWindow is how long before expiry a key starts reporting StateExpiring.
	ExpiringWindow = 30 * 24 * time.Hour
)

// graceDays is the effective grace period: absent means the default, and a negative
// value counts as no grace.
func (k *Key) graceDays() int {
	if k.GraceDays == nil {
		return DefaultGraceDays
	}
	return max(*k.GraceDays, 0)
}

// GraceEndsAt is the instant the grace period runs out.
func (k *Key) GraceEndsAt() time.Time {
	return k.ExpiresAt.Add(time.Duration(k.graceDays()) * 24 * time.Hour)
}

// StateAt derives the lifecycle state from the clock alone; IssuedAt plays no part.
func (k *Key) StateAt(now time.Time) State {
	switch {
	case !now.Before(k.GraceEndsAt()):
		return StateFrozen
	case !now.Before(k.ExpiresAt):
		return StateGrace
	case now.Add(ExpiringWindow).After(k.ExpiresAt):
		return StateExpiring
	default:
		return StateValid
	}
}

// Allows reports whether the license permits a connection type. A nil receiver or an
// empty list permits everything.
func (l *Limits) Allows(connectionType string) bool {
	if l == nil || len(l.AllowedConnectionTypes) == 0 {
		return true
	}
	for _, allowed := range l.AllowedConnectionTypes {
		if allowed == connectionType {
			return true
		}
	}
	return false
}
