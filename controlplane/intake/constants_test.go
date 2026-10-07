package intake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The bounds are written here a second time, in figures: changing one is changing two lines.
func Test_Bounds_AreTheOnesAgreed(t *testing.T) {
	require.Equal(t, 100, PendingCapPerFingerprint)
	require.Equal(t, 10000, PendingCapTotal)
	require.Equal(t, 1080*time.Hour, PendingKept, "45 days")
	require.Equal(t, 50, InstanceCapPerLicense)
	require.Equal(t, 60, MaxDaysBehind, "60 days")
}
