package recommendations

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
	"github.com/openai/openai-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_buildProposalPrompt(t *testing.T) {
	prompt := buildProposalPrompt(ProposalRequest{
		ColumnName:       "customer_reference",
		Category:         CategoryFinancial,
		DataType:         mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
		GenericRationale: "column is financial PII; recommending a strong identifier replacement",
	})

	require.Contains(t, prompt, "customer_reference")
	require.Contains(t, prompt, string(CategoryFinancial))
	require.Contains(t, prompt, "TRANSFORMER_DATA_TYPE_STRING")
	require.Contains(t, prompt, "recommending a strong identifier replacement")
	// The contract: input variable name and mandatory return, matching
	// constructJavascriptCode's `function fn1(value) { ... }` wrapper.
	require.Contains(t, prompt, "`value`")
	require.Contains(t, prompt, "return")
	require.Contains(t, prompt, "goja")
}

func Test_GenerateProposal_NilClient(t *testing.T) {
	result, ok := GenerateProposal(context.Background(), nil, "", alwaysValid, ProposalRequest{})
	require.False(t, ok)
	require.Nil(t, result)
}

func Test_GenerateProposal_Success(t *testing.T) {
	mockClient := piidetect_table_activities.NewMockOpenAiCompletionsClient(t)
	mockClient.EXPECT().New(mock.Anything, mock.Anything).Return(&openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Content: `{"name":"anonymize_customer_ref","description":"Scrambles the numeric suffix of a customer reference","javascript_code":"return value;","rationale":"no catalog transformer preserves this format"}`,
				},
			},
		},
	}, nil)

	result, ok := GenerateProposal(
		context.Background(),
		mockClient,
		"",
		alwaysValid,
		ProposalRequest{ColumnName: "customer_ref", Category: CategoryFinancial},
	)
	require.True(t, ok)
	require.NotNil(t, result)
	require.Equal(t, "anonymize_customer_ref", result.Name)
	require.Equal(t, "return value;", result.JavascriptCode)
}

func Test_GenerateProposal_DropsOnValidationFailure(t *testing.T) {
	mockClient := piidetect_table_activities.NewMockOpenAiCompletionsClient(t)
	mockClient.EXPECT().New(mock.Anything, mock.Anything).Return(&openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Content: `{"name":"bad","description":"d","javascript_code":"return value","rationale":"r"}`,
				},
			},
		},
	}, nil)

	result, ok := GenerateProposal(
		context.Background(),
		mockClient,
		"",
		neverValid,
		ProposalRequest{ColumnName: "customer_ref", Category: CategoryFinancial},
	)
	require.False(t, ok)
	require.Nil(t, result)
}

func Test_GenerateProposal_DropsOnLLMError(t *testing.T) {
	mockClient := piidetect_table_activities.NewMockOpenAiCompletionsClient(t)
	mockClient.EXPECT().New(mock.Anything, mock.Anything).Return(nil, errors.New("boom"))

	result, ok := GenerateProposal(
		context.Background(),
		mockClient,
		"",
		alwaysValid,
		ProposalRequest{ColumnName: "customer_ref", Category: CategoryFinancial},
	)
	require.False(t, ok)
	require.Nil(t, result)
}

func Test_GenerateProposal_DropsOnMalformedJSON(t *testing.T) {
	mockClient := piidetect_table_activities.NewMockOpenAiCompletionsClient(t)
	mockClient.EXPECT().New(mock.Anything, mock.Anything).Return(&openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Content: "not json",
				},
			},
		},
	}, nil)

	result, ok := GenerateProposal(
		context.Background(),
		mockClient,
		"",
		alwaysValid,
		ProposalRequest{ColumnName: "customer_ref", Category: CategoryFinancial},
	)
	require.False(t, ok)
	require.Nil(t, result)
}

func Test_GenerateProposal_DropsOnEmptyName(t *testing.T) {
	mockClient := piidetect_table_activities.NewMockOpenAiCompletionsClient(t)
	mockClient.EXPECT().New(mock.Anything, mock.Anything).Return(&openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Content: `{"name":"","description":"d","javascript_code":"return value;","rationale":"r"}`,
				},
			},
		},
	}, nil)

	result, ok := GenerateProposal(
		context.Background(),
		mockClient,
		"",
		alwaysValid,
		ProposalRequest{ColumnName: "customer_ref", Category: CategoryFinancial},
	)
	require.False(t, ok)
	require.Nil(t, result)
}

func alwaysValid(code string) bool { return true }
func neverValid(code string) bool  { return false }
