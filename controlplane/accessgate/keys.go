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

	"github.com/lestrrat-go/jwx/v3/jwa"
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
	// maxKeySetBytes is the largest key set that is read. A real one is a few kilobytes.
	maxKeySetBytes = 1 << 20
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
	client keyClient
	now    func() time.Time
	logger *slog.Logger

	set atomic.Pointer[jwk.Set]

	// mu serializes the fetches asked for by an unknown key id; lastAsked is the time of the last.
	mu        sync.Mutex
	lastAsked time.Time

	// stopped is closed when the background refresh has ended.
	stopped chan struct{}
}

// keyClient fetches the key set. It never follows a redirect: the keys come from the address of
// the team or from nowhere, and a redirect shows as an answer that is not 200. It cuts a body
// at maxKeySetBytes, which fails the fetch, and bounds the whole exchange by fetchTimeout.
type keyClient struct {
	client *http.Client
}

// newKeyClient makes a keyClient that reaches the network as base does. base itself is not
// changed.
func newKeyClient(base *http.Client) keyClient {
	hardened := *base
	hardened.Timeout = fetchTimeout
	hardened.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return keyClient{client: &hardened}
}

// Do is what jwk.Fetch asks of a client.
func (c keyClient) Do(request *http.Request) (*http.Response, error) {
	// The request is the one fetch builds, for the address made of the team domain New checked.
	response, err := c.client.Do(request) //nolint:gosec // the address is configuration, not input
	if err != nil {
		return nil, err
	}
	response.Body = http.MaxBytesReader(nil, response.Body, maxKeySetBytes)
	return response, nil
}

// fetch replaces the set by the one the team serves now. A set that holds no key a token could
// be checked with is a failure: it would refuse everybody, and is more likely a wrong answer
// than a team without keys.
func (k *remoteKeys) fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	set, err := jwk.Fetch(ctx, k.url, jwk.WithHTTPClient(k.client))
	if err != nil {
		return fmt.Errorf("unable to fetch the keys of Access: %w", err)
	}
	if !usable(set) {
		return errors.New("unable to fetch the keys of Access: the set holds no RSA key with a key id")
	}
	k.set.Store(&set)
	return nil
}

// usable says whether a set holds at least one key the gate could hand to a verification: an
// RSA key that has a key id.
func usable(set jwk.Set) bool {
	for i := range set.Len() {
		key, ok := set.Key(i)
		if !ok || key.KeyType() != jwa.RSA() {
			continue
		}
		if kid, ok := key.KeyID(); ok && kid != "" {
			return true
		}
	}
	return false
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
	defer close(k.stopped)
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
