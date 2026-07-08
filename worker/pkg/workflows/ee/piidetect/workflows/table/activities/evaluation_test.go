package piidetect_table_activities

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/ee/recommendations/evaluation"
	"github.com/fishtre-compagnie/husonym/internal/llm"
	"github.com/openai/openai-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classifyColumnNameStage1 mirrors the stage-1 cascade decision: regex first
// (confidence 1.0), then dictionary (0.9). Both are at or above the default
// confidence threshold (0.8), so a match means the column skips the LLM stage.
func classifyColumnNameStage1(columnName string) (PiiCategory, bool) {
	if category, ok := isPiiColumn(columnName); ok {
		return category, true
	}
	if match, ok := lookupColumnInDictionary(columnName); ok {
		return match.Category, true
	}
	return "", false
}

// Test_Stage1_EvaluationDataset runs the metadata stage (column-name regex +
// multilingual dictionary) against the versioned evaluation dataset. Pure Go,
// runs on every PR. Two hard requirements:
//   - a stage-1 prediction must never disagree with the ground truth (a wrong
//     high-confidence category would bypass the LLM stage entirely);
//   - every column marked stage1_expected must be classified correctly.
func Test_Stage1_EvaluationDataset(t *testing.T) {
	datasets, err := evaluation.LoadDatasets()
	require.NoError(t, err)
	require.NotEmpty(t, datasets)

	for _, dataset := range datasets {
		t.Run(dataset.Language, func(t *testing.T) {
			require.NoError(t, dataset.Validate(GetAllPiiCategoriesAsStrings()))

			predictions := map[string]string{}
			for _, table := range dataset.Tables {
				for _, column := range table.Columns {
					if category, ok := classifyColumnNameStage1(column.Name); ok {
						predictions[evaluation.ColumnKey(table.Schema, table.Table, column.Name)] = category.String()
					}
				}
			}

			report := evaluation.Evaluate(dataset, predictions)
			t.Logf("stage-1 metrics (%s):\n%s", dataset.Language, report)

			for _, table := range dataset.Tables {
				for _, column := range table.Columns {
					key := evaluation.ColumnKey(table.Schema, table.Table, column.Name)
					predicted := predictions[key]
					if predicted != "" {
						assert.Equal(t, column.ExpectedCategory, predicted,
							"stage 1 must never disagree with ground truth on %s", key)
					}
					if column.Stage1Expected {
						assert.Equal(t, column.ExpectedCategory, predicted,
							"stage 1 is expected to classify %s", key)
					}
				}
			}
		})
	}
}

// Test_LLM_EvaluationDataset evaluates the full cascade (stage 1 + LLM residual)
// against a live OpenAI-compatible endpoint, once per sampling mode, and logs
// per-language precision/recall so sampling modes and candidate models can be
// compared. It is informational (no hard assertions on model quality) and only
// runs on demand:
//
//	PII_LLM_EVAL=1 LLM_BASE_URL=http://localhost:8081/v1 LLM_MODEL=<model> \
//	  go test ./worker/pkg/workflows/ee/piidetect/... -run Test_LLM_EvaluationDataset -v
func Test_LLM_EvaluationDataset(t *testing.T) {
	if os.Getenv("PII_LLM_EVAL") != "1" {
		t.Skip("set PII_LLM_EVAL=1 (with LLM_BASE_URL / LLM_API_KEY / LLM_MODEL) to run the LLM evaluation")
	}
	viper.AutomaticEnv()
	client, ok := llm.NewOpenAIClientFromEnv()
	require.True(t, ok, "LLM_BASE_URL / LLM_API_KEY / OPENAI_API_KEY must be configured")
	completions := &client.Chat.Completions

	model := viper.GetString("LLM_MODEL")
	if model == "" {
		model = defaultLLMModel
	}

	datasets, err := evaluation.LoadDatasets()
	require.NoError(t, err)

	ctx := context.Background()
	for _, mode := range []SamplingMode{SamplingModeNone, SamplingModeProfile, SamplingModeRaw} {
		for _, dataset := range datasets {
			t.Run(fmt.Sprintf("%s/%s", mode, dataset.Language), func(t *testing.T) {
				predictions := map[string]string{}
				for _, table := range dataset.Tables {
					residual := []*ColumnData{}
					for _, column := range table.Columns {
						key := evaluation.ColumnKey(table.Schema, table.Table, column.Name)
						if category, ok := classifyColumnNameStage1(column.Name); ok {
							predictions[key] = category.String()
						} else {
							residual = append(residual, &ColumnData{Column: column.Name, DataType: column.DataType})
						}
					}
					if len(residual) == 0 {
						continue
					}

					prompt, err := buildEvalPrompt(table, residual, mode, model)
					require.NoError(t, err)

					llmPredictions, err := callEvalLLM(ctx, completions, model, prompt)
					require.NoError(t, err)
					for column, report := range llmPredictions {
						predictions[evaluation.ColumnKey(table.Schema, table.Table, column)] = report.Category.String()
					}
				}

				report := evaluation.Evaluate(dataset, predictions)
				t.Logf("cascade metrics (mode=%s, language=%s, model=%s):\n%s", mode, dataset.Language, model, report)
			})
		}
	}
}

// buildEvalPrompt builds the LLM prompt for the residual columns of a dataset
// table, mirroring getLLMPrompt for each sampling mode but sourcing sampled
// rows from the dataset instead of a live connection.
func buildEvalPrompt(
	table *evaluation.Table,
	residual []*ColumnData,
	mode SamplingMode,
	model string,
) (string, error) {
	switch mode {
	case SamplingModeProfile:
		records := recordsFromDatasetTable(table)
		profiles := BuildColumnProfiles(records, columnNames(residual))
		return getProfilePrompt(profiles, table.Table, "", model)
	case SamplingModeRaw:
		records := filterRecordsToColumns(recordsFromDatasetTable(table), residual)
		return getPrompt(records, table.Table, "", model, maxDataSamples)
	default:
		records := Records{}
		for _, col := range residual {
			records = append(records, map[string]any{col.Column: map[string]any{}})
		}
		return getPrompt(records, table.Table, "", model, maxDataSamples)
	}
}

// recordsFromDatasetTable turns the per-column samples of a dataset table into
// row-shaped records, the format the sampling pipeline produces.
func recordsFromDatasetTable(table *evaluation.Table) Records {
	rowCount := 0
	for _, column := range table.Columns {
		if len(column.Samples) > rowCount {
			rowCount = len(column.Samples)
		}
	}

	records := make(Records, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		record := map[string]any{}
		for _, column := range table.Columns {
			if i < len(column.Samples) {
				record[column.Name] = column.Samples[i]
			}
		}
		records = append(records, record)
	}
	return records
}

// callEvalLLM mirrors the request path of DetectPiiLLM (json_schema constrained
// output with a json_object fallback) against a live endpoint.
func callEvalLLM(
	ctx context.Context,
	client OpenAiCompletionsClient,
	model string,
	prompt string,
) (map[string]LLMPiiDetectReport, error) {
	newParams := func(responseFormat openai.ChatCompletionNewParamsResponseFormatUnion) openai.ChatCompletionNewParams {
		return openai.ChatCompletionNewParams{
			Temperature:    openai.Float(0.0),
			Model:          model,
			ResponseFormat: responseFormat,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemMessage),
				openai.UserMessage(prompt),
			},
		}
	}

	chatResp, err := client.New(ctx, newParams(openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
			JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        "pii_detection",
				Description: openai.String("PII classification of database columns"),
				Schema:      piiDetectionResponseSchema,
				Strict:      openai.Bool(true),
			},
		},
	}))
	if err != nil {
		chatResp, err = client.New(ctx, newParams(openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &openai.ResponseFormatJSONObjectParam{},
		}))
		if err != nil {
			return nil, err
		}
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no completion choices")
	}

	var resp openAiResponse
	if err := json.Unmarshal([]byte(chatResp.Choices[0].Message.Content), &resp); err != nil {
		return nil, err
	}
	predictions := map[string]LLMPiiDetectReport{}
	for _, column := range resp.Output {
		predictions[column.FieldName] = LLMPiiDetectReport{
			Category:   column.Category,
			Confidence: column.Confidence,
		}
	}
	return predictions, nil
}
