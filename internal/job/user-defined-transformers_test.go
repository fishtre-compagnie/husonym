package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// A transformer runs the user-defined transformer it names, and those TransformPiiText hands the
// PII it finds to.
func Test_UserDefinedTransformerIds(t *testing.T) {
	userDefined := func(id string) *mgmtv1alpha1.TransformerConfig {
		return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
			UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: id},
		}}
	}
	transform := func(config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.PiiAnonymizer {
		return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
			Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: config},
		}}
	}

	require.Equal(t, []string{"a"}, UserDefinedTransformerIds(userDefined("a")))
	require.Empty(t, UserDefinedTransformerIds(&mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	}))
	require.Empty(t, UserDefinedTransformerIds(nil))
	require.ElementsMatch(t, []string{"default", "person", "email"}, UserDefinedTransformerIds(&mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{
			DefaultAnonymizer: transform(userDefined("default")),
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{
				"PERSON":        transform(userDefined("person")),
				"EMAIL_ADDRESS": transform(userDefined("email")),
				"PHONE_NUMBER":  {Config: &mgmtv1alpha1.PiiAnonymizer_Redact_{Redact: &mgmtv1alpha1.PiiAnonymizer_Redact{}}},
			},
		}},
	}))
}
