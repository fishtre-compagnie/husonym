package license

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/stretchr/testify/require"
)

func Test_Refusal_Unwraps(t *testing.T) {
	cause := husonymerrors.NewForbidden("this license does not include rbac")
	refusal := NewRefusal("acc", cause, FeatureGate(FeatureRbac))

	var found *Refusal
	require.True(t, errors.As(fmt.Errorf("x: %w", refusal), &found))
	require.Equal(t, "acc", found.AccountId)
	require.Equal(t, []Gate{"rbac"}, found.Gates)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(refusal))
	require.Equal(t, cause.Error(), refusal.Error())
	require.Equal(t, "this license does not include rbac", refusal.Message())

	// A cause that is not a Connect error is its own sentence.
	require.Equal(t, "plain", NewRefusal("acc", errors.New("plain")).Message())
}

func Test_AllGates(t *testing.T) {
	gates := AllGates()
	require.Len(t, gates, 18)
	seen := map[Gate]struct{}{}
	for _, gate := range gates {
		seen[gate] = struct{}{}
	}
	require.Len(t, seen, 18)
	for i, f := range AllFeatures() {
		require.Equal(t, FeatureGate(f), gates[i])
	}
	require.Equal(t,
		[]Gate{GateNotInForce, GateSourceCap, GateJobCap, GateConnectionCap, GateConnectionType}, gates[13:])
}
