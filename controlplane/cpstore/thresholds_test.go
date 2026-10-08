package cpstore

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// The thresholds are written here a second time, in figures: changing one is changing two lines.
// The integration tests place their rows from the constants; this is what holds the figures.
func Test_Thresholds_AreTheOnesAgreed(t *testing.T) {
	require.Equal(t, 3, SilentAfterDays)
	require.Equal(t, 30, RecentInstanceDays)
	require.Equal(t, 720*time.Hour, ExpiringWithin, "30 days")
	require.Equal(t, 24*time.Hour, OldPendingAfter)
	require.Equal(t, 7, SealRejectionDays)
	require.Equal(t, 400, InstanceReportsCap)
}

// A license listed as expiring for its date is one its key calls expiring.
func Test_ExpiringWithin_IsTheWindowOfTheKey(t *testing.T) {
	require.Equal(t, license.ExpiringWindow, ExpiringWithin)
}
