package license

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Key_HasFeature(t *testing.T) {
	absent := &Key{}
	wildcard := &Key{Features: []string{"*"}}
	explicit := &Key{Features: []string{"job_hooks", "not_a_feature"}}
	empty := &Key{Features: []string{}}
	for _, f := range AllFeatures() {
		require.True(t, absent.HasFeature(f))
		require.True(t, wildcard.HasFeature(f))
	}
	require.True(t, explicit.HasFeature(FeatureJobHooks))
	require.False(t, explicit.HasFeature(FeaturePiiText))
	// An explicit empty list is a key that allows no optional feature.
	require.False(t, empty.HasFeature(FeatureJobHooks))
}

func Test_Key_AllowsEveryFeature(t *testing.T) {
	require.True(t, (&Key{}).AllowsEveryFeature())
	require.True(t, (&Key{Features: []string{"*"}}).AllowsEveryFeature())
	// Naming the thirteen is not allowing every feature: a fourteenth would be left out.
	named := make([]string, 0, len(AllFeatures()))
	for _, f := range AllFeatures() {
		named = append(named, string(f))
	}
	require.False(t, (&Key{Features: named}).AllowsEveryFeature())
	require.False(t, (&Key{Features: []string{}}).AllowsEveryFeature())
}

func Test_Key_AnExplicitEmptyFeatureListSurvivesBeingWritten(t *testing.T) {
	raw, err := json.Marshal(Key{Features: []string{}})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"features":[]`)

	var back Key
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Features)
	for _, f := range AllFeatures() {
		require.False(t, back.HasFeature(f), f)
	}
}

func Test_Key_AnAbsentFeatureListIsNotWritten(t *testing.T) {
	raw, err := json.Marshal(Key{})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "features")

	var back Key
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Nil(t, back.Features)
}

func Test_AllFeatures_AreThirteenDistinctParsableNames(t *testing.T) {
	all := AllFeatures()
	require.Len(t, all, 13)

	seen := map[Feature]bool{}
	for _, f := range all {
		require.False(t, seen[f], "duplicate feature %q", f)
		seen[f] = true

		parsed, ok := ParseFeature(string(f))
		require.True(t, ok)
		require.Equal(t, f, parsed)
	}

	_, ok := ParseFeature(FeatureWildcard)
	require.False(t, ok)
	_, ok = ParseFeature("not_a_feature")
	require.False(t, ok)

	// The caller may reorder or edit the result without touching the next call's.
	all[0] = "changed"
	require.Equal(t, FeatureJobHooks, AllFeatures()[0])
}

func Test_Key_TelemetryMode(t *testing.T) {
	tests := []struct {
		telemetry string
		want      TelemetryMode
	}{
		{"", TelemetryOnline},
		{"online", TelemetryOnline},
		{"offline_report", TelemetryOfflineReport},
		{"none", TelemetryNone},
		{"something_else", TelemetryOnline},
	}
	for _, tt := range tests {
		t.Run(tt.telemetry, func(t *testing.T) {
			require.Equal(t, tt.want, (&Key{Telemetry: tt.telemetry}).TelemetryMode())
		})
	}
}
