package telemetry

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

func Test_EffectiveMode_FollowsTheTable(t *testing.T) {
	tests := []struct {
		key     license.TelemetryMode
		setting string
		mode    Mode
		below   bool
	}{
		{license.TelemetryOnline, "", ModeOnline, false},
		{license.TelemetryOnline, "offline", ModeOfflineReport, true},
		{license.TelemetryOnline, "off", ModeNone, true},
		{license.TelemetryOfflineReport, "", ModeOfflineReport, false},
		{license.TelemetryOfflineReport, "offline", ModeOfflineReport, false},
		{license.TelemetryOfflineReport, "off", ModeNone, true},
		{license.TelemetryNone, "", ModeNone, false},
		{license.TelemetryNone, "offline", ModeNone, false},
		{license.TelemetryNone, "off", ModeNone, false},
	}
	for _, tc := range tests {
		mode, below := EffectiveMode(tc.key, tc.setting)
		require.Equal(t, tc.mode, mode, "key %q, setting %q", tc.key, tc.setting)
		require.Equal(t, tc.below, below, "key %q, setting %q", tc.key, tc.setting)
	}
}

func Test_EffectiveMode_AnyOtherValueIsNotSet(t *testing.T) {
	for _, setting := range []string{"", "   ", "on", "online", "false", "0", "offline_report"} {
		mode, below := EffectiveMode(license.TelemetryOnline, setting)
		require.Equal(t, ModeOnline, mode, "setting %q", setting)
		require.False(t, below, "setting %q", setting)
	}
}

func Test_EffectiveMode_IgnoresCaseAndSurroundingSpace(t *testing.T) {
	for _, setting := range []string{"OFF", " Off\n", "oFf"} {
		mode, below := EffectiveMode(license.TelemetryOnline, setting)
		require.Equal(t, ModeNone, mode, "setting %q", setting)
		require.True(t, below)
	}
	for _, setting := range []string{"OFFLINE", "\tOffline "} {
		mode, below := EffectiveMode(license.TelemetryOnline, setting)
		require.Equal(t, ModeOfflineReport, mode, "setting %q", setting)
		require.True(t, below)
	}
}
