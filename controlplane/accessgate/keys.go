package accessgate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
)

const (
	certsPath = "/cdn-cgi/access/certs"
	// refreshInterval is how often the keys of the team are fetched again in the background.
	refreshInterval = time.Hour
	// minRefreshGap is the least time between two fetches asked for by a key id nobody knows.
	// Anybody can send such a token: without the gap each one would cost a fetch.
	minRefreshGap = time.Minute
	// fetchTimeout bounds one fetch of the keys.
	fetchTimeout = 10 * time.Second
)

// keySource gives the key of a key id.
type keySource interface {
	lookup(ctx context.Context, kid string) (jwk.Key, bool)
}

// fixedKeys is a set that never changes.
type fixedKeys struct {
	set jwk.Set
}

func (k fixedKeys) lookup(_ context.Context, kid string) (jwk.Key, bool) {
	return k.set.LookupKeyID(kid)
}

// remoteKeys is the key set of a team, held in memory. It is fetched once at start, again at
// every interval, and when a token names a key id it does not hold, which is how a new key of
// the team is taken up without waiting for the interval. No request fetches it otherwise. A
// fetch that fails leaves the last set in place.
type remoteKeys struct {
	url    string
	client *http.Client
	now    func() time.Time
	logger *slog.Logger

	set atomic.Pointer[jwk.Set]

	// mu serializes the fetches asked for by an unknown key id; lastAsked is the time of the last.
	mu        sync.Mutex
	lastAsked time.Time
}

// fetch replaces the set by the one the team serves now. A set without a key is a failure: it
// would refuse everybody, and is more likely a wrong answer than a team without keys.
func (k *remoteKeys) fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	set, err := jwk.Fetch(ctx, k.url, jwk.WithHTTPClient(k.client))
	if err != nil {
		return fmt.Errorf("unable to fetch the keys of Access: %w", err)
	}
	if set.Len() == 0 {
		return errors.New("unable to fetch the keys of Access: the set holds no key")
	}
	k.set.Store(&set)
	return nil
}

func (k *remoteKeys) held(kid string) (jwk.Key, bool) {
	return (*k.set.Load()).LookupKeyID(kid)
}

func (k *remoteKeys) lookup(ctx context.Context, kid string) (jwk.Key, bool) {
	if key, ok := k.held(kid); ok {
		return key, true
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	// A request that waited here may find the key brought in by the one before it.
	if key, ok := k.held(kid); ok {
		return key, true
	}
	now := k.now()
	if !k.lastAsked.IsZero() && now.Sub(k.lastAsked) < minRefreshGap {
		return nil, false
	}
	k.lastAsked = now
	// The fetch serves every request to come, not only this one: a caller that goes away does
	// not cut it short.
	if err := k.fetch(context.WithoutCancel(ctx)); err != nil {
		k.logger.Error("unable to refresh the keys of Access", "error", err.Error())
		return nil, false
	}
	return k.held(kid)
}

// refreshEvery fetches the set again at every interval until ctx ends.
func (k *remoteKeys) refreshEvery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := k.fetch(ctx); err != nil && ctx.Err() == nil {
				k.logger.Error("unable to refresh the keys of Access", "error", err.Error())
			}
		}
	}
}
