package issuers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const deploymentIssuer = "https://idp.deployment.example.com/"

func staticLoad(issuers ...string) Load {
	return func(context.Context) ([]string, error) { return issuers, nil }
}

func Test_Resolver_AlwaysCarriesTheDeploymentIssuer(t *testing.T) {
	// §10.2 of the plan: a wrong setting must never lock an account's administrators out,
	// so the deployment's own issuer is accepted whatever the accounts have declared.
	r := NewResolver(deploymentIssuer, staticLoad("https://account.example.com/"), time.Minute, nil)

	got, err := r.Resolve(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{deploymentIssuer, "https://account.example.com/"}, got)
	require.Equal(t, deploymentIssuer, got[0], "the fallback comes first, so it is never crowded out")
}

func Test_Resolver_DropsWhatCannotBeAnIssuer(t *testing.T) {
	// The validator compares by exact string equality, so an empty entry would accept a
	// token with no iss -- and every identity keyed on it would be unattributable.
	r := NewResolver(deploymentIssuer, staticLoad("", "https://a.example.com/"), time.Minute, nil)

	got, err := r.Resolve(t.Context())
	require.NoError(t, err)
	require.NotContains(t, got, "")
	require.Len(t, got, 2)
}

func Test_Resolver_DoesNotRepeatAnIssuer(t *testing.T) {
	// Two accounts on the same provider, and an account on the deployment's own.
	r := NewResolver(
		deploymentIssuer,
		staticLoad("https://a.example.com/", "https://a.example.com/", deploymentIssuer),
		time.Minute, nil,
	)

	got, err := r.Resolve(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{deploymentIssuer, "https://a.example.com/"}, got)
}

func Test_Resolver_NoDeploymentIssuerConfigured(t *testing.T) {
	r := NewResolver("", staticLoad("https://a.example.com/"), time.Minute, nil)

	got, err := r.Resolve(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"https://a.example.com/"}, got)
}

func Test_Resolver_CachesWithinTheWindow(t *testing.T) {
	var calls int
	r := NewResolver(deploymentIssuer, func(context.Context) ([]string, error) {
		calls++
		return []string{"https://a.example.com/"}, nil
	}, time.Minute, nil)

	for range 5 {
		_, err := r.Resolve(t.Context())
		require.NoError(t, err)
	}
	require.Equal(t, 1, calls, "the validator calls this on every request; it must not be a query")
}

func Test_Resolver_RefreshesAfterTheWindow(t *testing.T) {
	var calls int
	r := NewResolver(deploymentIssuer, func(context.Context) ([]string, error) {
		calls++
		return []string{"https://a.example.com/"}, nil
	}, time.Minute, nil)

	now := time.Now()
	r.now = func() time.Time { return now }

	_, err := r.Resolve(t.Context())
	require.NoError(t, err)
	now = now.Add(2 * time.Minute)
	_, err = r.Resolve(t.Context())
	require.NoError(t, err)

	require.Equal(t, 2, calls)
}

func Test_Resolver_Invalidate(t *testing.T) {
	// A setting just written must take effect now, not at the end of the window.
	var calls int
	r := NewResolver(deploymentIssuer, func(context.Context) ([]string, error) {
		calls++
		return nil, nil
	}, time.Hour, nil)

	_, err := r.Resolve(t.Context())
	require.NoError(t, err)
	r.Invalidate()
	_, err = r.Resolve(t.Context())
	require.NoError(t, err)

	require.Equal(t, 2, calls)
}

// A database hiccup must not sign everybody out, including the administrators who would
// have to fix it.
func Test_Resolver_DegradesRatherThanFails(t *testing.T) {
	t.Run("keeps the last good answer", func(t *testing.T) {
		fail := false
		r := NewResolver(deploymentIssuer, func(context.Context) ([]string, error) {
			if fail {
				return nil, errors.New("the database is unreachable")
			}
			return []string{"https://a.example.com/"}, nil
		}, time.Minute, nil)

		now := time.Now()
		r.now = func() time.Time { return now }

		warm, err := r.Resolve(t.Context())
		require.NoError(t, err)

		fail = true
		now = now.Add(2 * time.Minute)
		got, err := r.Resolve(t.Context())
		require.NoError(t, err, "a failure to read the accounts is not a failure to authenticate")
		require.Equal(t, warm, got)
	})

	t.Run("falls back on the deployment issuer when it never warmed", func(t *testing.T) {
		r := NewResolver(deploymentIssuer, func(context.Context) ([]string, error) {
			return nil, errors.New("the database is unreachable")
		}, time.Minute, nil)

		got, err := r.Resolve(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{deploymentIssuer}, got)
	})
}

// The validator calls this from every request handler at once.
func Test_Resolver_IsSafeUnderConcurrency(t *testing.T) {
	r := NewResolver(deploymentIssuer, staticLoad("https://a.example.com/"), time.Millisecond, nil)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.NotEmpty(t, got)
		}()
	}
	wg.Wait()
}

// The expiry of the window sets every in-flight request refreshing at once, and none of
// them has written the answer back yet. Without coalescing that is a burst of identical
// queries every thirty seconds.
func Test_Resolver_CoalescesConcurrentRefreshes(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	release := make(chan struct{})

	r := NewResolver(deploymentIssuer, func(context.Context) ([]string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release // hold the first load open so the others pile up behind it
		return []string{"https://a.example.com/"}, nil
	}, time.Minute, nil)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.Len(t, got, 2)
		}()
	}

	// Let them all reach the load before any returns.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls, "twenty requests on a cold cache must be one query, not twenty")
}
