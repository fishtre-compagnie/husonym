package ee_transformer_fns

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/internal/queue"
	transformer_utils "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers/utils"
)

const supportedLanguage = "en"

// Used when using the PII Anonymizer with Husonym Transformers
type HusonymOperatorApi interface {
	Transform(
		ctx context.Context,
		config *mgmtv1alpha1.TransformerConfig,
		value string,
	) (string, error)
}

func TransformPiiText(
	ctx context.Context,
	analyzeClient presidio.Analyzer,
	anonymizeClient presidio.Anonymizer,
	husonymOperatorApi HusonymOperatorApi,
	config *mgmtv1alpha1.TransformPiiText,
	value string,
	logger *slog.Logger,
) (string, error) {
	if value == "" {
		return value, nil
	}
	threshold := float64(config.GetScoreThreshold())
	findings, err := analyzeClient.Analyze(ctx, &presidio.AnalyzeRequest{
		Text:             value,
		Language:         config.GetLanguage(),
		ScoreThreshold:   &threshold,
		AdHocRecognizers: buildAdhocRecognizers(config.GetDenyRecognizers()),
		Entities:         config.GetAllowedEntities(),
	})
	if err != nil {
		return "", fmt.Errorf("unable to analyze input: %w", err)
	}

	analysisResults := removeAllowedPhrases(findings, value, config.GetAllowedPhrases())

	analysisResults, husonymEntityMap := processAnalysisResultsForHusonymTransformers(
		analysisResults,
		getHusonymConfiguredEntities(config),
		value,
	)
	anonymized, err := anonymizeClient.Anonymize(ctx, &presidio.AnonymizeRequest{
		Text:      value,
		Findings:  analysisResults,
		Operators: buildAnonymizers(config),
	})
	if err != nil {
		return "", fmt.Errorf("unable to anonymize input: %w", err)
	}
	if len(husonymEntityMap) == 0 {
		return anonymized.Text, nil
	}

	outputText, err := handleHusonymEntityAnonymization(
		ctx,
		anonymized,
		config.GetDefaultAnonymizer(),
		config.GetEntityAnonymizers(),
		husonymEntityMap,
		husonymOperatorApi,
		logger,
	)
	if err != nil {
		return "", fmt.Errorf("unable to handle husonym entity anonymization: %w", err)
	}
	return outputText, nil
}

func handleHusonymEntityAnonymization(
	ctx context.Context,
	resp *presidio.AnonymizeResult,
	defaultAnonymizer *mgmtv1alpha1.PiiAnonymizer,
	entityAnonymizerMap map[string]*mgmtv1alpha1.PiiAnonymizer,
	entityValueMap map[string]*queue.Queue[string],
	husonymOperatorApi HusonymOperatorApi,
	logger *slog.Logger,
) (string, error) {
	outputText := resp.Text

	var defaultTransformerConfig *mgmtv1alpha1.TransformerConfig
	if defaultAnonymizer != nil {
		// We only care about GetTransform() as the others are presidio native and will be handled by presidio
		if defaultAnonymizer.GetTransform() != nil {
			if defaultAnonymizer.GetTransform().GetConfig() != nil {
				defaultTransformerConfig = defaultAnonymizer.GetTransform().GetConfig()
			} else {
				defaultTransformerConfig = getDefaultTransformerConfigByEntity(
					presidio.DefaultOperatorKey,
				) // DEFAULT here will fall through to the switch case statement
			}
		}
	}

	entityConfigMap := map[string]*mgmtv1alpha1.TransformerConfig{}
	for entity, config := range entityAnonymizerMap {
		transformConfig := config.GetTransform().GetConfig()
		if transformConfig == nil {
			transformConfig = getDefaultTransformerConfigByEntity(entity)
		}
		entityConfigMap[entity] = transformConfig
	}

	for _, item := range resp.Items {
		presidioEntity := strings.TrimPrefix(item.EntityType, husonymEntityPrefix)

		var transformerConfig *mgmtv1alpha1.TransformerConfig
		entityTransformerConfig, ok := entityConfigMap[presidioEntity]
		if ok {
			transformerConfig = entityTransformerConfig
		} else if defaultTransformerConfig != nil {
			transformerConfig = defaultTransformerConfig
		}
		if transformerConfig == nil {
			logger.Warn(
				"no transformer config found for entity (a default presidio profile may have been used)",
				"entity",
				presidioEntity,
			)
			continue
		}

		valueQueue, ok := entityValueMap[item.EntityType]
		if !ok {
			logger.Warn("no original value queue found for entity", "entity", item.EntityType)
			continue
		}
		originalValue, err := valueQueue.Dequeue()
		if err != nil {
			logger.Warn("no original values found in queue for entity", "entity", item.EntityType)
			continue
		}
		transformedSnippet, err := husonymOperatorApi.Transform(
			ctx,
			transformerConfig,
			originalValue,
		)
		if err != nil {
			return "", fmt.Errorf("unable to transform husonym entity %s: %w", presidioEntity, err)
		}
		logger.Debug(
			fmt.Sprintf("transformed snippet %s replacing %s", transformedSnippet, item.Text),
		)
		outputText = strings.Replace(outputText, item.Text, transformedSnippet, 1)
	}
	return outputText, nil
}

func getDefaultTransformerConfigByEntity(entity string) *mgmtv1alpha1.TransformerConfig {
	switch entity {
	// case "IN_PASSPORT":
	// case "ES_NIF":
	// case "AU_TFN":
	// case "ES_NIE":
	// case "MEDICAL_LICENSE":
	// case "AU_MEDICARE":
	// case "IN_AADHAAR":
	// case "AU_ACN":
	// case "UK_NINO":
	// case "IN_VOTER":
	// case "IN_PAN":
	case "CREDIT_CARD":
		validLuhn := true
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateCardNumberConfig{
				GenerateCardNumberConfig: &mgmtv1alpha1.GenerateCardNumber{
					ValidLuhn: &validLuhn,
				},
			},
		}
	// case "NRP":
	// case "IT_FISCAL_CODE":
	case "PERSON":
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateFullNameConfig{
				GenerateFullNameConfig: &mgmtv1alpha1.GenerateFullName{},
			},
		}
	// case "US_DRIVER_LICENSE":
	// case "SG_NRIC_FIN":
	// case "IT_DRIVER_LICENSE":
	// case "URL":
	// case "LOCATION":
	// case "US_PASSPORT":
	// case "IN_VEHICLE_REGISTRATION":
	case "PHONE_NUMBER":
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateStringPhoneNumberConfig{
				GenerateStringPhoneNumberConfig: &mgmtv1alpha1.GenerateStringPhoneNumber{},
			},
		}
	// case "DATE_TIME":
	// case "CRYPTO":
	case "US_SSN":
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateSsnConfig{
				GenerateSsnConfig: &mgmtv1alpha1.GenerateSSN{},
			},
		}
	// case "US_BANK_NUMBER":
	case "IP_ADDRESS":
		ipType := mgmtv1alpha1.GenerateIpAddressType_GENERATE_IP_ADDRESS_TYPE_V4_PUBLIC
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateIpAddressConfig{
				GenerateIpAddressConfig: &mgmtv1alpha1.GenerateIpAddress{IpType: &ipType},
			},
		}
	// case "UK_NHS":
	// case "IBAN_CODE":
	// case "IT_VAT_CODE":
	// case "IT_PASSPORT":
	// case "IT_IDENTITY_CARD":
	// case "AU_ABN":
	// case "US_ITIN":
	case "EMAIL_ADDRESS":
		emailType := mgmtv1alpha1.GenerateEmailType_GENERATE_EMAIL_TYPE_UUID_V4
		invalidEmailAction := mgmtv1alpha1.InvalidEmailAction_INVALID_EMAIL_ACTION_GENERATE
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{
				TransformEmailConfig: &mgmtv1alpha1.TransformEmail{
					EmailType:          &emailType,
					InvalidEmailAction: &invalidEmailAction,
				},
			},
		}
	default:
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateSha256HashConfig{
				GenerateSha256HashConfig: &mgmtv1alpha1.GenerateSha256Hash{},
			},
		}
	}
}
func getHusonymConfiguredEntities(config *mgmtv1alpha1.TransformPiiText) []string {
	entities := []string{}
	for entity := range config.GetEntityAnonymizers() {
		entities = append(entities, entity)
	}
	return entities
}

func buildAnonymizers(
	config *mgmtv1alpha1.TransformPiiText,
) map[string]presidio.Operator {
	output := map[string]presidio.Operator{}
	defaultAnon, ok := toPresidioAnonymizerConfig(presidio.DefaultOperatorKey, config.GetDefaultAnonymizer())
	if ok {
		output[presidio.DefaultOperatorKey] = defaultAnon
	}
	for entity, anonymizer := range config.GetEntityAnonymizers() {
		operator, ok := toPresidioAnonymizerConfig(entity, anonymizer)
		if ok {
			if anonymizer.GetTransform() != nil {
				output[fmt.Sprintf("%s%s", husonymEntityPrefix, entity)] = operator
			} else {
				output[entity] = operator
			}
		}
	}

	return output
}

func removeAllowedPhrases(
	results []presidio.Finding,
	text string,
	allowedPhrases []string,
) []presidio.Finding {
	output := []presidio.Finding{}
	uniquePhrases := transformer_utils.ToSet(allowedPhrases)
	textLen := len(text)
	for _, result := range results {
		if result.Start < 0 || result.End > textLen {
			continue // Skip invalid ranges
		}

		phrase := text[result.Start:result.End]
		if _, ok := uniquePhrases[phrase]; !ok {
			output = append(output, result)
		}
	}

	return output
}

const (
	husonymEntityPrefix = "HUSONYM_"
)

func processAnalysisResultsForHusonymTransformers(
	inputResults []presidio.Finding,
	husonymEnabledEntities []string,
	inputText string,
) (analysisResults []presidio.Finding, entityValueMap map[string]*queue.Queue[string]) {
	entitySet := map[string]struct{}{}
	for _, entity := range husonymEnabledEntities {
		entitySet[entity] = struct{}{}
	}

	output := make([]presidio.Finding, 0, len(inputResults))
	entityValueMap = map[string]*queue.Queue[string]{} // entity -> list of original values
	for _, result := range inputResults {
		if _, ok := entitySet[result.EntityType]; ok {
			result.EntityType = fmt.Sprintf("%s%s", husonymEntityPrefix, result.EntityType)
		}
		if _, ok := entityValueMap[result.EntityType]; !ok {
			entityValueMap[result.EntityType] = queue.NewQueue[string]()
		}
		entityValueMap[result.EntityType].Enqueue(inputText[result.Start:result.End])
		output = append(output, result)
	}

	return output, entityValueMap
}

func buildAdhocRecognizers(dtos []*mgmtv1alpha1.PiiDenyRecognizer) []presidio.AdHocRecognizer {
	output := []presidio.AdHocRecognizer{}
	for _, dto := range dtos {
		output = append(output, presidio.AdHocRecognizer{
			Name:              dto.GetName(),
			SupportedEntity:   dto.GetName(),
			DenyList:          dto.GetDenyWords(),
			SupportedLanguage: supportedLanguage,
		})
	}
	return output
}

// toPresidioAnonymizerConfig returns the operator of an anonymizer, and false when the
// anonymizer sets none.
func toPresidioAnonymizerConfig(
	entity string,
	dto *mgmtv1alpha1.PiiAnonymizer,
) (presidio.Operator, bool) {
	switch cfg := dto.GetConfig().(type) {
	case *mgmtv1alpha1.PiiAnonymizer_Redact_:
		return presidio.Redact(), true
	case *mgmtv1alpha1.PiiAnonymizer_Replace_:
		return presidio.Replace(cfg.Replace.GetValue()), true
	case *mgmtv1alpha1.PiiAnonymizer_Hash_:
		return presidio.Hash(toPresidioHashType(cfg.Hash.GetAlgo())), true
	case *mgmtv1alpha1.PiiAnonymizer_Mask_:
		return presidio.Mask(
			cfg.Mask.GetMaskingChar(),
			int(cfg.Mask.GetCharsToMask()),
			cfg.Mask.GetFromEnd(),
		), true
	case *mgmtv1alpha1.PiiAnonymizer_Transform_:
		return presidio.Replace(
			withHusonymEntityBumpers(fmt.Sprintf("%s%s", husonymEntityPrefix, entity)),
		), true
	}
	return presidio.Operator{}, false
}

func withHusonymEntityBumpers(text string) string {
	return fmt.Sprintf("{{%s}}", text)
}

func toPresidioHashType(dto mgmtv1alpha1.PiiAnonymizer_Hash_HashType) presidio.HashType {
	switch dto {
	case mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_MD5:
		return presidio.HashMD5
	case mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256:
		return presidio.HashSHA256
	case mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA512:
		return presidio.HashSHA512
	default:
		return presidio.HashMD5
	}
}
