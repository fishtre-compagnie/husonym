// Package issuers answers, per request, which identity providers this deployment accepts
// tokens from.
package issuers

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// DefaultTTL is how long a resolved list is reused.
//
// The resolver runs on every authenticated request, and the library that calls it asks for
// under five milliseconds, so reading the database each time is out of the question. The
// other end of the trade is how long a provider an account has just declared takes to
// start working, and how long one it has just removed keeps working -- half a minute is
// short enough for a human setting it up, and long enough that the query is noise.
const DefaultTTL = 30 * time.Second

// Load returns the issuers the accounts of this deployment have declared.
type Load func(ctx context.Context) ([]string, error)

// Resolver hands the token validator the list of issuers it may accept.
//
// Two properties matter more than the caching:
//
//   - the deployment's own issuer is always in the list. That is the decision of §10.2 of
//     the plan: a wrong setting must never lock an account's administrators out, and the
//     operator of a deployment already holds its database. Binding an issuer to an account
//     is a separate check, made once the account is known -- this list only says which
//     tokens are authentic, never which account they open.
//   - a failure to read the accounts degrades to the last good answer, and then to the
//     deployment issuer alone. A database hiccup must not sign everybody out.
type Resolver struct {
	deploymentIssuer string
	load             Load
	ttl              time.Duration
	logger           *slog.Logger
	now              func() time.Time

	mu       sync.RWMutex
	cached   []string
	cachedAt time.Time
	warm     bool
}

func NewResolver(deploymentIssuer string, load Load, ttl time.Duration, logger *slog.Logger) *Resolver {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{
		deploymentIssuer: deploymentIssuer,
		load:             load,
		ttl:              ttl,
		logger:           logger,
		now:              time.Now,
	}
}

// Resolve returns the issuers to accept.
//
// It never returns an error. The validator treats one as a failure to authenticate, which
// would turn a slow database into a sign-out for everybody, including the administrators
// who would have to fix it.
func (r *Resolver) Resolve(ctx context.Context) ([]string, error) {
	r.mu.RLock()
	cached, cachedAt, warm := r.cached, r.cachedAt, r.warm
	r.mu.RUnlock()

	if warm && r.now().Sub(cachedAt) < r.ttl {
		return cached, nil
	}

	declared, err := r.load(ctx)
	if err != nil {
		r.logger.Warn("unable to read the issuers declared by accounts", "error", err.Error())
		if warm {
			return cached, nil
		}
		return r.baseline(), nil
	}

	fresh := r.combine(declared)
	r.mu.Lock()
	r.cached, r.cachedAt, r.warm = fresh, r.now(), true
	r.mu.Unlock()
	return fresh, nil
}

// Invalidate drops the cache, so that a setting just written takes effect at once rather
// than at the end of the window.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.warm = false
	r.mu.Unlock()
}

func (r *Resolver) baseline() []string {
	if r.deploymentIssuer == "" {
		return nil
	}
	return []string{r.deploymentIssuer}
}

// combine puts the deployment issuer first and drops what cannot be an issuer.
//
// The empty string is dropped rather than passed through: the validator compares issuers
// by exact string equality, so an empty entry would accept a token whose iss claim is
// missing -- and every identity keyed on it would be unattributable.
func (r *Resolver) combine(declared []string) []string {
	combined := r.baseline()
	for _, issuer := range declared {
		if issuer == "" || slices.Contains(combined, issuer) {
			continue
		}
		combined = append(combined, issuer)
	}
	return combined
}
