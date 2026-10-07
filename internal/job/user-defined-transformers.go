package job

import mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"

// TransformerConfigsRun lists the transformer configurations a transformer runs: itself, and those
// the anonymizers of TransformPiiText hand the PII they find to, which may be any transformer.
func TransformerConfigsRun(config *mgmtv1alpha1.TransformerConfig) []*mgmtv1alpha1.TransformerConfig {
	if config == nil {
		return nil
	}
	configs := []*mgmtv1alpha1.TransformerConfig{config}
	piiText := config.GetTransformPiiTextConfig()
	if piiText == nil {
		return configs
	}
	configs = append(configs, TransformerConfigsRun(piiText.GetDefaultAnonymizer().GetTransform().GetConfig())...)
	for _, anonymizer := range piiText.GetEntityAnonymizers() {
		configs = append(configs, TransformerConfigsRun(anonymizer.GetTransform().GetConfig())...)
	}
	return configs
}

// UserDefinedTransformerIds lists the user-defined transformers a transformer runs: itself, or
// those the anonymizers of TransformPiiText hand the PII they find to.
func UserDefinedTransformerIds(config *mgmtv1alpha1.TransformerConfig) []string {
	var ids []string
	for _, run := range TransformerConfigsRun(config) {
		if userDefined := run.GetUserDefinedTransformerConfig(); userDefined != nil {
			ids = append(ids, userDefined.GetId())
		}
	}
	return ids
}
