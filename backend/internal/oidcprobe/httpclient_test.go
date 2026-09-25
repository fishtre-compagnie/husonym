package oidcprobe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishtre-compagnie/husonym/backend/internal/safehttp"
	"github.com/stretchr/testify/require"
)

// The end-to-end version of the above: a real server on loopback, which is what a hostile
// issuer pointing at the deployment itself would look like.
func Test_get_refusesLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Over https, to get past the scheme check and reach the dialer.
	target := "https://" + server.Listener.Addr().String()
	_, err := get(context.Background(), newSafeClient(safehttp.Policy{}), safehttp.Policy{}, target)
	require.Error(t, err)
	require.Contains(t, err.Error(), safehttp.ErrBlockedAddress.Error())
}

func Test_get_refusesPlainHTTP(t *testing.T) {
	_, err := get(context.Background(), newSafeClient(safehttp.Policy{}), safehttp.Policy{}, "http://example.com/.well-known/openid-configuration")
	require.ErrorIs(t, err, safehttp.ErrNotHTTPS)
}

func Test_get_refusesSomethingThatIsNotAURL(t *testing.T) {
	_, err := get(context.Background(), newSafeClient(safehttp.Policy{}), safehttp.Policy{}, "://nope")
	require.Error(t, err)
}
