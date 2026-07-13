package piidetect_table_workflow

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func Test_New(t *testing.T) {
	t.Run("nil config uses default threshold", func(t *testing.T) {
		wf := New(nil)
		assert.Equal(t, DefaultConfidenceThreshold, wf.confidenceThreshold)
	})

	t.Run("zero threshold uses default", func(t *testing.T) {
		wf := New(&Config{ConfidenceThreshold: 0})
		assert.Equal(t, DefaultConfidenceThreshold, wf.confidenceThreshold)
	})

	t.Run("explicit threshold is kept", func(t *testing.T) {
		wf := New(&Config{ConfidenceThreshold: 0.5})
		assert.Equal(t, float32(0.5), wf.confidenceThreshold)
	})
}

func Test_TablePiiDetect(t *testing.T) {
	t.Run("successful_workflow_with_pii_detected", func(t *testing.T) {
		var ts testsuite.WorkflowTestSuite
		env := ts.NewTestWorkflowEnvironment()

		// Register workflow
		wf := New(nil)
		env.RegisterWorkflow(wf.TablePiiDetect)

		// Mock activities
		var activities *piidetect_table_activities.Activities

		// Setup GetColumnData activity expectations
		env.OnActivity(activities.GetColumnData, mock.Anything, &piidetect_table_activities.GetColumnDataRequest{
			ConnectionId: "conn-123",
			TableSchema:  "public",
			TableName:    "users",
		}).
			Return(&piidetect_table_activities.GetColumnDataResponse{
				ColumnData: []*piidetect_table_activities.ColumnData{
					{
						Column:     "email",
						DataType:   "varchar",
						IsNullable: true,
					},
					{
						Column:     "geburtsdatum",
						DataType:   "date",
						IsNullable: true,
					},
					{
						Column:     "notes",
						DataType:   "text",
						IsNullable: true,
					},
				},
			}, nil)

		// Setup DetectPiiRegex activity expectations
		env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiRegexResponse{
				PiiColumns: map[string]piidetect_table_activities.PiiCategory{
					"email": piidetect_table_activities.PiiCategoryContact,
				},
			}, nil)

		// Setup DetectPiiDictionary activity expectations
		env.OnActivity(activities.DetectPiiDictionary, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiDictionaryResponse{
				PiiColumns: map[string]piidetect_table_activities.DictionaryMatch{
					"geburtsdatum": {
						Category: piidetect_table_activities.PiiCategoryPersonal,
						Token:    "geburtsdatum",
					},
				},
			}, nil)

		// Cascade: only "notes" is unresolved by stage 1 and reaches the LLM.
		env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.MatchedBy(func(req *piidetect_table_activities.DetectPiiLLMRequest) bool {
			return len(req.ColumnData) == 1 && req.ColumnData[0].Column == "notes" &&
				req.SamplingMode == piidetect_table_activities.SamplingModeProfile
		})).
			Return(&piidetect_table_activities.DetectPiiLLMResponse{
				PiiColumns: map[string]piidetect_table_activities.LLMPiiDetectReport{
					"notes": {
						Category:   piidetect_table_activities.PiiCategoryPersonal,
						Confidence: 0.7,
					},
				},
			}, nil)

		// Setup SaveTablePiiDetectReport activity expectations
		expectedKey := &mgmtv1alpha1.RunContextKey{
			AccountId:  "acc-123",
			JobRunId:   "job-123",
			ExternalId: "public.users--table-pii-report",
		}
		env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.SaveTablePiiDetectReportResponse{
				Key: expectedKey,
			}, nil)

		// Execute workflow
		req := &TablePiiDetectRequest{
			AccountId:    "acc-123",
			JobId:        "job-123",
			ConnectionId: "conn-123",
			TableSchema:  "public",
			TableName:    "users",
			SamplingMode: piidetect_table_activities.SamplingModeProfile,
			UserPrompt:   "Please detect PII",
		}

		var result *TablePiiDetectResponse
		env.ExecuteWorkflow(wf.TablePiiDetect, req)

		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())
		require.NoError(t, env.GetWorkflowResult(&result))

		assert.NotNil(t, result)
		assert.Equal(t, expectedKey, result.ResultKey)
		assert.Len(t, result.PiiColumns, 3)

		// Verify the regex-detected column
		emailReport, exists := result.PiiColumns["email"]
		require.True(t, exists)
		require.NotNil(t, emailReport.Regex)
		assert.Equal(t, piidetect_table_activities.PiiCategoryContact, emailReport.Regex.Category)
		assert.Nil(t, emailReport.LLM)

		// Verify the dictionary-detected column
		dobReport, exists := result.PiiColumns["geburtsdatum"]
		require.True(t, exists)
		require.NotNil(t, dobReport.Dictionary)
		assert.Equal(t, piidetect_table_activities.PiiCategoryPersonal, dobReport.Dictionary.Category)
		assert.Equal(t, "geburtsdatum", dobReport.Dictionary.Token)
		assert.Equal(t, piidetect_table_activities.DictionaryMatchConfidence, dobReport.Dictionary.Confidence)
		assert.Nil(t, dobReport.LLM)

		// Verify the LLM-detected column
		notesReport, exists := result.PiiColumns["notes"]
		require.True(t, exists)
		require.NotNil(t, notesReport.LLM)
		assert.Equal(t, piidetect_table_activities.PiiCategoryPersonal, notesReport.LLM.Category)
		assert.Equal(t, float32(0.7), notesReport.LLM.Confidence)
	})

	t.Run("llm_skipped_when_stage1_resolves_all_columns", func(t *testing.T) {
		var ts testsuite.WorkflowTestSuite
		env := ts.NewTestWorkflowEnvironment()

		wf := New(nil)
		env.RegisterWorkflow(wf.TablePiiDetect)

		var activities *piidetect_table_activities.Activities

		env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.GetColumnDataResponse{
				ColumnData: []*piidetect_table_activities.ColumnData{
					{
						Column:     "email",
						DataType:   "varchar",
						IsNullable: true,
					},
					{
						Column:     "date_naissance",
						DataType:   "date",
						IsNullable: true,
					},
				},
			}, nil)

		env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiRegexResponse{
				PiiColumns: map[string]piidetect_table_activities.PiiCategory{
					"email": piidetect_table_activities.PiiCategoryContact,
				},
			}, nil)

		env.OnActivity(activities.DetectPiiDictionary, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiDictionaryResponse{
				PiiColumns: map[string]piidetect_table_activities.DictionaryMatch{
					"date_naissance": {
						Category: piidetect_table_activities.PiiCategoryPersonal,
						Token:    "date_naissance",
					},
				},
			}, nil)

		expectedKey := &mgmtv1alpha1.RunContextKey{
			AccountId:  "acc-123",
			JobRunId:   "job-123",
			ExternalId: "public.users--table-pii-report",
		}
		env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.SaveTablePiiDetectReportResponse{
				Key: expectedKey,
			}, nil)

		req := &TablePiiDetectRequest{
			AccountId:    "acc-123",
			JobId:        "job-123",
			ConnectionId: "conn-123",
			TableSchema:  "public",
			TableName:    "users",
		}

		var result *TablePiiDetectResponse
		env.ExecuteWorkflow(wf.TablePiiDetect, req)

		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())
		require.NoError(t, env.GetWorkflowResult(&result))

		assert.NotNil(t, result)
		assert.Len(t, result.PiiColumns, 2)
		// DetectPiiLLM was not mocked: reaching it would have failed the workflow.
		env.AssertNotCalled(t, "DetectPiiLLM", mock.Anything, mock.Anything)
	})

	t.Run("workflow_with_no_pii_detected", func(t *testing.T) {
		var ts testsuite.WorkflowTestSuite
		env := ts.NewTestWorkflowEnvironment()

		wf := New(nil)
		env.RegisterWorkflow(wf.TablePiiDetect)

		var activities *piidetect_table_activities.Activities

		// Setup GetColumnData activity expectations
		env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.GetColumnDataResponse{
				ColumnData: []*piidetect_table_activities.ColumnData{
					{
						Column:     "id",
						DataType:   "uuid",
						IsNullable: false,
					},
					{
						Column:     "created_at",
						DataType:   "timestamp",
						IsNullable: false,
					},
				},
			}, nil)

		// Setup activities to return no PII
		env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiRegexResponse{
				PiiColumns: map[string]piidetect_table_activities.PiiCategory{},
			}, nil)

		env.OnActivity(activities.DetectPiiDictionary, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiDictionaryResponse{
				PiiColumns: map[string]piidetect_table_activities.DictionaryMatch{},
			}, nil)

		env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.DetectPiiLLMResponse{
				PiiColumns: map[string]piidetect_table_activities.LLMPiiDetectReport{},
			}, nil)

		expectedKey := &mgmtv1alpha1.RunContextKey{
			AccountId:  "acc-123",
			JobRunId:   "job-123",
			ExternalId: "public.users--table-pii-report",
		}
		env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything, mock.Anything).
			Return(&piidetect_table_activities.SaveTablePiiDetectReportResponse{
				Key: expectedKey,
			}, nil)

		req := &TablePiiDetectRequest{
			AccountId:        "acc-123",
			JobId:            "job-123",
			ConnectionId:     "conn-123",
			TableSchema:      "public",
			TableName:        "users",
			ShouldSampleData: false,
		}

		var result *TablePiiDetectResponse
		env.ExecuteWorkflow(wf.TablePiiDetect, req)

		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())
		require.NoError(t, env.GetWorkflowResult(&result))

		assert.NotNil(t, result)
		assert.Equal(t, expectedKey, result.ResultKey)
		assert.Empty(t, result.PiiColumns)
	})

	t.Run("workflow_fails_when_get_column_data_fails", func(t *testing.T) {
		var ts testsuite.WorkflowTestSuite
		env := ts.NewTestWorkflowEnvironment()

		wf := New(nil)
		env.RegisterWorkflow(wf.TablePiiDetect)

		var activities *piidetect_table_activities.Activities

		// Setup GetColumnData to fail
		env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).
			Return(nil, assert.AnError)

		req := &TablePiiDetectRequest{
			AccountId:        "acc-123",
			JobId:            "job-123",
			ConnectionId:     "conn-123",
			TableSchema:      "public",
			TableName:        "users",
			ShouldSampleData: false,
		}

		env.ExecuteWorkflow(wf.TablePiiDetect, req)

		require.True(t, env.IsWorkflowCompleted())
		workflowErr := env.GetWorkflowError()
		require.Error(t, workflowErr)
		assert.Contains(t, workflowErr.Error(), assert.AnError.Error())
	})
}

func Test_getLLMCandidateColumns(t *testing.T) {
	columns := []*piidetect_table_activities.ColumnData{
		{Column: "email"},
		{Column: "date_naissance"},
		{Column: "notes"},
	}
	regexResp := &piidetect_table_activities.DetectPiiRegexResponse{
		PiiColumns: map[string]piidetect_table_activities.PiiCategory{
			"email": piidetect_table_activities.PiiCategoryContact,
		},
	}
	dictResp := &piidetect_table_activities.DetectPiiDictionaryResponse{
		PiiColumns: map[string]piidetect_table_activities.DictionaryMatch{
			"date_naissance": {Category: piidetect_table_activities.PiiCategoryPersonal, Token: "date_naissance"},
		},
	}

	tests := []struct {
		name      string
		threshold float32
		expected  []string
	}{
		{
			name:      "default threshold skips regex and dictionary matches",
			threshold: 0.8,
			expected:  []string{"notes"},
		},
		{
			name:      "high threshold sends dictionary matches to the LLM too",
			threshold: 0.95,
			expected:  []string{"date_naissance", "notes"},
		},
		{
			name:      "threshold above regex confidence sends everything",
			threshold: 1.01,
			expected:  []string{"email", "date_naissance", "notes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := getLLMCandidateColumns(columns, regexResp, dictResp, tt.threshold)
			actual := make([]string, 0, len(candidates))
			for _, col := range candidates {
				actual = append(actual, col.Column)
			}
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func Test_buildFinalReport(t *testing.T) {
	regexResp := &piidetect_table_activities.DetectPiiRegexResponse{
		PiiColumns: map[string]piidetect_table_activities.PiiCategory{
			"email": piidetect_table_activities.PiiCategoryContact,
		},
	}
	dictResp := &piidetect_table_activities.DetectPiiDictionaryResponse{
		PiiColumns: map[string]piidetect_table_activities.DictionaryMatch{
			"email":          {Category: piidetect_table_activities.PiiCategoryContact, Token: "email"},
			"date_naissance": {Category: piidetect_table_activities.PiiCategoryPersonal, Token: "date_naissance"},
		},
	}
	llmResp := &piidetect_table_activities.DetectPiiLLMResponse{
		PiiColumns: map[string]piidetect_table_activities.LLMPiiDetectReport{
			"notes": {Category: piidetect_table_activities.PiiCategoryPersonal, Confidence: 0.6},
		},
	}

	report := buildFinalReport(regexResp, dictResp, llmResp)
	require.Len(t, report, 3)

	assert.Equal(t, piidetect_table_activities.PiiCategoryContact, report["email"].Regex.Category)
	assert.Equal(t, "email", report["email"].Dictionary.Token)
	assert.Nil(t, report["email"].LLM)

	assert.Nil(t, report["date_naissance"].Regex)
	assert.Equal(t, piidetect_table_activities.PiiCategoryPersonal, report["date_naissance"].Dictionary.Category)

	assert.Nil(t, report["notes"].Regex)
	assert.Nil(t, report["notes"].Dictionary)
	assert.Equal(t, float32(0.6), report["notes"].LLM.Confidence)
}
