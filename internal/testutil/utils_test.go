package testutil

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

func Test_FakeEELicense_HasFeature(t *testing.T) {
	t.Run("a valid fake allows every feature unless restricted", func(t *testing.T) {
		f := NewFakeEELicense(WithIsValid())
		require.True(t, f.HasFeature(license.FeatureSso))

		f.SetFeatures(license.FeatureMcp)
		require.True(t, f.HasFeature(license.FeatureMcp))
		require.False(t, f.HasFeature(license.FeatureSso))

		f.SetFeatures()
		require.False(t, f.HasFeature(license.FeatureMcp))
	})

	t.Run("WithFeatures with no argument allows none", func(t *testing.T) {
		require.False(t, NewFakeEELicense(WithIsValid(), WithFeatures()).HasFeature(license.FeatureMcp))
	})

	t.Run("an invalid fake allows none", func(t *testing.T) {
		f := NewFakeEELicense()
		require.False(t, f.HasFeature(license.FeatureSso))

		f.SetFeatures(license.FeatureMcp)
		require.False(t, f.HasFeature(license.FeatureMcp))
	})
}

// The description of a fake agrees with what the fake answers, one question at a time.
func Test_FakeEELicense_Describe(t *testing.T) {
	maxJobs := 1
	f := NewFakeEELicense(WithIsValid(), WithLimits(&license.Limits{MaxJobs: &maxJobs}))

	described := f.Describe()
	require.True(t, described.InForce())
	require.True(t, described.Key.AllowsEveryFeature())
	require.Equal(t, f.Limits(), described.Key.Limits)
	require.WithinDuration(t, f.ExpiresAt(), described.Key.ExpiresAt, time.Minute)

	f.SetFeatures(license.FeatureMcp)
	described = f.Describe()
	require.True(t, described.Key.HasFeature(license.FeatureMcp))
	require.False(t, described.Key.HasFeature(license.FeatureSso))

	f.SetFeatures()
	require.False(t, f.Describe().Key.HasFeature(license.FeatureMcp))

	f.SetValid(false)
	described = f.Describe()
	require.False(t, described.InForce())
	require.Nil(t, described.Key)
}
