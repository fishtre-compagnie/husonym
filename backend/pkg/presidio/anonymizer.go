package presidio

import (
	"context"
	"net/http"
)

const operationAnonymize = "anonymize"

// AnonymizeRequest is one text to rewrite.
type AnonymizeRequest struct {
	// Text is the text the findings are about.
	Text string
	// Findings are the spans to rewrite, sent in this order.
	Findings []Finding
	// Operators tells what to do by entity type, and under DefaultOperatorKey for the entity
	// types that have none. Without one the anonymizer replaces a finding with its entity type.
	Operators map[string]Operator
}

// AnonymizeResult is a rewritten text.
type AnonymizeResult struct {
	// Text is the text once rewritten.
	Text string
	// Items tells what was put in place of each finding.
	Items []AnonymizedItem
}

// AnonymizedItem is what stands in place of one finding. Start and End place Text in the
// rewritten text, in characters as those of a Finding.
type AnonymizedItem struct {
	EntityType string
	Start      int
	End        int
	// Text is what was put in place of the finding.
	Text string
	// Operator names the operator that produced it.
	Operator string
}

// AnonymizerClient calls a Presidio anonymizer.
type AnonymizerClient struct {
	endpoint *endpoint
}

// NewAnonymizerClient returns the client of the anonymizer at baseURL, an absolute http(s)
// URL. The HTTP client is used as it is given: what it adds to a request, such as an
// authorization, accompanies every call.
func NewAnonymizerClient(baseURL string, httpClient *http.Client) (*AnonymizerClient, error) {
	endpoint, err := newEndpoint(baseURL, httpClient)
	if err != nil {
		return nil, err
	}
	return &AnonymizerClient{endpoint: endpoint}, nil
}

type anonymizeBody struct {
	Text            string              `json:"text"`
	AnalyzerResults []Finding           `json:"analyzer_results"`
	Anonymizers     map[string]Operator `json:"anonymizers,omitempty"`
}

// anonymizeAnswer is the answer of the anonymizer: what it must carry is told from what it
// left out.
type anonymizeAnswer struct {
	Text  *string `json:"text"`
	Items []struct {
		EntityType *string `json:"entity_type"`
		Start      *int    `json:"start"`
		End        *int    `json:"end"`
		Text       *string `json:"text"`
		Operator   string  `json:"operator"`
	} `json:"items"`
}

// Anonymize returns the text rewritten. The result always has its text, and every item its
// own: an answer that lacks one is ErrInvalidResponse.
func (c *AnonymizerClient) Anonymize(ctx context.Context, req *AnonymizeRequest) (*AnonymizeResult, error) {
	body := anonymizeBody{
		Text: req.Text,
		// The anonymizer requires the list, even empty.
		AnalyzerResults: append([]Finding{}, req.Findings...),
		Anonymizers:     req.Operators,
	}
	answer, err := c.endpoint.do(ctx, operationAnonymize, http.MethodPost, "anonymize", nil, body)
	if err != nil {
		return nil, err
	}
	answered, err := decode[*anonymizeAnswer](operationAnonymize, answer)
	if err != nil {
		return nil, err
	}
	if answered == nil || answered.Text == nil {
		return nil, invalid(operationAnonymize, "the answer has no text")
	}

	result := &AnonymizeResult{Text: *answered.Text, Items: make([]AnonymizedItem, 0, len(answered.Items))}
	for i, item := range answered.Items {
		if item.EntityType == nil || item.Start == nil || item.End == nil || item.Text == nil {
			return nil, invalid(operationAnonymize, "item %d lacks its entity type, its start, its end or its text", i)
		}
		result.Items = append(result.Items, AnonymizedItem{
			EntityType: *item.EntityType,
			Start:      *item.Start,
			End:        *item.End,
			Text:       *item.Text,
			Operator:   item.Operator,
		})
	}
	return result, nil
}
