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

	t.Run("the entry of the PII text transformer", func(t *testing.T) {
		source := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT
		entry, ok := BySource(true)[source]
		require.True(t, ok)
		require.Equal(t, "Transform PII Text", entry.GetName())
		require.Equal(t, "Transforms free-form text using PII analyzers", entry.GetDescription())
		require.Equal(t, []mgmtv1alpha1.TransformerDataType{
			mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
		}, entry.GetDataTypes())
		require.Equal(t, []mgmtv1alpha1.SupportedJobType{
			mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_SYNC,
		}, entry.GetSupportedJobTypes())

		config := entry.GetConfig().GetTransformPiiTextConfig()
		require.InDelta(t, 0.5, config.GetScoreThreshold(), 0)
		require.NotNil(t, config.GetDefaultAnonymizer().GetReplace())
		require.Nil(t, config.GetDefaultAnonymizer().GetReplace().Value)
		require.Nil(t, config.Language)
		require.Empty(t, config.GetEntityAnonymizers())
		require.Empty(t, config.GetDenyRecognizers())
		require.Empty(t, config.GetAllowedEntities())
		require.Empty(t, config.GetAllowedPhrases())
	})

	t.Run("an unknown source", func(t *testing.T) {
		_, ok := DefaultConfig(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED, true)
		require.False(t, ok)
	})
}

func Test_SourceOf(t *testing.T) {
	t.Run("each transformer is read back from its config", func(t *testing.T) {
		kinds := map[mgmtv1alpha1.TransformerSource]bool{}
		for _, transformer := range Transformers(true) {
			source, ok := SourceOf(transformer.GetConfig())
			require.True(t, ok, "%s", transformer.GetSource())
			require.Equal(t, transformer.GetSource(), source, "two transformers share a kind of config")
			kinds[source] = true
		}
		require.Len(t, kinds, len(Transformers(true)))
	})

	t.Run("a user-defined transformer is no system one", func(t *testing.T) {
		_, ok := SourceOf(&mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
			UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{},
		}})
		require.False(t, ok)
		_, ok = SourceOf(&mgmtv1alpha1.TransformerConfig{})
		require.False(t, ok)
	})
}
