package job

import mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"

// UserDefinedTransformerIds lists the user-defined transformers a transformer runs: itself, or
// those the anonymizers of TransformPiiText hand the PII they find to.
func UserDefinedTransformerIds(config *mgmtv1alpha1.TransformerConfig) []string {
	if userDefined := config.GetUserDefinedTransformerConfig(); userDefined != nil {
		return []string{userDefined.GetId()}
	}
	piiText := config.GetTransformPiiTextConfig()
	if piiText == nil {
		return nil
	}
	ids := UserDefinedTransformerIds(piiText.GetDefaultAnonymizer().GetTransform().GetConfig())
	for _, anonymizer := range piiText.GetEntityAnonymizers() {
		ids = append(ids, UserDefinedTransformerIds(anonymizer.GetTransform().GetConfig())...)
	}
	return ids
}
