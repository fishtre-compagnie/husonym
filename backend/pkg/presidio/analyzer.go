package presidio

import (
	"context"
	"net/http"
	"net/url"
	"unicode/utf8"
)

const (
	operationAnalyze           = "analyze"
	operationSupportedEntities = "supported entities"
)

// AnalyzeRequest is one text to analyze.
type AnalyzeRequest struct {
	// Text is the text to analyze.
	Text string
	// Language is the language of the text, in two letters ("en"). The analyzer refuses a
	// request without one, and a language it has no recognizer for.
	Language string
	// ScoreThreshold leaves out what the analyzer is less sure of than this, from 0 to 1.
	// Nil is not sent: the analyzer applies its own.
	ScoreThreshold *float64
	// Entities limits what is looked for to these entity types. Empty looks for all of them.
	Entities []string
	// AdHocRecognizers are recognizers that exist for this request only.
	AdHocRecognizers []AdHocRecognizer
}

// AdHocRecognizer finds the words of a list, as an entity of its own.
type AdHocRecognizer struct {
	Name              string
	SupportedEntity   string
	SupportedLanguage string
	DenyList          []string
}

// Finding is a span of personal data in a text. Start and End count characters (Unicode code
// points), not bytes, and End is exclusive: the span is []rune(text)[Start:End].
type Finding struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// AnalyzerClient calls a Presidio analyzer.
type AnalyzerClient struct {
	endpoint *endpoint
}

// NewAnalyzerClient returns the client of the analyzer at baseURL, an absolute http(s) URL.
// The HTTP client is used as it is given: what it adds to a request, such as an authorization,
// accompanies every call.
func NewAnalyzerClient(baseURL string, httpClient *http.Client) (*AnalyzerClient, error) {
	endpoint, err := newEndpoint(baseURL, httpClient)
	if err != nil {
		return nil, err
	}
	return &AnalyzerClient{endpoint: endpoint}, nil
}

type analyzeBody struct {
	Text             string                `json:"text"`
	Language         string                `json:"language"`
	ScoreThreshold   *float64              `json:"score_threshold,omitempty"`
	Entities         []string              `json:"entities,omitempty"`
	AdHocRecognizers []adHocRecognizerBody `json:"ad_hoc_recognizers,omitempty"`
}

type adHocRecognizerBody struct {
	Name              string   `json:"name"`
	SupportedEntity   string   `json:"supported_entity"`
	SupportedLanguage string   `json:"supported_language"`
	DenyList          []string `json:"deny_list"`
}

// findingAnswer is a finding as the analyzer answers it: what it must carry is told from what
// it left out.
type findingAnswer struct {
	EntityType *string `json:"entity_type"`
	Start      *int    `json:"start"`
	End        *int    `json:"end"`
	Score      float64 `json:"score"`
}

// Analyze returns what the analyzer finds in the text, in the order it answers, and an empty
// list when it finds nothing. Every finding lies inside the text: an answer with one that does
// not is ErrInvalidResponse.
func (c *AnalyzerClient) Analyze(ctx context.Context, req *AnalyzeRequest) ([]Finding, error) {
	body := analyzeBody{
		Text:           req.Text,
		Language:       req.Language,
		ScoreThreshold: req.ScoreThreshold,
		Entities:       req.Entities,
	}
	for _, recognizer := range req.AdHocRecognizers {
		body.AdHocRecognizers = append(body.AdHocRecognizers, adHocRecognizerBody(recognizer))
	}

	answer, err := c.endpoint.do(ctx, operationAnalyze, http.MethodPost, "analyze", nil, body)
	if err != nil {
		return nil, err
	}
	answered, err := decode[[]findingAnswer](operationAnalyze, answer)
	if err != nil {
		return nil, err
	}
	if answered == nil {
		return nil, invalid(operationAnalyze, "the answer is not a list")
	}

	length := utf8.RuneCountInString(req.Text)
	findings := make([]Finding, 0, len(answered))
	for i, found := range answered {
		if found.EntityType == nil || found.Start == nil || found.End == nil {
			return nil, invalid(operationAnalyze, "finding %d lacks its entity type, its start or its end", i)
		}
		if *found.Start < 0 || *found.Start > *found.End || *found.End > length {
			return nil, invalid(
				operationAnalyze,
				"finding %d spans %d to %d in a text of %d characters", i, *found.Start, *found.End, length,
			)
		}
		findings = append(findings, Finding{
			EntityType: *found.EntityType,
			Start:      *found.Start,
			End:        *found.End,
			Score:      found.Score,
		})
	}
	return findings, nil
}

// SupportedEntities returns the entity types the analyzer can find in a language, which may be
// none.
func (c *AnalyzerClient) SupportedEntities(ctx context.Context, language string) ([]string, error) {
	query := url.Values{"language": []string{language}}
	answer, err := c.endpoint.do(ctx, operationSupportedEntities, http.MethodGet, "supportedentities", query, nil)
	if err != nil {
		return nil, err
	}
	entities, err := decode[[]string](operationSupportedEntities, answer)
	if err != nil {
		return nil, err
	}
	if entities == nil {
		return nil, invalid(operationSupportedEntities, "the answer is not a list")
	}
	return entities, nil
}
