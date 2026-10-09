package usagestore

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

func Test_knownGates_DropsWhatTheLicenseDoesNotDefine(t *testing.T) {
	got := knownGates([]license.Gate{license.GateJobCap, "made_up", license.GateNotInForce, ""})
	require.Equal(t, []license.Gate{license.GateJobCap, license.GateNotInForce}, got)
	require.Empty(t, knownGates(nil))
	require.Equal(t, []license.Gate{license.GateJobCap},
		knownGates([]license.Gate{license.GateJobCap, license.GateJobCap}))
}

func Test_Store_RefusesAnIdThatIsNotAUuid(t *testing.T) {
	s := New(nil)
	err := s.RunStarted(t.Context(), RunStart{RunId: "r", AccountId: "nope", JobId: "nope"})
	require.ErrorContains(t, err, "account id")
	err = s.RunEnded(t.Context(), RunEnd{RunId: "r", AccountId: "00000000-0000-0000-0000-000000000001", JobId: "nope", Status: StatusCompleted})
	require.ErrorContains(t, err, "job id")
}

func Test_RunEnded_RefusesAStatusThatIsNotAnEnd(t *testing.T) {
	s := New(nil)
	for _, status := range []Status{StatusRunning, StatusTerminated, StatusTimedOut, "", "bogus"} {
		err := s.RunEnded(t.Context(), RunEnd{
			RunId: "r", AccountId: "00000000-0000-0000-0000-000000000001",
			JobId: "00000000-0000-0000-0000-000000000002", Status: status,
		})
		require.ErrorContains(t, err, "cannot end", "status %q", status)
	}
}

func Test_CloseRun_RefusesAStatusThatIsNotAnEnd(t *testing.T) {
	s := New(nil)
	for _, status := range []Status{StatusRunning, StatusTerminated, StatusTimedOut, "", "bogus"} {
		err := s.CloseRun(t.Context(), "r", status, time.Now(), 0, 0, 0, 0, "", RunError{})
		require.ErrorContains(t, err, "cannot end", "status %q", status)
	}
}
