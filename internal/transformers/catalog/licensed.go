package catalog

import mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"

// defaultPiiScoreThreshold is the confidence the analyzer must reach for a finding to count,
// in the config TransformPiiText starts with.
const defaultPiiScoreThreshold = 0.5

// licensedSystemTransformers are the system transformers an account sees only under a valid
// license.
var licensedSystemTransformers = []*mgmtv1alpha1.SystemTransformer{
	{
		Name:        "Transform PII Text",
		Description: "Transforms free-form text using PII analyzers",
		DataTypes: []mgmtv1alpha1.TransformerDataType{
			mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
			mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
		},
		SupportedJobTypes: []mgmtv1alpha1.SupportedJobType{
			mgmtv1alpha1.SupportedJobType_SUPPORTED_JOB_TYPE_SYNC,
		},
		Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT,
		Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{
				TransformPiiTextConfig: &mgmtv1alpha1.TransformPiiText{
					ScoreThreshold: defaultPiiScoreThreshold,
					DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
						Config: &mgmtv1alpha1.PiiAnonymizer_Replace_{
							Replace: &mgmtv1alpha1.PiiAnonymizer_Replace{},
						},
					},
				},
			},
		},
	},
}
