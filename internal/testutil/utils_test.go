package testutil

import (
	"testing"

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
