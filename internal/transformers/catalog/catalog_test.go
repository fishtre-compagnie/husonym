package catalog

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func Test_DefaultConfig(t *testing.T) {
	t.Run("the config a phone column starts with keeps its format", func(t *testing.T) {
		config, ok := DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER, false)
		require.True(t, ok)
		require.True(t, config.GetTransformPhoneNumberConfig().GetPreserveFormat())
	})

	t.Run("a copy: changing it leaves the catalogue as it was", func(t *testing.T) {
		config, ok := DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER, false)
		require.True(t, ok)
		preserve := false
		config.GetTransformPhoneNumberConfig().PreserveFormat = &preserve

		again, _ := DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PHONE_NUMBER, false)
		require.True(t, again.GetTransformPhoneNumberConfig().GetPreserveFormat())
	})

	t.Run("an enterprise transformer needs the license", func(t *testing.T) {
		_, ok := DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT, false)
		require.False(t, ok)
		_, ok = DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT, true)
		require.True(t, ok)
	})

	t.Run("an unknown source", func(t *testing.T) {
		_, ok := DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED, true)
		require.False(t, ok)
	})
}
