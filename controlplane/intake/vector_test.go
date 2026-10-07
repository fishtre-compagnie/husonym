package intake

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/stretchr/testify/require"
)

// The seal vector is published by the product: a seal the product computed must verify through
// the call Receive makes.
func Test_SealVector_IsAccepted(t *testing.T) {
	raw, err := os.ReadFile("../../internal/telemetry/testdata/seal-vector.json")
	require.NoError(t, err)
	var vector struct {
		Key      string `json:"key"`
		Document string `json:"document"`
		Seal     string `json:"seal"`
	}
	require.NoError(t, json.Unmarshal(raw, &vector))
	issued := &cpstore.License{Encoded: vector.Key}

	require.True(t, sealed(issued, []byte(vector.Document), vector.Seal))
	require.False(t, sealed(issued, []byte(vector.Document+" "), vector.Seal))
}
