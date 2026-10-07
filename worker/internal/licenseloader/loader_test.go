package licenseloader

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// fakeAPI answers GetSystemLicenseKey with what the test sets: a key, or a failure when down.
type fakeAPI struct {
	key  atomic.Value // string
	down atomic.Bool
}

func (f *fakeAPI) client(t *testing.T) mgmtv1alpha1connect.UserAccountServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(
		mgmtv1alpha1connect.UserAccountServiceGetSystemLicenseKeyProcedure,
		connect.NewUnaryHandler(
			mgmtv1alpha1connect.UserAccountServiceGetSystemLicenseKeyProcedure,
			func(context.Context, *connect.Request[mgmtv1alpha1.GetSystemLicenseKeyRequest]) (*connect.Response[mgmtv1alpha1.GetSystemLicenseKeyResponse], error) {
				if f.down.Load() {
					return nil, connect.NewError(connect.CodeUnavailable, errors.New("the API is down"))
				}
				key, _ := f.key.Load().(string)
				return connect.NewResponse(&mgmtv1alpha1.GetSystemLicenseKeyResponse{Key: key}), nil
			},
		),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return mgmtv1alpha1connect.NewUserAccountServiceClient(srv.Client(), srv.URL)
}

// issuedKey mints a key signed by a fresh pair and gives it with the ring that verifies it.
func issuedKey(t *testing.T, id string) (string, license.Keyring) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ring := license.Keyring{"test": pub}
	issued, err := license.Issue(&license.IssueRequest{
		Id:         id,
		IssuedTo:   "Acme Co.",
		CustomerId: "cust-001",
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}, priv, ring)
	require.NoError(t, err)
	return issued.Encoded, ring
}

func Test_FromAPI_ReturnsTheKeyTheAPIHolds(t *testing.T) {
	api := &fakeAPI{}
	api.key.Store("the-signed-key")
	load := FromAPI(api.client(t))

	value, err := load(t.Context())
	require.NoError(t, err)
	require.Equal(t, "the-signed-key", value)

	api.key.Store("")
	value, err = load(t.Context())
	require.NoError(t, err, "an instance with no key is not a failure")
	require.Empty(t, value)

	api.down.Store(true)
	_, err = load(t.Context())
	require.Error(t, err)
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
}

func Test_Worker_StartsWithoutTheAPI(t *testing.T) {
	api := &fakeAPI{}
	key, ring := issuedKey(t, "lic-1")
	api.key.Store(key)
	api.down.Store(true)
	provider := license.NewProviderWithKeyring(FromAPI(api.client(t)), ring, nil)

	require.Error(t, provider.Refresh(t.Context()))
	require.False(t, provider.IsValid(), "the worker starts with no license")
	require.ErrorIs(t, provider.Problem(), license.ErrKeyNotLoaded, "and knows that it was not answered")

	api.down.Store(false)
	require.NoError(t, provider.Refresh(t.Context()))
	require.True(t, provider.IsValid(), "the next refresh gives the license")
	require.NoError(t, provider.Problem())
}

// countingLoader fails its first calls, then answers the value.
type countingLoader struct {
	failures int64
	value    string
	calls    atomic.Int64
	// called is signalled on every call, when set.
	called chan struct{}
}

func (l *countingLoader) load(context.Context) (string, error) {
	n := l.calls.Add(1)
	if l.called != nil {
		select {
		case l.called <- struct{}{}:
		default:
		}
	}
	if n <= l.failures {
		return "", errors.New("the API is down")
	}
	return l.value, nil
}

func Test_AwaitFirstAnswer(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Run("an instance without a key is an answer", func(t *testing.T) {
		loader := &countingLoader{failures: 2}
		provider := license.NewProviderWithKeyring(loader.load, nil, logger)

		require.NoError(t, AwaitFirstAnswer(t.Context(), provider, time.Millisecond, logger))
		require.EqualValues(t, 3, loader.calls.Load(), "asked again until the API answered, and no more")
		require.False(t, provider.IsValid())
		require.NoError(t, provider.Problem())
	})

	t.Run("the key the API holds is in force once it answers", func(t *testing.T) {
		key, ring := issuedKey(t, "lic-1")
		loader := &countingLoader{failures: 2, value: key}
		provider := license.NewProviderWithKeyring(loader.load, ring, logger)

		require.NoError(t, AwaitFirstAnswer(t.Context(), provider, time.Millisecond, logger))
		require.True(t, provider.IsValid())
	})

	t.Run("a key the worker refuses is an answer too", func(t *testing.T) {
		loader := &countingLoader{value: "not-a-key"}
		provider := license.NewProviderWithKeyring(loader.load, nil, logger)

		require.NoError(t, AwaitFirstAnswer(t.Context(), provider, time.Hour, logger))
		require.EqualValues(t, 1, loader.calls.Load())
		require.Error(t, provider.Problem())
	})

	t.Run("it stops waiting when its context is cancelled", func(t *testing.T) {
		loader := &countingLoader{failures: 1 << 30, called: make(chan struct{}, 1)}
		provider := license.NewProviderWithKeyring(loader.load, nil, logger)
		ctx, cancel := context.WithCancel(t.Context())

		done := make(chan error, 1)
		go func() { done <- AwaitFirstAnswer(ctx, provider, time.Hour, logger) }()
		<-loader.called
		cancel()

		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(5 * time.Second):
			t.Fatal("the wait did not end with its context")
		}
		require.EqualValues(t, 1, loader.calls.Load())
	})
}

func Test_Worker_KeepsTheLastKeyWhenTheAPIStopsAnswering(t *testing.T) {
	api := &fakeAPI{}
	key, ring := issuedKey(t, "lic-1")
	api.key.Store(key)
	provider := license.NewProviderWithKeyring(FromAPI(api.client(t)), ring, nil)
	require.NoError(t, provider.Refresh(t.Context()))
	require.True(t, provider.IsValid())

	api.down.Store(true)
	require.Error(t, provider.Refresh(t.Context()))
	require.True(t, provider.IsValid(), "an API that does not answer takes no license away")
}
