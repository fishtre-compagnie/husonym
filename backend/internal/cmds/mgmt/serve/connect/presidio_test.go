package serve_connect

import (
	"net/http"
	"testing"

	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	"github.com/stretchr/testify/require"
)

// Presidio is waited for a limited time: a Presidio that never answers must not hold the call
// that asks it, which is a row a run transforms.
func Test_getPresidioClient_WaitsForALimitedTime(t *testing.T) {
	client, ok, err := getPresidioClient("http://presidio:3000")
	require.NoError(t, err)
	require.True(t, ok)

	inner, isClient := client.ClientInterface.(*presidioapi.Client)
	require.True(t, isClient)
	httpClient, isHTTP := inner.Client.(*http.Client)
	require.True(t, isHTTP)
	require.Equal(t, presidioTimeout, httpClient.Timeout)
	require.Positive(t, presidioTimeout)
}
