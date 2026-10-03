package serve_connect

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Worker keys are separated by commas, as the chart joins them, spaces around them ignored; a
// value that is not a worker key stops the API at startup rather than lock the worker out.
func Test_parseWorkerApiKeys(t *testing.T) {
	a := "neo_wt_v1_1b4e28ba-2fa1-41d2-883f-0016d3cca427"
	b := "neo_wt_v1_6fa459ea-ee8a-4ca4-894e-db77e160355e"

	keys, err := parseWorkerApiKeys(a + ", " + b + ",")
	require.NoError(t, err)
	require.Equal(t, []string{a, b}, keys)

	keys, err = parseWorkerApiKeys("")
	require.NoError(t, err)
	require.Empty(t, keys)

	_, err = parseWorkerApiKeys(a + ",neo_at_v1_1b4e28ba-2fa1-41d2-883f-0016d3cca427")
	require.Error(t, err, "an account key is not a worker key")
}

// With authentication on, the worker has a key of its own, or the API does not start.
func Test_requireWorkerApiKeys(t *testing.T) {
	require.NoError(t, requireWorkerApiKeys([]string{"neo_wt_v1_1b4e28ba-2fa1-41d2-883f-0016d3cca427"}))
	require.ErrorContains(t, requireWorkerApiKeys(nil), "HUSONYM_ALLOWED_WORKER_API_KEYS")
}
