package usagestore

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

func Test_knownGates_DropsWhatTheLicenseDoesNotDefine(t *testing.T) {
	got := knownGates([]license.Gate{license.GateJobCap, "made_up", license.GateNotInForce, ""})
	require.Equal(t, []license.Gate{license.GateJobCap, license.GateNotInForce}, got)
	require.Empty(t, knownGates(nil))
}

func Test_Store_RefusesAnIdThatIsNotAUuid(t *testing.T) {
	s := New(nil)
	err := s.RunStarted(t.Context(), RunStart{RunId: "r", AccountId: "nope", JobId: "nope"})
	require.ErrorContains(t, err, "account id")
	err = s.RunEnded(t.Context(), RunEnd{RunId: "r", AccountId: "00000000-0000-0000-0000-000000000001", JobId: "nope"})
	require.ErrorContains(t, err, "job id")
}
