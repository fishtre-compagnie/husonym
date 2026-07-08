package recommendations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
	"github.com/openai/openai-go"
)

// OpenAiCompletionsClient is the minimal OpenAI-compatible chat completions
// surface needed to draft a user-defined transformer proposal. It is a type
// alias for the interface already used by the piidetect LLM stage
// (worker/pkg/workflows/ee/piidetect/workflows/table/activities), so that a
// single implementation/mock covers both call sites rather than duplicating
// the interface and its mock.
type OpenAiCompletionsClient = piidetect_table_activities.OpenAiCompletionsClient

// MaxProposalsPerRequest bounds how many transformer proposals are generated
// during a single GetJobMappingRecommendations call, to bound the added
// latency and, in external API mode, cost.
const MaxProposalsPerRequest = 5

// DefaultProposalModel is used when no LLM_CODEGEN_MODEL/LLM_MODEL is
// configured for the backend.
const DefaultProposalModel = openai.ChatModelGPT4oMini

// ProposalValidator validates a draft javascript transformer body the same
// way TransformersService.ValidateUserJavascriptCode does (compiling it with
// goja), without going over the network. It returns true when the code
// compiles and is safe to surface to the reviewing user.
type ProposalValidator func(javascriptCode string) bool

// ProposalRequest describes the column a transformer proposal is being
// drafted for. Sample data/shape information is intentionally not part of
// this request: the piidetect report persisted server-side only carries the
// detected category and confidence, never the sampled values or their shape
// (plans/assistant-ia-config-anonymisation.md §4.4 allows shape info "if
// present in the report/profile data available server-side" -- it isn't).
type ProposalRequest struct {
	ColumnName string
	Category   Category
	DataType   mgmtv1alpha1.TransformerDataType
	// GenericRationale is the rationale produced by the deterministic
	// category/transformer map for the generic fallback, surfaced to the LLM
	// as context on why a system transformer wasn't a good enough match.
	GenericRationale string
}

// ProposalResult is a validated draft user-defined transformer proposal,
// ready to be attached to a TransformerRecommendation.
type ProposalResult struct {
	Name           string
	Description    string
	JavascriptCode string
	Rationale      string
}

// proposalDraft mirrors the strict JSON schema the LLM is constrained to.
type proposalDraft struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	JavascriptCode string `json:"javascript_code"`
	Rationale      string `json:"rationale"`
}

const proposalSystemMessage = "You are a senior data engineer drafting a JavaScript data anonymization function for a database column."

// proposalResponseSchema constrains the LLM output to the expected JSON structure.
var proposalResponseSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":            map[string]any{"type": "string"},
		"description":     map[string]any{"type": "string"},
		"javascript_code": map[string]any{"type": "string"},
		"rationale":       map[string]any{"type": "string"},
	},
	"required":             []string{"name", "description", "javascript_code", "rationale"},
	"additionalProperties": false,
}

// buildProposalPrompt builds the user message sent to the LLM. It documents
// the exact contract the generated code must respect: the code is the body
// of `function fn1(value) { <code> }` (see
// backend/services/mgmt/v1alpha1/transformers-service/userdefined_transformers.go:constructJavascriptCode),
// so it must reference the input as `value` and end with a `return`
// statement, and it runs in the same goja sandbox as hand-written
// TransformJavascript code (no Node/browser APIs, no imports, no network
// access).
func buildProposalPrompt(req ProposalRequest) string {
	var b strings.Builder
	b.WriteString("Draft a JavaScript anonymization transformer for a single database column.\n\n")
	fmt.Fprintf(&b, "Column name: %s\n", req.ColumnName)
	fmt.Fprintf(&b, "Detected PII category: %s\n", req.Category)
	fmt.Fprintf(&b, "Column data type: %s\n", req.DataType.String())
	if req.GenericRationale != "" {
		fmt.Fprintf(&b, "Why no system transformer fits: %s\n", req.GenericRationale)
	}
	b.WriteString("\nContract for the code you produce:\n")
	b.WriteString("- The code is the BODY of a JavaScript function `function fn1(value) { <your code> }`.\n")
	b.WriteString("  Do not write the function declaration itself, only the statements that go inside it.\n")
	b.WriteString("- The original column value is available as the variable `value` (a string unless the data type says otherwise).\n")
	b.WriteString("- The code MUST end with a `return <transformed value>;` statement.\n")
	b.WriteString("- The code runs in a sandboxed goja JavaScript VM: no `require`, no `import`,\n")
	b.WriteString("  no network/file access, no Node.js or browser globals.\n")
	b.WriteString("- Goal: preserve the overall format/shape of the value (length, casing,\n")
	b.WriteString("  separators, any fixed business-code prefix) while destroying any information\n")
	b.WriteString("  that could re-identify the original value (e.g. permute/replace digits,\n")
	b.WriteString("  replace letters with other random letters of the same case).\n")
	b.WriteString("- Keep the code short and free of external dependencies.\n")
	b.WriteString("\nRespond with the name, a one-sentence description, the javascript_code\n")
	b.WriteString("(function body only), and a short rationale explaining the chosen approach.")
	return b.String()
}

// GenerateProposal drafts a user-defined transformer proposal for a single
// column via the configured LLM, validates the generated code with the same
// Go validation ValidateUserJavascriptCode uses, and returns (nil, false)
// whenever the client is unset, the LLM call fails, the response can't be
// parsed, or the code fails validation -- callers should silently keep the
// generic system-transformer recommendation in all of these cases.
func GenerateProposal(
	ctx context.Context,
	client OpenAiCompletionsClient,
	model string,
	validate ProposalValidator,
	req ProposalRequest,
) (*ProposalResult, bool) {
	if client == nil {
		return nil, false
	}
	if model == "" {
		model = DefaultProposalModel
	}

	prompt := buildProposalPrompt(req)

	chatResp, err := client.New(ctx, openai.ChatCompletionNewParams{
		Temperature: openai.Float(0.2),
		Model:       model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "transformer_proposal",
					Description: openai.String("Draft user-defined JavaScript transformer for a database column"),
					Schema:      proposalResponseSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(proposalSystemMessage),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return nil, false
	}
	if len(chatResp.Choices) == 0 {
		return nil, false
	}

	var draft proposalDraft
	if err := json.Unmarshal([]byte(chatResp.Choices[0].Message.Content), &draft); err != nil {
		return nil, false
	}
	if strings.TrimSpace(draft.Name) == "" || strings.TrimSpace(draft.JavascriptCode) == "" {
		return nil, false
	}
	if validate != nil && !validate(draft.JavascriptCode) {
		return nil, false
	}

	return &ProposalResult{
		Name:           draft.Name,
		Description:    draft.Description,
		JavascriptCode: draft.JavascriptCode,
		Rationale:      draft.Rationale,
	}, true
}
