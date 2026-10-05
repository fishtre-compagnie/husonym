package piitext

import (
	"slices"
	"strconv"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
)

// analyzeRequest is what the analyzer is asked for every value of a configuration, its text
// apart.
//
// The language is the one of the configuration, else the one of the deployment. The recognizers
// of deny lists are declared in that language: the analyzer uses a recognizer only for the
// language it is declared in. Under a restricted list of entity types, the names of the deny
// lists are added to it: the analyzer looks for nothing the list leaves out.
func analyzeRequest(config *mgmtv1alpha1.TransformPiiText, defaultLanguage string) presidio.AnalyzeRequest {
	language := config.GetLanguage()
	if language == "" {
		language = defaultLanguage
	}
	threshold := decimal(config.GetScoreThreshold())

	request := presidio.AnalyzeRequest{
		Language:       language,
		ScoreThreshold: &threshold,
		Entities:       slices.Clone(config.GetAllowedEntities()),
	}
	for _, recognizer := range config.GetDenyRecognizers() {
		request.AdHocRecognizers = append(request.AdHocRecognizers, presidio.AdHocRecognizer{
			Name:              recognizer.GetName(),
			SupportedEntity:   recognizer.GetName(),
			SupportedLanguage: language,
			DenyList:          slices.Clone(recognizer.GetDenyWords()),
		})
		if len(request.Entities) > 0 && !slices.Contains(request.Entities, recognizer.GetName()) {
			request.Entities = append(request.Entities, recognizer.GetName())
		}
	}
	return request
}

// decimal widens a 32-bit threshold to the decimal it was written as: 0.85 stays 0.85, where a
// plain conversion gives 0.8500000238418579, above a finding scored 0.85.
func decimal(value float32) float64 {
	widened, err := strconv.ParseFloat(strconv.FormatFloat(float64(value), 'g', -1, 32), 64)
	if err != nil {
		return float64(value)
	}
	return widened
}
